package wrapper_test

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
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
	env.Runner.KeepAgentSessions()
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
