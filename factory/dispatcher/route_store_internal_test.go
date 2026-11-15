package dispatcher

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A ticket whose Run is queued, running or waiting on a person has an
// Active Run: a new edit of the ticket starts no second Run. A ticket
// whose Run is done or failed can start a Run of a new edit.
func TestOnlyATicketWithAnActiveRunOrARunOfItsEditHasARun(t *testing.T) {
	active := map[string]bool{
		dispatcherapi.StateQueued:          true,
		dispatcherapi.StateRunning:         true,
		dispatcherapi.StateWaitingOnPerson: true,
		dispatcherapi.StateDone:            false,
		dispatcherapi.StateFailed:          false,
	}
	for state, want := range active {
		t.Run(state, func(t *testing.T) {
			s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
			run := storedRun{Run: dispatcherapi.Run{ID: "run1", Organization: "example", Project: "shop", Prompt: "Fix it",
				State: state, StartedBy: dispatcherapi.Person{Kind: dispatcherapi.ActorTracker, ID: "u-7", Name: "Rosa"},
				CreatedAt: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)},
				organizationID: "7", projectID: "101", repository: "bitbucket.org/example/shop", trigger: triggerTracker,
				ticketConnectionID: "c1", ticketID: "T-1", ticketEdit: "1"}
			if err := s.insertRun(run); err != nil {
				t.Fatal(err)
			}
			if seen, err := s.ticketRun("c1", Ticket{ID: "T-1", Edit: "1"}); err != nil || !seen {
				t.Errorf("the Run's own edit: %v, %v; want a Run", seen, err)
			}
			if seen, err := s.ticketRun("c1", Ticket{ID: "T-1", Edit: "2"}); err != nil || seen != want {
				t.Errorf("a new edit with a %s Run: %v, %v; want %v", state, seen, err, want)
			}
			second := run
			second.ID, second.State, second.ticketEdit = "run2", dispatcherapi.StateQueued, "2"
			if inserted, err := s.insertTicketRun(second); err != nil || inserted == want {
				t.Errorf("the insert of a Run of a new edit with a %s Run: %v, %v; want %v", state, inserted, err, !want)
			}
		})
	}
}
