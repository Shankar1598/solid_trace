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
	case <-time.After(haltInterruptWait):
		t.Fatalf("the check had not stopped %s after the marker", haltInterruptWait)
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

// A pr call whose push hangs (the forge's side holds it) when the
// preempting marker comes stops at once, with the forge's side of the
// push, and frees the push before the end for the save of the work.
func TestTheMarkerStopsAPRCallWhosePushHangs(t *testing.T) {
	e, d := executionWithDispatcher(t, &fakeBoundary{emptyAfter: 1})
	e.cfg.dir = t.TempDir()
	root := t.TempDir()
	forge := filepath.Join(root, "forge.git")
	started := filepath.Join(root, "started")
	e.checkout = filepath.Join(root, "checkout")
	e.task.Branch, e.task.CloneURL = "agent/run-1", forge
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git(root, "init", "--quiet", "--bare", forge)
	hook := "#!/bin/sh\necho $$ > " + started + ".tmp && mv " + started + ".tmp " + started + "\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(forge, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	git(root, "init", "--quiet", "-b", e.task.Branch, e.checkout)
	git(e.checkout, "commit", "--quiet", "--allow-empty", "-m", "Fix the cart")

	w := httptest.NewRecorder()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		e.pr(w, httptest.NewRequest(http.MethodPost, "/pr",
			strings.NewReader(`{"title":"Fix","description":"Fixed"}`)), os.Environ())
	}()
	var pid int
	for deadline := time.Now().Add(10 * time.Second); pid == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the push of the pr call did not reach the forge in 10 s")
		}
		if b, err := os.ReadFile(started); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
	}

	marked := time.Now()
	e.setMarker(dispatcherapi.EndPreempted)
	select {
	case <-returned:
	case <-time.After(haltInterruptWait):
		t.Fatalf("the pr call had not stopped %s after the marker", haltInterruptWait)
	}
	if took := time.Since(marked); took > time.Second {
		t.Errorf("the pr call stopped %s after the marker, want at once", took)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("the forge's side of the push (process %d) runs after the marker", pid)
	}
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "the pr call stopped") {
		t.Errorf("the pr call that the marker stopped: %d %s, want 409", w.Code, w.Body)
	}
	if !e.prMu.TryLock() {
		t.Error("the pr call that the marker stopped holds the push before the end")
	} else {
		e.prMu.Unlock()
	}
	if n := len(d.got("pr")); n != 0 {
		t.Errorf("the Dispatcher got %d pr calls after the marker", n)
	}
}
