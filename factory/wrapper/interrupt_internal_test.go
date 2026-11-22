package wrapper

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// followed is an Execution whose follow loop reads the output that the
// test writes, and whose stdin lines the test reads.
type followed struct {
	e      *execution
	output *os.File
	lines  chan string
	exited chan struct{}
	ended  chan dispatcherapi.ExecutionEnd
}

func startFollow(t *testing.T) *followed {
	t.Helper()
	e, _ := executionWithDispatcher(t, &fakeBoundary{emptyAfter: 1})
	e.cfg.dir = t.TempDir()
	e.cfg.clock, e.cfg.limitEvery = time.Now, time.Hour
	if err := os.WriteFile(e.cfg.runState(), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// An exited process: follow reads its exit code at the end.
	harness := exec.Command("true")
	if err := harness.Run(); err != nil {
		t.Fatal(err)
	}
	e.stdin, e.harness = stdinW, harness
	f := &followed{e: e, output: stdoutW, lines: make(chan string, 16), exited: make(chan struct{}),
		ended: make(chan dispatcherapi.ExecutionEnd, 1)}
	go func() {
		defer close(f.lines)
		sc := bufio.NewScanner(stdinR)
		for sc.Scan() {
			f.lines <- sc.Text()
		}
	}()
	go func() { f.ended <- e.follow(stdoutR, f.exited) }()
	t.Cleanup(func() {
		f.output.Close()
		close(f.exited)
		select {
		case <-f.ended:
		case <-time.After(10 * time.Second):
			t.Error("the follow loop did not end in 10 s")
		}
	})
	f.next(t, "the task")
	return f
}

// next is the next line on stdin.
func (f *followed) next(t *testing.T, what string) string {
	t.Helper()
	select {
	case line, ok := <-f.lines:
		if !ok {
			t.Fatalf("stdin closed; want %s", what)
		}
		return line
	case <-time.After(10 * time.Second):
		t.Fatalf("no %s on stdin in 10 s", what)
	}
	return ""
}

func interruptWith(seq int64, id, text string) dispatcherapi.Command {
	return dispatcherapi.Command{Seq: seq, Kind: dispatcherapi.CommandInterrupt,
		Message: &dispatcherapi.Message{ID: id, Text: text}}
}

// A second interrupt with a message that comes after the result of the
// first one, before that result released the messages: the result
// releases both messages, and the second interrupt's signal then finds no
// held message. That signal must not end the Execution: each interrupt had
// a message, so the agent works on.
func TestAnInterruptSignalWhoseMessageTheResultReleasedDoesNotEndTheExecution(t *testing.T) {
	f := startFollow(t)
	e := f.e
	e.takeInterrupt(interruptWith(1, "m-1", "Use the new API"))
	if line := f.next(t, "the interrupt request"); !strings.Contains(line, `"subtype":"interrupt"`) {
		t.Fatalf("stdin got %s, want the interrupt request", line)
	}

	// The test holds the run state's lock: the follow loop waits at the
	// result (its turn count) until the test frees it.
	state, err := os.OpenFile(e.cfg.runState(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := syscall.Flock(int(state.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	before := e.lastOutput.Load()
	fmt.Fprintln(f.output, `{"type":"result","subtype":"error_during_execution","is_error":true}`)
	deadline := time.Now().Add(10 * time.Second)
	for e.lastOutput.Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("the follow loop did not take the result in 10 s")
		}
		time.Sleep(time.Millisecond)
	}
	e.takeInterrupt(interruptWith(2, "m-2", "And keep the old one"))
	if err := syscall.Flock(int(state.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"m-1", "m-2"} {
		if line := f.next(t, "the message "+id); !strings.Contains(line, `"uuid":"`+id+`"`) {
			t.Fatalf("stdin got %s, want the message %s", line, id)
		}
	}
	select {
	case line, ok := <-f.lines:
		if !ok {
			t.Fatal("stdin closed after the messages: the Execution ends")
		}
		t.Fatalf("stdin got %s after the messages", line)
	case <-time.After(300 * time.Millisecond):
	}
	if end, ending := e.endedEarly(); ending {
		t.Errorf("the Execution ends with %q, after two interrupts that each had a message", end.Reason)
	}
}
