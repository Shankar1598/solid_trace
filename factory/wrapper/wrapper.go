// Package wrapper is the program that each Execution runs on the agent
// machine. It takes the task and secrets from the Dispatcher with its
// workload identity JWT, clones the repository, starts the headless form
// of the Harness (Claude Code), writes the task as stream-json input,
// serves the agent's socket, and ends the Execution. The outcome is the
// agent's last valid report, never the harness's exit code. During the
// Execution and before the end call, it uploads the Run's Agent sessions
// with the Execution's child token (uploads.go).
//
// Later tickets add the cgroup end procedure (31), the fixed paths and the
// Run's branch (33), the run user (39), the restore of the Agent session
// directory (59), the commands and the limits (32), and the Stop hook (38).
package wrapper

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/dispatcherapi"
)

// endWait is how long the wrapper waits for the harness to exit after it
// closes stdin, before it kills the harness's process group.
const endWait = 5 * time.Second

// outputWait is how long the wrapper reads the harness's output after the
// harness exits. A process that the harness started, out of its process
// group, can hold the output open.
const outputWait = 2 * time.Second

// Main is the wrapper program. environ is the wrapper's environment; the
// harness gets it with no WRAPPER_ name. It returns the exit code: 0 when
// the Dispatcher took the end call.
func Main(ctx context.Context, getenv func(string) string, environ []string, stderr io.Writer) int {
	cfg, err := loadConfig(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		return 2
	}
	// The Dispatcher's private listener is reached directly, never
	// through the proxy that the uploads use.
	direct := http.DefaultTransport.(*http.Transport).Clone()
	direct.Proxy = nil
	c := &client{url: cfg.dispatcherURL, execution: cfg.execution, identityFile: cfg.identityFile,
		http: &http.Client{Timeout: 30 * time.Second, Transport: direct}}
	task, err := c.task(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		return 1
	}
	e := &execution{cfg: cfg, client: c, task: task, log: stderr, needsPerson: make(chan struct{}, 1),
		uploads: newUploads(cfg, c, task, stderr)}
	end := e.run(ctx, environ)
	if err := c.end(ctx, end); err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		return 1
	}
	return 0
}

// execution is one Execution on this machine.
type execution struct {
	cfg    config
	client *client
	task   dispatcherapi.Task
	log    io.Writer

	// mu holds a report's forward and the start of the end, so that a
	// report counts only when it reached the Dispatcher before the end.
	mu        sync.Mutex
	ending    bool
	endReason string
	endError  string
	reported  bool

	needsPerson chan struct{}
	uploads     *uploads
	stdin       io.WriteCloser
	harness     *exec.Cmd
	kill        *time.Timer
}

// run sets up and runs the harness, and gives the end call's body.
func (e *execution) run(ctx context.Context, environ []string) dispatcherapi.ExecutionEnd {
	if err := e.setup(ctx, environ); err != nil {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: err.Error()}
	}
	server, err := e.serveSocket()
	if err != nil {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: err.Error()}
	}
	if err := e.uploads.prepare(); err != nil {
		_ = server.Close()
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: "make the project directory: " + err.Error()}
	}
	e.uploads.start()
	end := e.runHarness(environ)
	// The last bytes go before the end call: the end step revokes the
	// Execution's token.
	e.uploads.finish()
	// No report is in flight after this: the end call can go.
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	return end
}

