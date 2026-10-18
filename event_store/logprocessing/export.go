package logprocessing

import (
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
)

// retryExportAfter is how soon a failed hourly export runs again.
const retryExportAfter = time.Minute

// Exporter moves the Logs of hour h-2 and earlier to files.
// *storage.LogStore is the production adapter.
type Exporter interface {
	ExportLogs(now time.Time) error
}

// HourlyExport is the timer inside EventStore that runs the hourly export at
// each hour start by its clock, and once when it starts, to catch up on the
// hours it missed while EventStore was down. A failed export runs again a
// minute later; the Logs it didn't move wait in DuckDB meanwhile.
type HourlyExport struct {
	store Exporter
	clock clock.Clock

	*worker
}

// NewHourlyExport constructs the hourly export. It reads the time from clock.
func NewHourlyExport(store Exporter, clock clock.Clock) *HourlyExport {
	return &HourlyExport{store: store, clock: clock, worker: newWorker()}
}

// Start runs the timer in its own goroutine.
func (e *HourlyExport) Start() {
	e.start(e.run)
}

// Stop stops the timer, waiting for an export in progress to end.
func (e *HourlyExport) Stop() {
	e.halt()
}

func (e *HourlyExport) run() {
	for {
		wait := retryExportAfter
		if err := e.store.ExportLogs(e.clock.Now()); err != nil {
			logger.L.Error("Hourly Log export failed, retrying", "error", err, "retry_in", wait)
		} else {
			now := e.clock.Now()
			wait = now.Truncate(time.Hour).Add(time.Hour).Sub(now)
		}
		timer := time.NewTimer(wait)
		select {
		case <-e.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
