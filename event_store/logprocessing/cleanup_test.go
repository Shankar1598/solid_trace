package logprocessing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// fakeCleanupStore has one Project with Logs, and records the days it is
// asked to delete.
type fakeCleanupStore struct {
	mu      sync.Mutex
	deleted []time.Time
}

func (f *fakeCleanupStore) DayProjects(storage.DayFamily) ([]uint32, error) { return []uint32{1}, nil }

func (f *fakeCleanupStore) DeleteDaysBefore(_ storage.DayFamily, _ uint32, day time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, day)
	return nil
}

func (f *fakeCleanupStore) DeleteProjectDays(storage.DayFamily, uint32) error { return nil }

func (f *fakeCleanupStore) EnforceDiskLimit(storage.LogDiskLimit, []storage.DayFamily) error {
	return nil
}

type noSQLUsers struct{}

func (noSQLUsers) DropProjectUser(uint32) error { return nil }

// fakeEvidence records each run of Evidence Retention.
type fakeEvidence struct {
	runs chan evidenceRun
}

type evidenceRun struct {
	now       time.Time
	retention map[int64]int
}

func (f *fakeEvidence) DeletePastRetention(_ context.Context, now time.Time, retention map[int64]int) (storage.EvidenceSweep, error) {
	f.runs <- evidenceRun{now, retention}
	return storage.EvidenceSweep{Deleted: 3, Unreferenced: 1}, nil
}

func newTestCleanup(now time.Time) (*Cleanup, *fakeCleanupStore, *fakeEvidence) {
	logger.Init()
	store := &fakeCleanupStore{}
	evidence := &fakeEvidence{runs: make(chan evidenceRun, 10)}
	cleanup := NewCleanup(store, noSQLUsers{}, storage.LogDiskLimit{}, clock.NewManual(now),
		Family{Days: storage.LogDays, Retention: func() (map[uint32]int, error) { return map[uint32]int{1: 30}, nil }})
	cleanup.Evidence = &Evidence{Files: evidence, Retention: func() (map[int64]int, error) { return map[int64]int{1: 7}, nil }}
	return cleanup, store, evidence
}

func TestTheDailyPassAtStartRunsEvidenceRetentionAfterTheDays(t *testing.T) {
	now := time.Date(2026, 12, 1, 6, 0, 0, 0, time.UTC)
	cleanup, store, evidence := newTestCleanup(now)
	cleanup.Start()
	defer cleanup.Stop()

	select {
	case run := <-evidence.runs:
		if !run.now.Equal(now) || run.retention[1] != 7 {
			t.Errorf("Expected Evidence Retention at %v with 7 days, got %+v", now, run)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Expected Evidence Retention in the daily pass at start")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleted) != 1 {
		t.Errorf("Expected the days past Retention deleted first, got %v", store.deleted)
	}
}

func TestRunDailyPassRunsTheDaysAndEvidenceRetentionNow(t *testing.T) {
	now := time.Date(2026, 12, 1, 6, 0, 0, 0, time.UTC)
	cleanup, store, evidence := newTestCleanup(now)
	pass, err := cleanup.RunDailyPass()
	if err != nil {
		t.Fatal(err)
	}
	if pass.EvidenceSweep != (storage.EvidenceSweep{Deleted: 3, Unreferenced: 1}) {
		t.Errorf("Expected the sweep's counts, got %+v", pass.EvidenceSweep)
	}
	if len(store.deleted) != 1 || len(evidence.runs) != 1 {
		t.Errorf("Expected one delete of days and one Evidence Retention, got %v and %d", store.deleted, len(evidence.runs))
	}
}
