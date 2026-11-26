package logprocessing

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type fakeCleanup struct {
	mu           sync.Mutex
	steps        []string
	evidenceRuns []evidenceRun
	daysErr      error
	deleteErr    error
	retentionErr error
}

type evidenceRun struct {
	now       time.Time
	retention map[int64]int
}

func (f *fakeCleanup) step(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, name)
}

func (f *fakeCleanup) done() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.steps)
}

func (f *fakeCleanup) fail(days, delete, retention error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.daysErr, f.deleteErr, f.retentionErr = days, delete, retention
}

func (f *fakeCleanup) DayProjects(storage.DayFamily) ([]uint32, error) { return []uint32{1}, nil }

func (f *fakeCleanup) DeleteDaysBefore(storage.DayFamily, uint32, time.Time) error {
	f.step("days")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.daysErr
}

func (f *fakeCleanup) DeleteProjectDays(storage.DayFamily, uint32) error { return nil }

func (f *fakeCleanup) EnforceDiskLimit(storage.LogDiskLimit, []storage.DayFamily) error {
	f.step("disk limit")
	return nil
}

func (f *fakeCleanup) DeletePastRetention(_ context.Context, now time.Time, retention map[int64]int) (storage.EvidenceSweep, error) {
	f.step("evidence")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evidenceRuns = append(f.evidenceRuns, evidenceRun{now, retention})
	return storage.EvidenceSweep{Deleted: 3, Unreferenced: 1}, f.deleteErr
}

func (f *fakeCleanup) evidenceRetention() (map[int64]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.retentionErr != nil {
		return nil, f.retentionErr
	}
	return map[int64]int{1: 7}, nil
}

type noSQLUsers struct{}

func (noSQLUsers) DropProjectUser(uint32) error { return nil }

var testStart = time.Date(2026, 12, 1, 6, 0, 0, 0, time.UTC)

func newTestCleanup() (*Cleanup, *fakeCleanup, *clock.Manual) {
	logger.Init()
	fake := &fakeCleanup{}
	manual := clock.NewManual(testStart)
	cleanup := NewCleanup(fake, noSQLUsers{}, storage.LogDiskLimit{}, manual,
		&Evidence{Files: fake, Retention: fake.evidenceRetention},
		Family{Days: storage.LogDays, Retention: func() (map[uint32]int, error) { return map[uint32]int{1: 30}, nil }})
	return cleanup, fake, manual
}

func tick(manual *clock.Manual, d time.Duration) {
	manual.Advance(d)
	manual.BlockUntilTimers(1)
}

func expectSteps(t *testing.T, fake *fakeCleanup, want ...string) {
	t.Helper()
	if got := fake.done(); !slices.Equal(got, want) {
		t.Errorf("Expected the steps %q, got %q", want, got)
	}
}

func TestTheDailyPassAtStartRunsEvidenceRetentionAfterTheDays(t *testing.T) {
	cleanup, fake, manual := newTestCleanup()
	cleanup.Start()
	defer cleanup.Stop()
	manual.BlockUntilTimers(1)

	expectSteps(t, fake, "days", "evidence", "disk limit")
	if run := fake.evidenceRuns[0]; !run.now.Equal(testStart) || run.retention[1] != 7 {
		t.Errorf("Expected Evidence Retention at %v with 7 days, got %+v", testStart, run)
	}
}

func TestAnEvidenceFileThatCannotBeDeletedWaitsForTheNextDaysRun(t *testing.T) {
	cleanup, fake, manual := newTestCleanup()
	fake.fail(nil, errors.New("the bucket refused"), nil)
	cleanup.Start()
	defer cleanup.Stop()
	manual.BlockUntilTimers(1)

	tick(manual, time.Minute)
	expectSteps(t, fake, "days", "evidence", "disk limit")
	tick(manual, 4*time.Minute)
	expectSteps(t, fake, "days", "evidence", "disk limit", "disk limit")

	tick(manual, 24*time.Hour)
	expectSteps(t, fake, "days", "evidence", "disk limit", "disk limit", "days", "evidence", "disk limit")
}

func TestTheDaysAndEvidenceRetentionKeepTheirOwnDays(t *testing.T) {
	t.Run("a failed delete of days runs again a minute later, Evidence Retention does not", func(t *testing.T) {
		cleanup, fake, manual := newTestCleanup()
		fake.fail(errors.New("ClickHouse refused"), nil, nil)
		cleanup.Start()
		defer cleanup.Stop()
		manual.BlockUntilTimers(1)

		fake.fail(nil, nil, nil)
		tick(manual, time.Minute)
		expectSteps(t, fake, "days", "evidence", "disk limit", "days", "disk limit")
	})
	t.Run("a failed read of Evidence Retention runs again a minute later, the days do not", func(t *testing.T) {
		cleanup, fake, manual := newTestCleanup()
		fake.fail(nil, nil, errors.New("Console's database is locked"))
		cleanup.Start()
		defer cleanup.Stop()
		manual.BlockUntilTimers(1)

		fake.fail(nil, nil, nil)
		tick(manual, time.Minute)
		expectSteps(t, fake, "days", "disk limit", "evidence", "disk limit")
	})
}

func TestRunDailyPassRunsTheDaysAndEvidenceRetentionNow(t *testing.T) {
	cleanup, fake, _ := newTestCleanup()
	pass, err := cleanup.RunDailyPass()
	if err != nil {
		t.Fatal(err)
	}
	if pass.EvidenceSweep != (storage.EvidenceSweep{Deleted: 3, Unreferenced: 1}) {
		t.Errorf("Expected the sweep's counts, got %+v", pass.EvidenceSweep)
	}
	expectSteps(t, fake, "days", "evidence")
}
