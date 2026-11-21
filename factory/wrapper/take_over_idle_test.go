package wrapper_test

import (
	"regexp"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// The take-over of a Run with no live Execution (ticket 62): a Run that
// waits on a person, is done, failed or stopped is queued, then taken
// over in a new Execution in the interactive form.

var takeOverQueuedLine = regexp.MustCompile(`(?m)^Run (\S+) is queued for the take-over: a new Execution ` +
	`continues its Agent session in a terminal\.$`)

// takeOverIdleCLI runs 'solidtrace take-over' as p for a Run with no live
// Execution, and checks that it says that the Run is queued.
func takeOverIdleCLI(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string) {
	t.Helper()
	got := p.CLI("take-over", "--dispatcher", env.URL(), id)
	m := takeOverQueuedLine.FindStringSubmatch(got.Stdout)
	if got.Code != 0 || m == nil || m[1] != id {
		t.Fatalf("solidtrace take-over: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
}

func TestATakeOverOfADoneRunQueuesItThenContinuesTheAgentSessionInATerminal(t *testing.T) {
	env, ana := newEnv(t)
	ben := env.Member("Ben")
	env.Runner.Agent(takeTask, initLine(t), done("Fixed."), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	onlyExecution(t, waitFor(t, env, ana, id, dispatcherapi.StateDone))

	takeOverIdleCLI(t, env, ben, id)

	// The take-over event names the person, at once: the Run is queued.
	ev := waitForEvent(t, env, id, "take_over")
	if ev["actor.name"] != "ben@example.com" || ev["actor.kind"] != "person" || ev["run.state"] != dispatcherapi.StateQueued ||
		ev["run.continued_from"] != dispatcherapi.StateDone {
		t.Errorf("the take-over event: %v", ev)
	}
	run, terminal := takeOverTerminal(t, env, ana, id)
	if run.State != dispatcherapi.StateTakenOver || run.Reason != "taken over by ben@example.com" {
		t.Errorf("the Run is %s (%s), want taken over by Ben", run.State, run.Reason)
	}
	takeOver := run.Executions[1]
	if takeOver.Form != dispatcherapi.FormInteractive || takeOver.TakenOverBy == nil ||
		takeOver.TakenOverBy.Name != "ben@example.com" {
		t.Errorf("the new Execution %+v is not a take-over by Ben", takeOver)
	}
	// A new Execution on a new machine: no headless Execution ran, and
	// the Runner started a new workload.
	if got := env.Runner.Dispatched(); len(got) != 2 || takeOver.VM != factorytest.VM(takeOver.ID) {
		t.Errorf("the take-over is not a new dispatch on its own machine: %v, VM %q", got, takeOver.VM)
	}
	runUser := env.Runner.RunUser().Username
	if want := "docker exec -i -u engineer " + takeOver.VM + " sudo -u " + runUser + " hc connect"; takeOver.Attach != want {
		t.Errorf("the attach command is %q, want %q", takeOver.Attach, want)
	}
	// The same Agent session, resumed, with AskUserQuestion.
	init := screenInit(t, env.Runner.TakeOver.Screen(terminal))
	if init["session_id"] != sessionOf(t, env, id) || init["resumed"] != true {
		t.Errorf("the interactive harness has the Agent session %v (resumed %v), want %v resumed", init["session_id"],
			init["resumed"], sessionOf(t, env, id))
	}
	started := eventsOf(env, id, "execution_started")
	if len(started) != 2 || started[1]["actor.name"] != "ben@example.com" ||
		started[1]["execution.form"] != dispatcherapi.FormInteractive || started[1]["execution.attach"] != takeOver.Attach {
		t.Errorf("the take-over Execution's start events: %v", started)
	}
	if len(eventsOf(env, id, "take_over")) != 1 {
		t.Errorf("want one take-over event: %v", eventsOf(env, id, "take_over"))
	}

	typeLine(t, env, terminal, "Why does the cart fail?", "Done.")
	leave(t, env, terminal)
	run = waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if run.Reason != dispatcherapi.ReasonTakeOverNoReport || len(run.Executions) != 2 {
		t.Errorf("after the take-over the Run is %s (%s) with %d Executions", run.State, run.Reason, len(run.Executions))
	}
}
