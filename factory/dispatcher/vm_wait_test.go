package dispatcher_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/factorytest"
)

// noVMRunner is a Runner on a cluster at its maximum: it takes each
// Execution, and gives no machine to it until a test says so.
type noVMRunner struct {
	mu     sync.Mutex
	events []dispatcher.RunnerEvent
}

func (r *noVMRunner) Dispatch(_ context.Context, x dispatcher.RunnerExecution) (string, error) {
	return "no-vm:" + x.Execution, nil
}

func (r *noVMRunner) Workload(context.Context, dispatcher.Identity) (string, error) {
	return "", dispatcher.ErrUnknownWorkload
}

func (r *noVMRunner) Events(ctx context.Context, from int) ([]dispatcher.RunnerEvent, error) {
	for {
		r.mu.Lock()
		if len(r.events) > from {
			events := append([]dispatcher.RunnerEvent(nil), r.events[from:]...)
			r.mu.Unlock()
			return events, nil
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (r *noVMRunner) Stop(context.Context, string) error { return nil }
func (r *noVMRunner) JWKSURL() string                    { return "" }

// place says that Nomad placed the Execution on a machine.
func (r *noVMRunner) place(execution string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, dispatcher.RunnerEvent{Execution: execution, State: dispatcher.RunnerPending})
}

// vmWaitEnv is an Environment with a Run whose Execution waits for a VM.
func vmWaitEnv(t *testing.T) (*factorytest.Env, *noVMRunner, string, string) {
	t.Helper()
	runner := &noVMRunner{}
	env := factorytest.New(t, factorytest.WithRunner(runner))
	env.Project("checkout")
	ana := env.Member("Ana")
	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")
	var run dispatcherapi.Run
	deadline := time.Now().Add(10 * time.Second)
	for {
		ana.API("GET", "/api/v1/runs/"+id, nil).JSON(t, &run)
		if len(run.Executions) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Run has no Execution after 10 s: %+v\n%s", run, env.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
	return env, runner, id, run.Executions[0].ID
}

// vmWaitWarnings counts the warning Logs about a wait for a VM.
func vmWaitWarnings(env *factorytest.Env) []map[string]any {
	var found []map[string]any
	for _, ev := range env.SolidTrace.RunEvents() {
		if message, _ := ev["message"].(string); ev["level"] == "warn" && strings.Contains(message, "for a VM") {
			found = append(found, ev)
		}
	}
	return found
}

// loopLooks gives the dispatch loop (50 ms in the tests) time to look.
func loopLooks() { time.Sleep(300 * time.Millisecond) }

func TestARunThatWaitsMoreThanTenMinutesForAVMGivesOneWarningLog(t *testing.T) {
	env, _, id, execution := vmWaitEnv(t)

	env.Clock.Advance(10*time.Minute - time.Second)
	loopLooks()
	if got := vmWaitWarnings(env); len(got) != 0 {
		t.Fatalf("a warning after 9 min 59 s: %v", got)
	}
	env.Clock.Advance(2 * time.Second)
	warning := env.WaitForWarning("for a VM")
	env.Clock.Advance(time.Hour)
	loopLooks()

	if warning["level"] != "warn" || warning["factory.run"] != id || warning["factory.execution"] != execution {
		t.Errorf("the warning is %v, want a warn Log that names Run %s and Execution %s", warning, id, execution)
	}
	if got := vmWaitWarnings(env); len(got) != 1 {
		t.Errorf("%d warnings after an hour of waiting, want one: %v", len(got), got)
	}
}

func TestARunThatWaitsForAVMIsQueuedWithThatReason(t *testing.T) {
	env, _, id, _ := vmWaitEnv(t)

	var run dispatcherapi.Run
	env.Member("Ana").API("GET", "/api/v1/runs/"+id, nil).JSON(t, &run)

	if run.State != dispatcherapi.StateQueued || !strings.Contains(run.Reason, "waiting for a VM") {
		t.Errorf("the Run is %s with reason %q, want queued with a reason that says waiting for a VM", run.State, run.Reason)
	}
}

func TestARunThatGotAMachineBeforeTenMinutesGivesNoWarningLog(t *testing.T) {
	env, runner, _, execution := vmWaitEnv(t)
	env.Clock.Advance(9 * time.Minute)
	runner.place(execution)
	loopLooks()

	env.Clock.Advance(time.Hour)
	loopLooks()

	if got := vmWaitWarnings(env); len(got) != 0 {
		t.Errorf("a warning for an Execution that Nomad placed: %v", got)
	}
}

func TestTheWarningLogComesOnceAlsoWhenTheDispatcherStartsAgain(t *testing.T) {
	env, _, _, _ := vmWaitEnv(t)
	env.Clock.Advance(11 * time.Minute)
	env.WaitForWarning("for a VM")

	env.Restart()
	env.Clock.Advance(time.Minute)
	loopLooks()

	if got := vmWaitWarnings(env); len(got) != 1 {
		t.Errorf("%d warnings after a restart, want one: %v", len(got), got)
	}
}

func TestTheRunEventOfADispatchedExecutionCarriesTheReasonForTheWait(t *testing.T) {
	env, _, id, _ := vmWaitEnv(t)

	events := env.WaitForRunEvents(id, 3)
	last := events[len(events)-1]

	reason, _ := last["run.reason"].(string)
	if last["run.state"] != dispatcherapi.StateQueued || !strings.Contains(reason, "waiting for a VM") {
		t.Errorf("the last run event has state %v and reason %q, want queued with a reason that says waiting for a VM", last["run.state"], reason)
	}
}
