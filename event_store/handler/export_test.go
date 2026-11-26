package handler

import "github.com/google/uuid"

// FailWaitingLogBatchReads makes every read of h for a Project's oldest
// waiting Log batch fail with err, as a Pebble read error does.
func FailWaitingLogBatchReads(h *LogsHandler, err error) {
	h.waiting = failingWaiting{err}
}

type failingWaiting struct{ err error }

func (f failingWaiting) OldestWaitingLogBatch(uint32) (uuid.UUID, bool, error) {
	return uuid.Nil, false, f.err
}
