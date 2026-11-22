package wrapper_test

import (
	"slices"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// kinds are the event kinds of events, in order.
func kinds(events []map[string]any) []string {
	var got []string
	for _, ev := range events {
		got = append(got, ev["event.kind"].(string))
	}
	return got
}

// placedReason is the reason of the Dispatcher's queued event when the
// Runner places an Execution before its wrapper's task call.
const placedReason = "starting on a machine: the Runner placed the Execution, and the wrapper has not started it yet"

// withoutPlaced are the events with no placement event. The fake Runner
// places each Execution as it starts its wrapper, so the placement event
// comes before the task call nearly always, but not always.
func withoutPlaced(events []map[string]any) []map[string]any {
	return slices.DeleteFunc(slices.Clone(events), func(ev map[string]any) bool {
		return ev["event.kind"] == "queued" && ev["run.reason"] == placedReason
	})
}

func checkFields(t *testing.T, ev map[string]any, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if ev[k] != v {
			t.Errorf("%s event: %s = %#v, want %#v", ev["event.kind"], k, ev[k], v)
		}
	}
}

func TestARunIsDoneWhileEventStoreIsDownAndItsEventsArriveInOrderWhenItIsBack(t *testing.T) {
	env, ana := newEnv(t)
	env.SolidTrace.EventStoreDown(true)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed the cart"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if got := env.SolidTrace.RunEvents(); len(got) != 0 {
		t.Fatalf("EventStore is down, but it has %d events", len(got))
	}

	env.SolidTrace.EventStoreDown(false)
	// The second queued event is the Dispatcher's: it gives the Run the reason
	// for the wait for a VM. A third queued event, of the placement, gives
	// the reason that it starts.
	var all []map[string]any
	waitUntil(t, "the execution_ended event", func() bool {
		all = env.RunEvents(id)
		return len(all) > 0 && all[len(all)-1]["event.kind"] == "execution_ended"
	})
	var seqs []int
	for i, ev := range all {
		if ev["run.seq"] != float64(i+1) {
			t.Errorf("event %d: %v has run.seq %v, want %d", i, ev["event.kind"], ev["run.seq"], i+1)
		}
		seqs = append(seqs, i+1)
	}
	if got := env.ArrivalSeqs(id); !slices.Equal(got, seqs) {
		t.Errorf("the events arrived with run.seq %v, want %v in order", got, seqs)
	}
	execution := onlyExecution(t, run).ID
	if len(all) == 7 {
		checkFields(t, all[3], map[string]any{"event.kind": "queued", "actor.kind": "dispatcher",
			"execution.id": execution, "run.reason": placedReason})
	}
	events := withoutPlaced(all)
	want := []string{"created", "queued", "queued", "execution_started", "report", "execution_ended"}
	if got := kinds(events); !slices.Equal(got, want) {
		t.Fatalf("event kinds %v, want %v and at most one placement event", kinds(all), want)
	}
	common := map[string]any{"run.id": id, "factory.project": "shop", "repository": "bitbucket.org/example/shop",
		"run.title": "Fix the cart", "run.trigger": "person"}
	person := map[string]any{"actor.kind": "person", "actor.name": ana.Answer.Username}
	for i, ev := range events {
		checkFields(t, ev, common)
		if i != 2 {
			checkFields(t, ev, person)
		}
	}
	checkFields(t, events[2], map[string]any{"actor.kind": "dispatcher", "run.state": "queued", "execution.id": execution,
		"run.reason": "waiting for a VM: the Runner has not placed the Execution on a machine yet"})
	checkFields(t, events[3], map[string]any{"run.state": "running", "run.executions": float64(1), "execution.id": execution})
	checkFields(t, events[4], map[string]any{"run.state": "running", "execution.id": execution,
		"report.outcome": "done", "report.summary": "Fixed the cart"})
	checkFields(t, events[5], map[string]any{"run.state": "done", "run.reason": "", "run.executions": float64(1),
		"execution.id": execution, "execution.reason": dispatcherapi.EndReported, "execution.exit_code": float64(0)})
	for _, ev := range events[3:] {
		if s, _ := ev["agent.session_id"].(string); s == "" {
			t.Errorf("%s event has no agent.session_id", ev["event.kind"])
		}
	}
}

func TestALostMachineSendsTheMachineLostEventAndRestartsTheRun(t *testing.T) {
	env, ana := newEnv(t)
	wait, _ := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	env.WaitForRunEvents(id, 4)

	env.Runner.HoldMachines()
	if err := env.Runner.Fake.LoseMachine(run.Executions[0].ID); err != nil {
		t.Fatal(err)
	}

	waitForRestart(t, env, ana, id)
	var events []map[string]any
	waitUntil(t, "the execution_ended event", func() bool {
		events = withoutPlaced(env.RunEvents(id))
		return len(events) >= 6
	})
	restarted := "machine lost: a new Execution continues the Run (1 of 3 lost machines in a row)"
	lost, ended := eventOf(t, events, "machine_lost"), eventOf(t, events, "execution_ended")
	checkFields(t, lost, map[string]any{"run.state": "queued", "run.reason": restarted,
		"machine_lost.reason": "machine lost", "machine_lost.in_a_row": float64(1), "machine_lost.restart": true,
		"execution.id": run.Executions[0].ID})
	checkFields(t, ended, map[string]any{"run.state": "queued", "run.reason": restarted,
		"execution.reason": "the machine was lost", "execution.id": run.Executions[0].ID})
	if seq, _ := lost["run.seq"].(float64); ended["run.seq"] != seq+1 {
		t.Errorf("machine_lost has run.seq %v and execution_ended %v: want the end right after", seq, ended["run.seq"])
	}
	if warning := env.WaitForWarning("no end call"); warning["execution.id"] != run.Executions[0].ID {
		t.Errorf("the warning: %v", warning)
	}
}

func TestAWrapperThatExitsBeforeItsEndCallSendsItsExitCodeOnTheExecutionEndedEvent(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.BreakWrapper()
	id := start(t, env, ana, "Fix the cart")

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	// The wrapper did not ask for its task, so the Execution never started.
	var events []map[string]any
	waitUntil(t, "the execution_ended event", func() bool {
		events = withoutPlaced(env.RunEvents(id))
		return len(events) >= 4
	})
	reason := "the wrapper exited with code 2 before the end of the Execution"
	checkFields(t, events[3], map[string]any{"event.kind": "execution_ended", "run.state": "failed",
		"run.reason": reason, "execution.reason": reason, "execution.exit_code": float64(2),
		"execution.id": run.Executions[0].ID})
	if e := onlyExecution(t, run); e.ExitCode == nil || *e.ExitCode != 2 {
		t.Errorf("the Execution has exit code %v, want 2", e.ExitCode)
	}
}
