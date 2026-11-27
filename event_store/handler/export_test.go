package handler

import (
	"time"

	"github.com/solidtrace/event_store/storage"
)

// FailWaitingLogBatchReads makes every read of h for a Project's oldest
// waiting Log batch fail with err, as a Pebble read error does.
func FailWaitingLogBatchReads(h *LogsHandler, err error) {
	h.waiting = failingWaiting{err}
}

type failingWaiting struct{ err error }

func (f failingWaiting) WaitingLogBatch(uint32, time.Time) (storage.WaitingLogBatch, bool, error) {
	return storage.WaitingLogBatch{}, false, f.err
}
