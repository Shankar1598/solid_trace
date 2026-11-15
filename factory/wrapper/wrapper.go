// Package wrapper is the program that each Execution runs on the agent
// machine. It takes the task and secrets from the Dispatcher with its
// workload identity JWT, clones the repository, starts the headless form
// of the Harness (Claude Code), writes the task as stream-json input,
// serves the agent's socket, and ends the Execution. The outcome is the
// agent's last valid report, never the harness's exit code. During the
// Execution and before the end call, it uploads the Run's Agent sessions
// with the Execution's child token (uploads.go).
//
// Later tickets add the fixed paths and the Run's branch (33), the run user (39), the restore of the Agent
// session directory (59), the commands and the limits (32), and the Stop hook (38).
package wrapper

import (
	"bufio"
	"bytes"
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
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/killboundary"
)

// endWait is how long the wrapper waits for the harness to exit after it
// closes stdin, before it kills the Execution's cgroup.
const endWait = 5 * time.Second

// dockerWait is the limit of the end's container step.
const dockerWait = 3 * time.Second

// emptyWait is how long the end waits for the Execution's cgroup to be
// empty after cgroup.kill.
const emptyWait = 5 * time.Second

// outputWait is how long the wrapper reads the harness's output after the
// harness exits. A process that the harness started can hold the output
// open until the cgroup kill.
const outputWait = 2 * time.Second

// CgroupName is the name of the Execution's child cgroup, in the
// wrapper's own cgroup.
func CgroupName(execution string) string { return "execution-" + execution }

// Main is the wrapper program. environ is the wrapper's environment; the
// harness gets it with no WRAPPER_ name. It returns the exit code: 0 when
// the Dispatcher took the end call and the Execution's cgroup is empty and
// removed. It refuses to start (code 2) with no kill boundary: cgroup v2
// with cgroup.kill, in a cgroup that the wrapper can write to.
func Main(ctx context.Context, getenv func(string) string, environ []string, stderr io.Writer) int {
	cfg, err := loadConfig(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		return 2
	}
	boundary, err := killboundary.Open(CgroupName(cfg.execution))
	if err != nil {
		fmt.Fprintln(stderr, "wrapper: no kill boundary, so the wrapper does not start:", err)
		return 2
	}
	runUser, err := lookupRunUser(getenv(EnvRunUser))
	if err == nil {
		err = runUser.checkDir(cfg.dir)
	}
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		boundary.Remove(emptyWait)
		return 2
	}
	c := newClient(cfg)
	task, err := c.task(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		boundary.Remove(emptyWait)
		return 1
	}
	e := &execution{cfg: cfg, client: c, task: task, log: stderr, boundary: boundary, runUser: runUser, needsPerson: make(chan struct{}, 1),
		uploads: newUploads(cfg, c, task, stderr)}
	end := e.run(ctx, environ)
	if err := c.end(ctx, end); err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		return 1
	}
	if e.left != nil {
		return 1
	}
	return 0
}

// killBoundary is the Execution's kill boundary, a *killboundary.Boundary.
// A test of the end procedure gives a fake.
type killBoundary interface {
	Start(cmd *exec.Cmd) error
	Populated() (bool, error)
	Kill() error
	Remove(within time.Duration) error
}

// newClient is the client of the Dispatcher's API. The Dispatcher's
// private listener is reached directly, never through the proxy of the
// wrapper's environment, which the uploads use.
func newClient(cfg config) *client {
	direct := http.DefaultTransport.(*http.Transport).Clone()
	direct.Proxy = nil
	return &client{url: cfg.dispatcherURL, execution: cfg.execution, identityFile: cfg.identityFile,
		http: &http.Client{Timeout: 30 * time.Second, Transport: direct}}
}

// execution is one Execution on this machine.
type execution struct {
	cfg      config
	client   *client
	task     dispatcherapi.Task
	log      io.Writer
	boundary killBoundary
	// runUser is the user of each process that the wrapper starts.
	runUser runUser

	// mu holds a report's forward and the start of the end, so that a
	// report counts only when it reached the Dispatcher before the end.
	mu        sync.Mutex
	ending    bool
	endReason string
	endError  string
	reported  bool
	// backgroundTasks is the count of the agent's background tasks that
	// run, from the last background_tasks_changed event. populatedAtKill
	// tells if a process was in the cgroup when the end killed it.
	backgroundTasks int
	populatedAtKill bool

	needsPerson chan struct{}
	uploads     *uploads
	stdin       io.WriteCloser
	harness     *exec.Cmd
	kill        *time.Timer
	killOnce    sync.Once
	// left is the error when the cgroup was not empty after the end.
	left error
}

