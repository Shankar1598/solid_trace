package wrapper_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

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

// idleRun gives a Run in one of the states with no live Execution, and
// queues no script for the take-over Execution's dispatch.
func idleRun(t *testing.T, env *factorytest.Env, p *factorytest.Person, state string) string {
	t.Helper()
	needsAPerson := reportStep("--outcome", "needs-a-person", "--summary", "Two fixes.", "--question", "Which one?")
	switch state {
	case dispatcherapi.StateDone:
		env.Runner.Agent(takeTask, initLine(t), done("Fixed."), reply, result, waitForEnd)
	case dispatcherapi.StateFailed:
		env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "failed", "--summary", "No fix."), reply, result,
			waitForEnd)
	case dispatcherapi.StateWaitingOnPerson, dispatcherapi.StateStopped:
		env.Runner.Agent(takeTask, initLine(t), needsAPerson, waitForEnd)
	}
	id := start(t, env, p, "Fix the cart")
	if state != dispatcherapi.StateStopped {
		waitFor(t, env, p, id, state)
		return id
	}
	waitFor(t, env, p, id, dispatcherapi.StateWaitingOnPerson)
	if got := p.CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}
	waitFor(t, env, p, id, dispatcherapi.StateStopped)
	return id
}

func TestATakeOverOfARunThatWaitsOnAPersonFailedOrStoppedQueuesItThenTakesItOver(t *testing.T) {
	for _, state := range []string{dispatcherapi.StateWaitingOnPerson, dispatcherapi.StateFailed,
		dispatcherapi.StateStopped} {
		t.Run(state, func(t *testing.T) {
			env, ana := newEnv(t)
			id := idleRun(t, env, ana, state)

			takeOverIdleCLI(t, env, ana, id)
			if ev := waitForEvent(t, env, id, "take_over"); ev["actor.name"] != "ana@example.com" ||
				ev["run.state"] != dispatcherapi.StateQueued || ev["run.continued_from"] != state {
				t.Errorf("the take-over event: %v", ev)
			}
			run, terminal := takeOverTerminal(t, env, ana, id)
			if run.State != dispatcherapi.StateTakenOver || run.Executions[1].Form != dispatcherapi.FormInteractive ||
				run.Question != "" || run.Summary != "" {
				t.Errorf("the Run is %s (question %q, summary %q) with %+v, want taken over in a take-over Execution",
					run.State, run.Question, run.Summary, run.Executions)
			}
			typeLine(t, env, terminal, `!solidtrace report --outcome done --summary "Fixed by hand." && echo reported`,
				"reported")
			leave(t, env, terminal)
			if run := waitFor(t, env, ana, id, dispatcherapi.StateDone); run.Summary != "Fixed by hand." {
				t.Errorf("the summary is %q", run.Summary)
			}
		})
	}
}

func TestATakeOverOfARunThatWaitsOnAnAccountLimitIsRefusedWithTheResetTime(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(script(takeTask, initLine(t), usageLimit(env.Clock.Now().Add(2*time.Hour)))...)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)

	got := ana.CLI("take-over", "--dispatcher", env.URL(), id)
	if got.Code != 1 || !strings.Contains(got.Stderr, "waits on an account limit until 2026-11-14 11:00 UTC") {
		t.Errorf("a take-over of a Run that waits on an account limit: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	time.Sleep(settle)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateWaitingOnAccountLimit || len(run.Executions) != 1 {
		t.Errorf("the refused take-over changed the Run: %s %+v", run.State, run.Executions)
	}
	if events := eventsOf(env, id, "take_over"); len(events) != 0 {
		t.Errorf("a refused take-over has events: %v", events)
	}
}

func TestATakeOverOfAClosedRunIsRefused(t *testing.T) {
	env, ana := newEnv(t)
	id := idleRun(t, env, ana, dispatcherapi.StateDone)
	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")

	// The CLI's sign-in has ended after 30 days: the API, as a person.
	a := ana.API("POST", "/api/v1/runs/"+id+"/take-over", nil)
	if a.Status != http.StatusConflict || !strings.Contains(string(a.Body), "the Run is closed") {
		t.Errorf("a take-over of a Closed Run: %d %s, want 409 that says it is closed", a.Status, a.Body)
	}
	time.Sleep(settle)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateDone || len(run.Executions) != 1 {
		t.Errorf("the refused take-over changed the Closed Run: %s %+v", run.State, run.Executions)
	}
	if events := eventsOf(env, id, "take_over"); len(events) != 0 {
		t.Errorf("a refused take-over has events: %v", events)
	}
}

