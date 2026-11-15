package wrapper_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// toolCall is a step that runs a foreground tool call until the interrupt
// control request: the harness then stops the tool's processes, as Claude
// Code does. The tool writes the pid of its child process to the file
// that it gives, and its child runs for 300 s. The step's eof_code is 7.
func toolCall(t *testing.T) (step, pidFile string) {
	t.Helper()
	pidFile = filepath.Join(t.TempDir(), "tool-pid")
	b, _ := json.Marshal(map[string]any{"fake": "tool", "eof_code": 7,
		"args": []string{"sh", "-c", `sleep 300 & echo $! >` + pidFile + `.part && mv ` + pidFile + `.part ` + pidFile + `; wait`}})
	return string(b), pidFile
}

// toolPid waits until the tool call's child process started, and gives
// its pid.
func toolPid(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatalf("the tool's pid file holds %q", b)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatal("the tool call did not start its child in 30 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stopped waits up to 10 s until pid ended.
func stopped(t *testing.T, pid int, when string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%s, the tool's child process %d still runs", when, pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var interruptedLine = regexp.MustCompile(`(?m)^Run (\S+) is interrupted\.`)

// interrupt runs 'solidtrace interrupt' as p, with a message or none.
func interrupt(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string, message ...string) factorytest.Result {
	t.Helper()
	got := p.CLI(append([]string{"interrupt", "--dispatcher", env.URL(), id}, message...)...)
	if got.Code != 0 {
		t.Fatalf("solidtrace interrupt: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	if m := interruptedLine.FindStringSubmatch(got.Stdout); m == nil || m[1] != id {
		t.Fatalf("solidtrace interrupt printed %q", got.Stdout)
	}
	return got
}

func TestAnInterruptWithNoMessageEndsTheExecutionWithInterruptedAndTheRunWaitsOnAPerson(t *testing.T) {
	env, ana := newEnv(t)
	ben := env.Member("Ben")
	tool, pidFile := toolCall(t)
	env.Runner.Agent(takeTask, initLine(t), tool, interrupted, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	pid := toolPid(t, pidFile)

	interrupt(t, env, ben, id)

	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	// error_during_execution after the wrapper's own interrupt is not a
	// failure.
	if run.Reason != dispatcherapi.EndInterrupted {
		t.Errorf("the Run waits on a person with the reason %q, want %q", run.Reason, dispatcherapi.EndInterrupted)
	}
	e := onlyExecution(t, run)
	if e.EndReason != dispatcherapi.EndInterrupted {
		t.Errorf("the end reason is %q, want %q", e.EndReason, dispatcherapi.EndInterrupted)
	}
	if e.ExitCode == nil || *e.ExitCode != interruptedCode {
		t.Errorf("the harness's exit code is %v: the wrapper did not interrupt it before it closed stdin", e.ExitCode)
	}
	stopped(t, pid, "after the interrupt")
	ev := waitForEvent(t, env, id, "interrupt")
	if ev["actor.kind"] != "person" || ev["actor.name"] != "ben@example.com" || ev["execution.id"] != e.ID {
		t.Errorf("the interrupt event: actor %v %v, execution %v", ev["actor.kind"], ev["actor.name"], ev["execution.id"])
	}
	if _, ok := ev["message.text"]; ok {
		t.Errorf("an interrupt with no message has a message: %v", ev)
	}
	ended := waitForEvent(t, env, id, "execution_ended")
	if ended["execution.reason"] != dispatcherapi.EndInterrupted || ended["run.state"] != dispatcherapi.StateWaitingOnPerson {
		t.Errorf("the Execution ended event: reason %v, state %v", ended["execution.reason"], ended["run.state"])
	}
	if len(env.Runner.Dispatched()) != 1 {
		t.Errorf("an interrupted Run got another Execution: %v", env.Runner.Dispatched())
	}
}

func TestAnInterruptWithAMessageStopsTheToolCallAndTheAgentGetsTheMessageInTheSameExecution(t *testing.T) {
	env, ana := newEnv(t)
	tool, pidFile := toolCall(t)
	here, reached := marker(t)
	wait, open := gate(t)
	// After the interrupt's result, the next message starts a new turn,
	// as Claude Code does.
	env.Runner.Agent(takeTask, initLine(t), tool, interrupted, takeTask, here, wait,
		reportStep("--outcome", "done", "--summary", "Used the new API"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	pid := toolPid(t, pidFile)

	got := interrupt(t, env, ana, id, "Use", "the new API")

	reached()
	// The tool call stopped at the interrupt, not at the end.
	stopped(t, pid, "when the agent got the message")
	stillRunning(t, env, ana, id, "the interrupt's message")
	open()
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	e := onlyExecution(t, run)
	if e.EndReason != dispatcherapi.EndReported {
		t.Errorf("the Execution ended with %q, want %q", e.EndReason, dispatcherapi.EndReported)
	}
	ev := waitForEvent(t, env, id, "interrupt")
	mid, _ := ev["message.id"].(string)
	if ev["actor.name"] != "ana@example.com" || ev["message.text"] != "Use the new API" || mid == "" ||
		!strings.Contains(got.Stdout, mid) {
		t.Errorf("the interrupt event: actor %v, message %v %v; the CLI printed %q", ev["actor.name"], mid,
			ev["message.text"], got.Stdout)
	}
	written := waitForMessageEvent(t, env, id, mid, "message_written")
	delivered := waitForMessageEvent(t, env, id, mid, "message_delivered")
	seq := func(ev map[string]any) float64 { return ev["run.seq"].(float64) }
	if seq(written) < seq(ev) || seq(delivered) < seq(written) {
		t.Errorf("the events are not interrupt, written, delivered: %v %v %v", seq(ev), seq(written), seq(delivered))
	}
	// One intervention: no message_accepted event as well.
	if accepted := messageEvents(env, id, mid)["message_accepted"]; len(accepted) != 0 {
		t.Errorf("the interrupt's message has an accepted event too: %v", accepted)
	}
	reports := eventsOf(env, id, "report")
	if len(reports) != 1 || reports[0]["report.delivered_messages"] != float64(2) {
		t.Errorf("the report's delivered-message counter (the task and the message): %v", reports)
	}
	if len(env.Runner.Dispatched()) != 1 {
		t.Errorf("the interrupt's message started another Execution: %v", env.Runner.Dispatched())
	}
}

func TestAnInterruptThatTheHarnessGivesNoResultForEndsTheExecutionAfterTheWait(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	wait, open := gate(t)
	// A tool call that the interrupt does not stop: no result comes.
	env.Runner.Agent(takeTask, initLine(t), here, wait, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	reached()
	interrupt(t, env, ana, id)
	time.Sleep(settle)

	env.Clock.Advance(9 * time.Second)
	stillRunning(t, env, ana, id, "9 s with no result after the interrupt")
	env.Clock.Advance(time.Second)
	open()

	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if run.Reason != dispatcherapi.EndInterrupted {
		t.Errorf("the Run waits on a person with the reason %q", run.Reason)
	}
	if e := onlyExecution(t, run); !strings.HasPrefix(e.EndReason, dispatcherapi.EndInterrupted+": ") ||
		!strings.Contains(e.EndReason, "no result") {
		t.Errorf("the end reason is %q, want interrupted with no result", e.EndReason)
	}
}

func TestAnInterruptDuringTheCloneEndsTheExecutionWithNoHarness(t *testing.T) {
	env, ana := newEnv(t)
	reached, harnessStarted := hangingClone(t, env)
	id := start(t, env, ana, "Fix the cart")
	waitForClone(t, reached)

	interrupt(t, env, ana, id)

	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if e := onlyExecution(t, run); e.EndReason != dispatcherapi.EndInterrupted || run.Reason != dispatcherapi.EndInterrupted {
		t.Errorf("the end reason is %q and the Run's reason %q, want %q", e.EndReason, run.Reason,
			dispatcherapi.EndInterrupted)
	}
	if _, err := os.Stat(harnessStarted); err == nil {
		t.Error("the harness started after the interrupt")
	}
}

func TestCompareAndEndGivesALaterInterruptAndAfterTheAgreementAnInterruptIsRefused(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	wait, open := gate(t)
	// The agent's tool call does not stop at the interrupt; the agent
	// then reports in the turn that the interrupt was for.
	env.Runner.Agent(takeTask, initLine(t), here, wait, reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	reached()
	execution := onlyExecution(t, getRun(t, ana, id)).ID

	interrupt(t, env, ana, id)
	answer := compareEnd(t, env, execution, 0)
	if answer.Agreed || len(answer.Commands) != 1 || answer.Commands[0].Kind != dispatcherapi.CommandInterrupt ||
		answer.Commands[0].Message != nil {
		t.Fatalf("compare-end with a later interrupt: %+v", answer)
	}
	if answer := compareEnd(t, env, execution, answer.Commands[0].Seq); !answer.Agreed {
		t.Fatalf("compare-end with no later command: %+v", answer)
	}
	got := ana.API("POST", "/api/v1/runs/"+id+"/interrupt", dispatcherapi.Interrupt{Text: "Go on"})
	if got.Status != http.StatusConflict || !strings.Contains(string(got.Body), "ending") {
		t.Errorf("an interrupt after the agreement: %d %s, want 409", got.Status, got.Body)
	}
	open()

	// The interrupt is a person's choice: it wins over the report of the
	// turn that it stopped, and the report's summary stays.
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if run.Reason != dispatcherapi.EndInterrupted || run.Summary != "Fixed" {
		t.Errorf("the Run waits on a person with reason %q and summary %q", run.Reason, run.Summary)
	}
	if list := eventsOf(env, id, "interrupt"); len(list) != 1 {
		t.Errorf("the interrupt events: %v, want the first only", list)
	}
}

func TestTheDispatcherRefusesAnInterruptOfARunWithNoLiveExecution(t *testing.T) {
	env, ana := newEnv(t)
	for range 2 {
		env.Runner.Agent(takeTask, initLine(t),
			reportStep("--outcome", "needs-a-person", "--summary", "Two fixes.", "--question", "Keep the old API?"),
			waitForEnd)
	}
	waiting := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, waiting, dispatcherapi.StateWaitingOnPerson)
	stoppedRun := start(t, env, ana, "Fix the tax")
	waitFor(t, env, ana, stoppedRun, dispatcherapi.StateWaitingOnPerson)
	if got := ana.CLI("stop", stoppedRun); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}
	other := env.PersonIn("Bo", recorder.Organization{ID: 8, Slug: "other", Name: "Other"}, "admin")

	for name, c := range map[string]struct {
		p      *factorytest.Person
		run    string
		text   string
		status int
		reason string
	}{
		"a Run that waits on a person": {ana, waiting, "", http.StatusConflict, "the Run is waiting_on_person: it has no live Execution"},
		"a stopped Run":                {ana, stoppedRun, "Go on", http.StatusConflict, "the Run is stopped: it has no live Execution"},
		"a Run that does not exist":    {ana, "nosuchrun", "", http.StatusNotFound, "no Run nosuchrun"},
		"another Organization's Run":   {other, waiting, "", http.StatusForbidden, ""},
		"a message of more than 64 KiB": {ana, waiting, strings.Repeat("x", dispatcherapi.MaxMessage+1),
			http.StatusBadRequest, "64 KiB"},
	} {
		got := c.p.API("POST", "/api/v1/runs/"+c.run+"/interrupt", dispatcherapi.Interrupt{Text: c.text})
		if got.Status != c.status || !strings.Contains(string(got.Body), c.reason) {
			t.Errorf("an interrupt of %s: %d %s, want %d with %q", name, got.Status, got.Body, c.status, c.reason)
		}
	}
	for _, id := range []string{waiting, stoppedRun} {
		if list := eventsOf(env, id, "interrupt"); len(list) != 0 {
			t.Errorf("a refused interrupt is an event: %v", list)
		}
	}
	// The CLI gives the Dispatcher's reason.
	got := ana.CLI("interrupt", "--dispatcher", env.URL(), waiting)
	if got.Code == 0 || !strings.Contains(got.Stderr, "no live Execution") {
		t.Errorf("solidtrace interrupt of a Run that waits: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	if run := getRun(t, ana, waiting); run.State != dispatcherapi.StateWaitingOnPerson || len(run.Executions) != 1 {
		t.Errorf("a refused interrupt changed the Run: %s, %d Executions", run.State, len(run.Executions))
	}
}
