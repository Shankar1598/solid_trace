// Package wrapper is the program that each Execution runs on the agent
// machine. It takes the task and secrets from the Dispatcher with its
// workload identity JWT, clones the repository, starts the headless form
// of the Harness (Claude Code), writes the task as stream-json input,
// serves the agent's socket, and ends the Execution. The outcome is the
// agent's last valid report, never the harness's exit code. During the
// Execution and before the end call, it uploads the Run's Agent sessions
// with the Execution's child token (uploads.go). The harness's Stop hook
// is the wrapper's own program (stop_hook.go). When the Machine notice
// says that the machine will be taken back, or a SIGTERM or a SIGINT comes
// with no stop command, the preemption handler saves what it can and ends
// the Execution in at most 100 seconds (preemption.go).
//
// The checkout is at a fixed path for each repository, on the Run's branch
// (checkout.go). Each process that it starts runs as the run user
// (runuser.go). An Execution that resumes the Run's Agent session
// restores its directory from SolidTrace first (restore.go).
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
	"os/signal"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	runUser, err := lookupRunUser(getenv(EnvRunUser))
	if err == nil {
		err = cfg.makeDir()
	}
	if err == nil {
		err = runUser.checkDir(cfg.dir)
	}
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		boundary.Remove(emptyWait)
		return 2
	}
	// A take-over Execution follows in the same allocation: the same
	// wrapper starts it, on the same machine, in a new cgroup.
	var kept *keptWork
	for {
		code, next, left := execute(ctx, &cfg, boundary, runUser, environ, stderr, kept)
		if next == "" {
			return code
		}
		fmt.Fprintf(stderr, "wrapper: the take-over Execution %s starts on this machine\n", next)
		cfg.execution, kept = next, left
		if boundary, err = killboundary.Open(CgroupName(next)); err != nil {
			fmt.Fprintln(stderr, "wrapper: no kill boundary for the take-over, so it does not start:", err)
			kept.cleanUp(cfg, stderr)
			return 2
		}
	}
}

// keptWork is what an Execution that a take-over follows leaves on the
// machine for it: the checkout and the run user's home stay, and the
// project directory stays open (it was opened before the run user's
// first process, so a link that the agent made does not move it). With
// no project directory, nothing is kept: the take-over does the full
// setup.
type keptWork struct {
	projectDir *os.Root
}

// cleanUp deletes what a take-over did not use: the workspace and the
// run user's home.
func (k *keptWork) cleanUp(cfg config, log io.Writer) {
	if k.projectDir != nil {
		k.projectDir.Close()
	}
	(&execution{cfg: cfg, log: log}).cleanUp()
}