// setup writes the forge token and the credential helper outside the
// checkout, and clones the base branch with them.
func (e *execution) setup(ctx context.Context, environ []string) error {
	secrets := e.cfg.secrets()
	if strings.ContainsAny(secrets, "'\n") {
		return fmt.Errorf("the secrets directory %q has a quote or a new line", secrets)
	}
	if err := os.MkdirAll(secrets, 0o700); err != nil {
		return err
	}
	clone := []string{"clone", "--quiet"}
	var helper string
	if e.task.ForgeToken != "" {
		helper = filepath.Join(secrets, "git-credential")
		token := filepath.Join(secrets, "forge-token")
		if err := os.WriteFile(token, []byte(e.task.ForgeToken), 0o600); err != nil {
			return err
		}
		if strings.ContainsAny(e.task.ForgeUsername, "\n\x00") {
			return errors.New("the forge user name has a new line")
		}
		script := "#!/bin/sh\n# The git credential helper of the Execution: the forge connection's token.\n" +
			"test \"$1\" = get || exit 0\n" +
			"printf 'username=%s\\n' " + shellQuote(e.task.ForgeUsername) + "\n" +
			"printf 'password=%s\\n' \"$(cat '" + token + "')\"\n"
		if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
			return err
		}
		clone = append([]string{"-c", "credential.helper=", "-c", "credential.helper=" + helper}, clone...)
	}
	if e.task.BaseBranch != "" {
		clone = append(clone, "--branch", e.task.BaseBranch)
	}
	clone = append(clone, "--", e.task.CloneURL, e.cfg.checkout())
	if err := e.git(ctx, environ, "", clone...); err != nil {
		return fmt.Errorf("clone %s: %w", e.task.Repository, err)
	}
	if helper != "" {
		// The agent's own git calls (push) use the same helper.
		for _, value := range []string{"", helper} {
			if err := e.git(ctx, environ, e.cfg.checkout(), "config", "--add", "credential.helper", value); err != nil {
				return fmt.Errorf("set the credential helper: %w", err)
			}
		}
	}
	return nil
}

func (e *execution) git(ctx context.Context, environ []string, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(harnessEnv(environ, ""), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %v: %s", args[len(args)-1], err, lastLines(string(out), 20))
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// harnessEnv is environ with no WRAPPER_ name and no agent's socket, and
// the agent's socket at socket ("" sets none).
func harnessEnv(environ []string, socket string) []string {
	var env []string
	for _, kv := range environ {
		if strings.HasPrefix(kv, "WRAPPER_") || strings.HasPrefix(kv, agentsocket.Env+"=") {
			continue
		}
		env = append(env, kv)
	}
	if socket != "" {
		env = append(env, agentsocket.Env+"="+socket)
	}
	return env
}

// serveSocket serves the agent's socket. A report goes to the Dispatcher
// for this Execution only: what the agent sends names no Run.
func (e *execution) serveSocket() (*http.Server, error) {
	path := e.cfg.socket()
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("serve the agent's socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+agentsocket.ReportPath, e.report)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "the agent's socket has no "+r.Method+" "+r.URL.Path)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go server.Serve(listener)
	return server, nil
}

func (e *execution) report(w http.ResponseWriter, r *http.Request) {
	var report dispatcherapi.Report
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&report); err != nil {
		writeProblem(w, http.StatusBadRequest, "the body must be a JSON object with outcome, summary and question")
		return
	}
	// Only these fields go on: the Run and the Execution are the wrapper's.
	report = dispatcherapi.Report{Outcome: report.Outcome, Summary: report.Summary, Question: report.Question}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ending {
		writeProblem(w, http.StatusConflict, "the Execution is ending: a report now does not count")
		return
	}
	a, err := e.client.call(r.Context(), "report", report)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "the wrapper could not send the report: "+err.Error())
		return
	}
	if a.status/100 != 2 {
		writeProblem(w, a.status, a.message)
		return
	}
	e.reported = true
	if report.Outcome == dispatcherapi.OutcomeNeedsAPerson {
		select {
		case e.needsPerson <- struct{}{}:
		default:
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(a.body)
}

func writeProblem(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dispatcherapi.Problem{Error: message})
}

// headless is the command line of the headless form of Claude Code.
func (e *execution) headless() []string {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--replay-user-messages", "--permission-mode", "bypassPermissions", "--disallowedTools", "AskUserQuestion"}
	if e.task.Resume {
		return append(args, "--resume", e.task.SessionID)
	}
	return append(args, "--session-id", e.task.SessionID)
}

// runHarness starts the harness, writes the task, reads the output
// stream until the harness exits, and gives the end call's body. The exit
// starts the end, also when a process holds the output open.
func (e *execution) runHarness(environ []string) dispatcherapi.ExecutionEnd {
	cmd := exec.Command(e.cfg.harness, e.headless()...)
	cmd.Dir = e.cfg.checkout()
	cmd.Env = append(harnessEnv(environ, e.cfg.socket()), e.uploads.claudeEnv(e.cfg)...)
	cmd.Stderr = e.log
	// A process group of its own, so that the end can kill what is left.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	failed := func(err error) dispatcherapi.ExecutionEnd {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: "start the harness: " + err.Error()}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return failed(err)
	}
	// A pipe of the wrapper's own, not cmd.StdoutPipe: the wrapper waits
	// for the harness's exit while it reads the output.
	stdout, w, err := os.Pipe()
	if err != nil {
		return failed(err)
	}
	defer stdout.Close()
	cmd.Stdout = w
	err = cmd.Start()
	w.Close()
	if err != nil {
		return failed(err)
	}
	e.harness, e.stdin = cmd, stdin
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	return e.follow(stdout, exited)
}

