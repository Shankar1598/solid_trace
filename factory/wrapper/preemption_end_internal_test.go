package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// fakeDispatcher records the wrapper's calls by verb, and answers each
// with 204.
type fakeDispatcher struct {
	mu    sync.Mutex
	calls map[string][][]byte
}

func (d *fakeDispatcher) got(verb string) [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls[verb]
}

// executionWithDispatcher is an Execution whose client calls a fake
// Dispatcher.
func executionWithDispatcher(t *testing.T, boundary killBoundary) (*execution, *fakeDispatcher) {
	t.Helper()
	d := &fakeDispatcher{calls: map[string][][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		d.mu.Lock()
		verb := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		d.calls[verb] = append(d.calls[verb], body)
		d.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	identity := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(identity, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config{dispatcherURL: server.URL, execution: "ex-1", identityFile: identity, docker: "/no/docker"}
	return newExecution(context.Background(), cfg, newClient(cfg), dispatcherapi.Task{}, io.Discard, boundary, runUser{}), d
}

// A save that leaves out more files than the end call keeps: the end call
// names maxSkipped of them, and a problem gives the count of the others.
func TestTheEndCallAfterThePreemptionNamesAtMostMaxSkippedFiles(t *testing.T) {
	e, d := executionWithDispatcher(t, &fakeBoundary{emptyAfter: 1})
	var skipped []string
	for i := range maxSkipped + 50 {
		skipped = append(skipped, fmt.Sprintf("build/%d.bin", i))
	}

	e.endPreempted(context.Background(), dispatcherapi.Preemption{Reason: dispatcherapi.EndPreempted, Skipped: skipped})

	ends := d.got("end")
	if len(ends) != 1 {
		t.Fatalf("%d end calls, want 1", len(ends))
	}
	var end dispatcherapi.ExecutionEnd
	if err := json.Unmarshal(ends[0], &end); err != nil || end.Preemption == nil {
		t.Fatalf("the end call %s: %v", ends[0], err)
	}
	if got := end.Preemption.Skipped; len(got) != maxSkipped || !slices.Equal(got, skipped[:maxSkipped]) {
		t.Errorf("the end call names %d skipped files, want the first %d", len(got), maxSkipped)
	}
	if !slices.Contains(end.Preemption.Problems, "the save left out 50 more files") {
		t.Errorf("the problems: %v", end.Preemption.Problems)
	}
}

// A report on the agent's socket after the preempting marker is refused
// before the wrapper sends it: the Dispatcher gets no report call.
func TestAReportAfterTheMarkerDoesNotGoToTheDispatcher(t *testing.T) {
	e, d := executionWithDispatcher(t, &fakeBoundary{emptyAfter: 1})
	e.setMarker(dispatcherapi.EndPreempted)

	w := httptest.NewRecorder()
	e.report(w, httptest.NewRequest(http.MethodPost, "/report",
		strings.NewReader(`{"outcome":"done","summary":"Fixed"}`)))

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "a report now does not count") {
		t.Errorf("the report after the marker: %d %s, want 409, refused before the call", w.Code, w.Body)
	}
	if n := len(d.got("report")); n != 0 {
		t.Errorf("the Dispatcher got %d report calls after the marker", n)
	}
	if e.reported {
		t.Error("the report after the marker counts")
	}
}

// After the preemption handler, the wrapper exits with 0 only when the
// Dispatcher took the end call and the end procedure emptied the
// Execution's cgroup.
func TestTheWrapperExitsWith1WhenTheEndProcedureLeftProcesses(t *testing.T) {
	cases := []struct {
		name       string
		emptyAfter int
		code       int
	}{
		{"the cgroup is empty", 1, 0},
		{"processes are left in the cgroup", 3, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, d := executionWithDispatcher(t, &fakeBoundary{emptyAfter: c.emptyAfter})

			e.endProcedure(context.Background())
			e.endPreempted(context.Background(), dispatcherapi.Preemption{Reason: dispatcherapi.EndPreempted})

			if n := len(d.got("end")); n != 1 {
				t.Fatalf("%d end calls, want 1", n)
			}
			if code := e.preemptionCode(); code != c.code {
				t.Errorf("exit code %d, want %d", code, c.code)
			}
		})
	}
}
