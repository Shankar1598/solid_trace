package wrapper_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
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
