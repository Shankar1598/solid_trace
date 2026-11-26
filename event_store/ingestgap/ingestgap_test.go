package ingestgap_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/ingestgap"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

var start = time.Date(2026, 11, 26, 9, 46, 40, 0, time.UTC)

func openPebble(t *testing.T, path string) *storage.PebbleWriter {
	t.Helper()
	pebble, err := storage.NewPebbleWriter(&config.Config{PebblePath: path})
	if err != nil {
		t.Fatalf("Open Pebble: %v", err)
	}
	return pebble
}

// openAt opens ingest at start on a Pebble whose last heartbeat is lastBeat
// before it, or which has none when lastBeat is 0.
func openAt(t *testing.T, lastBeat time.Duration) ingestgap.Gap {
	t.Helper()
	logger.Init()
	pebble := openPebble(t, filepath.Join(t.TempDir(), "pebble"))
	t.Cleanup(pebble.Close)
	if lastBeat > 0 {
		if err := pebble.WriteIngestBeat(start.Add(-lastBeat)); err != nil {
			t.Fatalf("Write a heartbeat: %v", err)
		}
	}
	gap, stop, err := ingestgap.Open(pebble, clock.NewManual(start))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(stop)
	return gap
}

func TestTheIngestGapHoldsForItsLengthFromOneToTenMinutes(t *testing.T) {
	cases := []struct {
		name     string
		lastBeat time.Duration
		until    time.Time // zero: no gap
	}{
		{"a first start, with no heartbeat", 0, time.Time{}},
		{"a heartbeat 5 s old", 5 * time.Second, time.Time{}},
		{"a heartbeat 15 s old", 15 * time.Second, time.Time{}},
		{"a heartbeat 20 s old", 20 * time.Second, start.Add(time.Minute)},
		{"a heartbeat 5 min old", 5 * time.Minute, start.Add(5 * time.Minute)},
		{"a heartbeat 20 min old", 20 * time.Minute, start.Add(10 * time.Minute)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gap := openAt(t, c.lastBeat)
			if !gap.Until.Equal(c.until) {
				t.Fatalf("Expected the gap to hold until %v, got %v", c.until, gap.Until)
			}
			if c.until.IsZero() {
				if gap.Holds(start) {
					t.Fatal("Expected no hold at the start")
				}
				return
			}
			if !gap.Holds(start) || !gap.Holds(c.until.Add(-time.Millisecond)) || gap.Holds(c.until) {
				t.Fatalf("Expected the gap to hold from the start up to %v only", c.until)
			}
		})
	}
}

// A heartbeat later than the start, as after the clock steps back, is no
// gap.
func TestAHeartbeatAfterTheStartIsNoGap(t *testing.T) {
	if gap := ingestgap.Measure(start.Add(time.Hour), true, start); gap.Holds(start) {
		t.Fatalf("Expected no gap, got one until %v", gap.Until)
	}
}

// Ingest writes a heartbeat as it opens and every 5 s after, so the next
// start measures the gap from the last time ingest was open.
func TestIngestWritesAHeartbeatAtItsOpenAndEveryFiveSeconds(t *testing.T) {
	logger.Init()
	path := filepath.Join(t.TempDir(), "pebble")
	pebble := openPebble(t, path)
	clk := clock.NewManual(start)
	_, stop, err := ingestgap.Open(pebble, clk)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	assertLastBeat(t, pebble, start)

	for beat := 1; beat <= 3; beat++ {
		clk.BlockUntilTimers(1)
		clk.Advance(ingestgap.BeatEvery)
		want := start.Add(time.Duration(beat) * ingestgap.BeatEvery)
		waitForLastBeat(t, pebble, want)
	}
	stop()
	pebble.StopWrites()
	pebble.Close()

	// The next start, 2 min after the last heartbeat, holds 2 min.
	reopened := openPebble(t, path)
	defer reopened.Close()
	last := start.Add(3 * ingestgap.BeatEvery)
	restart := last.Add(2 * time.Minute)
	gap, stopAgain, err := ingestgap.Open(reopened, clock.NewManual(restart))
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	defer stopAgain()
	if want := restart.Add(2 * time.Minute); !gap.Until.Equal(want) {
		t.Fatalf("Expected the gap to hold until %v, got %v", want, gap.Until)
	}
	assertLastBeat(t, reopened, restart)
}

// As ingest closes, it writes a last heartbeat, so the next start measures
// its gap from the close: a deploy's restart of some seconds holds nothing,
// although the last regular heartbeat was up to 5 s before the close.
func TestIngestWritesALastHeartbeatAsItCloses(t *testing.T) {
	logger.Init()
	path := filepath.Join(t.TempDir(), "pebble")
	pebble := openPebble(t, path)
	clk := clock.NewManual(start)
	_, stop, err := ingestgap.Open(pebble, clk)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	clk.BlockUntilTimers(1)
	closed := start.Add(ingestgap.BeatEvery - time.Second)
	clk.Set(closed)
	stop()
	assertLastBeat(t, pebble, closed)

	// After stop, ingest writes no more heartbeats.
	clk.Advance(ingestgap.BeatEvery)
	assertLastBeat(t, pebble, closed)
	pebble.StopWrites()
	pebble.Close()

	// A restart 12 s after the close holds nothing, though it is 16 s after
	// the last regular heartbeat.
	reopened := openPebble(t, path)
	defer reopened.Close()
	restart := closed.Add(12 * time.Second)
	gap, stopAgain, err := ingestgap.Open(reopened, clock.NewManual(restart))
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	defer stopAgain()
	if gap.Holds(restart) {
		t.Fatalf("Expected no gap 12 s after the close, got one until %v", gap.Until)
	}
}

func assertLastBeat(t *testing.T, pebble *storage.PebbleWriter, want time.Time) {
	t.Helper()
	got, ok, err := pebble.LastIngestBeat()
	if err != nil || !ok || !got.Equal(want) {
		t.Fatalf("Expected the last heartbeat at %v, got %v (%v, %v)", want, got, ok, err)
	}
}

func waitForLastBeat(t *testing.T, pebble *storage.PebbleWriter, want time.Time) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _, err := pebble.LastIngestBeat()
		if err == nil && got.Equal(want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Expected the last heartbeat at %v, got %v (%v)", want, got, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