// execute runs one Execution, from its task call to its end call, and
// gives the wrapper's exit code. When a take-over Execution follows, it
// gives its id and what this Execution kept for it. kept is what an
// earlier Execution kept (nil for the first one).
func execute(ctx context.Context, cfg *config, boundary killBoundary, runUser runUser, environ []string, stderr io.Writer,
	kept *keptWork) (int, string, *keptWork) {
	c := newClient(*cfg)
	// The first Execution's task call goes once: the Runner sees the
	// wrapper's exit. A take-over's goes again, as the end call does, so
	// that the work that it keeps is not lost.
	tries := 1
	if kept != nil {
		tries = endTries
	}
	task, err := c.task(ctx, dispatcherapi.TaskRequest{VM: cfg.vm, Attach: cfg.attachCommand(runUser.name)}, tries)
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		boundary.Remove(emptyWait)
		if kept != nil {
			kept.cleanUp(*cfg, stderr)
		}
		return 1, "", nil
	}
	if cfg.projectName == "" {
		// One project directory name for each repository: a resume on
		// another machine finds the Agent session at the same place.
		cfg.projectName = projectDirName(task.Repository)
	}
	notice, err := newMachineNotice(*cfg, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "wrapper:", err)
		boundary.Remove(emptyWait)
		if kept != nil {
			kept.cleanUp(*cfg, stderr)
		}
		return 2, "", nil
	}
	e := newExecution(ctx, *cfg, c, task, stderr, boundary, runUser)
	e.environ = environ
	e.kept = kept
	// The preempting marker also stops the setup and the work.
	work, cancelWork := context.WithCancel(e.marked)
	defer cancelWork()
	e.cancelWork = cancelWork
	// A SIGTERM or a SIGINT with no stop command is the preemption
	// handler's second trigger. Before the task call, a signal stops the
	// wrapper at once: there is nothing to save.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	// The commands, the limits, the Machine notice and the message events
	// count from the task, and stop at the end call.
	watching, stopWatching := context.WithCancel(ctx)
	defer stopWatching()
	go e.watch(watching, notice, signals)
	go e.pollCommands(watching)
	go e.watchLimits(watching, cfg.clock())
	go e.sendEvents(watching)

	ran := make(chan dispatcherapi.ExecutionEnd, 1)
	go func() { ran <- e.run(work, environ) }()
	select {
	case end := <-ran:
		// run took the end, unless the preemption handler had it first.
		if e.owner.Load() == ownerEnd {
			stopWatching()
			next, err := c.end(ctx, end)
			if err != nil {
				fmt.Fprintln(stderr, "wrapper:", err)
			}
			// After the end call: no workspace and no home of the run
			// user, unless a take-over follows on this machine. Then the
			// project directory stays open for it. A take-over that
			// stopped the setup does the full setup again: what the setup
			// left is not a checkout that the setup command ran in.
			if err != nil || e.left != nil || next == "" || !e.setupDone {
				e.uploads.close()
				e.cleanUp()
				if err != nil || e.left != nil {
					return 1, "", nil
				}
				if next == "" {
					return 0, "", nil
				}
				return 0, next, &keptWork{}
			}
			return 0, next, &keptWork{projectDir: e.uploads.root()}
		}
		// The preemption handler has the end.
		<-e.preempted
	case <-e.preempted:
	}
	e.uploads.close()
	return e.preemptionCode(), "", nil
}

func newExecution(ctx context.Context, cfg config, c *client, task dispatcherapi.Task, log io.Writer,
	boundary killBoundary, runUser runUser) *execution {
	e := &execution{cfg: cfg, client: c, task: task, log: log, boundary: boundary, runUser: runUser,
		needsPerson: make(chan struct{}, 1), interrupts: make(chan struct{}, 1), uploads: newUploads(cfg, c, task, log), inbox: newInbox(),
		clock:   realClock{},
		results: make(chan struct{}, 1), followed: make(chan struct{}), finished: make(chan struct{}),
		preempted: make(chan struct{})}
	e.saver = newWIPSaver(e)
	e.marked, e.mark = context.WithCancel(ctx)
	e.preemptEnd = errNotSent
	return e
}

