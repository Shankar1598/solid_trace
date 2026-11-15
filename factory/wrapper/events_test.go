package wrapper_test

import (
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
	events := env.WaitForRunEvents(id, 5)
	want := []string{"created", "queued", "execution_started", "report", "execution_ended"}
	if got := kinds(events); len(got) != len(want) {
		t.Fatalf("event kinds %v, want %v", got, want)
	}
	for i, kind := range want {
		if events[i]["event.kind"] != kind || events[i]["run.seq"] != float64(i+1) {
			t.Errorf("event %d: %v %v, want %s with run.seq %d", i, events[i]["event.kind"], events[i]["run.seq"], kind, i+1)
		}
	}
	execution := onlyExecution(t, run).ID
	common := map[string]any{"run.id": id, "factory.project": "shop", "repository": "bitbucket.org/example/shop",
		"run.title": "Fix the cart", "run.trigger": "person", "actor.kind": "person", "actor.name": ana.Answer.Username}
	for _, ev := range events {
		checkFields(t, ev, common)
	}
	checkFields(t, events[2], map[string]any{"run.state": "running", "run.executions": float64(1), "execution.id": execution})
	checkFields(t, events[3], map[string]any{"run.state": "running", "execution.id": execution,
		"report.outcome": "done", "report.summary": "Fixed the cart"})
	checkFields(t, events[4], map[string]any{"run.state": "done", "run.reason": "", "run.executions": float64(1),
		"execution.id": execution, "execution.reason": dispatcherapi.EndReported, "execution.exit_code": float64(0)})
	for _, ev := range events[2:] {
		if s, _ := ev["agent.session_id"].(string); s == "" {
			t.Errorf("%s event has no agent.session_id", ev["event.kind"])
		}
	}
}

func TestALostMachineSendsTheExecutionEndedEventWithTheFailedState(t *testing.T) {
	env, ana := newEnv(t)
	wait, _ := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	env.WaitForRunEvents(id, 3)

	if err := env.Runner.Fake.LoseMachine(run.Executions[0].ID); err != nil {
		t.Fatal(err)
	}

	waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	events := env.WaitForRunEvents(id, 4)
	checkFields(t, events[3], map[string]any{"event.kind": "execution_ended", "run.seq": float64(4),
		"run.state": "failed", "run.reason": "the machine was lost", "execution.reason": "the machine was lost",
		"execution.id": run.Executions[0].ID})
	if warning := env.WaitForWarning("no end call"); warning["execution.id"] != run.Executions[0].ID {
		t.Errorf("the warning: %v", warning)
	}
}
