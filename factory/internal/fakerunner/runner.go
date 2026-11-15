// Package fakerunner is the fake Runner of Seam 3. In place of Nomad, it
// starts the wrapper of each Execution as a local process, reports the
// Execution's state changes and its end, stops it, and loses the machine
// when a test asks.
package fakerunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// State is the state of an Execution on the Runner.
type State string

const (
	Pending  State = "pending"
	Running  State = "running"
	Complete State = "complete" // the wrapper exited with code 0
	Failed   State = "failed"   // the wrapper exited with another code, or did not start
	Lost     State = "lost"     // the machine went with no notice
)

// Ended tells if the state is an end.
func (s State) Ended() bool { return s == Complete || s == Failed || s == Lost }

// Execution is what the Dispatcher gives the Runner to start.
type Execution struct {
	ID  string
	Env []string // added to the Runner's environment for the wrapper
}

// Event is one state change of an Execution.
type Event struct {
	Execution string
	State     State
	ExitCode  int  // when the state is Complete or Failed
	Stopped   bool // the end came from Stop
	Err       string
	At        time.Time
}

// Options configure the Runner.
type Options struct {
	// Command is the wrapper program and its arguments. Required.
	Command []string
	// Env is the base environment of each wrapper (default os.Environ()).
	Env []string
	// Dir is where each Execution gets its own working directory and its
	// output file (default a temp directory that Close removes).
	Dir string
	// KillTimeout is the time from SIGTERM to SIGKILL on Stop (default 5 s).
	KillTimeout time.Duration
}

// MarkEnv is the environment variable that marks each process of an
// Execution, as a cgroup marks the processes of a Nomad task. A process
// keeps it when it goes to a process group or a session of its own.
const MarkEnv = "FAKERUNNER_EXECUTION"

// Runner runs Executions as local processes.
type Runner struct {
	opts    Options
	tempDir string

	mu      sync.Mutex
	changed *sync.Cond
	events  []Event
	procs   map[string]*proc
}

type proc struct {
	mark    string // the value of MarkEnv for this Execution
	cmd     *exec.Cmd
	state   State
	stopped bool
	lost    bool
	done    chan struct{}
}

// New makes a Runner.
func New(opts Options) (*Runner, error) {
	if len(opts.Command) == 0 {
		return nil, errors.New("fakerunner: Options.Command is required")
	}
	r := &Runner{opts: opts, procs: map[string]*proc{}}
	r.changed = sync.NewCond(&r.mu)
	if r.opts.Env == nil {
		r.opts.Env = os.Environ()
	}
	if r.opts.KillTimeout == 0 {
		r.opts.KillTimeout = 5 * time.Second
	}
	if r.opts.Dir == "" {
		d, err := os.MkdirTemp("", "fakerunner-")
		if err != nil {
			return nil, err
		}
		r.opts.Dir, r.tempDir = d, d
	}
	return r, nil
}

// Dispatch starts the Execution's wrapper. The Execution is pending, then
// running; its end comes as an event.
func (r *Runner) Dispatch(ctx context.Context, e Execution) error {
	r.mu.Lock()
	if _, ok := r.procs[e.ID]; ok || e.ID == "" {
		r.mu.Unlock()
		return fmt.Errorf("fakerunner: Execution %q is already dispatched or has no id", e.ID)
	}
	dir := filepath.Join(r.opts.Dir, e.ID)
	p := &proc{mark: dir, done: make(chan struct{})}
	r.procs[e.ID] = p
	r.record(e.ID, Event{State: Pending})
	r.mu.Unlock()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return r.failStart(e.ID, p, err)
	}
	out, err := os.Create(filepath.Join(dir, "output.log"))
	if err != nil {
		return r.failStart(e.ID, p, err)
	}
	cmd := exec.Command(r.opts.Command[0], r.opts.Command[1:]...)
	cmd.Dir = dir
	cmd.Env = append(append(append([]string{}, r.opts.Env...), e.Env...), MarkEnv+"="+p.mark)
	cmd.Stdout, cmd.Stderr = out, out
	// A process group of its own, and the mark, so that a kill takes each
	// process of the Execution.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		out.Close()
		return r.failStart(e.ID, p, err)
	}
	r.mu.Lock()
	p.cmd = cmd
	r.record(e.ID, Event{State: Running})
	r.mu.Unlock()
	go r.wait(e.ID, p, out)
	return nil
}

func (r *Runner) failStart(id string, p *proc, err error) error {
	r.mu.Lock()
	r.record(id, Event{State: Failed, ExitCode: -1, Err: err.Error()})
	r.mu.Unlock()
	close(p.done)
	return fmt.Errorf("fakerunner: start Execution %s: %w", id, err)
}

