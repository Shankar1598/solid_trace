package wrapper_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// Lines of an agent in a long tool call: it waits for the interrupt
// control request. An interrupt gives the error_during_execution result,
// and the harness then exits with code 1 when stdin closes. With no
// interrupt, it exits with code 7 when stdin closes.
const (
	inToolCall  = `{"fake":"await_interrupt","eof_code":7}`
	interrupted = `{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":1,"session_id":"","uuid":"","total_cost_usd":0}`
	thenEOF     = `{"fake":"await_message","eof_code":1}`
	// interruptedCode is the harness's exit code after the interrupt.
	interruptedCode = 1
)

// marker is a step that makes a file, and a wait until the step ran.
func marker(t *testing.T) (step string, wait func()) {
	path := filepath.Join(t.TempDir(), "here")
	return shell("touch " + path), func() {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			if _, err := os.Stat(path); err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the agent did not reach the marker in 30 s")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// eventsOf are the Run's events that SolidTrace has, by kind.
func eventsOf(env *factorytest.Env, id, kind string) []map[string]any {
	var list []map[string]any
	for _, ev := range env.RunEvents(id) {
		if ev["event.kind"] == kind {
			list = append(list, ev)
		}
	}
	return list
}

// waitForEvent waits up to 10 s for an event of kind of the Run.
func waitForEvent(t *testing.T, env *factorytest.Env, id, kind string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if list := eventsOf(env, id, kind); len(list) > 0 {
			return list[len(list)-1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s event of the Run after 10 s: %v", kind, env.RunEvents(id))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStopOnARunningRunInterruptsTheAgentAndEndsTheExecutionWithTheReasonStopped(t *testing.T) {
	env, ana := newEnv(t)
	ben := env.Member("Ben")
	here, reached := marker(t)
	env.Runner.Agent(takeTask, initLine(t), here, inToolCall, interrupted, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	reached()

	// The wrapper has no listener for the Dispatcher: the stop reaches it
	// by its long poll.
	if got := ben.CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}

	run := waitFor(t, env, ana, id, dispatcherapi.StateStopped)
	e := onlyExecution(t, run)
	if e.EndReason != dispatcherapi.EndStopped {
		t.Errorf("the end reason is %q, want %q", e.EndReason, dispatcherapi.EndStopped)
	}
	if e.ExitCode == nil || *e.ExitCode != interruptedCode {
		t.Errorf("the harness's exit code is %v: the wrapper did not interrupt it before it closed stdin", e.ExitCode)
	}
	stop := waitForEvent(t, env, id, "stop")
	if stop["actor.name"] != "ben@example.com" || stop["run.state"] != dispatcherapi.StateStopped {
		t.Errorf("the stop event: actor %v, state %v", stop["actor.name"], stop["run.state"])
	}
	ended := waitForEvent(t, env, id, "execution_ended")
	if ended["execution.reason"] != dispatcherapi.EndStopped || ended["run.state"] != dispatcherapi.StateStopped {
		t.Errorf("the Execution ended event: reason %v, state %v", ended["execution.reason"], ended["run.state"])
	}
	if ended["run.seq"].(float64) < stop["run.seq"].(float64) {
		t.Errorf("the Execution ended before the stop")
	}
	if len(env.Runner.Dispatched()) != 1 {
		t.Errorf("a stopped Run got another Execution: %v", env.Runner.Dispatched())
	}
}

func TestStopOnARunThatWaitsOnAPersonMakesItStopped(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "needs-a-person", "--summary", "Two fixes.", "--question", "Keep the old API?"),
		waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)

	if got := ana.CLI("stop", id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}

	run := getRun(t, ana, id)
	if run.State != dispatcherapi.StateStopped || run.Question != "" {
		t.Errorf("the Run is %s with question %q, want stopped with none", run.State, run.Question)
	}
	if e := onlyExecution(t, run); e.EndReason != dispatcherapi.EndReported {
		t.Errorf("the ended Execution changed: %+v", e)
	}
	if stop := waitForEvent(t, env, id, "stop"); stop["actor.name"] != "ana@example.com" {
		t.Errorf("the stop's actor is %v", stop["actor.name"])
	}
}

// hangingClone makes the clone of the Project shop hang, as a forge that
// accepts the connection and does not answer, and queues an agent that
// makes the file that it gives when the harness starts.
func hangingClone(t *testing.T, env *factorytest.Env) (reached <-chan struct{}, harnessStarted string) {
	harnessStarted = filepath.Join(t.TempDir(), "harness-started")
	env.Runner.Agent(shell("touch "+harnessStarted), takeTask, waitForEnd)
	return env.Forge.Hang("example/shop"), harnessStarted
}

func waitForClone(t *testing.T, reached <-chan struct{}) {
	t.Helper()
	select {
	case <-reached:
	case <-time.After(30 * time.Second):
		t.Fatal("the wrapper did not start the clone in 30 s")
	}
	time.Sleep(settle)
}

func TestStopDuringTheCloneEndsTheExecutionWithTheReasonStoppedAndNoHarness(t *testing.T) {
	env, ana := newEnv(t)
	reached, harnessStarted := hangingClone(t, env)
	id := start(t, env, ana, "Fix the cart")
	waitForClone(t, reached)

	if got := ana.CLI("stop", id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}

	run := waitFor(t, env, ana, id, dispatcherapi.StateStopped)
	if e := onlyExecution(t, run); e.EndReason != dispatcherapi.EndStopped {
		t.Errorf("the end reason is %q, want %q", e.EndReason, dispatcherapi.EndStopped)
	}
	if _, err := os.Stat(harnessStarted); err == nil {
		t.Error("the harness started after the stop")
	}
}

func TestTheTotalLimitDuringTheCloneFailsTheRunWithTheReasonTimeout(t *testing.T) {
	env, ana := newEnv(t)
	reached, harnessStarted := hangingClone(t, env)
	id := start(t, env, ana, "Fix the cart")
	waitForClone(t, reached)

	env.Clock.Advance(8 * time.Hour)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if !strings.HasPrefix(run.Reason, dispatcherapi.EndTimeout+": ") || !strings.Contains(run.Reason, "total limit") {
		t.Errorf("the reason is %q, want timeout with the total limit", run.Reason)
	}
	if _, err := os.Stat(harnessStarted); err == nil {
		t.Error("the harness started after the limit")
	}
}
