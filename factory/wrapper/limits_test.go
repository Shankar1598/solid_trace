package wrapper_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// settle gives the wrapper time to read what the harness wrote, and to
// see a change of the machines' clock. The clock moves only when the test
// moves it, so this wait is in real time only.
const settle = 300 * time.Millisecond

// stillRunning fails the test when the Run is not running with its one
// Execution.
func stillRunning(t *testing.T, env *factorytest.Env, p *factorytest.Person, id, after string) {
	t.Helper()
	time.Sleep(settle)
	run := getRun(t, p, id)
	if run.State != dispatcherapi.StateRunning || len(run.Executions) != 1 ||
		run.Executions[0].State != dispatcherapi.ExecutionRunning {
		t.Fatalf("after %s the Run is %s (%s): %+v", after, run.State, run.Reason, run.Executions)
	}
}

func TestThirtyMinutesWithNoOutputEventFailTheRunWithTheReasonTimeout(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	// A long tool call: no event on the output stream.
	env.Runner.Agent(takeTask, initLine(t), here, inToolCall, interrupted, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	reached()
	time.Sleep(settle)

	env.Clock.Advance(30*time.Minute - time.Second)
	stillRunning(t, env, ana, id, "29m59s with no output")
	env.Clock.Advance(time.Second)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if !strings.HasPrefix(run.Reason, dispatcherapi.EndTimeout+": ") || !strings.Contains(run.Reason, "30m0s") {
		t.Errorf("the reason is %q, want timeout with the idle limit", run.Reason)
	}
	e := onlyExecution(t, run)
	if !strings.HasPrefix(e.EndReason, dispatcherapi.EndTimeout) {
		t.Errorf("the end reason is %q", e.EndReason)
	}
	if e.ExitCode == nil || *e.ExitCode != interruptedCode {
		t.Errorf("the harness's exit code is %v: the wrapper did not interrupt it first", e.ExitCode)
	}
	ended := waitForEvent(t, env, id, "execution_ended")
	if reason, _ := ended["execution.reason"].(string); !strings.HasPrefix(reason, dispatcherapi.EndTimeout) {
		t.Errorf("the Execution ended event's reason is %q", reason)
	}
}

func TestEightHoursInTotalFailTheRunWithTheReasonTimeoutAlsoWithOutput(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	var event map[string]any
	if err := json.Unmarshal([]byte(reply), &event); err != nil {
		t.Fatal(err)
	}
	busy, _ := json.Marshal(map[string]any{"fake": "busy", "ms": 20, "event": event, "eof_code": 7})
	// The agent works with output all the time: only the total limit
	// can end it.
	env.Runner.Agent(takeTask, initLine(t), here, string(busy), interrupted, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	reached()

	// Steps shorter than the idle limit, each with output after it.
	for range 16 {
		env.Clock.Advance(29 * time.Minute)
		time.Sleep(250 * time.Millisecond)
	}
	env.Clock.Advance(8*time.Hour - 16*29*time.Minute - time.Second)
	stillRunning(t, env, ana, id, "7h59m59s with output")
	env.Clock.Advance(time.Second)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if !strings.HasPrefix(run.Reason, dispatcherapi.EndTimeout+": ") || !strings.Contains(run.Reason, "total limit of 8h0m0s") {
		t.Errorf("the reason is %q, want timeout with the total limit", run.Reason)
	}
	if e := onlyExecution(t, run); e.ExitCode == nil || *e.ExitCode != interruptedCode {
		t.Errorf("the harness's exit code is %v: the wrapper did not interrupt it first", e.ExitCode)
	}
}

func TestTheDispatcherStopsAnExecutionThatPassesItsTotalLimitPlusFifteenMinutes(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	env.Runner.Agent(takeTask, initLine(t), here, inToolCall, interrupted, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	reached()
	// The wrapper's timers do not fire: only the Dispatcher's backstop
	// can end the Execution.
	env.Runner.HoldClock()

	env.Clock.Advance(8*time.Hour + 15*time.Minute)
	stillRunning(t, env, ana, id, "the total limit plus 15 minutes")
	env.Clock.Advance(time.Second)

	run := waitFor(t, env, ana, id, dispatcherapi.StateStopped)
	if !strings.Contains(run.Reason, "total limit plus 15 minutes") {
		t.Errorf("the reason is %q", run.Reason)
	}
	e := onlyExecution(t, run)
	if e.EndReason != dispatcherapi.EndStopped {
		t.Errorf("the end reason is %q, want %q", e.EndReason, dispatcherapi.EndStopped)
	}
	stop := waitForEvent(t, env, id, "stop")
	if stop["actor.kind"] != "dispatcher" || stop["run.state"] != dispatcherapi.StateStopped {
		t.Errorf("the stop event: actor %v, state %v", stop["actor.kind"], stop["run.state"])
	}
	if !strings.Contains(env.Logs(), "the backstop stopped an Execution") {
		t.Errorf("the Dispatcher's log does not say that the backstop stopped the Execution:\n%s", env.Logs())
	}
}

func TestTheIdleLimitEndsAnExecutionWhoseHarnessDoesNotReadItsTask(t *testing.T) {
	env, ana := newEnv(t)
	// A harness that hangs at its start and never reads stdin. The task is
	// larger than a pipe's buffer (64 KiB), so the write of the task blocks.
	here := filepath.Join(t.TempDir(), "here")
	program := filepath.Join(t.TempDir(), "claude-that-hangs")
	if err := os.WriteFile(program, []byte("#!/bin/sh\ntouch "+here+"\nexec sleep 600\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	env.Runner.UseHarness(program)
	id := start(t, env, ana, "Fix the cart. "+strings.Repeat("The cart loses items. ", 5000))
	deadline := time.Now().Add(30 * time.Second)
	for _, err := os.Stat(here); err != nil; _, err = os.Stat(here) {
		if time.Now().After(deadline) {
			t.Fatal("the harness did not start in 30 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(settle)

	env.Clock.Advance(30 * time.Minute)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if !strings.HasPrefix(run.Reason, dispatcherapi.EndTimeout+": ") || !strings.Contains(run.Reason, "idle limit") {
		t.Errorf("the reason is %q, want timeout with the idle limit", run.Reason)
	}
}

func TestTheDispatcherStopsAWedgedWrapperThroughTheRunnerAfterTheBackstop(t *testing.T) {
	env, ana := newEnv(t)
	here, reached := marker(t)
	env.Runner.Agent(takeTask, initLine(t), here, inToolCall, interrupted, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	reached()
	// The wrapper hangs: it takes no command, and its timers do not fire.
	execution := env.Runner.Dispatched()[0]
	if err := env.Runner.Fake.Freeze(execution); err != nil {
		t.Fatal(err)
	}

	env.Clock.Advance(8*time.Hour + 15*time.Minute + time.Second)
	waitForEvent(t, env, id, "stop")
	time.Sleep(settle)
	// The wrapper gets the time of the end procedure for the stop command.
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateStopped ||
		run.Executions[0].State != dispatcherapi.ExecutionRunning {
		t.Fatalf("before the stop's grace the Run is %s, its Execution %s", run.State, run.Executions[0].State)
	}
	env.Clock.Advance(5 * time.Minute)

	run := waitFor(t, env, ana, id, dispatcherapi.StateStopped)
	e := onlyExecution(t, run)
	if !strings.HasPrefix(e.EndReason, dispatcherapi.EndStopped+": ") || !strings.Contains(e.EndReason, "Runner") {
		t.Errorf("the end reason is %q, want stopped by the Runner", e.EndReason)
	}
	if state, _ := env.Runner.Fake.State(execution); !state.Ended() {
		t.Errorf("the wrapper is %s on the Runner", state)
	}
}
