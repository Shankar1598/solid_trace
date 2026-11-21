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

// Each state of an Active Run makes a schedule's last Run active: it
// works, or it waits on a person or on an account limit. A failed Run is
// not active.
func TestEachStateOfAnActiveRunMakesAScheduleRunActive(t *testing.T) {
	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	for i, state := range []string{dispatcherapi.StateFailed, dispatcherapi.StateQueued, dispatcherapi.StateRunning,
		dispatcherapi.StateWaitingOnPerson, dispatcherapi.StateWaitingOnAccountLimit} {
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