// killBoundary is the Execution's kill boundary, a *killboundary.Boundary.
// A test of the end procedure gives a fake.
type killBoundary interface {
	Start(cmd *exec.Cmd) error
	Populated() (bool, error)
	Kill() error
	Remove(within time.Duration) error
	// Holds tells if the process pid is in the boundary.
	Holds(pid int) (bool, error)
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
	// containerStep is what the end's container step did, for the end call.
	containerStep string
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
	// interrupts tells the follow loop that an interrupt command came
	// (interrupt.go).
	interrupts chan struct{}
	// inbox holds the messages of the Execution (messages.go).
	inbox   *inbox
	uploads *uploads
	// stdin is the harness's stdin: a pipe of the wrapper's own, on which
	// the interrupt sets a write deadline. writeStdin and writeStdinLocked
	// are the one path of a write to it. writing is the one lock of the
	// writes, so that two lines never mix and an interrupt never goes into
	// the middle of a message. It is not mu: a write blocks while the
	// harness does not read, and the end must not wait for it. No step of
	// the end and no marker waits for writing with no limit: the close of
	// stdin and the marker do not take it, and the interrupt sets the
	// write deadline before it takes it. stdinClosed is set before the
	// close.
	stdin       io.WriteCloser
	writing     sync.Mutex
	stdinClosed atomic.Bool
	harness     *exec.Cmd
	kill        *time.Timer
	killOnce    sync.Once
	// stopping is set when a stop command comes, before the end starts:
	// a SIGTERM from the Runner after it is not the machine's stop.
	stopping atomic.Bool
	// environ is the wrapper's environment, for the push of the
	// preemption handler's save.
	environ []string
	// followed closes when the output stream was read to its end and
	// the harness exited; followEnd is then what follow gave.
	followed  chan struct{}
	followEnd dispatcherapi.ExecutionEnd
	// results gets a signal for each result on the output stream after
	// the preempting marker.
	results chan struct{}
	// finishOnce runs the end procedure once; finished closes after it.
	finishOnce sync.Once
	finished   chan struct{}
	// killed is true after the cgroup kill.
	killed atomic.Bool
	// left is the error when the cgroup was not empty after the end.
	left error
	// checkout is the clone ("" before the clone), and workDir the
	// agent's working directory in it: the Project's folder.
	checkout, workDir string
	// startTip is the commit of the Run's branch after the clone (the
	// remote branch, or the base branch for a new one): a push is
	// necessary only when the branch moved. A restore after a lost
	// machine can move it to commits that were not pushed.
	startTip string
	// resumed is true when the task resumes the Agent session and the
	// restore put its transcript back: the harness gets --resume.
	resumed bool
	// lostNote is the first message's text after a lost machine
	// (restoreWork): what is back and what is not ("": no lost machine).
	lostNote string
	// restoring is true from the start of a restarted Execution's setup
	// (the task has a Restore) until its harness starts: no save and no
	// push to the WIP ref replaces the lost Execution's save, which the
	// WIP ref then keeps for the next Execution. Before the harness, the
	// checkout has no work of the agent's: a save of a clone, or of a
	// restore that did not end, would put a wrong save on the forge.
	restoring atomic.Bool
	// prMu holds one pr call at a time, and the push before the end until
	// a pr call's push ended. pushedTip is the commit that the last pr
	// call pushed.
	prMu      sync.Mutex
	pushedTip string
	// kept is what the Execution that this take-over follows left on the
	// machine (nil: this is the allocation's first Execution).
	kept *keptWork
	// setupDone is true when the setup ended with no error: only then does
	// a take-over that follows keep the checkout (execute).
	setupDone bool
	// stopTerminal ends the interactive form's terminal (nil: the
	// headless form, or no terminal yet). The end calls it, under mu.
	stopTerminal func()
	// coreEnv is the environment of the hc commands (the interactive
	// form): the run user's names and home, and no secret.
	coreEnv []string

	preemption
}

// interactive tells if the Execution is a take-over, in the interactive
// form.
func (e *execution) interactive() bool { return e.task.Form == dispatcherapi.FormInteractive }

// run sets up and runs the harness, then does the end procedure, and gives
// the end call's body.
func (e *execution) run(ctx context.Context, environ []string) dispatcherapi.ExecutionEnd {
	end := e.work(ctx, environ)
	e.endProcedure(context.Background())
	// The usual end takes the end before its push: a notice or a signal
	// after this starts no handler. When the preemption handler took it
	// first, the handler pushes and ends: this end call does not go.
	if !e.owner.CompareAndSwap(ownerNone, ownerEnd) {
		return end
	}
	// Before the end: each commit that is not pushed, and no secret file.
	// After the end call: no workspace and no home of the run user,
	// unless a take-over follows on this machine (execute).
	if failed := e.push(environ, ""); failed != "" {
		if end.Error != "" {
			end.Error += "; "
		}
		end.Error += failed
	}
	e.removeSecrets()
	// The output stream is read to its end now: the last
	// background_tasks_changed event counts, also when the harness wrote
	// it just before it exited.
	e.mu.Lock()
	if e.populatedAtKill {
		end.BackgroundTasksKilled = e.backgroundTasks
	}
	end.ContainerStep = e.containerStep
	e.mu.Unlock()
	return end
}

// endProcedure runs finish once, and waits for it until ctx ends. The
// usual end, the end after a stop command or a limit, and the preemption
// handler's halt share it.
func (e *execution) endProcedure(ctx context.Context) {
	e.finishOnce.Do(func() {
		go func() {
			defer close(e.finished)
			e.finish()
		}()
	})
	select {
	case <-e.finished:
	case <-ctx.Done():
	}
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
		defer e.killed.Store(true)
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
	step := e.containers()
	fmt.Fprintf(e.log, "wrapper: the end's container step: %s\n", step)
	e.mu.Lock()
	e.containerStep = step
	e.mu.Unlock()
}

