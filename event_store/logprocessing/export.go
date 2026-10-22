package logprocessing

import (
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// exportEvery is how often the Log export runs: at each quarter-hour start.
const exportEvery = storage.QuarterHour

// retryExportAfter is how soon a failed Log export runs again.
const retryExportAfter = time.Minute

// Exporter moves the Logs that have left the hot window to files.
// *storage.LogStore is the production adapter.
type Exporter interface {
	ExportLogs(now time.Time) error
}

// LogExport is the timer inside EventStore that runs the Log export at each
// quarter-hour start by its clock, and once when it starts, to catch up on
// the quarter-hours it missed while EventStore was down. A run that lasts past
// the next quarter-hour start is followed by one at once. A failed export runs
// again a minute later; the Logs it didn't move wait in DuckDB meanwhile.
type LogExport struct {
	store Exporter
	clock clock.Clock

	*worker
}

// NewLogExport constructs the Log export. It reads the time from clock.
func NewLogExport(store Exporter, clock clock.Clock) *LogExport {
	return &LogExport{store: store, clock: clock, worker: newWorker()}
}

// Start runs the timer in its own goroutine.
func (e *LogExport) Start() {
	e.start(e.run)
}

// Stop stops the timer, waiting for an export in progress to end.
func (e *LogExport) Stop() {
	e.halt()
}

func (e *LogExport) run() {
	for {
		wait := retryExportAfter
		start := e.clock.Now()
		if err := e.store.ExportLogs(start); err != nil {
			logger.L.Error("Log export failed, retrying", "error", err, "retry_in", wait)
		} else {
			// The next run is at the quarter-hour start after this run's
			// start, or at once if a long run has passed it.
			wait = start.Truncate(exportEvery).Add(exportEvery).Sub(e.clock.Now())
		}
		select {
		case <-e.stop:
			return
		case <-e.clock.After(wait):
		}
	}
}
