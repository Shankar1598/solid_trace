package storage

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// These tests check that Retention and a Project's deletion delete the
// promoted sets and popularity counts of the Logs they delete. A Query can't
// see either once the Logs are gone, but a set left behind would start a
// later day's set, and any row left behind keeps the Project listed for
// another cleanup pass.

// setDays returns the UTC days, within a year of October 2026, a Project has
// a stored promoted set for.
func setDays(t *testing.T, store *LogStore, projectID uint32) []string {
	t.Helper()
	sets, err := storedSets(context.Background(), store.db, projectID, october(1, 0, 0).AddDate(-1, 0, 0), october(1, 0, 0).AddDate(1, 0, 0))
	if err != nil {
		t.Fatalf("storedSets: %v", err)
	}
	var days []string
	for _, day := range slices.SortedFunc(maps.Keys(sets), time.Time.Compare) {
		days = append(days, dayString(day))
	}
	return days
}

// counts returns a Project's popularity counts of the UTC day of day.
func counts(t *testing.T, store *LogStore, projectID uint32, day time.Time) map[typedKey]int64 {
	t.Helper()
	ctx := context.Background()
	conn, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer conn.Close()
	counts, err := keyCounts(ctx, conn, projectID, LogDay(day))
	if err != nil {
		t.Fatalf("keyCounts: %v", err)
	}
	return counts
}

// assertDays checks the UTC days a Project has a stored set for.
func assertDays(t *testing.T, store *LogStore, projectID uint32, days ...time.Time) {
	t.Helper()
	var want []string
	for _, day := range days {
		want = append(want, dayString(day))
	}
	if got := setDays(t, store, projectID); !slices.Equal(got, want) {
		t.Errorf("Project %d: expected sets of %v, got %v", projectID, want, got)
	}
}

// assertCounts checks a Project's popularity counts of a day.
func assertCounts(t *testing.T, store *LogStore, projectID uint32, day time.Time, want map[typedKey]int64) {
	t.Helper()
	if got := counts(t, store, projectID, day); !maps.Equal(got, want) {
		t.Errorf("Project %d on %s: expected counts %v, got %v", projectID, dayString(day), want, got)
	}
}

func TestRetentionDeletesTheSetsAndCountsOfTheDaysItDeletes(t *testing.T) {
	store := newTestLogStore(t)
	yesterday, today := october(1, 12, 0), october(2, 12, 0)
	appendLogs(t, store, yesterday, stringLog(yesterday, map[string]string{"user": "ada"}))
	appendLogs(t, store, today, stringLog(today, map[string]string{"region": "eu"}))

	if err := store.DeleteLogDaysBefore(promotionProject, today); err != nil {
		t.Fatalf("DeleteLogDaysBefore: %v", err)
	}

	user, region := typedKey{StringAttribute, "user"}, typedKey{StringAttribute, "region"}
	assertDays(t, store, promotionProject, today)
	assertSet(t, store, today, map[typedKey]bool{user: false, region: false}, today, nil)
	assertCounts(t, store, promotionProject, yesterday, map[typedKey]int64{})
	assertCounts(t, store, promotionProject, today, map[typedKey]int64{region: 1})
}

func TestADayAfterTheDaysRetentionDeletedStartsItsSetAfresh(t *testing.T) {
	store := newTestLogStore(t)
	yesterday, today := october(1, 12, 0), october(2, 12, 0)
	appendLogs(t, store, yesterday, stringLog(yesterday, map[string]string{"user": "ada"}))
	if err := store.DeleteLogDaysBefore(promotionProject, today); err != nil {
		t.Fatalf("DeleteLogDaysBefore: %v", err)
	}

	// No set is left to start today's from.
	appendLogs(t, store, today, stringLog(today, map[string]string{"region": "eu"}))

	assertDays(t, store, promotionProject, today)
	assertSet(t, store, today, map[typedKey]bool{{StringAttribute, "region"}: false}, today, nil)
}

func TestAProjectsDeletionDeletesItsSetsAndCountsAndNoOtherProjects(t *testing.T) {
	store := newTestLogStore(t)
	const deleted uint32 = 456
	yesterday, today := october(1, 12, 0), october(2, 12, 0)
	for _, projectID := range []uint32{promotionProject, deleted} {
		for _, at := range []time.Time{yesterday, today} {
			batch := LogBatch{ID: uuid.New(), Logs: []models.Log{stringLog(at, map[string]string{"user": "ada"})}}
			if err := store.AppendLogBatches(projectID, []LogBatch{batch}, at); err != nil {
				t.Fatalf("AppendLogBatches: %v", err)
			}
		}
	}

	if err := store.DeleteProjectLogs(deleted); err != nil {
		t.Fatalf("DeleteProjectLogs: %v", err)
	}

	user := typedKey{StringAttribute, "user"}
	assertDays(t, store, deleted)
	assertDays(t, store, promotionProject, yesterday, today)
	for _, day := range []time.Time{yesterday, today} {
		assertCounts(t, store, deleted, day, map[typedKey]int64{})
		assertCounts(t, store, promotionProject, day, map[typedKey]int64{user: 1})
	}
	projects, err := store.LogProjects()
	if err != nil {
		t.Fatalf("LogProjects: %v", err)
	}
	if !slices.Equal(projects, []uint32{promotionProject}) {
		t.Errorf("Expected only Project %d to have Logs, got %v", promotionProject, projects)
	}
}