// containers kills each running container of the machine, and says what
// it did.
func (e *execution) containers() string {
	if e.cfg.docker == "" {
		return "skipped: no Docker program (" + EnvDocker + " is not set)"
	}
	docker, err := exec.LookPath(e.cfg.docker)
	if err != nil {
		return "skipped: no Docker (" + e.cfg.docker + ") on the machine"
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
		return fmt.Sprintf("failed: docker ps: %v: %s", err, lastLines(out, 5))
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return "no container ran"
	}
	if out, err := run(append([]string{"kill"}, ids...)...); err != nil {
		return fmt.Sprintf("failed: docker kill: %v: %s", err, lastLines(out, 5))
	}
	return fmt.Sprintf("killed %d containers", len(ids))
}

// work sets up and runs the harness, and gives the end call's body.
func (e *execution) work(ctx context.Context, environ []string) dispatcherapi.ExecutionEnd {
	// With no account token the harness would use another login of the
	// machine, or none: the Execution ends before its setup.
	if e.task.AccountToken == "" {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: "the task has no account token"}
	}
	setup, harness := e.setup, e.runHarness
	if e.interactive() {
		setup, harness = e.setupTakeOver, e.runInteractive
	}
	if err := setup(ctx, environ); err != nil {
		// A stop or a limit during the setup stops the clone.
		if end, ok := e.endedEarly(); ok {
			return end
		}
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: err.Error()}
	}
	e.setupDone = true
	server, err := e.serveSocket(environ)
	if err != nil {
		return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndSetupFailed, Error: err.Error()}
	}
	e.uploads.start()
	// The checkpoint runs while the harness runs, and stops at the
	// preempting marker: the preemption handler then saves.
	checkpointing, stopCheckpoints := context.WithCancel(e.marked)
	go e.checkpoints(checkpointing, e.cfg.clock())
	end := harness(environ)
	stopCheckpoints()
	// The last bytes go before the end call: the end step revokes the
	// Execution's token. After the preempting marker, the preemption
	// handler sends them, so this upload stops at once. A stop command or
	// a limit does not stop it.
	e.uploads.finish(e.marked)
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

// EnvAccountToken is the name under which the harness (Claude Code) gets
// the token of the Execution's subscription account.
const EnvAccountToken = "CLAUDE_CODE_OAUTH_TOKEN"

