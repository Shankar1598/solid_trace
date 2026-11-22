package wrapper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// After the end of the Execution started, the agent's socket refuses a
// report: the Dispatcher gets no report call, and the Execution has no
// report.
func TestAfterTheEndStartedTheSocketRefusesAReport(t *testing.T) {
	e, d := executionWithDispatcher(t, &fakeBoundary{emptyAfter: 1})
	e.mu.Lock()
	e.ending = true
	e.mu.Unlock()

	w := httptest.NewRecorder()
	e.report(w, httptest.NewRequest(http.MethodPost, "/report", strings.NewReader(`{"outcome":"done","summary":"Fixed"}`)))

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "the Execution is ending") {
		t.Errorf("the report after the end started: %d %s, want 409", w.Code, w.Body)
	}
	if got := d.got("report"); len(got) != 0 {
		t.Errorf("the Dispatcher got %d report calls after the end started, want none", len(got))
	}
	if e.reported {
		t.Error("a report after the end started counts")
	}
}

// After the end of the Execution started, the agent's socket refuses a pr
// call: the wrapper pushes the Run's branch at the end, and the Dispatcher
// gets no pr call.
func TestAfterTheEndStartedTheSocketRefusesAPRCall(t *testing.T) {
	e, d := executionWithDispatcher(t, &fakeBoundary{emptyAfter: 1})
	e.mu.Lock()
	e.ending = true
	e.mu.Unlock()

	w := httptest.NewRecorder()
	e.pr(w, httptest.NewRequest(http.MethodPost, "/pr", strings.NewReader(`{"title":"Fix the cart","description":"Done"}`)), nil)

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "the Execution is ending") {
		t.Errorf("the pr call after the end started: %d %s, want 409", w.Code, w.Body)
	}
	if got := d.got("pr"); len(got) != 0 {
		t.Errorf("the Dispatcher got %d pr calls after the end started, want none", len(got))
	}
}
