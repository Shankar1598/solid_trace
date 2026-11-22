package wrapper_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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

// With the operator's wait setting, the Run waits on its own account: the
// refusal names that account's reset time, not an earlier reset time of
// another account.
func TestWithTheWaitSettingATakeOverIsRefusedWithTheResetTimeOfTheRunsOwnAccount(t *testing.T) {
	env, ana := newEnv(t)
	waitAtLimit(t, env)
	env.AddAccount("pool-2", secondToken)
	env.Runner.Agent(script(takeTask, initLine(t), usageLimit(env.Clock.Now().Add(2*time.Hour)))...)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
	own := onlyExecution(t, run).Account
	other := "pool-2"
	if own == other {
		other = factorytest.DefaultAccount
	}
	env.MarkAtLimit(other, env.Clock.Now().Add(time.Hour))

	got := ana.CLI("take-over", "--dispatcher", env.URL(), id)
	if got.Code != 1 || !strings.Contains(got.Stderr, "waits on an account limit until 2026-11-14 11:00 UTC") {
		t.Errorf("a take-over of a Run that waits on its own account %s: exit %d\n%s%s", own, got.Code, got.Stdout, got.Stderr)
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateWaitingOnAccountLimit || len(run.Executions) != 1 {
		t.Errorf("the refused take-over changed the Run: %s %+v", run.State, run.Executions)
	}
}

// A Run whose Agent session never started (its first setup failed): the
// take-over starts the Agent session with the Run's task, so the agent
// has the task, also in the Executions after the take-over.
func TestATakeOverThatStartsTheAgentSessionGivesTheAgentTheRunsTask(t *testing.T) {
	env, ana := newEnv(t)
	runs := filepath.Join(env.Runner.SharedDir("setup-"+filepath.Base(t.TempDir())), "runs")
	// The first run of the setup command fails.
	env.Forge.Push("example/shop", "main", map[string]string{
		factoryFile(""): `setup = "echo run >> ` + runs + ` && test $(wc -l < ` + runs + `) -gt 1"`,
	}, "A setup command")
	id := start(t, env, ana, "Fix the cart total")
	if run := waitFor(t, env, ana, id, dispatcherapi.StateFailed); !strings.Contains(run.Reason, "the setup command") {
		t.Fatalf("the first Execution failed with %q, want the setup command", run.Reason)
	}

	takeOverIdleCLI(t, env, ana, id)
	_, terminal := takeOverTerminal(t, env, ana, id)

	if init := screenInit(t, env.Runner.TakeOver.Screen(terminal)); init["resumed"] != false ||
		init["session_id"] != sessionOf(t, env, id) {
		t.Errorf("the take-over's Agent session is %v (resumed %v), want %v started", init["session_id"], init["resumed"],
			sessionOf(t, env, id))
	}
	if _, err := env.Runner.TakeOver.WaitForScreen(terminal, "Done.", 30*time.Second); err != nil {
		t.Errorf("the agent did not answer the Run's task: %v", err)
	}
	leave(t, env, terminal)
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	transcript := env.SolidTrace.Stored(sessionOf(t, env, id), sessionOf(t, env, id)+".jsonl")
	if !strings.Contains(string(transcript), `"content":"Fix the cart total"`) {
		t.Errorf("the take-over's transcript has no Run's task:\n%s", transcript)
	}
}

// The Run's task in a take-over that starts the Agent session is the
// factory's message: the take-over's started event names it with its
// message id and the SHA-256 of the text that the agent got (for an Issue
// Run, with the standing task), so the Agent session page says that the
// factory said it. The event's actor stays the person who took over.
func TestTheTaskOfATakeOverThatStartsTheAgentSessionIsTheFactorysMessage(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.FactorySender("Factory")
	runs := filepath.Join(env.Runner.SharedDir("setup-"+filepath.Base(t.TempDir())), "runs")
	env.Forge.Push("example/shop", "main", map[string]string{
		factoryFile(""): `setup = "echo run >> ` + runs + ` && test $(wc -l < ` + runs + `) -gt 1"`,
	}, "A setup command")
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	takeOverIdleCLI(t, env, ana, id)
	_, terminal := takeOverTerminal(t, env, ana, id)
	if _, err := env.Runner.TakeOver.WaitForScreen(terminal, "Done.", 30*time.Second); err != nil {
		t.Fatalf("the agent did not answer the Run's task: %v", err)
	}
	leave(t, env, terminal)
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)

	session := sessionOf(t, env, id)
	var task string
	for line := range strings.SplitSeq(string(env.SolidTrace.Stored(session, session+".jsonl")), "\n") {
		var l struct {
			Type    string `json:"type"`
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &l) == nil && l.Type == "user" {
			task, _ = l.Message.Content.(string)
			break
		}
	}
	if !strings.Contains(task, "SolidTrace Issue #7") {
		t.Fatalf("the transcript's first person message is %q, want the Run's task", task)
	}
	started := eventsOf(env, id, "execution_started")
	if len(started) != 2 || len(run.Executions) != 2 {
		t.Fatalf("want 2 Executions with a started event each, got %d events: %+v", len(started), run.Executions)
	}
	checkFields(t, started[1], map[string]any{"execution.id": run.Executions[1].ID,
		"message.id": dispatcherapi.TaskMessageID(run.Executions[1].ID), "message.sha256": dispatcherapi.TextSHA256(task),
		"actor.kind": "person", "actor.name": ana.Answer.Username})
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

// The take-over Execution ran no agent and no person attached: the
// messages that the Run kept while it was queued start a new Execution,
// as they do after any end of the take-over.
func TestMessagesKeptForATakeOverThatDoesNotStartStartANewExecution(t *testing.T) {
	env, ana := newEnv(t)
	id := idleRun(t, env, ana, dispatcherapi.StateDone)
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(time.Hour))
	takeOverIdleCLI(t, env, ana, id)
	message := send(t, env, ana, id, "Add a test for the cart")
	env.Runner.BreakWrapper()

	// The take-over Execution's dispatch, then the Execution after it.
	env.Runner.Agent(takeTask)
	env.Runner.Agent(takeTask, done("Added the test."), reply, result, waitForEnd)
	env.Clock.Advance(time.Hour)

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 3 || run.Executions[1].StartedAt != nil || run.Executions[2].Form != dispatcherapi.FormHeadless {
		t.Fatalf("want a take-over Execution that did not start, then a headless Execution: %+v", run.Executions)
	}
	if delivered := waitForMessageEvent(t, env, id, message, "message_delivered"); delivered["execution.id"] != run.Executions[2].ID {
		t.Errorf("the message was delivered in %v, want the Execution after the take-over", delivered["execution.id"])
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
