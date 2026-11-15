package wrapper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// startingBoundary is a kill boundary that starts its commands, so that a
// check runs.
type startingBoundary struct{ fakeBoundary }

func (*startingBoundary) Start(cmd *exec.Cmd) error { return cmd.Start() }

// A check that runs when the preempting marker comes stops at once: its
// process group is gone well before the halt's kill, and it is no record.
func TestTheMarkerStopsARunningCheckAtOnce(t *testing.T) {
	e, _ := executionWithDispatcher(t, &startingBoundary{fakeBoundary{emptyAfter: 1}})
	e.workDir = t.TempDir()
	pidFile := filepath.Join(e.workDir, "pid")

	w := httptest.NewRecorder()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		e.check(w, httptest.NewRequest(http.MethodPost, "/check",
			strings.NewReader(`{"command":"echo $$ > pid.tmp && mv pid.tmp pid && exec sleep 20"}`)), nil)
	}()
	var pid int
	for deadline := time.Now().Add(5 * time.Second); pid == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the check did not start in 5 s")
		}
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
	}

	marked := time.Now()
	e.setMarker(dispatcherapi.EndPreempted)
	select {
	case <-returned:
	case <-time.After(interruptWait):
		t.Fatalf("the check had not stopped %s after the marker", interruptWait)
	}
	if took := time.Since(marked); took > time.Second {
		t.Errorf("the check stopped %s after the marker, want at once", took)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("the check's process %d runs after the marker", pid)
	}
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "the check stopped") {
		t.Errorf("the check that the marker stopped: %d %s, want 409", w.Code, w.Body)
	}
	if len(e.checks) != 0 || len(e.running) != 0 {
		t.Errorf("the check that the marker stopped is a record: %v, running %v", e.checks, e.running)
	}
}

// After the preempting marker, the agent's socket refuses a new check and
// a new pr call: no check runs, and the Dispatcher gets no pr call.
func TestAfterTheMarkerTheSocketRefusesANewCheckAndANewPRCall(t *testing.T) {
	e, d := executionWithDispatcher(t, &startingBoundary{fakeBoundary{emptyAfter: 1}})
	e.workDir = t.TempDir()
	e.setMarker(dispatcherapi.EndPreempted)

	w := httptest.NewRecorder()
	e.check(w, httptest.NewRequest(http.MethodPost, "/check", strings.NewReader(`{"command":"touch ran"}`)), nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "a check now does not count") {
		t.Errorf("the check after the marker: %d %s, want 409", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(e.workDir, "ran")); err == nil {
		t.Error("the check after the marker ran")
	}
	if len(e.checks) != 0 {
		t.Errorf("the check after the marker is a record: %v", e.checks)
	}

	w = httptest.NewRecorder()
	e.pr(w, httptest.NewRequest(http.MethodPost, "/pr",
		strings.NewReader(`{"title":"Fix","description":"Fixed"}`)), nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "opens no PR now") {
		t.Errorf("the pr call after the marker: %d %s, want 409", w.Code, w.Body)
	}
	if n := len(d.got("pr")); n != 0 {
		t.Errorf("the Dispatcher got %d pr calls after the marker", n)
	}
}
