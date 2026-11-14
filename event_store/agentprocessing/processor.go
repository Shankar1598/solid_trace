// Package agentprocessing is Agent file processing: it moves the units of
// Agent sessions' files that the agent ingest API stored in Pebble into
// ClickHouse's agent_session_files, the raw layer (ADR 0020).
package agentprocessing

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// maxBytesPerInsert is roughly how many bytes of units one insert takes
	// for a Project: whole batches, at least one.
	maxBytesPerInsert = 16 << 20
	tickInterval      = time.Second

	minBackoff = 100 * time.Millisecond
	maxBackoff = 30 * time.Second
)

// ErrStopped is returned when Stop ends processing.
var ErrStopped = errors.New("agent file processing stopped")

// Store is ClickHouse's raw table of Agent sessions as Agent file
// processing uses it. *storage.ClickHouse is the production adapter.
type Store interface {
	InsertAgentFiles(projectID uint32, insert storage.AgentFileInsert) error
}

// Processor is Agent file processing: one worker that reads each Project's
// Agent file batches from Pebble after its cursor and inserts them into
// ClickHouse, each once, as Span processing does its Spans:
//  1. it records in Pebble the range of batches it is about to insert;
//  2. it inserts their units, with a dedup token made from the range;
//  3. it moves the cursor past the range and deletes the batches.
//
// After a crash or a failed insert, the recorded range is inserted again
// with the same token, and ClickHouse drops what it already has. A Project
// whose insert fails is retried with backoff while the others carry on.
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

// failure is when a Project whose units failed to store is tried again.
type failure struct {
	retryAt time.Time
	backoff time.Duration
}

// New constructs Agent file processing.
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

// Prepare runs once, before the worker starts and before the agent ingest
// API writes. It keeps new batch ids above every Project's cursor, deletes
// the batches a crash left in Pebble after they were stored, and marks
// every Project with batches as pending.
func (p *Processor) Prepare() error {
	if err := p.batches.KeepBatchIDsAboveAgentFileCursors(); err != nil {
		return fmt.Errorf("read agent file processing cursors: %w", err)
	}
	projects, err := p.batches.ProjectsWithAgentFileBatches()
	if err != nil {
		return fmt.Errorf("list projects with agent file batches: %w", err)
	}
	for _, projectID := range projects {
		cursor, err := p.batches.AgentFileCursor(projectID)
		if err != nil {
			return fmt.Errorf("read agent file processing cursor of project %d: %w", projectID, err)
		}
		if cursor != uuid.Nil {
			if err := p.batches.DeleteAgentFileBatchesUpTo(projectID, cursor); err != nil {
				return fmt.Errorf("delete stored agent file batches of project %d: %w", projectID, err)
			}
		}
		p.Notify(projectID)
	}
	return nil
}

// Notify marks a Project as having new Agent file batches and wakes the
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

// ProcessUntilCaughtUp processes Agent file batches until no Project has
// new ones. A failing Project is retried after its backoff, so it only
// returns ErrStopped.
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

// processProject stores a Project's next units, or has it retried later.
func (p *Processor) processProject(projectID uint32) {
	more, err := p.storeUnits(projectID)
	if err != nil {
		failed := p.failing[projectID]
		if failed == nil {
			failed = &failure{backoff: minBackoff}
			p.failing[projectID] = failed
		}
		logger.L.Error("Agent file processing failed, retrying", "project_id", projectID, "error", err, "retry_in", failed.backoff)
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

// storeUnits stores the units of a Project's batches after its cursor, up
// to about maxBytesPerInsert: or, while the range it last recorded is past
// the cursor, that range again. more reports whether more may be waiting.
func (p *Processor) storeUnits(projectID uint32) (more bool, err error) {
	cursor, err := p.batches.AgentFileCursor(projectID)
	if err != nil {
		return false, fmt.Errorf("read agent file processing cursor: %w", err)
	}
	recorded, err := p.batches.RecordedAgentFileInsert(projectID)
	if err != nil {
		return false, fmt.Errorf("read the recorded range of agent file batches: %w", err)
	}
	batches, err := p.batches.AgentFileBatchesAfter(projectID, cursor, maxBytesPerInsert)
	if err != nil {
		return false, fmt.Errorf("read agent file batches: %w", err)
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
		if err := p.batches.RecordAgentFileInsert(projectID, last); err != nil {
			return false, fmt.Errorf("record the range of agent file batches: %w", err)
		}
	}

	units := make([]storage.AgentFileUnit, len(batches))
	for i, batch := range batches {
		units[i] = batch.Unit
	}
	if err := p.store.InsertAgentFiles(projectID, storage.AgentFileInsert{After: cursor, UpTo: last, Units: units}); err != nil {
		return false, fmt.Errorf("store agent session files: %w", err)
	}
	if err := p.batches.SetAgentFileCursor(projectID, last); err != nil {
		return false, fmt.Errorf("move agent file processing cursor: %w", err)
	}
	if err := p.batches.DeleteAgentFileBatchesUpTo(projectID, last); err != nil {
		logger.L.Warn("Deleting stored agent file batches failed", "project_id", projectID, "error", err)
	}
	// Batches may be waiting past this range: the next pass finds out.
	return true, nil
}
