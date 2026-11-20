package wrapper_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// Lines of an agent that takes a person's message (message N of the
// harness: the task is message 0).
const (
	// readMessage waits for the next message and does not deliver it: a
	// tool call runs.
	readMessage = `{"fake":"await_message","echo":false}`
	// slowly is the rest of the tool call, after the message came.
	slowly = `{"fake":"sleep","ms":400}`
)

// deliverMidTurn delivers message n when the tool call ends, in the same
// turn.
func deliverMidTurn(n int) string {
	b, _ := json.Marshal(map[string]any{"fake": "deliver", "message": n, "mid_turn": true})
	return string(b)
}

var acceptedLine = regexp.MustCompile(`(?m)^Message (\S+) is accepted for Run (\S+)\.$`)

// send runs 'solidtrace message' as p, and gives the message id.
func send(t *testing.T, env *factorytest.Env, p *factorytest.Person, id, text string) string {
	t.Helper()
	got := p.CLI("message", "--dispatcher", env.URL(), id, text)
	if got.Code != 0 {
		t.Fatalf("solidtrace message: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	m := acceptedLine.FindStringSubmatch(got.Stdout)
	if m == nil || m[2] != id {
		t.Fatalf("solidtrace message printed %q", got.Stdout)
	}
	return m[1]
}

// messageEvents are the Run's events of the message, by kind, in order.
func messageEvents(env *factorytest.Env, runID, messageID string) map[string][]map[string]any {
	events := map[string][]map[string]any{}
	for _, ev := range env.RunEvents(runID) {
		if ev["message.id"] == messageID {
			kind, _ := ev["event.kind"].(string)
			events[kind] = append(events[kind], ev)
		}
	}
	return events
}

func waitForMessageEvent(t *testing.T, env *factorytest.Env, runID, messageID, kind string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if list := messageEvents(env, runID, messageID)[kind]; len(list) > 0 {
			return list[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s event of message %s after 15 s: %v", kind, messageID, env.RunEvents(runID))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func timeAttribute(t *testing.T, ev map[string]any, name string) time.Time {
	t.Helper()
	s, _ := ev[name].(string)
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("the event's %s is %v: %v", name, ev[name], err)
	}
	return at
}

func TestAMessageDuringAToolCallReachesTheAgentWhenTheCallEndsInTheSameTurn(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	env.Runner.Agent(takeTask, initLine(t), here, readMessage, slowly, deliverMidTurn(1),
		reportStep("--outcome", "done", "--summary", "Used the new API"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	reached()

	mid := send(t, env, ana, id, "Use the new API")

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if e := onlyExecution(t, run); e.EndReason != dispatcherapi.EndReported {
		t.Errorf("the Execution ended with %q", e.EndReason)
	}
	accepted := waitForMessageEvent(t, env, id, mid, "message_accepted")
	written := waitForMessageEvent(t, env, id, mid, "message_written")
	delivered := waitForMessageEvent(t, env, id, mid, "message_delivered")
	if accepted["actor.kind"] != "person" || accepted["actor.name"] != "ana@example.com" ||
		accepted["message.text"] != "Use the new API" {
		t.Errorf("the accepted event: actor %v %v, text %v", accepted["actor.kind"], accepted["actor.name"],
			accepted["message.text"])
	}
	seq := func(ev map[string]any) float64 { return ev["run.seq"].(float64) }
	if !(seq(accepted) < seq(written) && seq(written) < seq(delivered)) {
		t.Errorf("the events are not accepted, written, delivered: %v %v %v", seq(accepted), seq(written), seq(delivered))
	}
	// Each event names the message by the wrapper's uuid and the hash of its
	// text, with the actor, so that the Agent session page can say who said
	// it.
	for _, ev := range []map[string]any{accepted, written, delivered} {
		if ev["message.id"] != mid || ev["message.sha256"] != dispatcherapi.TextSHA256("Use the new API") ||
			ev["actor.kind"] != "person" || ev["actor.name"] != "ana@example.com" {
			t.Errorf("the %v event: message %v, hash %v, actor %v %v", ev["event.kind"], ev["message.id"],
				ev["message.sha256"], ev["actor.kind"], ev["actor.name"])
		}
	}
	// The task is a message of the factory, with the uuid that the
	// Execution's task has.
	started := eventsOf(env, id, "execution_started")
	if len(started) != 1 || started[0]["message.id"] != dispatcherapi.TaskMessageID(onlyExecution(t, run).ID) ||
		started[0]["message.sha256"] != dispatcherapi.TextSHA256("Fix the cart") {
		t.Errorf("the started event does not name the task as a message: %v", started)
	}
	// The agent got it when the tool call ended, not at the write.
	if gap := timeAttribute(t, delivered, "message.at").Sub(timeAttribute(t, written, "message.at")); gap < 350*time.Millisecond {
		t.Errorf("the delivered time is %s after the write: it is not the time of the replay echo", gap)
	}
	reports := eventsOf(env, id, "report")
	if len(reports) != 1 || reports[0]["report.delivered_messages"] != float64(2) {
		t.Errorf("the report's delivered-message counter (the task and the message): %v", reports)
	}
	if len(env.Runner.Dispatched()) != 1 {
		t.Errorf("a delivered message started another Execution: %v", env.Runner.Dispatched())
	}
}

func TestAMessageWhileTheAgentReportsStartsANewTurnAndTheStopHookAsksForANewReport(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	wait, open := gate(t)
	// The message comes after the report, in the same turn. The harness
	// does not take it in that turn: it starts a new turn with it after
	// the result, as Claude Code does with a message that it did not
	// absorb.
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "First"), here, wait,
		reply, result,
		takeTask, stopStep([]string{reportStep("--outcome", "done", "--summary", "Second")}), reply, result,
		waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	reached()
	mid := send(t, env, ana, id, "Also fix the tax")
	waitForMessageEvent(t, env, id, mid, "message_written")
	open()

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Second" || len(run.Executions) != 1 {
		t.Errorf("the Run is done with %q in %d Executions, want the second report in one", run.Summary,
			len(run.Executions))
	}
	waitForMessageEvent(t, env, id, mid, "message_delivered")
	if got := feedback(t, env); len(got) != 1 || got[0] != "a message arrived after your report; act on it and report again" {
		t.Errorf("the Stop hook's refusals: %q", got)
	}
	reports := eventsOf(env, id, "report")
	if len(reports) != 2 || reports[0]["report.delivered_messages"] != float64(1) ||
		reports[1]["report.delivered_messages"] != float64(2) {
		t.Fatalf("the reports' delivered-message counters: %v", reports)
	}
	if again := messageEvents(env, id, mid)["message_sent_again"]; len(again) != 0 {
		t.Errorf("a delivered message was sent again: %v", again)
	}
}

func TestTheStopHookRefusesOneTimeForAMessageAfterTheReport(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	wait, open := gate(t)
	// The agent does not report again: the second stop is allowed, and
	// the first report counts.
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "First"), here, wait,
		reply, result, takeTask, stopStep([]string{reply}), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	reached()
	mid := send(t, env, ana, id, "Also fix the tax")
	waitForMessageEvent(t, env, id, mid, "message_written")
	open()

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "First" || len(run.Executions) != 1 {
		t.Errorf("the Run's summary is %q in %d Executions", run.Summary, len(run.Executions))
	}
	if got := feedback(t, env); len(got) != 1 {
		t.Errorf("the Stop hook refused %d times, want one: %q", len(got), got)
	}
}

func TestAWrittenMessageWithNoEchoGoesToANewExecutionAfter30Seconds(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	wait, open := gate(t)
	// The harness reads the message and never delivers it.
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "First"), here, wait,
		reply, result, `{"fake":"await_eof"}`)
	// The second Execution continues the Agent session: its first message
	// is the message, not the prompt.
	env.Runner.Agent(takeTask, reportStep("--outcome", "done", "--summary", "Second"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	reached()
	mid := send(t, env, ana, id, "Also fix the tax")
	waitForMessageEvent(t, env, id, mid, "message_written")
	open()

	// The wait is 30 seconds on the agent machine's clock.
	waitForEvent(t, env, id, "report")
	time.Sleep(settle)
	env.Clock.Advance(29 * time.Second)
	time.Sleep(settle)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateRunning || len(run.Executions) != 1 {
		t.Fatalf("after 29 s the Run is %s with %d Executions", run.State, len(run.Executions))
	}
	env.Clock.Advance(2 * time.Second)

	waitForExecutions(t, env, ana, id, 2)
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	first, second := run.Executions[0], run.Executions[1]
	if first.EndReason != dispatcherapi.EndReported || run.Summary != "Second" {
		t.Errorf("the first Execution ended with %q, and the Run's summary is %q", first.EndReason, run.Summary)
	}
	events := messageEvents(env, id, mid)
	again, delivered := events["message_sent_again"], events["message_delivered"]
	if len(again) != 1 || again[0]["execution.id"] != first.ID {
		t.Errorf("the sent-again events: %v", again)
	}
	if len(delivered) != 1 || delivered[0]["execution.id"] != second.ID {
		t.Errorf("the delivered events: %v", delivered)
	}
	if len(events["message_written"]) != 2 {
		t.Errorf("want a written event in each Execution: %v", events["message_written"])
	}
	// The second Execution's first message is the message itself: its
	// report counts one delivered message.
	reports := eventsOf(env, id, "report")
	if len(reports) != 2 || reports[1]["report.delivered_messages"] != float64(1) {
		t.Errorf("the reports: %v", reports)
	}
	if second.TaskDeliveredAt != nil {
		t.Errorf("the second Execution wrote the prompt again")
	}
}

// compareEnd makes the wrapper's compare-and-end call of the Execution,
// with its workload identity.
func compareEnd(t *testing.T, env *factorytest.Env, execution string, after int64) dispatcherapi.CompareEndAnswer {
	t.Helper()
	got := env.API(env.Runner.Identity(factorytest.Workload(execution)), "POST",
		"/api/v1/executions/"+execution+"/compare-end", dispatcherapi.CompareEnd{After: after})
	if got.Status != http.StatusOK {
		t.Fatalf("compare-end after %d: %d %s", after, got.Status, got.Body)
	}
	var answer dispatcherapi.CompareEndAnswer
	got.JSON(t, &answer)
	return answer
}

func TestCompareAndEndGivesALaterMessageAndAfterItsAgreementANewMessageGoesToANewExecution(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), here, wait, readMessage, deliverMidTurn(1),
		reportStep("--outcome", "done", "--summary", "First"), reply, result, waitForEnd)
	env.Runner.Agent(takeTask, reportStep("--outcome", "done", "--summary", "Second"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	reached()
	execution := onlyExecution(t, getRun(t, ana, id)).ID

	// The wrapper asks to end before it got the message: the Dispatcher
	// gives the message, and does not agree.
	first := send(t, env, ana, id, "Also fix the tax")
	answer := compareEnd(t, env, execution, 0)
	if answer.Agreed || len(answer.Commands) != 1 || answer.Commands[0].Message == nil ||
		answer.Commands[0].Message.ID != first || answer.Commands[0].Message.Text != "Also fix the tax" {
		t.Fatalf("compare-end with a later message: %+v", answer)
	}
	seen := answer.Commands[0].Seq
	if answer := compareEnd(t, env, execution, seen); !answer.Agreed || len(answer.Commands) != 0 {
		t.Fatalf("compare-end with no later message: %+v", answer)
	}

	// After the agreement, a message is kept for a new Execution.
	second := send(t, env, ana, id, "And the shipping")
	if answer := compareEnd(t, env, execution, seen); !answer.Agreed || len(answer.Commands) != 0 {
		t.Errorf("compare-end gave a message after the agreement: %+v", answer)
	}
	open()

	waitForExecutions(t, env, ana, id, 2)
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Second" || len(run.Executions) != 2 {
		t.Fatalf("the Run is done with %q in %d Executions", run.Summary, len(run.Executions))
	}
	for message, execution := range map[string]string{first: run.Executions[0].ID, second: run.Executions[1].ID} {
		delivered := messageEvents(env, id, message)["message_delivered"]
		if len(delivered) != 1 || delivered[0]["execution.id"] != execution {
			t.Errorf("the delivered events of %s: %v, want one in %s", message, delivered, execution)
		}
	}
	if again := messageEvents(env, id, second)["message_sent_again"]; len(again) != 0 {
		t.Errorf("a kept message counts as sent again: %v", again)
	}
}

func TestOnlyTheReplayEchoOfAMessageCountsAndOnlyOneTime(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	// An echo with a uuid that the wrapper did not write, the message's
	// echo two times, and a synthetic user event.
	foreign := `{"type":"user","isReplay":true,"message":{"role":"user","content":"Also fix the tax"},` +
		`"parent_tool_use_id":null,"session_id":"","uuid":""}`
	synthetic := `{"type":"user","isSynthetic":true,"message":{"role":"user","content":[{"type":"text",` +
		`"text":"Not a message"}]},"parent_tool_use_id":null,"session_id":"","uuid":""}`
	env.Runner.Agent(takeTask, initLine(t), here, readMessage, foreign, deliverMidTurn(1), deliverMidTurn(1),
		synthetic, reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	reached()
	mid := send(t, env, ana, id, "Also fix the tax")

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 1 {
		t.Errorf("the Run has %d Executions", len(run.Executions))
	}
	if delivered := messageEvents(env, id, mid)["message_delivered"]; len(delivered) != 1 {
		t.Errorf("the delivered events: %v", delivered)
	}
	reports := eventsOf(env, id, "report")
	if len(reports) != 1 || reports[0]["report.delivered_messages"] != float64(2) {
		t.Errorf("the report's delivered-message counter (the task and the message): %v", reports)
	}
}

func TestTheDispatcherRefusesAMessageItCannotTake(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply, result,
		waitForEnd)
	done := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, done, dispatcherapi.StateDone)
	// The Run is closed: 30 days with no activity.
	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()
	closedEvent(t, env, done, dispatcherapi.StateDone, "", "no_activity")
	other := env.PersonIn("Bo", recorder.Organization{ID: 8, Slug: "other", Name: "Other"}, "admin")

	for name, c := range map[string]struct {
		p      *factorytest.Person
		run    string
		text   string
		status int
	}{
		"a Closed Run":               {ana, done, "Also fix the tax", http.StatusConflict},
		"a Run that does not exist":  {ana, "nosuchrun", "Also fix the tax", http.StatusNotFound},
		"another Organization's Run": {other, done, "Also fix the tax", http.StatusForbidden},
		"no text":                    {ana, done, "  \n", http.StatusBadRequest},
		"a text of more than 64 KiB": {ana, done, strings.Repeat("x", dispatcherapi.MaxMessage+1), http.StatusBadRequest},
	} {
		got := c.p.API("POST", "/api/v1/runs/"+c.run+"/messages", dispatcherapi.SendMessage{Text: c.text})
		if got.Status != c.status {
			t.Errorf("a message to %s: %d %s, want %d", name, got.Status, got.Body, c.status)
		}
	}
	for _, ev := range env.RunEvents(done) {
		if ev["event.kind"] == "message_accepted" {
			t.Errorf("a refused message is accepted: %v", ev)
		}
	}
	// The CLI gives the Dispatcher's reason, and how to go on. (Ana's
	// sign-in ended in the 30 days.)
	got := env.Member("Ben").CLI("message", "--dispatcher", env.URL(), done, "Also fix the tax")
	if got.Code == 0 || !strings.Contains(got.Stderr, "the Run is closed") || !strings.Contains(got.Stderr, "start a new Run") {
		t.Errorf("solidtrace message to a Closed Run: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	if run := getRun(t, ana, done); run.State != dispatcherapi.StateDone || len(run.Executions) != 1 {
		t.Errorf("the refused message changed the Closed Run: %s with %d Executions", run.State, len(run.Executions))
	}
}

// waitForExecutions waits until the Run has n Executions.
func waitForExecutions(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string, n int) dispatcherapi.Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		run := getRun(t, p, id)
		if len(run.Executions) >= n {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Run has %d Executions after 30 s, want %d: %s (%s)\n%s", len(run.Executions), n, run.State,
				run.Reason, env.Logs())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