// credentialNames are the names of Claude credentials that no process of
// the wrapper gets from the machine's environment. An API key would win
// over the account's token in Claude Code, and a Run never uses API
// pricing.
var credentialNames = []string{EnvAccountToken, "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

// harnessEnv is environ with no WRAPPER_ or NOMAD_ name, no agent's
// socket and no Claude credential, and the agent's socket at socket (""
// sets none). The names of the wrapper and of the Runner are not the
// agent's: NOMAD_TOKEN is a credential when a workload identity has env =
// true. runHarness adds the account's token for the harness only.
func harnessEnv(environ []string, socket string) []string {
	var env []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "WRAPPER_") || strings.HasPrefix(name, "NOMAD_") || name == agentsocket.Env ||
			slices.Contains(credentialNames, name) {
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
	// The run user connects (check and report); no other user does.
	if err := e.runUser.share(path, 0o660); err != nil {
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
	report = dispatcherapi.Report{Outcome: report.Outcome, Summary: report.Summary, Question: report.Question,
		CauseReport: report.CauseReport}
	a, ok := e.sendReport(w, r, &report)
	if !ok {
		return
	}
	// Before the answer, so that the agent's next stop finds the report;
	// not under mu, so that a lock on the file does not hold the end.
	// The count goes with the report: a message delivered after it makes
	// the Stop hook refuse (check 1).
	e.setRunState(func(s *runState) { s.Reported, s.ReportDelivered = true, report.DeliveredMessages })
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
func (e *execution) sendReport(w http.ResponseWriter, r *http.Request, report *dispatcherapi.Report) (answer, bool) {
	// The preempting marker stops a report in flight: the end started.
	ctx, stop := context.WithCancel(r.Context())
	defer stop()
	defer context.AfterFunc(e.marked, stop)()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isMarked() {
		writeProblem(w, http.StatusConflict, errMarked.Error()+": a report now does not count")
		return answer{}, false
	}
	if e.ending {
		writeProblem(w, http.StatusConflict, "the Execution is ending: a report now does not count")
		return answer{}, false
	}
	commit := e.addEvidence(report)
	a, err := e.client.call(ctx, "report", report)
	if err != nil && e.isMarked() {
		commit(false)
		writeProblem(w, http.StatusConflict, errMarked.Error()+": the report stopped")
		return answer{}, false
	}
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
	// The preempting marker stops a pr call in flight: the save of the
	// work pushes the branch.
	ctx, stop := context.WithCancel(r.Context())
	defer stop()
	defer context.AfterFunc(e.marked, stop)()
	e.prMu.Lock()
	defer e.prMu.Unlock()
	if e.isMarked() {
		writeProblem(w, http.StatusConflict, errMarked.Error()+": the wrapper opens no PR now")
		return
	}
	e.mu.Lock()
	ending := e.ending
	e.mu.Unlock()
	if ending {
		writeProblem(w, http.StatusConflict, "the Execution is ending: the wrapper pushes the Run's branch, and opens no PR now")
		return
	}
	if err := e.pushForPR(ctx, environ); err != nil {
		fmt.Fprintln(e.log, "wrapper: the push for a pr call:", err)
		if e.isMarked() {
			writeProblem(w, http.StatusConflict, errMarked.Error()+": the pr call stopped")
			return
		}
		writeProblem(w, http.StatusBadGateway, err.Error())
		return
	}
	if e.isMarked() {
		writeProblem(w, http.StatusConflict, errMarked.Error()+": the wrapper opens no PR now")
		return
	}
	a, err := e.client.callPR(ctx, text)
	if err != nil && e.isMarked() {
		writeProblem(w, http.StatusConflict, errMarked.Error()+": the pr call stopped")
		return
	}
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

// commandsNote starts the first message of each Agent session that an
// Execution starts (not of a resume, which has it): the factory's commands
// that the agent uses in each Run, from the CLI, a ticket, a schedule or a
// webhook.
const commandsNote = "You work in a SolidTrace factory Run. Use these commands:\n" +
	"- 'solidtrace check' runs the checks of the repository's factory file; 'solidtrace check COMMAND' runs " +
	"COMMAND as a check. Each check is Evidence of the Run.\n" +
	"- 'solidtrace pr --title TEXT --description TEXT' pushes the commits of the Run's branch and opens its PR; " +
	"a later call changes the title or the description. Commit your changes first. Open the PR only in this way.\n" +
	"- 'solidtrace report --outcome done|needs-a-person|failed --summary TEXT' reports the end of your work. " +
	"With needs-a-person, also give --question TEXT.\n\n" +
	"The task:\n\n"

// headless is the command line of the headless form of Claude Code.
func (e *execution) headless() []string {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--replay-user-messages", "--permission-mode", "bypassPermissions", "--disallowedTools", "AskUserQuestion",
		"--settings", e.cfg.harnessSettings()}
	if e.task.MCP != nil {
		args = append(args, "--mcp-config", e.cfg.mcpConfig())
	}
	if e.resumed {
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
	cmd.Env = append(append(append(e.gitEnv(environ, e.cfg.socket()), e.uploads.claudeEnv(e.cfg)...), e.mcpEnv()...),
		EnvRunState+"="+e.cfg.runState(),
		// The account's token is in the harness's environment only: not in
		// a file, and not in git's environment.
		EnvAccountToken+"="+e.task.AccountToken,
		// Last, so that the machine's environment cannot turn it on.
		EnvSubprocessScrub+"=0")
	e.runUser.start(cmd)
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
	// The start is under mu: a stop, a limit or the preempting marker
	// before it means no start, and one after it finds stdin. (The marker
	// does not take mu, but the halt reads stdin under mu after the
	// marker.)
	e.mu.Lock()
	if e.isMarked() {
		e.mu.Unlock()
		w.Close()
		in.Close()
		stdin.Close()
		return failed(errMarked)
	}
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
		// Under mu: the halt, which reads stdin under mu after the
		// marker, finds the harness and the save that follows saves.
		e.restoring.Store(false)
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
	taskUUID := e.task.MessageID
	if taskUUID == "" {
		taskUUID = newUUID()
	}
	// After a lost machine, the first message says what is back: in
	// place of the prompt when the Agent session resumes, else after it.
	text := e.task.Prompt
	if !e.resumed {
		text = commandsNote + text
	}
	if e.lostNote != "" {
		text = e.lostNote
		if !e.resumed {
			text = e.task.Prompt + "\n\n" + e.lostNote
		}
	}
	message, _ := json.Marshal(map[string]any{"type": "user", "uuid": taskUUID, "session_id": "",
		"parent_tool_use_id": nil, "message": map[string]any{"role": "user", "content": text}})
	// The write is not under mu: a harness that does not read its stdin
	// blocks it, and a stop, a limit or the preemption handler must still
	// end the Execution. An Execution that resumes the Agent session for its
	// messages does not write the prompt again: the first message is the
	// first of them, after the note of a lost machine.
	if _, ending := e.endedEarly(); !ending && (!e.resumed || len(e.task.Messages) == 0 || e.lostNote != "") {
		if err := e.writeTask(message); err != nil {
			fmt.Fprintln(e.log, "wrapper: write the task:", err)
		}
	}
	// The task's messages come after the prompt, then each message of the
	// Dispatcher's commands, in order.
	for _, m := range e.task.Messages {
		e.takeMessage(m, 0)
	}
	e.openInbox()
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
	// The end at a result (R3, R4): pending is the end reason of the last
	// result ("" is none: the agent works), giveBackAt the end of the wait
	// for a new turn that delivers a written message, and comparing tells
	// that a compare-and-end call is out.
	var pending string
	var giveBackAt time.Time
	var comparing bool
	compareDone := make(chan compared, 1)
	// interruptBy is the end of the wait for the result of the wrapper's
	// interrupt request (zero: no interrupt is out).
	var interruptBy time.Time
	// limit reads the stream for the usage limit of the account.
	var limit usageLimit
	tick := time.NewTicker(e.cfg.limitEvery)
	defer tick.Stop()
	tryEnd := func() {
		if pending == "" || comparing {
			return
		}
		n, seen := e.undelivered()
		if n > 0 {
			if giveBackAt.IsZero() {
				giveBackAt = e.cfg.clock().Add(giveBackWait)
			}
			return
		}
		giveBackAt = time.Time{}
		comparing = true
		go e.compareEnd(context.Background(), seen, compareDone)
	}
	// interruptDone is the end of an interrupt's wait (at its result, at
	// once when no turn runs, or after interruptWait): the messages that
	// waited are written, and the Execution goes on with them. With none,
	// the Execution ends with the reason interrupted.
	interruptDone := func(why string) {
		interruptBy = time.Time{}
		if e.releaseHeld() == 0 {
			e.startEnd(dispatcherapi.EndInterrupted, why)
			return
		}
		// A turn that does not start for the messages ends as at a
		// result with a message that was not delivered.
		tryEnd()
	}
	for open, exit := true, exited; open; {
		select {
		case <-e.interrupts:
			switch {
			case !e.inbox.holds():
				// A signal whose interrupt an earlier result settled:
				// that result released its message too.
			case pending != "":
				// The turn ended before the interrupt: no turn stops, and
				// no result comes.
				interruptDone("")
			case interruptBy.IsZero():
				interruptBy = e.cfg.clock().Add(interruptWait)
				e.writeInterrupt()
			}
		case <-tick.C:
			if !interruptBy.IsZero() && !e.cfg.clock().Before(interruptBy) {
				interruptDone(fmt.Sprintf("the harness gave no result in %s after the interrupt", interruptWait))
			}
			if !giveBackAt.IsZero() && !e.cfg.clock().Before(giveBackAt) && pending != "" {
				// No new turn delivered the written messages: the end call
				// gives them back, and a new Execution gets them.
				giveBackAt = time.Time{}
				e.startEnd(pending, "")
			}
		case c := <-compareDone:
			comparing = false
			switch {
			case c.err != nil:
				fmt.Fprintln(e.log, "wrapper: the compare-and-end call failed, so the Execution ends:", c.err)
				if pending != "" {
					e.startEnd(pending, "")
				}
			case c.answer.Agreed:
				// A turn that started since then ends with a result of its
				// own, and the wrapper asks again.
				if pending != "" {
					e.startEnd(pending, "")
				}
			default:
				for _, cmd := range c.answer.Commands {
					switch {
					case cmd.Kind == dispatcherapi.CommandInterrupt:
						e.takeInterrupt(cmd)
					case cmd.Message != nil:
						e.takeMessage(*cmd.Message, cmd.Seq)
					}
				}
				tryEnd()
			}
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
			case ev["type"] == "user" && ev["isSynthetic"] == true:
				// Hook feedback is not a message of the wrapper.
			case ev["type"] == "user" && ev["isReplay"] == true && ev["uuid"] == taskUUID:
				if delivered == nil {
					now := time.Now().UTC()
					delivered = &now
					e.countDelivered()
				}
			case ev["type"] == "user" && ev["isReplay"] == true:
				id, _ := ev["uuid"].(string)
				if e.echo(id) && pending != "" {
					// A new turn started for the message.
					pending, giveBackAt = "", time.Time{}
				}
			case ev["type"] == "system" && ev["subtype"] == "background_tasks_changed":
				tasks, _ := ev["tasks"].([]any)
				e.mu.Lock()
				e.backgroundTasks = len(tasks)
				e.mu.Unlock()
				e.setRunState(func(s *runState) { s.BackgroundTasks = len(tasks) })
			case ev["type"] == "system" && ev["subtype"] == "init" && offers(ev, "AskUserQuestion"):
				e.startEnd(dispatcherapi.EndSetupFailed, "the harness offers AskUserQuestion, which no person answers in a Run")
			case ev["type"] == "rate_limit_event" || (ev["type"] == "assistant" && ev["error"] != nil):
				limit.event(ev)
			case ev["type"] == "result" && e.isMarked():
				// The halt waits for it; the handler ends the Execution.
				select {
				case e.results <- struct{}{}:
				default:
				}
			case ev["type"] == "result" && limit.result(ev):
				// The account is at its usage limit: no turn can work on
				// it, so the Execution ends at once. The Dispatcher gives
				// the Run another free account. A report made before it
				// still sets the Run's outcome.
				e.setRunState(func(s *runState) { s.Turn++ })
				e.startEnd(dispatcherapi.EndUsageLimit, "")
			case ev["type"] == "result":
				// The turn ended: the next stop is in a new turn. This
				// comes before the end closes stdin.
				e.setRunState(func(s *runState) { s.Turn++ })
				e.mu.Lock()
				reported := e.reported
				e.mu.Unlock()
				pending = dispatcherapi.EndNoReport
				if reported {
					pending = dispatcherapi.EndReported
				}
				if !interruptBy.IsZero() {
					// The result of the wrapper's own interrupt
					// (error_during_execution) is not a failure.
					interruptDone("")
					break
				}
				tryEnd()
			}
		case <-e.needsPerson:
			// Needs a person ends the Execution at once, but not while a
			// message that the wrapper took is not delivered: the turn's
			// result then decides. After the preempting marker, the
			// preemption handler ends it.
			if n, _ := e.undelivered(); n == 0 && !e.isMarked() {
				e.startEnd(dispatcherapi.EndReported, "")
			}
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
	e.followEnd = dispatcherapi.ExecutionEnd{Reason: reason, Error: why, ExitCode: &code, TaskDeliveredAt: delivered,
		Messages: e.messageStates()}
	if reason == dispatcherapi.EndUsageLimit {
		e.followEnd.UsageLimitResetsAt = limit.resetsAt
	}
	close(e.followed)
	return e.followEnd
}

// startEnd starts the end once: it closes stdin, and kills the
// Execution's cgroup if the harness did not exit in endWait.
func (e *execution) startEnd(reason, why string) { e.beginEnd(reason, why, false) }

// beginEnd starts the end once, with reason, and kills the Execution's
// cgroup if the harness did not exit in endWait. With interrupt, it first
// writes the interrupt control request, so that the agent's turn stops
// (a long tool call too) before stdin closes. A stop command and a limit
// use it.
func (e *execution) beginEnd(reason, why string, interrupt bool) {
	e.beginEndWithin(reason, why, interrupt, endWait)
}

// beginEndWithin is beginEnd with wait in place of endWait. It is the one
// start of the end: the usual end, a stop command, a limit and the
// preemption handler's halt use it, and only the first counts. Before the
// harness starts, it stops the setup, and the harness does not start.
//
// The writes to stdin are not under mu, and wait at most writeWait: a
// harness that does not read its stdin does not hold the end.
func (e *execution) beginEndWithin(reason, why string, interrupt bool, wait time.Duration) {
	e.mu.Lock()
	if e.ending {
		e.mu.Unlock()
		return
	}
	e.ending, e.endReason, e.endError = true, reason, why
	if e.cancelWork != nil {
		e.cancelWork()
	}
	stdin, stopTerminal := e.stdin, e.stopTerminal
	if stdin != nil || stopTerminal != nil {
		e.kill = time.AfterFunc(wait, e.killAll)
	}
	e.mu.Unlock()
	if stopTerminal != nil {
		// The interactive form: the terminal's program ends, and the
		// kill after wait ends what is left.
		go stopTerminal()
		return
	}
	if stdin == nil {
		return
	}
	if interrupt {
		if err := e.interrupt(); err != nil {
			fmt.Fprintln(e.log, "wrapper: write the interrupt:", err)
		}
	}
	e.closeStdin()
}

// interruptRequest is Claude Code's interrupt control request.
func interruptRequest() []byte {
	b, _ := json.Marshal(map[string]any{"type": "control_request", "request_id": newUUID(),
		"request": map[string]any{"subtype": "interrupt"}})
	return b
}

// interrupt writes the interrupt control request. It is not refused after
// the preempting marker. It first sets a write deadline of writeWait on
// stdin: the deadline also ends a message write that blocks and holds
// writing, so the interrupt waits at most writeWait.
func (e *execution) interrupt() error {
	if d, ok := e.stdin.(interface{ SetWriteDeadline(time.Time) error }); ok {
		if err := d.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			return err
		}
	}
	return e.writeStdin(interruptRequest(), nil)
}

// writeTask writes the task's user message to the harness's stdin. After
// the preempting marker, it is not written.
func (e *execution) writeTask(line []byte) error { return e.writeStdin(line, e.isMarked) }

// writeStdin is the one path of a write to stdin: one line, under
// writing, unless stdin is closed or refuse says no. It asks before and
// after the wait for writing, so that a write that waits behind a write
// that blocks is refused at once after the marker. A write whose check
// came before the marker can end after it; no write starts after a
// refusal.
func (e *execution) writeStdin(line []byte, refuse func() bool) error {
	if err := e.mayWrite(refuse); err != nil {
		return err
	}
	e.writing.Lock()
	defer e.writing.Unlock()
	return e.writeStdinLocked(line, refuse)
}

// writeStdinLocked is writeStdin for a caller that holds writing.
func (e *execution) writeStdinLocked(line []byte, refuse func() bool) error {
	if err := e.mayWrite(refuse); err != nil {
		return err
	}
	_, err := e.stdin.Write(append(line, '\n'))
	return err
}

func (e *execution) mayWrite(refuse func() bool) error {
	switch {
	case e.stdinClosed.Load():
		return errors.New("stdin is closed")
	case refuse != nil && refuse():
		return errMarked
	}
	return nil
}

// closeStdin closes stdin once, with no wait for a write: the close stops
// a write that blocks.
func (e *execution) closeStdin() {
	if e.stdinClosed.CompareAndSwap(false, true) {
		e.stdin.Close()
	}
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