// run sets up and runs the harness, then does the end procedure, and gives
// the end call's body.
func (e *execution) run(ctx context.Context, environ []string) dispatcherapi.ExecutionEnd {
	end := e.work(ctx, environ)
	e.finish()
	// The output stream is read to its end now: the last
	// background_tasks_changed event counts, also when the harness wrote
	// it just before it exited.
	e.mu.Lock()
	if e.populatedAtKill {
		end.BackgroundTasksKilled = e.backgroundTasks
	}
	e.mu.Unlock()
	return end
}

// finish is the end procedure after the harness exited (or did not
// start): cgroup.kill, docker kill each running container, and the check
// that the cgroup is empty, which then goes.
func (e *execution) finish() {
	e.killAll()
	e.stopContainers()
	err := e.boundary.Remove(emptyWait)
	if err != nil {
		// A process that started after the kill (it cannot, but a check
		// costs nothing): kill again, once.
		e.boundary.Kill()
		err = e.boundary.Remove(emptyWait)
	}
	if err != nil {
		e.left = err
		fmt.Fprintln(e.log, "wrapper: the end procedure left processes of the Execution:", err)
	}
}

// killAll writes 1 to cgroup.kill, once. It records if a process was
// still in the cgroup: the background tasks that the harness listed then
// count as killed.
func (e *execution) killAll() {
	e.killOnce.Do(func() {
		populated, err := e.boundary.Populated()
		if err != nil {
			fmt.Fprintln(e.log, "wrapper: read the Execution's cgroup:", err)
		}
		if err := e.boundary.Kill(); err != nil {
			fmt.Fprintln(e.log, "wrapper: cgroup.kill:", err)
		}
		e.mu.Lock()
		e.populatedAtKill = populated
		e.mu.Unlock()
	})
}

// stopContainers is the end's container step: docker kill each running
// container, best effort, within dockerWait. Containers run outside the
// Execution's cgroup, in Docker's own. With no docker program, the step
// is skipped.
func (e *execution) stopContainers() {
	docker, err := exec.LookPath(e.cfg.docker)
	if err != nil {
		fmt.Fprintf(e.log, "wrapper: no Docker (%s), so the end skips the container step\n", e.cfg.docker)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), dockerWait)
	defer cancel()
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, docker, args...)
		cmd.WaitDelay = 500 * time.Millisecond
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		if ctx.Err() != nil {
			err = fmt.Errorf("no answer in %s", dockerWait)
		}
		return out.String(), err
	}
	out, err := run("ps", "--quiet")
	if err != nil {
		fmt.Fprintf(e.log, "wrapper: the end's container step: docker ps: %v: %s\n", err, lastLines(out, 5))
		return
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return
	}
	if out, err := run(append([]string{"kill"}, ids...)...); err != nil {
		fmt.Fprintf(e.log, "wrapper: the end's container step: docker kill: %v: %s\n", err, lastLines(out, 5))
	}
}

