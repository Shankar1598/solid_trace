// Package logprocessing is Log processing: it moves the Log batches that Log
// ingest stored in Pebble into each Project's hot table in DuckDB.
package logprocessing

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// maxLogsPerCommit is roughly how many Logs one transaction appends for
	// a Project: whole Log batches, at least one, until they reach it.
	maxLogsPerCommit = 10000
	tickInterval     = time.Second
)

// ErrStopped is returned when Stop ends processing.
var ErrStopped = errors.New("log processing stopped")

// Processor is Log processing: one worker, separate from Event processing,
// that reads each Project's Log batches from Pebble after its Log processing
// cursor and appends their Logs to the Project's hot table, moving the cursor
// in the same DuckDB transaction. Only then does it delete the batches from
// Pebble. A crash between the commit and the delete leaves batches at or
// below the cursor, which are never read again.
type Processor struct {
	batches *storage.PebbleWriter
	store   *storage.LogStore
	clock   clock.Clock

	mu      sync.Mutex
	pending map[uint32]struct{} // Projects that may have new Log batches
	wake    chan struct{}

	stop     chan struct{}
	stopOnce sync.Once
	started  bool
	done     chan struct{}
}

// New constructs Log processing. It reads the time from clock.
func New(batches *storage.PebbleWriter, store *storage.LogStore, clock clock.Clock) *Processor {
	return &Processor{
		batches: batches,
		store:   store,
		clock:   clock,
		pending: make(map[uint32]struct{}),
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Prepare runs once, before the worker starts. It deletes the Log batches a
// crash left in Pebble after they were stored, and marks every Project that
// has Log batches as pending.
func (p *Processor) Prepare() error {
	projects, err := p.batches.ProjectsWithLogBatches()
	if err != nil {
		return fmt.Errorf("list projects with log batches: %w", err)
	}
	for _, projectID := range projects {
		cursor, err := p.store.LogProcessingCursor(projectID)
		if err != nil {
			return fmt.Errorf("read log processing cursor of project %d: %w", projectID, err)
		}
		if cursor != uuid.Nil {
			if err := p.batches.DeleteLogBatchesUpTo(projectID, cursor); err != nil {
				return fmt.Errorf("delete stored log batches of project %d: %w", projectID, err)
			}
		}
		p.Notify(projectID)
	}
	return nil
}

// Notify marks a Project as having new Log batches and wakes the worker.
func (p *Processor) Notify(projectID uint32) {
	p.markPending(projectID)
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Processor) markPending(projectID uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending[projectID] = struct{}{}
}

func (p *Processor) takePending() (uint32, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for projectID := range p.pending {
		delete(p.pending, projectID)
		return projectID, true
	}
	return 0, false
}

// Start runs the worker in its own goroutine.
func (p *Processor) Start() {
	p.started = true
	go p.run()
}

// Stop stops the worker and waits for it to exit. Batches not yet committed
// are processed again after a restart, from the cursor.
func (p *Processor) Stop() {
	p.stopOnce.Do(func() { close(p.stop) })
	if p.started {
		<-p.done
	}
}

func (p *Processor) run() {
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
}

// ProcessUntilCaughtUp processes Log batches until no Project has new ones. A
// Project whose Logs fail to store stays pending until the next wake or tick,
// with its cursor unmoved, and the other Projects carry on. It returns the
// first such failure, or ErrStopped.
func (p *Processor) ProcessUntilCaughtUp() error {
	var failed []uint32
	var firstErr error
	defer func() {
		for _, projectID := range failed {
			p.markPending(projectID)
		}
	}()

	for {
		if p.stopped() {
			return ErrStopped
		}
		projectID, ok := p.takePending()
		if !ok {
			return firstErr
		}
		more, err := p.processProject(projectID)
		if err != nil {
			logger.L.Error("Log processing failed, retrying later", "project_id", projectID, "error", err)
			failed = append(failed, projectID)
			if firstErr == nil {
				firstErr = fmt.Errorf("project %d: %w", projectID, err)
			}
			continue
		}
		if more {
			p.markPending(projectID)
		}
	}
}

// processProject stores a Project's Log batches after its cursor, up to about
// maxLogsPerCommit Logs. more reports whether more may be waiting.
func (p *Processor) processProject(projectID uint32) (more bool, err error) {
	cursor, err := p.store.LogProcessingCursor(projectID)
	if err != nil {
		return false, fmt.Errorf("read log processing cursor: %w", err)
	}
	batches, err := p.batches.LogBatchesAfter(projectID, cursor, maxLogsPerCommit)
	if err != nil {
		return false, fmt.Errorf("read log batches: %w", err)
	}
	if len(batches) == 0 {
		return false, nil
	}

	if err := p.store.AppendLogBatches(projectID, batches, p.clock.Now()); err != nil {
		return false, fmt.Errorf("store logs: %w", err)
	}

	last := batches[len(batches)-1].ID
	if err := p.batches.DeleteLogBatchesUpTo(projectID, last); err != nil {
		// The Logs are stored and the cursor is past these batches, so they
		// go with the next delete, or at startup.
		logger.L.Warn("Deleting stored log batches failed", "project_id", projectID, "error", err)
	}

	logs := 0
	for _, batch := range batches {
		logs += len(batch.Logs)
	}
	return logs >= maxLogsPerCommit, nil
}

func (p *Processor) stopped() bool {
	select {
	case <-p.stop:
		return true
	default:
		return false
	}
}
