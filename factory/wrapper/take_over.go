package wrapper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The take-over (ticket 61). The Take-over interface starts the
// interactive form of the harness where a person can attach, and gives
// the command to attach (config.attachCommand). Its implementation is
// hc: the wrapper starts the hc core as the run user, in the Execution's
// cgroup, and starts 'claude --resume <session id>' in an hc terminal of
// that core. A person attaches with 'sudo -u <run user> hc connect' (the
// image's one sudoers rule), through ssh on a VM, or docker exec on the
// local factory stack.
//
// In the interactive form there is no stream: the Stop hook is not
// installed (it never refuses during a take-over), the idle limit does
// not apply (no output event comes; only the total limit counts), and
// AskUserQuestion stays, because a person is there. A report through the
// agent's socket still works: the end of the interactive Agent session
// ends the Execution, and the Dispatcher sets the Run's state from the
// report.

// HarnessEnvCommand is the wrapper program's command that starts the
// harness with the environment of a file: the hc terminal runs it, so
// that the account's token is not on a command line.
const HarnessEnvCommand = "harness-env"

// hcWait is the limit of one hc command.
const hcWait = 30 * time.Second

// hcFailures is how many hc get calls in a row can fail before the
// wrapper takes the terminal as gone (the core is not there).
const hcFailures = 3

// setupTakeOver sets up a take-over Execution on the machine of the
// Execution that it follows: the checkout and the run user's home stay,
// with the Agent session directory. The end of that Execution killed
// each process of the run user, so root writes the new secret files and
// the git configuration before the run user's next process, as the first
// setup does, and follows no link that the agent left. With nothing kept
// (the setup of that Execution failed before the clone), it is the full
// setup.
func (e *execution) setupTakeOver(ctx context.Context, environ []string) error {
	repository, err := checkRepository(e.task.Repository)
	if err != nil {
		return err
	}
	folder, err := checkFolder(e.task.Folder)
	if err != nil {
		return err
	}
	checkout := e.cfg.checkout(repository)
	info, err := os.Lstat(filepath.Join(checkout, ".git"))
	if e.kept == nil || e.kept.projectDir == nil || err != nil || !info.IsDir() {
		if e.kept != nil && e.kept.projectDir != nil {
			e.kept.projectDir.Close()
		}
		return e.setup(ctx, environ)
	}
	secrets := e.cfg.secrets()
	if err := removeAll(secrets); err != nil {
		return fmt.Errorf("delete the secrets of the Execution before: %w", err)
	}
	if err := os.Mkdir(secrets, 0o700); err != nil {
		return err
	}
	if err := e.runUser.share(secrets, 0o750); err != nil {
		return err
	}
	// The agent's own .gitconfig, or a link in its place, goes: the
	// wrapper writes a new one.
	if err := os.Remove(e.cfg.gitConfig()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace the git configuration: %w", err)
	}
	if err := e.writeGitConfig(secrets); err != nil {
		return err
	}
	// The take-over Execution's own MCP token (an Issue or a Monitor Run).
	if err := e.writeMCP(); err != nil {
		return err
	}
	e.uploads.adopt(e.kept.projectDir)
	e.checkout = checkout
	tip, _, err := e.git(ctx, environ, checkout, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("find the tip of the Run's branch: %w", err)
	}
	e.startTip = strings.TrimSpace(tip)
	e.workDir = filepath.Join(checkout, folder)
	if info, err := os.Stat(e.workDir); err != nil || !info.IsDir() {
		return fmt.Errorf("the checkout has no folder %s, the Project's folder", folder)
	}
	return nil
}

// interactiveArgs is the command line of the interactive form: the same
// Agent session, in bypass mode, with no Stop hook, with AskUserQuestion,
// and with SolidTrace's MCP server for an Issue or a Monitor Run. With no transcript of the Agent session on the machine
// (the Execution before ended before its harness started), the session
// starts with its id.
func (e *execution) interactiveArgs() []string {
	args := []string{"--permission-mode", "bypassPermissions"}
	if e.task.MCP != nil {
		args = append(args, "--mcp-config", e.cfg.mcpConfig())
	}
	if _, err := e.uploads.root().Stat(e.task.SessionID + ".jsonl"); err == nil {
		return append(args, "--resume", e.task.SessionID)
	}
	return append(args, "--session-id", e.task.SessionID)
}

