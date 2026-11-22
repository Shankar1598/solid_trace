package wrapper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// After the end of the Execution started, the agent's socket refuses a
// new check: no check runs, and it is no record.
func TestAfterTheEndStartedTheSocketRefusesANewCheck(t *testing.T) {
	e, _ := executionWithDispatcher(t, &startingBoundary{fakeBoundary{emptyAfter: 1}})
	e.workDir = t.TempDir()
	e.mu.Lock()
	e.ending = true
	e.mu.Unlock()

	w := httptest.NewRecorder()
	e.check(w, httptest.NewRequest(http.MethodPost, "/check", strings.NewReader(`{"command":"touch ran"}`)), nil)

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "the Execution is ending") {
		t.Errorf("the check after the end started: %d %s, want 409", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(e.workDir, "ran")); err == nil {
		t.Error("a check ran after the end started")
	}
	if len(e.checks) != 0 {
		t.Errorf("a check after the end started is a record: %v", e.checks)
	}
}
