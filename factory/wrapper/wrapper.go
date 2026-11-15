// Package wrapper is the program that each Execution runs on the agent
// machine. It takes the task and secrets from the Dispatcher with its
// workload identity JWT, clones the repository, starts the headless form
// of the Harness (Claude Code), writes the task as stream-json input,
// serves the agent's socket, and ends the Execution. The outcome is the
// agent's last valid report, never the harness's exit code. During the
// Execution and before the end call, it uploads the Run's Agent sessions
// with the Execution's child token (uploads.go). The harness's Stop hook
// is the wrapper's own program (stop_hook.go).
//
// The checkout is at a fixed path for each repository, on the Run's branch
// (checkout.go). Later tickets add the run user (39) and the restore of
// the Agent session directory (59).
package wrapper

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
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

// writeWait is the longest time that the end waits to write the
// interrupt to a harness that does not read its stdin. It is shorter than
// endWait.
const writeWait = time.Second

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
	c := newClient(cfg)
	task, err := c.task(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		boundary.Remove(emptyWait)
		return 1
	}
	if cfg.projectName == "" {
		// One project directory name for each repository: a resume on
		// another machine finds the Agent session at the same place.
		cfg.projectName = projectDirName(task.Repository)
	}
	work, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	e := &execution{cfg: cfg, client: c, task: task, log: stderr, boundary: boundary, needsPerson: make(chan struct{}, 1),
		uploads: newUploads(cfg, c, task, stderr), cancelWork: cancelWork}
	// The commands and the limits count from the task, and stop at the
	// end call.
	watch, stopWatching := context.WithCancel(ctx)
	defer stopWatching()
	go e.pollCommands(watch)
	go e.watchLimits(watch, cfg.clock())
	end := e.run(work, environ)
	stopWatching()
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

// prCallTimeout is the limit of the wrapper's pr call to the Dispatcher.
// It is longer than the time that the Dispatcher gives the forge, so the
// wrapper gets the Dispatcher's answer also from a slow forge.
const prCallTimeout = dispatcherapi.PRBudget + 30*time.Second

// newClient is the client of the Dispatcher's API. The Dispatcher's
// private listener is reached directly, never through the proxy of the
// wrapper's environment, which the uploads use.
func newClient(cfg config) *client {
	direct := http.DefaultTransport.(*http.Transport).Clone()
	direct.Proxy = nil
	return &client{url: cfg.dispatcherURL, execution: cfg.execution, identityFile: cfg.identityFile,
		http:   &http.Client{Timeout: 30 * time.Second, Transport: direct},
		prHTTP: &http.Client{Timeout: prCallTimeout, Transport: direct}}
}

// execution is one Execution on this machine.
type execution struct {
	cfg      config
	client   *client
	task     dispatcherapi.Task
	log      io.Writer
	boundary killBoundary

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
	// cancelWork stops the setup (the clone) when the end starts before
	// the harness.
	cancelWork context.CancelFunc
	// lastOutput is when the last event of the harness's output came, on
	// cfg.clock, in Unix nanoseconds (0: the harness has not started).
	lastOutput atomic.Int64
	// delivered counts the wrapper's messages whose replay echo came (the
	// proof of delivery). A report sends the count.
	delivered atomic.Int64
	// checksMu holds checks: the record of each check that the wrapper
	// ran in this Execution, the last result of each command, and
	// running: the count of runs of each check that has no record yet.
	// A running check holds its place, so that the records never go
	// above MaxChecks.
	checksMu sync.Mutex
	checks   []dispatcherapi.CheckResult
	running  map[checkKey]int

	needsPerson chan struct{}
	uploads     *uploads
	// stdin is the harness's stdin. writing holds each write to it, so
	// that an interrupt never goes into the middle of a message. It is not
	// mu: a write blocks while the harness does not read, and the end must
	// not wait for it.
	stdin    *os.File
	writing  sync.Mutex
	harness  *exec.Cmd
	kill     *time.Timer
	killOnce sync.Once
	// left is the error when the cgroup was not empty after the end.
	left error
	// checkout is the clone ("" before the clone), and workDir the
	// agent's working directory in it: the Project's folder.
	checkout, workDir string
	// startTip is the commit of the Run's branch after the setup: a push
	// is necessary only when the branch moved.
	startTip string
	// prMu holds one pr call at a time, and the push before the end until
	// a pr call's push ended. pushedTip is the commit that the last pr
	// call pushed.
	prMu      sync.Mutex
	pushedTip string
}

