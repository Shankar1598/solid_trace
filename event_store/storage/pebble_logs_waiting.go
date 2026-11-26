package storage

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// WaitingLogBatch is a Project's oldest Log batch that Log processing has
// not stored (PebbleWriter.OldestWaitingLogBatch), and how long it has
// waited. A run that asks for hold_if_incomplete and the Overview's Health
// read the same wait.
type WaitingLogBatch struct {
	ProjectID uint32
	BatchID   uuid.UUID
	Waited    time.Duration
}

// waitingSeen is, for each Project, its oldest waiting Log batch as this
// process last read it, and when this process first read that batch as the
// oldest.
type waitingSeen struct {
	mu      sync.Mutex
	batches map[uint32]seenBatch
}

type seenBatch struct {
	id uuid.UUID
	at time.Time
}

// WaitingLogBatch is a Project's oldest waiting Log batch at now, and false
// if none waits. The batch has waited since the time in its id, or since
// this process first read it as the oldest, whichever is longer. A clock
// that was ahead when a batch was written puts the time in its id after
// now. The batch id floor keeps the ids after it there too, also after a
// restart. So with the time in the id alone, a Log processing that has
// stopped is never behind. With the time that this process keeps, it is
// behind once it has not reached the batch for LogProcessingLag. That time
// is now as the caller read it, with its monotonic reading, so a later
// step of the clock does not change the wait.
func (w *PebbleWriter) WaitingLogBatch(projectID uint32, now time.Time) (WaitingLogBatch, bool, error) {
	batchID, waiting, err := w.OldestWaitingLogBatch(projectID)
	if err != nil {
		return WaitingLogBatch{}, false, err
	}
	w.seen.mu.Lock()
	defer w.seen.mu.Unlock()
	if !waiting {
		delete(w.seen.batches, projectID)
		return WaitingLogBatch{}, false, nil
	}
	seen, ok := w.seen.batches[projectID]
	if !ok || seen.id != batchID {
		seen = seenBatch{batchID, now}
		if w.seen.batches == nil {
			w.seen.batches = map[uint32]seenBatch{}
		}
		w.seen.batches[projectID] = seen
	}
	waited := max(now.Sub(LogBatchWrittenAt(batchID)), now.Sub(seen.at))
	return WaitingLogBatch{ProjectID: projectID, BatchID: batchID, Waited: waited}, true, nil
}