func TestATakeOverIsRefusedForAQueuedRunAndForARunThatATakeOverQueued(t *testing.T) {
	env, ana := newEnv(t)
	ben := env.Member("Ben")
	id := idleRun(t, env, ana, dispatcherapi.StateDone)
	// No free account: each Run stays queued.
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(time.Hour))
	queued := start(t, env, ana, "Fix the tax")

	got := ana.CLI("take-over", "--dispatcher", env.URL(), queued)
	if got.Code != 1 || !strings.Contains(got.Stderr, "the Run is queued") {
		t.Errorf("a take-over of a queued Run: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	takeOverIdleCLI(t, env, ben, id)
	got = ana.CLI("take-over", "--dispatcher", env.URL(), id)
	if got.Code != 1 || !strings.Contains(got.Stderr, "queued for a take-over already, by ben@example.com") {
		t.Errorf("a second take-over: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	time.Sleep(settle)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateQueued || len(run.Executions) != 1 ||
		!strings.Contains(run.Reason, "no free account") {
		t.Errorf("the Run that the take-over queued is %s (%s) with %+v", run.State, run.Reason, run.Executions)
	}
	if events := eventsOf(env, queued, "take_over"); len(events) != 0 || len(eventsOf(env, id, "take_over")) != 1 {
		t.Errorf("the refused take-overs have events: %v", env.RunEvents(id))
	}

	// The account is free again: the take-over is Ben's. The older Run
	// is dispatched first.
	env.Runner.Agent(takeTask)
	env.Runner.Agent(takeTask, initLine(t), done("Fixed the tax."), reply, result, waitForEnd)
	env.Clock.Advance(time.Hour)
	run, _ := takeOverTerminal(t, env, ana, id)
	if by := run.Executions[1].TakenOverBy; by == nil || by.Name != "ben@example.com" || run.Reason != "taken over by ben@example.com" {
		t.Errorf("the take-over Execution is by %+v (%s), want Ben", by, run.Reason)
	}
}

func TestATakeOverExecutionThatDoesNotStartMakesTheRunWaitOnAPersonWithTheReason(t *testing.T) {
	env, ana := newEnv(t)
	id := idleRun(t, env, ana, dispatcherapi.StateDone)
	env.Runner.BreakWrapper()

	takeOverIdleCLI(t, env, ana, id)

	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if !strings.HasPrefix(run.Reason, "the take-over ended: ") || !strings.Contains(run.Reason, "code 2") {
		t.Errorf("the reason is %q, want the take-over's end and the wrapper's exit", run.Reason)
	}
	time.Sleep(settle)
	if got := env.Runner.Dispatched(); len(got) != 2 {
		t.Errorf("the take-over started again by itself: %v", got)
	}
	// The take-over is over: a message continues the Run headless.
	env.Runner.Agent(takeTask, done("Fixed again."), reply, result, waitForEnd)
	sendContinued(t, env, ana, id, "Fix it again")
	run = waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 3 || run.Executions[2].Form != dispatcherapi.FormHeadless || run.Summary != "Fixed again." {
		t.Errorf("after the message the Run is %s (%q) with %+v, want a third, headless Execution", run.State,
			run.Summary, run.Executions)
	}
}

func TestAStopOfARunThatATakeOverQueuedStopsItAndEndsTheTakeOver(t *testing.T) {
	env, ana := newEnv(t)
	id := idleRun(t, env, ana, dispatcherapi.StateDone)
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(time.Hour))
	takeOverIdleCLI(t, env, ana, id)

	if got := ana.CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}
	waitFor(t, env, ana, id, dispatcherapi.StateStopped)

	// A message after the stop continues the Run headless: the stop ended
	// the take-over.
	env.Runner.Agent(takeTask, done("Fixed again."), reply, result, waitForEnd)
	env.Clock.Advance(time.Hour)
	sendContinued(t, env, ana, id, "Fix it again")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[1].Form != dispatcherapi.FormHeadless {
		t.Errorf("after the stop and a message the Run has %+v, want a second, headless Execution", run.Executions)
	}
}

func TestAMessageToARunThatATakeOverQueuedIsKeptUntilTheTakeOverEnds(t *testing.T) {
	env, ana := newEnv(t)
	id := idleRun(t, env, ana, dispatcherapi.StateDone)
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(time.Hour))
	takeOverIdleCLI(t, env, ana, id)
	message := send(t, env, ana, id, "Add a test for the cart")

	// The take-over Execution's dispatch, then the Execution after it.
	env.Runner.Agent(takeTask)
	env.Runner.Agent(takeTask, done("Added the test."), reply, result, waitForEnd)
	env.Clock.Advance(time.Hour)
	_, terminal := takeOverTerminal(t, env, ana, id)
	time.Sleep(settle)
	if strings.Contains(env.Runner.TakeOver.Screen(terminal), "Add a test") {
		t.Error("the message was typed into the take-over's terminal")
	}
	if events := messageEvents(env, id, message); len(events["message_delivered"]) != 0 {
		t.Errorf("the take-over Execution got the message: %v", events)
	}

	leave(t, env, terminal)
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 3 || run.Executions[2].Form != dispatcherapi.FormHeadless {
		t.Fatalf("want a third, headless Execution after the take-over: %+v", run.Executions)
	}
	if delivered := waitForMessageEvent(t, env, id, message, "message_delivered"); delivered["execution.id"] != run.Executions[2].ID {
		t.Errorf("the message was delivered in %v, want the Execution after the take-over", delivered["execution.id"])
	}
}

func TestARunThatATakeOverQueuedGoesBackInTheQueueAsATakeOverWhenItsAccountIsRemoved(t *testing.T) {
	env, ana := newEnv(t)
	env.AddAccount("pool-2", secondToken)
	id := idleRun(t, env, ana, dispatcherapi.StateDone)
	// The operator removes pool-1 after the take-over Execution's dispatch
	// on it, before its task call.
	env.Runner.BeforeDispatch(func(string) {
		env.RemoveAccount(factorytest.DefaultAccount)
		env.Clock.Advance(time.Second)
	})

	takeOverIdleCLI(t, env, ana, id)

	run := takeOverRuns(t, env, ana, id)
	if len(run.Executions) != 3 {
		t.Fatalf("want three Executions, got %+v", run.Executions)
	}
	removed, takeOver := run.Executions[1], run.Executions[2]
	if removed.Form != dispatcherapi.FormInteractive || !strings.Contains(removed.EndReason, "no longer in the pool") {
		t.Errorf("the take-over Execution on the removed account is %+v", removed)
	}
	if run.State != dispatcherapi.StateTakenOver || takeOver.Form != dispatcherapi.FormInteractive ||
		takeOver.Account != "pool-2" {
		t.Errorf("the Run is %s with the Execution %+v, want taken over on pool-2", run.State, takeOver)
	}
}
