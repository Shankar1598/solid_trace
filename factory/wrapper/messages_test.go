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
