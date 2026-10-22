package logprocessing

import (
	"errors"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
)

// recordingExporter tells each export's time on runs, and fails the exports
// at the times in failAt. An export at a time in lastsUntil moves clock to
// the time it ends.
type recordingExporter struct {
	runs       chan time.Time
	failAt     map[time.Time]bool
	clock      *clock.Manual
	lastsUntil map[time.Time]time.Time
}

func (e *recordingExporter) ExportLogs(now time.Time) error {
	e.runs <- now
	if end, ok := e.lastsUntil[now]; ok {
		e.clock.Set(end)
	}
	if e.failAt[now] {
		return errors.New("disk full")
	}
	return nil
}

// expectRun waits for the export to run at want.
func expectRun(t *testing.T, store *recordingExporter, want time.Time) {
	t.Helper()
	select {
	case got := <-store.runs:
		if !got.Equal(want) {
			t.Fatalf("Expected the export to run at %v, it ran at %v", want, got)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Expected the export to run at %v, it didn't", want)
	}
}

func TestTheExportRunsAtStartUpAtEachQuarterHourStartAndAMinuteAfterAFailure(t *testing.T) {
	logger.Init()
	at := func(hour, minute, second int) time.Time {
		return time.Date(2026, 10, 22, hour, minute, second, 0, time.UTC)
	}
	clk := clock.NewManual(at(10, 7, 30))
	store := &recordingExporter{runs: make(chan time.Time), failAt: map[time.Time]bool{at(10, 30, 0): true}}
	export := NewLogExport(store, clk)
	export.Start()
	defer export.Stop()

	expectRun(t, store, at(10, 7, 30)) // at start-up

	// A run the clock reaches too early would come before the one expected.
	for _, step := range []struct {
		to   time.Time
		runs bool
	}{
		{at(10, 14, 59), false},
		{at(10, 15, 0), true},
		{at(10, 29, 59), false},
		{at(10, 30, 0), true}, // fails
		{at(10, 30, 59), false},
		{at(10, 31, 0), true},
		{at(10, 44, 59), false},
		{at(10, 45, 0), true},
		{at(11, 0, 0), true},
	} {
		clk.BlockUntilTimers(1)
		clk.Set(step.to)
		if step.runs {
			expectRun(t, store, step.to)
		}
	}
}

func TestARunLastingPastTheNextQuarterHourStartIsFollowedByARunAtOnce(t *testing.T) {
	logger.Init()
	at := func(hour, minute, second int) time.Time {
		return time.Date(2026, 10, 22, hour, minute, second, 0, time.UTC)
	}
	clk := clock.NewManual(at(10, 7, 30))
	store := &recordingExporter{runs: make(chan time.Time), clock: clk,
		lastsUntil: map[time.Time]time.Time{at(10, 15, 0): at(10, 31, 0)}}
	export := NewLogExport(store, clk)
	export.Start()
	defer export.Stop()
	expectRun(t, store, at(10, 7, 30))

	// The run at 10:15 ends at 10:31, after the 10:30 start it was due at,
	// so the next one runs then, and the one after at 10:45.
	clk.BlockUntilTimers(1)
	clk.Set(at(10, 15, 0))
	expectRun(t, store, at(10, 15, 0))
	expectRun(t, store, at(10, 31, 0))
	clk.BlockUntilTimers(1)
	clk.Set(at(10, 44, 59))
	clk.BlockUntilTimers(1)
	clk.Set(at(10, 45, 0))
	expectRun(t, store, at(10, 45, 0))
}
