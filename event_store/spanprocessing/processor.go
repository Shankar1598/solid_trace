// Package spanprocessing is Span processing: it moves the Transactions that
// envelope ingest stored in Pebble into ClickHouse's spans and trace_index
// tables.
package spanprocessing

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// maxBytesPerInsert is roughly how many bytes of Transactions one
	// insert takes for a Project: whole batches, at least one.
	maxBytesPerInsert = 8 << 20
	tickInterval      = time.Second

	minBackoff = 100 * time.Millisecond
	maxBackoff = 30 * time.Second
)

// ErrStopped is returned when Stop ends processing.
var ErrStopped = errors.New("span processing stopped")

// Store is ClickHouse's Span tables as Span processing uses them.
// *storage.ClickHouse is the production adapter.
type Store interface {
	InsertSpans(projectID uint32, insert storage.SpanInsert) error
}

// Processor is Span processing: one worker that reads each Project's
// Transaction batches from Pebble after its cursor and inserts their Spans
// into ClickHouse, each once, as Log processing does its Logs:
//  1. it records in Pebble the range of batches it is about to insert;
//  2. it inserts their Spans, with a dedup token made from the range;
//  3. it moves the cursor past the range and deletes the batches.
//
// After a crash or a failed insert, the recorded range is inserted again
// with the same token, and ClickHouse drops what it already has.
//
// A Transaction that can't be read, or whose times can't be stored, is
// skipped and logged when its batch is read, so an insert never fails on
// what it holds. A Project whose insert fails is retried with backoff while
// the others carry on. A Transaction never makes an Issue.
type Processor struct {
	batches *storage.PebbleWriter
	store   Store

	mu      sync.Mutex
	pending map[uint32]struct{}
	wake    chan struct{}

	// Used only by the worker.
	failing map[uint32]*failure

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
	started  bool
}

// failure is when a Project whose Spans failed to store is tried again.
type failure struct {
	retryAt time.Time
	backoff time.Duration
}

