package dispatcher

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Each state of activeStates makes a schedule's last Run active, also a
// state that a later ticket adds to the list.
func TestEachStateOfActiveStatesMakesAScheduleRunActive(t *testing.T) {
	const later = "waiting_on_an_account_limit"
	t.Cleanup(func(states []string) func() { return func() { activeStates = states } }(activeStates))
	activeStates = append(activeStates[:len(activeStates):len(activeStates)], later)

	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	for i, state := range append([]string{dispatcherapi.StateFailed}, activeStates...) {
		scheduleID := fmt.Sprintf("schedule%d", i)
		run := storedRun{Run: dispatcherapi.Run{ID: fmt.Sprintf("run%d", i), Organization: "example", Project: "shop",
			Prompt: "Update the dependencies", State: state, StartedBy: dispatcherapi.Person{Kind: actorSchedule,
				ID: scheduleID, Name: "Daily"}, CreatedAt: at},
			organizationID: "1", projectID: "2", repository: "bitbucket.org/example/shop", trigger: triggerSchedule,
			scheduleID: scheduleID, firedAt: at}
		if err := s.insertRun(run); err != nil {
			t.Fatal(err)
		}
		tx, err := s.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		id, err := activeRunOf(tx, scheduleID)
		tx.Rollback()
		switch {
		case state == dispatcherapi.StateFailed:
			if !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("a failed Run is active: %q, %v", id, err)
			}
		case err != nil || id != run.ID:
			t.Errorf("a Run in the state %s is not active: %q, %v", state, id, err)
		}
	}
}
