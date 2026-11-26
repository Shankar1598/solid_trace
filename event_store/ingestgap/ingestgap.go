// Package ingestgap measures how long Log ingest was down before EventStore
// started, from a heartbeat that ingest writes to Pebble while it is open.
//
// While ingest is down, senders keep their Logs and send them again when it
// comes back. Until they have, a Threshold monitor's range misses those
// Logs, and missing Logs look like "no data" or "too few". So after a gap,
// the Logs are not complete for a time that grows with the gap
// (.scratch/monitors-after-restart/spec.md).
package ingestgap

import (
	"context"
	"errors"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// BeatEvery is how often ingest writes its heartbeat.
	BeatEvery = 5 * time.Second
	// minGap is the shortest gap that holds. A deploy's restart of
	// EventStore takes some seconds, and holds nothing.
	minGap = 15 * time.Second
	// A gap holds for its own length, at least minHold and at most maxHold.
	minHold = time.Minute
	maxHold = 10 * time.Minute
)

// Beats is where the heartbeat is kept: the Pebble writer.
type Beats interface {
	LastIngestBeat() (time.Time, bool, error)
	WriteIngestBeat(at time.Time) error
}

// Gap is the ingest gap of the last start: the Logs are not complete until
// Until. The zero Gap holds nothing.
type Gap struct {
	Until time.Time
}

// Holds reports whether the gap holds at now.
func (g Gap) Holds(now time.Time) bool {
	return now.Before(g.Until)
}

// Measure is the gap of an ingest port that opened at opened, when the last
// heartbeat was at last (beat false for none, as at a first start).
func Measure(last time.Time, beat bool, opened time.Time) Gap {
	gap := opened.Sub(last)
	if !beat || gap <= minGap {
		return Gap{}
	}
	return Gap{Until: opened.Add(min(max(gap, minHold), maxHold))}
}

// Open runs as the ingest port opens. It measures the gap since the last
// heartbeat, writes a heartbeat at once and then every BeatEvery until ctx
// is done. A heartbeat that it cannot read gives no gap, and the error.
func Open(ctx context.Context, beats Beats, clk clock.Clock) (Gap, error) {
	opened := clk.Now()
	last, beat, err := beats.LastIngestBeat()
	gap := Measure(last, beat, opened)
	if gap.Holds(opened) {
		logger.L.Info("Ingest was down before this start: Monitors that ask wait for the senders' kept Logs",
			"last_heartbeat", last.UTC(), "gap", opened.Sub(last).Round(time.Second).String(), "until", gap.Until.UTC())
	}
	write(beats, opened)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-clk.After(BeatEvery):
				write(beats, now)
			}
		}
	}()
	return gap, err
}

func write(beats Beats, at time.Time) {
	if err := beats.WriteIngestBeat(at); err != nil && !errors.Is(err, storage.ErrShuttingDown) {
		logger.L.Warn("Writing the ingest heartbeat failed", "error", err)
	}
}