// New constructs Span processing.
func New(batches *storage.PebbleWriter, store Store) *Processor {
	return &Processor{
		batches: batches,
		store:   store,
		pending: map[uint32]struct{}{},
		wake:    make(chan struct{}, 1),
		failing: map[uint32]*failure{},
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Prepare runs once, before the worker starts and before ingest writes. It
// keeps new batch ids above every Project's cursor, deletes the batches a
// crash left in Pebble after they were stored, and marks every Project with
// batches as pending.
func (p *Processor) Prepare() error {
	if err := p.batches.KeepBatchIDsAboveSpanCursors(); err != nil {
		return fmt.Errorf("read span processing cursors: %w", err)
	}
	projects, err := p.batches.ProjectsWithTransactionBatches()
	if err != nil {
		return fmt.Errorf("list projects with transaction batches: %w", err)
	}
	for _, projectID := range projects {
		cursor, err := p.batches.SpanProcessingCursor(projectID)
		if err != nil {
			return fmt.Errorf("read span processing cursor of project %d: %w", projectID, err)
		}
		if cursor != uuid.Nil {
			if err := p.batches.DeleteTransactionBatchesUpTo(projectID, cursor); err != nil {
				return fmt.Errorf("delete stored transaction batches of project %d: %w", projectID, err)
			}
		}
		p.Notify(projectID)
	}
	return nil
}

// Notify marks a Project as having new Transaction batches and wakes the
// worker.
func (p *Processor) Notify(projectID uint32) {
	p.mu.Lock()
	p.pending[projectID] = struct{}{}
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Start runs the worker in its own goroutine.
func (p *Processor) Start() {
	p.started = true
	go func() {
		defer close(p.done)
		tick := time.NewTicker(tickInterval)
		defer tick.Stop()
		for {
			if err := p.ProcessUntilCaughtUp(); errors.Is(err, ErrStopped) {
				return
			}
			select {
			case <-p.stop:
				return
			case <-p.wake:
			case <-tick.C:
			}
		}
	}()
}

// Stop stops the worker and waits for it to exit. Batches not yet inserted
// are processed after a restart, from the cursor.
func (p *Processor) Stop() {
	p.stopOnce.Do(func() { close(p.stop) })
	if p.started {
		<-p.done
	}
}

// ProcessUntilCaughtUp processes Transaction batches until no Project has new
// ones. A failing Project is retried after its backoff, so it only returns
// ErrStopped.
func (p *Processor) ProcessUntilCaughtUp() error {
	for {
		select {
		case <-p.stop:
			return ErrStopped
		default:
		}
		projectID, ok, retryAt := p.takeDue()
		if ok {
			p.processProject(projectID)
			continue
		}
		if retryAt.IsZero() {
			return nil
		}
		timer := time.NewTimer(time.Until(retryAt))
		select {
		case <-p.stop:
			timer.Stop()
			return ErrStopped
		case <-p.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// takeDue takes a pending Project that isn't waiting to be retried, or
// returns the earliest retry time, zero if none is pending.
func (p *Processor) takeDue() (projectID uint32, ok bool, retryAt time.Time) {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for projectID := range p.pending {
		failed := p.failing[projectID]
		if failed == nil || !failed.retryAt.After(now) {
			delete(p.pending, projectID)
			return projectID, true, time.Time{}
		}
		if retryAt.IsZero() || failed.retryAt.Before(retryAt) {
			retryAt = failed.retryAt
		}
	}
	return 0, false, retryAt
}

// processProject stores a Project's next Spans, or has it retried later.
func (p *Processor) processProject(projectID uint32) {
	more, err := p.storeSpans(projectID)
	if err != nil {
		failed := p.failing[projectID]
		if failed == nil {
			failed = &failure{backoff: minBackoff}
			p.failing[projectID] = failed
		}
		logger.L.Error("Span processing failed, retrying", "project_id", projectID, "error", err, "retry_in", failed.backoff)
		failed.retryAt = time.Now().Add(failed.backoff)
		failed.backoff = min(2*failed.backoff, maxBackoff)
		more = true
	} else {
		delete(p.failing, projectID)
	}
	if more {
		p.mu.Lock()
		p.pending[projectID] = struct{}{}
		p.mu.Unlock()
	}
}

// storeSpans stores the Spans of a Project's batches after its cursor, up to
// about maxBytesPerInsert: or, while the range it last recorded is past the
// cursor, that range again. more reports whether more may be waiting.
func (p *Processor) storeSpans(projectID uint32) (more bool, err error) {
	cursor, err := p.batches.SpanProcessingCursor(projectID)
	if err != nil {
		return false, fmt.Errorf("read span processing cursor: %w", err)
	}
	recorded, err := p.batches.RecordedSpanInsert(projectID)
	if err != nil {
		return false, fmt.Errorf("read the recorded range of transaction batches: %w", err)
	}
	batches, err := p.batches.TransactionBatchesAfter(projectID, cursor, maxBytesPerInsert)
	if err != nil {
		return false, fmt.Errorf("read transaction batches: %w", err)
	}
	// The recorded range may have been inserted, whole or in part: it goes
	// again, the same batches under the same token, never a longer range.
	again := bytes.Compare(recorded[:], cursor[:]) > 0
	if again {
		for i, batch := range batches {
			if bytes.Compare(batch.ID[:], recorded[:]) > 0 {
				batches = batches[:i]
				break
			}
		}
		again = len(batches) > 0
	}
	if len(batches) == 0 {
		return false, nil
	}
	last := batches[len(batches)-1].ID
	if !again {
		if err := p.batches.RecordSpanInsert(projectID, last); err != nil {
			return false, fmt.Errorf("record the range of transaction batches: %w", err)
		}
	}

	var spans []models.Span
	for _, batch := range batches {
		for _, transaction := range batch.Transactions {
			rows, err := spansOf(transaction)
			if err != nil {
				logger.L.Error("Span processing skipped a Transaction", "project_id", projectID, "batch_id", batch.ID.String(), "error", err)
				continue
			}
			spans = append(spans, rows...)
		}
	}
	if err := p.store.InsertSpans(projectID, storage.SpanInsert{After: cursor, UpTo: last, Spans: spans}); err != nil {
		return false, fmt.Errorf("store spans: %w", err)
	}
	if err := p.batches.SetSpanProcessingCursor(projectID, last); err != nil {
		return false, fmt.Errorf("move span processing cursor: %w", err)
	}
	if err := p.batches.DeleteTransactionBatchesUpTo(projectID, last); err != nil {
		logger.L.Warn("Deleting stored transaction batches failed", "project_id", projectID, "error", err)
	}
	// Batches may be waiting past this range: the next pass finds out.
	return true, nil
}
