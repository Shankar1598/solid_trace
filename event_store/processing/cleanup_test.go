package processing

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
)

// fakeCleanupStores records the deletes the Event cleanup asks for, in order,
// and whether batchLock was held for each.
type fakeCleanupStores struct {
	withEvents []uint32
	existing   map[uint32]int
	failRowsOf uint32

	lock    recordingLock
	deletes []string
}

func (f *fakeCleanupStores) ProjectsWithEvents() ([]uint32, error) { return f.withEvents, nil }

func (f *fakeCleanupStores) ProjectLogRetention() (map[uint32]int, error) { return f.existing, nil }

func (f *fakeCleanupStores) record(step string, projectID uint32) {
	held := "unlocked"
	if f.lock.held {
		held = "locked"
	}
	f.deletes = append(f.deletes, step+" "+string(rune('0'+projectID))+" "+held)
}

func (f *fakeCleanupStores) DeleteProjectEventRows(projectID uint32) error {
	if projectID == f.failRowsOf {
		return errors.New("disk full")
	}
	f.record("rows", projectID)
	return nil
}

func (f *fakeCleanupStores) DeleteProjectIssues(projectID uint32) error {
	f.record("issues", projectID)
	return nil
}

func (f *fakeCleanupStores) DeleteProjectEvents(projectID uint32) error {
	f.record("events", projectID)
	return nil
}

type recordingLock struct {
	mu   sync.Mutex
	held bool
}

func (l *recordingLock) Lock()   { l.mu.Lock(); l.held = true }
func (l *recordingLock) Unlock() { l.held = false; l.mu.Unlock() }

func newTestCleanup(f *fakeCleanupStores) *Cleanup {
	logger.Init()
	return NewCleanup(f, f, f, f, &f.lock, clock.System{})
}

func TestCleanupDeletesTheEventsOfDeletedProjectsOnly(t *testing.T) {
	f := &fakeCleanupStores{withEvents: []uint32{1, 2, 3}, existing: map[uint32]int{2: 30}}

	if err := newTestCleanup(f).DeleteEventsOfDeletedProjects(); err != nil {
		t.Fatalf("DeleteEventsOfDeletedProjects: %v", err)
	}

	// The Events go last, so a failed step leaves the Project for the next pass.
	want := []string{
		"rows 1 locked", "issues 1 locked", "events 1 locked",
		"rows 3 locked", "issues 3 locked", "events 3 locked",
	}
	if !reflect.DeepEqual(f.deletes, want) {
		t.Errorf("deletes = %v, want %v", f.deletes, want)
	}
}

func TestCleanupDeletesNothingWhenTheProjectsTableIsEmpty(t *testing.T) {
	f := &fakeCleanupStores{withEvents: []uint32{1, 2}, existing: map[uint32]int{}}

	if err := newTestCleanup(f).DeleteEventsOfDeletedProjects(); err != nil {
		t.Fatalf("DeleteEventsOfDeletedProjects: %v", err)
	}
	if len(f.deletes) != 0 {
		t.Errorf("deleted %v from an empty projects table", f.deletes)
	}
}

func TestCleanupKeepsAProjectsEventsWhenAnEarlierStepFails(t *testing.T) {
	f := &fakeCleanupStores{withEvents: []uint32{1, 3}, existing: map[uint32]int{2: 30}, failRowsOf: 1}

	err := newTestCleanup(f).DeleteEventsOfDeletedProjects()
	if err == nil {
		t.Fatal("a failed delete reported success")
	}
	want := []string{"rows 3 locked", "issues 3 locked", "events 3 locked"}
	if !reflect.DeepEqual(f.deletes, want) {
		t.Errorf("deletes = %v, want %v", f.deletes, want)
	}
}
