package dispatcher

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// slowTracker is a Tracker whose backstop is every 5 minutes, and whose
// list of ready tickets takes the time in delay (on the world's clock).
// It counts the reads of each ticket's comments.
type slowTracker struct {
	w     *adminWorld
	mu    sync.Mutex
	delay time.Duration
	reads map[string]int
}

func (*slowTracker) Kind() string { return "slow" }
func (*slowTracker) Signature(http.Header, []byte) ([]byte, []byte, error) {
	return nil, nil, nil
}
func (*slowTracker) Delivery(http.Header, []byte, time.Time) (Delivery, error) { return Delivery{}, nil }
func (*slowTracker) Backstop(WebhookSource) time.Duration                      { return commentsAllEvery }
func (*slowTracker) Defaults() dispatcherapi.TrackerSettings {
	return dispatcherapi.TrackerSettings{ReadyStatus: "Ready"}
}
func (*slowTracker) Ticket(context.Context, TrackerConnection, string) (Ticket, error) {
	return Ticket{}, ErrNoTicket
}
func (s *slowTracker) ReadyTickets(context.Context, TrackerConnection) ([]Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.w.advance(s.delay)
	return nil, nil
}
func (*slowTracker) SetStatus(context.Context, TrackerConnection, string, string) error { return nil }
func (*slowTracker) Comment(context.Context, TrackerConnection, string, string) error   { return nil }
func (s *slowTracker) Comments(_ context.Context, _ TrackerConnection, id string) ([]TicketComment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads[id]++
	return nil, nil
}

// A backstop every 5 minutes reads the comments of each ticket whose Run
// is not Closed at each pass: also when the pass before it took longer
// before its comments part, so that the two reads are less than 5
// minutes apart.
func TestABackstopEvery5MinutesReadsTheCommentsOfAStoppedRunsTicketAtEachPass(t *testing.T) {
	w := newAdminWorld(t)
	s := w.d.store
	if _, err := s.db.Exec(`INSERT INTO connections (id, organization_id, name, kind, token, updated_at, updated_by_id,
		updated_by_name) VALUES ('c1', '7', 'board', 'slow', x'', 0, '1', 'ana@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO runs (id, organization_id, organization_slug, project, prompt, state,
		started_by_id, started_by_name, created_at, ticket_connection_id, ticket_id) VALUES ('run-1', '7', 'example',
		'shop', 'Fix the cart total', ?, '1', 'ana@example.com', ?, 'c1', 'T-1')`, dispatcherapi.StateStopped,
		w.clock().UnixMicro()); err != nil {
		t.Fatal(err)
	}
	tracker := &slowTracker{w: w, reads: map[string]int{}}
	source := WebhookSource{ID: "c1", Name: "board", Kind: "slow"}
	// Pass k waits 2 seconds for its list; pass k+1 comes 5 minutes after
	// pass k started, and its list takes no time.
	for i, delay := range []time.Duration{0, 2 * time.Second, 0} {
		tracker.delay = delay
		if _, err := w.d.reconcileTickets(context.Background(), tracker, source, ""); err != nil {
			t.Fatal(err)
		}
		if got := tracker.reads["T-1"]; got != i+1 {
			t.Fatalf("after pass %d, the comments of the stopped Run's ticket were read %d times, want %d", i+1, got, i+1)
		}
		w.advance(commentsAllEvery - delay)
	}
}

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
