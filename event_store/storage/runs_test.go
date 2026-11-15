package storage

import (
	"testing"

	"github.com/solidtrace/event_store/models"
)

func runEvent(kind, state, prURL, prOutcome string) models.Log {
	return models.Log{Attributes: models.Attributes{
		String: map[string]string{"run.id": "r1", "event.kind": kind, "run.state": state, "run.pr.url": prURL, "run.pr.outcome": prOutcome},
		Number: map[string]float64{"run.seq": 1},
	}}
}

// A Run is Active while an Execution is queued or live, while it waits on a
// person or an account limit, or while its PR is open; Closed after a
// closed event or a PR outcome.
func TestRunSummaryOfActiveAndClosed(t *testing.T) {
	cases := []struct {
		name                        string
		kind, state, prURL, outcome string
		active, closed              bool
	}{
		{"queued", "queued", "queued", "", "", true, false},
		{"running", "execution_started", "running", "", "", true, false},
		{"waiting on a person", "waiting", "waiting_on_person", "", "", true, false},
		{"waiting on an account limit", "waiting", "waiting_on_account_limit", "", "", true, false},
		{"taken over", "taken_over", "taken_over", "", "", true, false},
		{"done with an open PR", "pr_opened", "done", "https://forge.example/pr/1", "", true, false},
		{"done with no PR", "execution_finished", "done", "", "", false, false},
		{"failed", "execution_finished", "failed", "", "", false, false},
		{"stopped", "stopped", "stopped", "", "", false, false},
		{"PR merged", "pr_outcome", "done", "https://forge.example/pr/1", "merged", false, true},
		{"PR declined", "pr_outcome", "done", "https://forge.example/pr/1", "declined", false, true},
		{"closed event", "closed", "running", "", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := RunSummaryOf(runEvent(c.kind, c.state, c.prURL, c.outcome))
			if r.Active != c.active || r.Closed != c.closed {
				t.Fatalf("Expected Active=%v Closed=%v, got Active=%v Closed=%v", c.active, c.closed, r.Active, r.Closed)
			}
		})
	}
}