// follow reads the output stream until the harness exited and the stream
// ended (or outputWait passed after the exit), and gives the end call's
// body.
func (e *execution) follow(stdout *os.File, exited <-chan struct{}) dispatcherapi.ExecutionEnd {
	taskUUID := newUUID()
	message, _ := json.Marshal(map[string]any{"type": "user", "uuid": taskUUID, "session_id": "",
		"parent_tool_use_id": nil, "message": map[string]any{"role": "user", "content": e.task.Prompt}})
	if _, err := e.stdin.Write(append(message, '\n')); err != nil {
		fmt.Fprintln(e.log, "wrapper: write the task:", err)
	}
	events := make(chan map[string]any)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1<<20), 64<<20)
		for scanner.Scan() {
			var ev map[string]any
			if json.Unmarshal(scanner.Bytes(), &ev) == nil {
				events <- ev
			}
		}
		// Read the rest, so that the harness never blocks on a full pipe.
		io.Copy(io.Discard, stdout)
	}()
	var delivered *time.Time
	var stopReading *time.Timer
	pid := e.harness.Process.Pid
	for open, exit := true, exited; open; {
		select {
		case <-exit:
			// Each tool process that is left in the harness's group goes
			// too, and closes the output.
			exit = nil
			syscall.Kill(-pid, syscall.SIGKILL)
			stopReading = time.AfterFunc(outputWait, func() { stdout.Close() })
		case ev, ok := <-events:
			if !ok {
				open = false
				break
			}
			switch {
			case ev["type"] == "user" && ev["isReplay"] == true && ev["uuid"] == taskUUID && delivered == nil:
				now := time.Now().UTC()
				delivered = &now
			case ev["type"] == "system" && ev["subtype"] == "init" && offers(ev, "AskUserQuestion"):
				e.startEnd(dispatcherapi.EndSetupFailed, "the harness offers AskUserQuestion, which no person answers in a Run")
			case ev["type"] == "result":
				e.mu.Lock()
				reported := e.reported
				e.mu.Unlock()
				if reported {
					e.startEnd(dispatcherapi.EndReported, "")
				} else {
					e.startEnd(dispatcherapi.EndNoReport, "")
				}
			}
		case <-e.needsPerson:
			// Needs a person ends the Execution at once.
			e.startEnd(dispatcherapi.EndReported, "")
		}
	}
	if stopReading != nil {
		stopReading.Stop()
	}
	<-exited
	// The output can end before the exit: the group kill is then here.
	syscall.Kill(-pid, syscall.SIGKILL)
	e.mu.Lock()
	if e.kill != nil {
		e.kill.Stop()
	}
	if !e.ending {
		e.ending, e.endReason = true, dispatcherapi.EndHarnessExited
	}
	reason, why := e.endReason, e.endError
	e.mu.Unlock()
	code := e.harness.ProcessState.ExitCode()
	return dispatcherapi.ExecutionEnd{Reason: reason, Error: why, ExitCode: &code, TaskDeliveredAt: delivered}
}

// startEnd starts the end once: it closes stdin, and kills the harness's
// process group if the harness did not exit in endWait.
func (e *execution) startEnd(reason, why string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ending {
		return
	}
	e.ending, e.endReason, e.endError = true, reason, why
	e.stdin.Close()
	pid := e.harness.Process.Pid
	e.kill = time.AfterFunc(endWait, func() { syscall.Kill(-pid, syscall.SIGKILL) })
}

// offers tells if the init event lists tool among the agent's tools.
func offers(init map[string]any, tool string) bool {
	tools, _ := init["tools"].([]any)
	for _, t := range tools {
		if t == tool {
			return true
		}
	}
	return false
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