// runInteractive starts the hc core as the run user in the Execution's
// cgroup, and the harness in an hc terminal of it. It waits until the
// terminal's program ends (the person left the Agent session) or the end
// started, and gives the end call's body.
func (e *execution) runInteractive(environ []string) dispatcherapi.ExecutionEnd {
	failed := func(err error) dispatcherapi.ExecutionEnd {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: "start the take-over: " + err.Error()}
	}
	program, err := ownProgram()
	if err != nil {
		return failed(fmt.Errorf("find the wrapper's program: %w", err))
	}
	// The core has the run user's names and home, and no secret. Its
	// start is a process of the run user in the cgroup: the core stays
	// there, and the end kills it. It is the wrapper's child, so it has
	// no SUDO_ variable (a core that 'sudo hc connect' starts has them,
	// and takes the person's user from them).
	e.coreEnv = coreEnv(e.gitEnv(environ, ""))
	if out, err := e.hc("server", "start"); err != nil {
		fmt.Fprintf(e.log, "wrapper: hc server start: %v: %s\n", err, lastLines(out, 5))
	}
	// The core and its holders must be in the Execution's cgroup before a
	// terminal starts: a core that was there before (a person's early hc
	// connect, or systemd started it) runs its terminals outside it, and
	// the end would not kill the harness.
	if err := e.checkCore(); err != nil {
		return failed(err)
	}
	// The harness's environment is in a file of the secrets directory,
	// not on the terminal's command line.
	harnessEnv := append(append(append(coreEnv(e.gitEnv(environ, e.cfg.socket())), e.uploads.claudeEnv(e.cfg)...),
		e.mcpEnv()...), EnvAccountToken+"="+e.task.AccountToken, EnvSubprocessScrub+"=0")
	envFile := filepath.Join(e.cfg.secrets(), "harness-env")
	if err := writeSecret(e.runUser, envFile, []byte(strings.Join(harnessEnv, "\x00")), 0o440); err != nil {
		return failed(err)
	}
	e.mu.Lock()
	if e.ending {
		e.mu.Unlock()
		end, _ := e.endedEarly()
		return end
	}
	e.mu.Unlock()
	argv := append([]string{program, HarnessEnvCommand, envFile, e.cfg.claudeTemplate, "--", e.cfg.harness}, e.interactiveArgs()...)
	out, err := e.hc(append([]string{"run", "--bg", "--json", "--no-notify", "--name", "take-over", "--cwd", e.workDir, "--"},
		argv...)...)
	if err != nil {
		return failed(fmt.Errorf("hc run: %v: %s", err, lastLines(out, 5)))
	}
	var started struct{ Address string }
	if err := decodeJSON(out, &started); err != nil || started.Address == "" {
		return failed(fmt.Errorf("hc run gave no terminal: %s", lastLines(out, 5)))
	}
	address := started.Address
	// Check again with the terminal's holder: it must be in the cgroup
	// too, so that the end kills the take-over.
	if err := e.checkCore(); err != nil {
		e.hc("kill", address)
		return failed(err)
	}
	// The start of the end ends the terminal's program; the kill after
	// endWait ends what is left, the core too.
	e.mu.Lock()
	e.stopTerminal = func() {
		if out, err := e.hc("kill", address); err != nil {
			fmt.Fprintf(e.log, "wrapper: hc kill %s: %v: %s\n", address, err, lastLines(out, 5))
		}
	}
	ending := e.ending
	if ending {
		// The end started before the terminal: it ends now.
		e.kill = time.AfterFunc(endWait, e.killAll)
	}
	stop := e.stopTerminal
	e.mu.Unlock()
	if ending {
		go stop()
	}
	code := e.waitForTerminal(address)
	e.killAll()
	e.mu.Lock()
	if e.kill != nil {
		e.kill.Stop()
	}
	if !e.ending {
		e.ending, e.endReason = true, dispatcherapi.EndSessionEnded
	}
	reason, why := e.endReason, e.endError
	e.mu.Unlock()
	return dispatcherapi.ExecutionEnd{Reason: reason, Error: why, ExitCode: code}
}

