package dispatcher_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A queued Run waits for an Execution: it is an Active Run, and 30 days
// with no activity do not close it, also across a restart.
func TestAQueuedRunDoesNotCloseAfter30Days(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")

	env.Clock.Advance(31 * 24 * time.Hour)
	env.CheckClosedRuns()
	env.RestartAfter(time.Hour)
	env.CheckClosedRuns()

	a := ana.API("GET", "/api/v1/runs/"+id, nil)
	if a.Status != http.StatusOK {
		t.Fatalf("get the Run: %d %s", a.Status, a.Body)
	}
	var run dispatcherapi.Run
	a.JSON(t, &run)
	if run.State != dispatcherapi.StateQueued || run.ClosedAt != nil {
		t.Errorf("the queued Run is %s and closed at %v after 31 days; want queued and not closed", run.State, run.ClosedAt)
	}
	for _, ev := range env.RunEvents(id) {
		if ev["event.kind"] == "closed" {
			t.Errorf("the queued Run has a closed event: %v", ev)
		}
	}
}