// work sets up and runs the harness, and gives the end call's body.
func (e *execution) work(ctx context.Context, environ []string) dispatcherapi.ExecutionEnd {
	// Before the run user's first process (the clone).
	if err := e.uploads.prepare(e.runUser); err != nil {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: "make the project directory: " + err.Error()}
	}
	if err := e.setup(ctx, environ); err != nil {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: err.Error()}
	}
	server, err := e.serveSocket()
	if err != nil {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: err.Error()}
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
// checkout, and clones the base branch with them, as the run user.
func (e *execution) setup(ctx context.Context, environ []string) error {
	secrets := e.cfg.secrets()
	if strings.ContainsAny(secrets, "'\n") {
		return fmt.Errorf("the secrets directory %q has a quote or a new line", secrets)
	}
	if err := os.Mkdir(secrets, 0o700); err != nil {
		return err
	}
	if err := e.runUser.share(secrets, 0o750); err != nil {
		return err
	}
	if err := e.runUser.mkdirAll(e.cfg.checkout()); err != nil {
		return err
	}
	clone := []string{"clone", "--quiet"}
	var helper string
	if e.task.ForgeToken != "" {
		helper = filepath.Join(secrets, "git-credential")
		token := filepath.Join(secrets, "forge-token")
		if err := writeSecret(e.runUser, token, []byte(e.task.ForgeToken), 0o440); err != nil {
			return err
		}
		if strings.ContainsAny(e.task.ForgeUsername, "\n\x00") {
			return errors.New("the forge user name has a new line")
		}
		script := "#!/bin/sh\n# The git credential helper of the Execution: the forge connection's token.\n" +
			"test \"$1\" = get || exit 0\n" +
			"printf 'username=%s\\n' " + shellQuote(e.task.ForgeUsername) + "\n" +
			"printf 'password=%s\\n' \"$(cat '" + token + "')\"\n"
		if err := writeSecret(e.runUser, helper, []byte(script), 0o550); err != nil {
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

// git runs git as the run user in the Execution's kill boundary, as each
// process of the run user: a hook that git starts is in the child cgroup,
// and the end kills what it leaves.
func (e *execution) git(ctx context.Context, environ []string, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(e.runUser.env(harnessEnv(environ, "")), "GIT_TERMINAL_PROMPT=0")
	e.runUser.start(cmd)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// A process that git starts and that holds the output open does not
	// hold the setup.
	cmd.WaitDelay = time.Second
	err := e.boundary.Start(cmd)
	if err == nil {
		if err = cmd.Wait(); errors.Is(err, exec.ErrWaitDelay) {
			err = nil
		}
	}
	if err != nil {
		return fmt.Errorf("git %s: %v: %s", args[len(args)-1], err, lastLines(out.String(), 20))
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

// EnvSubprocessScrub is Claude Code's subprocess environment scrub. The
// wrapper turns it off (0), last in the harness's environment, so that the
// machine's environment cannot turn it on. In the pinned Claude Code
// (2.1.325) the scrub is a hardening for untrusted users: it forces the
// permission mode to default (so headless Bash calls are refused), it
// needs bubblewrap with user namespaces, and its sandbox stops Bash from
// writing package.json, the lock files and CLAUDE_CONFIG_DIR (evidence/
// is there), and from reading the Docker socket. A Run must do each of
// these. The Harness contract check TestContractSubprocessEnvScrub fails
// when a new version changes this. So the boundary is: no secret of the
// wrapper's in the harness's environment, a Run's own secrets in files of
// the secrets directory, and the redaction patterns for the Claude token,
// which the run user can read in the harness's environment.
const EnvSubprocessScrub = "CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"

// harnessEnv is environ with no WRAPPER_ or NOMAD_ name and no agent's
// socket, and the agent's socket at socket ("" sets none). The names of
// the wrapper and of the Runner are not the agent's: NOMAD_TOKEN is a
// credential when a workload identity has env = true.
func harnessEnv(environ []string, socket string) []string {
	var env []string
	for _, kv := range environ {
		if strings.HasPrefix(kv, "WRAPPER_") || strings.HasPrefix(kv, "NOMAD_") || strings.HasPrefix(kv, agentsocket.Env+"=") {
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
	// The run user connects (check and report); no other user does.
	if err := e.runUser.share(path, 0o660); err != nil {
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

// runHarness starts the harness in the Execution's cgroup, writes the
// task, reads the output stream until the harness exits, and gives the end
// call's body. The exit starts the end, also when a process holds the
// output open.
func (e *execution) runHarness(environ []string) dispatcherapi.ExecutionEnd {
	cmd := exec.Command(e.cfg.harness, e.headless()...)
	cmd.Dir = e.cfg.checkout()
	cmd.Env = append(append(e.runUser.env(harnessEnv(environ, e.cfg.socket())), e.uploads.claudeEnv(e.cfg)...),
		// Last, so that the machine's environment cannot turn it on.
		EnvSubprocessScrub+"=0")
	e.runUser.start(cmd)
	cmd.Stderr = e.log
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
	err = e.boundary.Start(cmd)
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
	for open, exit := true, exited; open; {
		select {
		case <-exit:
			// Each process that is left in the cgroup goes too, and
			// closes the output.
			exit = nil
			e.killAll()
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
			case ev["type"] == "system" && ev["subtype"] == "background_tasks_changed":
				tasks, _ := ev["tasks"].([]any)
				e.mu.Lock()
				e.backgroundTasks = len(tasks)
				e.mu.Unlock()
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
	// The output can end before the exit: the kill is then here.
	e.killAll()
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

// startEnd starts the end once: it closes stdin, and kills the
// Execution's cgroup if the harness did not exit in endWait.
func (e *execution) startEnd(reason, why string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ending {
		return
	}
	e.ending, e.endReason, e.endError = true, reason, why
	e.stdin.Close()
	e.kill = time.AfterFunc(endWait, e.killAll)
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
