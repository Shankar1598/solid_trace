//go:build factorystack

package nomadrunner_test

// The Runner's tests against the Nomad of the local factory stack (Seam 3
// with the stack's Nomad agent). They do not skip: with no stack, they
// fail. Run them with factory/local/test-runner, which starts a stack of
// its own, runs them, and stops it.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/nomadrunner"
)

// stack is the local factory stack that test-runner started.
type stack struct {
	addr, token string
	// machine is the agent machine's container.
	machine string
	api     *api.Client
}

func theStack(t *testing.T) *stack {
	t.Helper()
	s := &stack{addr: os.Getenv("NOMAD_ADDR"), token: os.Getenv("NOMAD_TOKEN"), machine: os.Getenv("AGENT_MACHINE")}
	if s.addr == "" || s.token == "" || s.machine == "" {
		t.Fatal("NOMAD_ADDR, NOMAD_TOKEN and AGENT_MACHINE are not set: run these tests with factory/local/test-runner")
	}
	config := api.DefaultConfig()
	config.Address, config.SecretID = s.addr, s.token
	client, err := api.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	s.api = client
	return s
}

func random(t *testing.T) string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// options change the Runner's configuration in a test.
type options func(*nomadrunner.Config)

// runner makes a Runner whose task runs script with /bin/sh in place of
// the wrapper, with jobs of their own. The kinds are default (the agent
// machine's node pool, 64 MB), unless an option changes them. The
// cleanup purges the Runner's jobs.
func (s *stack) runner(t *testing.T, script string, opts ...options) *nomadrunner.Runner {
	t.Helper()
	cfg := nomadrunner.Config{
		Address:    s.addr,
		Token:      s.token,
		JobPrefix:  "test-" + random(t),
		Kinds:      []dispatcher.MachineKind{{Name: "default", MemoryMB: 64}},
		Command:    "/bin/sh",
		Args:       []string{"-c", script},
		WrapperURL: "http://dispatcher:7600",
	}
	for _, o := range opts {
		o(&cfg)
	}
	r, err := nomadrunner.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.Close()
		jobs, _, _ := s.api.Jobs().PrefixList(cfg.JobPrefix + "-")
		for _, j := range jobs {
			s.api.Jobs().Deregister(j.ID, true, nil)
		}
	})
	return r
}

// events reads a Runner's events for one Execution.
type events struct {
	t    *testing.T
	r    *nomadrunner.Runner
	seen int
}

// next gives the next event of execution, within timeout.
func (e *events) next(execution string, timeout time.Duration) dispatcher.RunnerEvent {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		evs, err := e.r.Events(ctx, e.seen)
		if err != nil {
			e.t.Fatalf("no event of Execution %s in %s: %v", execution, timeout, err)
		}
		for i, ev := range evs {
			if ev.Execution == execution {
				e.seen += i + 1
				return ev
			}
		}
		e.seen += len(evs)
	}
}

// until gives the first event of execution in state, within timeout.
func (e *events) until(execution, state string, timeout time.Duration) dispatcher.RunnerEvent {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ev := e.next(execution, time.Until(deadline))
		if ev.State == state {
			return ev
		}
		if ev.Ended {
			e.t.Fatalf("Execution %s ended %s (exit %d), not %s", execution, ev.State, ev.ExitCode, state)
		}
	}
}

// none checks that execution has no event in d.
func (e *events) none(execution string, d time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	for {
		evs, err := e.r.Events(ctx, e.seen)
		if err != nil {
			return
		}
		for _, ev := range evs {
			if ev.Execution == execution {
				e.t.Fatalf("Execution %s has an event %+v, want none", execution, ev)
			}
		}
		e.seen += len(evs)
	}
}

func dispatch(t *testing.T, r *nomadrunner.Runner, run, execution, kind string) string {
	t.Helper()
	workload, err := r.Dispatch(context.Background(), dispatcher.RunnerExecution{Run: run, Execution: execution, MachineKind: kind})
	if err != nil {
		t.Fatalf("dispatch %s: %v", execution, err)
	}
	return workload
}

// allocs are the allocations of a dispatched job.
func (s *stack) allocs(t *testing.T, job string) []*api.AllocationListStub {
	t.Helper()
	allocs, _, err := s.api.Jobs().Allocations(job, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	return allocs
}

// alloc is the one allocation of a dispatched job.
func (s *stack) alloc(t *testing.T, job string) *api.AllocationListStub {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if allocs := s.allocs(t, job); len(allocs) == 1 {
			return allocs[0]
		} else if len(allocs) > 1 {
			t.Fatalf("the job %s has %d allocations, want 1", job, len(allocs))
		}
	}
	t.Fatalf("the job %s has no allocation after 30 s", job)
	return nil
}

// stdout is what the task wrote to stdout so far.
func (s *stack) stdout(t *testing.T, alloc string) string {
	t.Helper()
	a, _, err := s.api.Allocations().Info(alloc, nil)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := s.api.AllocFS().Cat(a, "alloc/logs/"+nomadrunner.TaskName+".stdout.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var b strings.Builder
	buf := make([]byte, 64<<10)
	for {
		n, err := rc.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String()
		}
	}
}

// waitForStdout waits until the task's stdout has want.
func (s *stack) waitForStdout(t *testing.T, alloc, want string) string {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		if out := s.stdout(t, alloc); strings.Contains(out, want) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("the task's stdout has no %q after 30 s: %q", want, s.stdout(t, alloc))
		}
	}
}

// job reads a dispatched job.
func (s *stack) job(t *testing.T, id string) *api.Job {
	t.Helper()
	job, _, err := s.api.Jobs().Info(id, nil)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

// pause stops each process of the agent machine (docker pause), so that
// its Nomad client misses its heartbeats. The cleanup continues it, and
// waits until its node is ready again.
func (s *stack) pause(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("docker", "pause", s.machine).CombinedOutput(); err != nil {
		t.Fatalf("docker pause: %v %s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("docker", "unpause", s.machine).Run()
		for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(time.Second) {
			nodes, _, err := s.api.Nodes().List(nil)
			if err == nil && len(nodes) > 0 && nodes[0].Status == api.NodeStatusReady {
				return
			}
		}
		t.Error("the agent machine's node is not ready 60 s after docker unpause")
	})
}
