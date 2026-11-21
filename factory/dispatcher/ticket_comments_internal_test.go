package dispatcher

import (
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A comment goes to the ticket's newest Run. When that Run is Closed,
// the comment goes to no Run: an older Run of the ticket that is not
// Closed does not come back.
func TestACommentGoesToNoRunWhenTheTicketsNewestRunIsClosed(t *testing.T) {
	w := newAdminWorld(t)
	s := w.d.store
	at := w.clock()
	addRun := func(id string, created time.Time) {
		t.Helper()
		if _, err := s.db.Exec(`INSERT INTO runs (id, organization_id, organization_slug, project, prompt, state,
			started_by_id, started_by_name, created_at, ticket_connection_id, ticket_id) VALUES (?, '7', 'example', 'shop',
			'Fix the cart total', ?, '1', 'ana@example.com', ?, 'c1', 'T-1')`, id, dispatcherapi.StateFailed,
			created.UnixMicro()); err != nil {
			t.Fatal(err)
		}
	}
	addRun("run-old", at)
	addRun("run-new", at.Add(time.Hour))
	if run, ok, err := s.commentRun("c1", "T-1"); err != nil || !ok || run.id != "run-new" {
		t.Fatalf("with two open Runs, the comments go to %q (%v %v), want run-new", run.id, ok, err)
	}

	if _, err := s.db.Exec(`UPDATE runs SET closed_at = ?, pr_outcome = 'merged' WHERE id = 'run-new'`,
		at.Add(2*time.Hour).UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if run, ok, err := s.commentRun("c1", "T-1"); err != nil || ok {
		t.Fatalf("the newest Run is Closed, but the comments go to %q (%v %v)", run.id, ok, err)
	}
	if run := runState(t, s, "run-old"); run.State != dispatcherapi.StateFailed {
		t.Errorf("the old Run is %s, want failed", run.State)
	}
}
