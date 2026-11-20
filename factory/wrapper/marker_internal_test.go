package wrapper

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// After the preempting marker, no message goes to the harness's stdin;
// only the halt's interrupt request does.
func TestAfterTheMarkerNoMessageIsWrittenToStdinButTheInterruptRequestIs(t *testing.T) {
	r, w := io.Pipe()
	e := newExecution(context.Background(), config{}, nil, dispatcherapi.Task{}, io.Discard, nil, runUser{})
	e.stdin = w
	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	if err := e.writeTask([]byte(`{"type":"user","n":1}`)); err != nil {
		t.Fatalf("a message before the marker: %v", err)
	}
	e.setMarker(dispatcherapi.EndPreempted)
	if err := e.writeTask([]byte(`{"type":"user","n":2}`)); !errors.Is(err, errMarked) {
		t.Errorf("a message after the marker: %v, want refused", err)
	}
	// A person's message after the marker is not written: the end call
	// gives it back.
	e.mu.Lock()
	e.stdin = w
	e.mu.Unlock()
	e.openInbox()
	e.takeMessage(dispatcherapi.Message{ID: "m-1", Text: "and the docs too"}, 1)
	if states := e.messageStates(); len(states) != 0 {
		t.Errorf("a person's message after the marker: states %+v, want not written", states)
	}
	if err := e.writeStdin(interruptRequest(), nil); err != nil {
		t.Errorf("the interrupt request after the marker: %v", err)
	}
	e.closeStdin()

	var got []string
	for line := range lines {
		got = append(got, line)
	}
	if len(got) != 2 || got[0] != `{"type":"user","n":1}` || !strings.Contains(got[1], `"subtype":"interrupt"`) {
		t.Errorf("stdin got %q, want the first message, then the interrupt request", got)
	}
	if e.marked.Err() == nil {
		t.Error("the marker did not stop the calls that run")
	}
}

// A harness that does not read its stdin blocks a message write. The
// marker and the close of stdin still return at once, and the close
// stops the write.
func TestAStdinWriteThatIsBlockedDoesNotHoldTheMarkerOrTheClose(t *testing.T) {
	_, w := io.Pipe()
	e := newExecution(context.Background(), config{}, nil, dispatcherapi.Task{}, io.Discard, nil, runUser{})
	e.stdin = w
	written := make(chan error, 1)
	go func() { written <- e.writeTask([]byte(`{"type":"user","n":1}`)) }()
	// The write blocks, because no one reads the pipe.
	select {
	case err := <-written:
		t.Fatalf("the write returned with no reader: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	returned := func(name string, step func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { step(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not return while a stdin write was blocked", name)
		}
	}
	returned("the marker", func() { e.setMarker(dispatcherapi.EndPreempted) })
	if err := e.writeTask([]byte(`{"type":"user","n":2}`)); !errors.Is(err, errMarked) {
		t.Errorf("a message after the marker: %v, want refused", err)
	}
	returned("the close of stdin", e.closeStdin)
	select {
	case err := <-written:
		if err == nil {
			t.Error("the blocked write did not fail when stdin closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the close of stdin did not stop the blocked write")
	}
}