// waitForTerminal waits until the terminal's program ended, and gives its
// exit code (nil when the core is gone, as after the end's kill).
func (e *execution) waitForTerminal(address string) *int {
	tick := time.NewTicker(e.cfg.limitEvery)
	defer tick.Stop()
	for failures := 0; ; <-tick.C {
		// After the cgroup kill, no program of the terminal is left.
		if e.killed.Load() {
			return nil
		}
		out, err := e.hc("get", "--json", address)
		var t struct {
			State    string `json:"state"`
			ExitCode *int   `json:"exit_code"`
		}
		if err == nil {
			err = decodeJSON(out, &t)
		}
		if err != nil {
			if failures++; failures >= hcFailures {
				fmt.Fprintf(e.log, "wrapper: the take-over's terminal %s is gone: %v: %s\n", address, err, lastLines(out, 5))
				return nil
			}
			continue
		}
		failures = 0
		if t.State == "finished" {
			return t.ExitCode
		}
	}
}

// hc runs an hc command as the run user, in the Execution's cgroup, with
// the core's environment: the run user's names and home, and no secret
// of the wrapper or of the Run. It gives the output.
func (e *execution) hc(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hcWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cfg.hc, args...)
	cmd.Dir = e.workDir
	cmd.Env = e.coreEnv
	e.runUser.start(cmd)
	var out bytes.Buffer
	if len(args) > 1 && args[0] == "server" && args[1] == "start" {
		// The core stays, with the output open: it writes to no pipe of
		// the wrapper.
		cmd.Stdout, cmd.Stderr = nil, nil
	} else {
		cmd.Stdout, cmd.Stderr = &out, &out
	}
	cmd.WaitDelay = time.Second
	if err := e.boundary.Start(cmd); err != nil {
		return "", err
	}
	err := cmd.Wait()
	if ctx.Err() != nil {
		err = fmt.Errorf("no answer in %s", hcWait)
	}
	return out.String(), err
}

// coreEnv is env with no SUDO_ name and no HC_ name: the wrapper's own
// start can be through sudo, and hc takes SUDO_USER as the user of its
// core; an HC_ name (HC_SESSION, HC_SOCKET) chooses another core.
func coreEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, "SUDO_") && !strings.HasPrefix(kv, "HC_") {
			out = append(out, kv)
		}
	}
	return out
}

// decodeJSON decodes the first JSON object of an hc command's output.
func decodeJSON(out string, v any) error {
	i := strings.IndexByte(out, '{')
	if i < 0 {
		return errors.New("no JSON object")
	}
	return json.NewDecoder(strings.NewReader(out[i:])).Decode(v)
}