func (r *Runner) wait(id string, p *proc, out *os.File) {
	err := p.cmd.Wait()
	out.Close()
	// As Nomad does when a task ends, kill the processes that are left.
	killAll(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	defer close(p.done)
	if p.lost {
		r.record(id, Event{State: Lost})
		return
	}
	ev := Event{State: Complete, Stopped: p.stopped, ExitCode: p.cmd.ProcessState.ExitCode()}
	if err != nil {
		ev.State = Failed
		if ev.ExitCode < 0 {
			ev.Err = err.Error()
		}
	}
	r.record(id, ev)
}

// record keeps an event. The caller holds r.mu.
func (r *Runner) record(id string, ev Event) {
	ev.Execution, ev.At = id, time.Now()
	if p := r.procs[id]; p != nil {
		p.state = ev.State
	}
	r.events = append(r.events, ev)
	r.changed.Broadcast()
}

// Stop asks the wrapper to stop (SIGTERM to the wrapper only, so that its
// child processes still run while it stops them), and kills each process
// of the Execution after the kill timeout, as Nomad stops a task. It
// returns when the Execution ended.
func (r *Runner) Stop(ctx context.Context, id string) error {
	r.mu.Lock()
	p, ok := r.procs[id]
	if ok && p.cmd != nil && !p.state.Ended() {
		p.stopped = true
		p.cmd.Process.Signal(syscall.SIGTERM)
	}
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("fakerunner: no Execution %q", id)
	}
	select {
	case <-p.done:
	case <-time.After(r.opts.KillTimeout):
		killAll(p)
		<-p.done
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Signal sends sig to the Execution's wrapper only, as the machine's
// shutdown does before the machine goes. Nothing is killed after it.
func (r *Runner) Signal(id string, sig os.Signal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[id]
	if !ok || p.cmd == nil || p.state.Ended() {
		return fmt.Errorf("fakerunner: no running Execution %q", id)
	}
	return p.cmd.Process.Signal(sig)
}

// LoseMachine takes the Execution's machine away with no notice: each of
// its processes is killed at once, and the Execution is lost.
func (r *Runner) LoseMachine(id string) error {
	r.mu.Lock()
	p, ok := r.procs[id]
	if !ok || p.cmd == nil || p.state.Ended() {
		r.mu.Unlock()
		return fmt.Errorf("fakerunner: no running Execution %q", id)
	}
	p.lost = true
	r.mu.Unlock()
	killAll(p)
	<-p.done
	return nil
}

// State gives the Execution's last state, and false when the Runner does
// not know it.
func (r *Runner) State(id string) (State, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[id]
	if !ok {
		return "", false
	}
	return p.state, true
}

// Events gives the events from number `from` on, waiting until there is
// at least one or ctx ends. The Dispatcher's watch loop calls it with the
// count that it has seen.
func (r *Runner) Events(ctx context.Context, from int) ([]Event, error) {
	stop := context.AfterFunc(ctx, func() {
		r.mu.Lock()
		r.changed.Broadcast()
		r.mu.Unlock()
	})
	defer stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.events) <= from {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.changed.Wait()
	}
	return append([]Event{}, r.events[from:]...), nil
}

// Wait gives the Execution's end event.
func (r *Runner) Wait(ctx context.Context, id string) (Event, error) {
	for seen := 0; ; {
		evs, err := r.Events(ctx, seen)
		if err != nil {
			return Event{}, err
		}
		seen += len(evs)
		for _, ev := range evs {
			if ev.Execution == id && ev.State.Ended() {
				return ev, nil
			}
		}
	}
}

// Output gives what the Execution's wrapper wrote to stdout and stderr.
func (r *Runner) Output(id string) string {
	b, _ := os.ReadFile(filepath.Join(r.opts.Dir, id, "output.log"))
	return string(b)
}

// Close kills each process that still runs, and removes the temp directory.
func (r *Runner) Close() {
	r.mu.Lock()
	var running []*proc
	for _, p := range r.procs {
		if p.cmd != nil && !p.state.Ended() {
			running = append(running, p)
		}
	}
	r.mu.Unlock()
	for _, p := range running {
		killAll(p)
		<-p.done
	}
	if r.tempDir != "" {
		os.RemoveAll(r.tempDir)
	}
}

// killAll sends SIGKILL to the wrapper's process group and to each process
// that has the Execution's mark, until no marked process is left.
func killAll(p *proc) {
	syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	mark := []byte(MarkEnv + "=" + p.mark + "\x00")
	for pass := 0; pass < 100; pass++ {
		found := false
		for _, pid := range markedPIDs(mark) {
			syscall.Kill(pid, syscall.SIGKILL)
			found = true
		}
		if !found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// markedPIDs gives each process whose environment holds the mark. An
// ended process that is not yet reaped has no environment.
func markedPIDs(mark []byte) []int {
	entries, _ := os.ReadDir("/proc")
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		env, err := os.ReadFile("/proc/" + e.Name() + "/environ")
		if err != nil {
			continue
		}
		if strings.HasPrefix(string(env), string(mark)) || strings.Contains(string(env), "\x00"+string(mark)) {
			pids = append(pids, pid)
		}
	}
	return pids
}