// run sets up and runs the harness, then does the end procedure, and gives
// the end call's body.
func (e *execution) run(ctx context.Context, environ []string) dispatcherapi.ExecutionEnd {
	end := e.work(ctx, environ)
	e.finish()
	// Before the end: each commit that is not pushed. After the
	// Execution: no workspace and no home of the run user.
	if failed := e.push(environ); failed != "" {
		if end.Error != "" {
			end.Error += "; "
		}
		end.Error += failed
	}
	e.cleanUp()
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
	if err := e.setup(ctx, environ); err != nil {
		// A stop or a limit during the setup stops the clone.
		if end, ok := e.endedEarly(); ok {
			return end
		}
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: err.Error()}
	}
	server, err := e.serveSocket(environ)
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

// serveSocket serves the agent's socket. A report or a pr call goes to
// the Dispatcher for this Execution only: what the agent sends names no
// Run.
func (e *execution) serveSocket(environ []string) (*http.Server, error) {
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
	mux.HandleFunc("POST "+agentsocket.CheckPath, func(w http.ResponseWriter, r *http.Request) { e.check(w, r, environ) })
	mux.HandleFunc("POST "+agentsocket.PRPath, func(w http.ResponseWriter, r *http.Request) { e.pr(w, r, environ) })
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
	// Only these fields go on: the Run and the Execution are the
	// wrapper's, and so are the checks, the Evidence paths and the
	// delivered-message counter.
	report = dispatcherapi.Report{Outcome: report.Outcome, Summary: report.Summary, Question: report.Question}
	a, ok := e.sendReport(w, r, report)
	if !ok {
		return
	}
	// Before the answer, so that the agent's next stop finds the report;
	// not under mu, so that a lock on the file does not hold the end.
	e.setRunState(func(s *runState) { s.Reported = true })
	if report.Outcome == dispatcherapi.OutcomeNeedsAPerson {
		select {
		case e.needsPerson <- struct{}{}:
		default:
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(a.body)
}

// sendReport sends the report to the Dispatcher under mu, and records
// that the Dispatcher took it. When not ok, it wrote the problem to w.
func (e *execution) sendReport(w http.ResponseWriter, r *http.Request, report dispatcherapi.Report) (answer, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ending {
		writeProblem(w, http.StatusConflict, "the Execution is ending: a report now does not count")
		return answer{}, false
	}
	commit := e.addEvidence(&report)
	a, err := e.client.call(r.Context(), "report", report)
	if err != nil {
		commit(false)
		writeProblem(w, http.StatusBadGateway, "the wrapper could not send the report: "+err.Error())
		return answer{}, false
	}
	commit(a.status/100 == 2)
	if a.status/100 != 2 {
		writeProblem(w, a.status, a.message)
		return answer{}, false
	}
	e.reported = true
	return a, true
}

// pr pushes the Run's branch, then sends the agent's pr call to the
// Dispatcher, which opens or updates the Run's PR through its Forge.
func (e *execution) pr(w http.ResponseWriter, r *http.Request, environ []string) {
	var text dispatcherapi.PRText
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&text); err != nil {
		writeProblem(w, http.StatusBadRequest, "the body must be a JSON object with title and description")
		return
	}
	text = dispatcherapi.PRText{Title: text.Title, Description: text.Description}
	e.prMu.Lock()
	defer e.prMu.Unlock()
	e.mu.Lock()
	ending := e.ending
	e.mu.Unlock()
	if ending {
		writeProblem(w, http.StatusConflict, "the Execution is ending: the wrapper pushes the Run's branch, and opens no PR now")
		return
	}
	if err := e.pushForPR(r.Context(), environ); err != nil {
		fmt.Fprintln(e.log, "wrapper: the push for a pr call:", err)
		writeProblem(w, http.StatusBadGateway, err.Error())
		return
	}
	a, err := e.client.callPR(r.Context(), text)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "the wrapper could not send the pr call: "+err.Error())
		return
	}
	if a.status/100 != 2 {
		writeProblem(w, a.status, a.message)
		return
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
		"--replay-user-messages", "--permission-mode", "bypassPermissions", "--disallowedTools", "AskUserQuestion",
		"--settings", e.cfg.harnessSettings()}
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
	cmd.Dir = e.workDir
	cmd.Env = append(append(e.gitEnv(environ, e.cfg.socket()), e.uploads.claudeEnv(e.cfg)...),
		EnvRunState+"="+e.cfg.runState())
	cmd.Stderr = e.log
	failed := func(err error) dispatcherapi.ExecutionEnd {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: "start the harness: " + err.Error()}
	}
	if err := e.installStopHook(); err != nil {
		return failed(err)
	}
	// A pipe of the wrapper's own, not cmd.StdinPipe: the end sets a write
	// deadline on it.
	in, stdin, err := os.Pipe()
	if err != nil {
		return failed(err)
	}
	cmd.Stdin = in
	// A pipe of the wrapper's own, not cmd.StdoutPipe: the wrapper waits
	// for the harness's exit while it reads the output.
	stdout, w, err := os.Pipe()
	if err != nil {
		return failed(err)
	}
	defer stdout.Close()
	cmd.Stdout = w
	// The start is under mu: a stop or a limit before it means no start,
	// and one after it finds stdin.
	e.mu.Lock()
	if e.ending {
		e.mu.Unlock()
		w.Close()
		in.Close()
		stdin.Close()
		end, _ := e.endedEarly()
		return end
	}
	err = e.boundary.Start(cmd)
	w.Close()
	in.Close()
	if err == nil {
		e.harness, e.stdin = cmd, stdin
		e.lastOutput.Store(e.cfg.clock().UnixNano())
	}
	e.mu.Unlock()
	if err != nil {
		stdin.Close()
		return failed(err)
	}
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
	// The write is not under mu: a harness that does not read its stdin
	// blocks it, and a stop or a limit must still end the Execution.
	e.writing.Lock()
	if _, ending := e.endedEarly(); !ending {
		if _, err := e.stdin.Write(append(message, '\n')); err != nil {
			fmt.Fprintln(e.log, "wrapper: write the task:", err)
		}
	}
	e.writing.Unlock()
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
			e.lastOutput.Store(e.cfg.clock().UnixNano())
			switch {
			case ev["type"] == "user" && ev["isReplay"] == true && ev["uuid"] == taskUUID && delivered == nil:
				now := time.Now().UTC()
				delivered = &now
				e.delivered.Add(1)
			case ev["type"] == "system" && ev["subtype"] == "background_tasks_changed":
				tasks, _ := ev["tasks"].([]any)
				e.mu.Lock()
				e.backgroundTasks = len(tasks)
				e.mu.Unlock()
				e.setRunState(func(s *runState) { s.BackgroundTasks = len(tasks) })
			case ev["type"] == "system" && ev["subtype"] == "init" && offers(ev, "AskUserQuestion"):
				e.startEnd(dispatcherapi.EndSetupFailed, "the harness offers AskUserQuestion, which no person answers in a Run")
			case ev["type"] == "result":
				// The turn ended: the next stop is in a new turn. This
				// comes before the end closes stdin.
				e.setRunState(func(s *runState) { s.Turn++ })
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
func (e *execution) startEnd(reason, why string) { e.beginEnd(reason, why, false) }

// beginEnd starts the end once, with reason. With interrupt, it first
// writes the interrupt control request, so that the agent's turn stops
// (a long tool call too) before stdin closes. Before the harness starts,
// it stops the setup, and the harness does not start.
//
// The writes to stdin are not under mu, and wait at most writeWait: a
// harness that does not read its stdin does not hold the end.
func (e *execution) beginEnd(reason, why string, interrupt bool) {
	e.mu.Lock()
	if e.ending {
		e.mu.Unlock()
		return
	}
	e.ending, e.endReason, e.endError = true, reason, why
	if e.cancelWork != nil {
		e.cancelWork()
	}
	stdin := e.stdin
	if stdin != nil {
		e.kill = time.AfterFunc(endWait, e.killAll)
	}
	e.mu.Unlock()
	if stdin == nil {
		return
	}
	// The deadline also ends a write of the task that blocks.
	if interrupt && stdin.SetWriteDeadline(time.Now().Add(writeWait)) == nil {
		request, _ := json.Marshal(map[string]any{"type": "control_request", "request_id": newUUID(),
			"request": map[string]any{"subtype": "interrupt"}})
		e.writing.Lock()
		if _, err := stdin.Write(append(request, '\n')); err != nil {
			fmt.Fprintln(e.log, "wrapper: write the interrupt:", err)
		}
		e.writing.Unlock()
	}
	stdin.Close()
}

// endedEarly gives the end call's body when the end started before the
// harness: a stop or a limit.
func (e *execution) endedEarly() (dispatcherapi.ExecutionEnd, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return dispatcherapi.ExecutionEnd{Reason: e.endReason, Error: e.endError}, e.ending
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