// checkCore checks that the hc core and each holder of its terminals are
// in the Execution's kill boundary. Else it kills them, if they are the
// run user's, and gives an error.
func (e *execution) checkCore() error {
	out, err := e.hc("server", "status", "--json")
	var status struct {
		PID     int `json:"pid"`
		Holders []struct {
			PID int `json:"pid"`
		} `json:"holders"`
	}
	if err == nil {
		err = decodeJSON(out, &status)
	}
	if err != nil || status.PID <= 0 {
		return fmt.Errorf("hc server status: %v: %s", err, lastLines(out, 5))
	}
	pids := []int{status.PID}
	for _, h := range status.Holders {
		if h.PID > 0 {
			pids = append(pids, h.PID)
		}
	}
	var outside []int
	for _, pid := range pids {
		if in, err := e.boundary.Holds(pid); err != nil {
			return fmt.Errorf("find the hc core in the Execution's cgroup: %w", err)
		} else if !in {
			outside = append(outside, pid)
		}
	}
	if len(outside) == 0 {
		return nil
	}
	for _, pid := range outside {
		var st syscall.Stat_t
		if syscall.Stat(fmt.Sprintf("/proc/%d", pid), &st) == nil && int(st.Uid) == e.runUser.uid {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	return fmt.Errorf("the hc core is not in the Execution's cgroup (processes %v): a core started before the wrapper's, "+
		"such as by an early hc connect, or by systemd; try the take-over again", outside)
}

// HarnessEnv is the harness-env command: args are the environment file
// (NUL-separated NAME=value), the template of .claude.json, "--", and the
// harness's command line. It runs as the run user, in the checkout's
// folder. It puts the template and the trust of that folder in
// $CLAUDE_CONFIG_DIR/.claude.json (prepareClaudeJSON), then starts the
// harness with environ and the file's names (the file wins).
func HarnessEnv(args []string, environ []string, stderr io.Writer) int {
	if len(args) < 4 || args[2] != "--" {
		fmt.Fprintln(stderr, "wrapper harness-env: give FILE TEMPLATE -- PROGRAM [ARGS...]")
		return 2
	}
	template := args[1]
	b, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "wrapper harness-env:", err)
		return 1
	}
	env := map[string]string{}
	var order []string
	add := func(kv string) {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			return
		}
		if _, seen := env[name]; !seen {
			order = append(order, name)
		}
		env[name] = kv
	}
	for _, kv := range environ {
		add(kv)
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, 0); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for sc.Scan() {
		add(sc.Text())
	}
	list := make([]string, 0, len(order))
	for _, name := range order {
		list = append(list, env[name])
	}
	if config, ok := env["CLAUDE_CONFIG_DIR"]; ok {
		if cwd, err := os.Getwd(); err != nil {
			fmt.Fprintln(stderr, "wrapper harness-env:", err)
		} else if err := prepareClaudeJSON(strings.TrimPrefix(config, "CLAUDE_CONFIG_DIR="), template, cwd); err != nil {
			// Claude Code then shows its first-run dialogs; the person
			// can still answer them.
			fmt.Fprintln(stderr, "wrapper harness-env: .claude.json:", err)
		}
	}
	if path, ok := env["PATH"]; ok {
		os.Setenv("PATH", strings.TrimPrefix(path, "PATH="))
	}
	program, err := exec.LookPath(args[3])
	if err != nil {
		fmt.Fprintln(stderr, "wrapper harness-env:", err)
		return 127
	}
	err = syscall.Exec(program, args[3:], list)
	fmt.Fprintln(stderr, "wrapper harness-env:", err)
	return 126
}

// prepareClaudeJSON puts the keys of the image's template (the onboarding
// done) and projects[checkout].hasTrustDialogAccepted in
// <config>/.claude.json, and keeps the other keys that the Executions
// before wrote there. With them, the interactive Claude Code opens at its
// prompt, with no first-run dialog. It runs as the run user (harness-env),
// so it writes only what that user can write. With no template, it writes
// the trust, and gives the error.
func prepareClaudeJSON(config, template, checkout string) error {
	path := filepath.Join(config, ".claude.json")
	got := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &got); err != nil || got == nil {
			return fmt.Errorf("%s is not a JSON object: %v", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var templateErr error
	if b, err := os.ReadFile(template); err != nil {
		templateErr = fmt.Errorf("the template: %w", err)
	} else {
		var keys map[string]any
		if err := json.Unmarshal(b, &keys); err != nil {
			templateErr = fmt.Errorf("the template %s: %w", template, err)
		}
		for name, value := range keys {
			if name != "projects" {
				got[name] = value
			}
		}
	}
	projects, _ := got["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
	}
	project, _ := projects[checkout].(map[string]any)
	if project == nil {
		project = map[string]any{}
	}
	project["hasTrustDialogAccepted"] = true
	projects[checkout] = project
	got["projects"] = projects
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config, 0o700); err != nil {
		return err
	}
	part, err := os.CreateTemp(config, ".claude.json.part")
	if err != nil {
		return err
	}
	_, err = part.Write(b)
	if cerr := part.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(part.Name(), path)
	}
	if err != nil {
		os.Remove(part.Name())
		return err
	}
	return templateErr
}
