package wrapper

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// After the preempting marker, no message goes to the harness's stdin;
// only the halt's interrupt request does.
func TestAfterTheMarkerNoMessageIsWrittenToStdinButTheInterruptRequestIs(t *testing.T) {
	r, w := io.Pipe()
	e := newExecution(context.Background(), config{}, nil, dispatcherapi.Task{}, io.Discard, nil)
	e.stdin = w
	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	if err := e.writeMessage([]byte(`{"type":"user","n":1}`)); err != nil {
		t.Fatalf("a message before the marker: %v", err)
	}
	e.setMarker(dispatcherapi.EndPreempted)
	if err := e.writeMessage([]byte(`{"type":"user","n":2}`)); !errors.Is(err, errMarked) {
		t.Errorf("a message after the marker: %v, want refused", err)
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
