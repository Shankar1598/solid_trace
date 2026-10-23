// Package logprocessing is Log processing: it moves the Log batches that Log
// ingest stored in Pebble into each Project's hot table in DuckDB.
package logprocessing

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// maxLogsPerCommit is roughly how many Logs one transaction appends for
	// a Project: whole Log batches, at least one, until they reach it.
	maxLogsPerCommit = 10000
	tickInterval     = time.Second

	// contentRetries is how often a Project's Logs that fail on their
	// content are retried before they are tried apart.
	contentRetries = 3

	minBackoff = 100 * time.Millisecond
	maxBackoff = 30 * time.Second

	// Every backlogInterval by the clock, Log processing names each Project
	// whose oldest Log batch has waited longer than backlogThreshold.
	backlogInterval  = time.Minute
	backlogThreshold = time.Minute
)

// ErrStopped is returned when Stop ends processing.
var ErrStopped = errors.New("log processing stopped")

// contentError marks Logs that can't be stored because of what they contain.
// Every other error is a store error.
type contentError struct{ err error }

func (e contentError) Error() string { return e.err.Error() }
func (e contentError) Unwrap() error { return e.err }

func isContentError(err error) bool {
	var content contentError
	return errors.As(err, &content)
}

// Store is logs.duckdb as Log processing uses it. *storage.LogStore is the
// production adapter.
type Store interface {
	LogProcessingCursor(projectID uint32) (uuid.UUID, error)
	LogProcessingCursors() ([]uuid.UUID, error)
	AppendLogBatches(projectID uint32, batches []storage.LogBatch, now time.Time) error
	TryLogBatches(projectID uint32, batches []storage.LogBatch, now time.Time) error
	Ping() error
}

// Transform runs each Project's Log transform on its Logs before they are
// stored. *logtransform.Transforms is the production adapter.
type Transform interface {
	// Apply returns a Project's Log batches as they are to be stored. A
	// batch may come back with fewer Logs, or none.
	Apply(projectID uint32, batches []storage.LogBatch) ([]storage.LogBatch, error)
}

// Processor is Log processing: one worker, separate from Event processing,
// that reads each Project's Log batches from Pebble after its Log processing
// cursor and appends their Logs to the Project's hot table, moving the cursor
// in the same DuckDB transaction. Only then does it delete the batches from
// Pebble. A crash between the commit and the delete leaves batches at or
// below the cursor, which are never read again.
//
// A Project whose Logs fail to store waits to be retried, with backoff, and
// the other Projects carry on meanwhile. A store error is retried until it
// clears and skips nothing. Logs that fail on their content are retried, then
// tried apart, and a Log that fails on its own is skipped and logged.
//
// Every minute it logs each Project whose Logs have waited too long, and what
// each Project's commits stored.
type Processor struct {
	batches   *storage.PebbleWriter
	store     Store
	transform Transform
	clock     clock.Clock

	mu      sync.Mutex
	pending map[uint32]struct{} // Projects that may have new Log batches
	wake    chan struct{}

	// Used only by the worker.
	failing          map[uint32]*failure // Projects whose Logs failed to store
	lastBacklogCheck time.Time
	commits          map[uint32]*commitCounts // each Project's commits since the last backlog check

	*worker
}

// failure is how a Project's Logs have failed to store since they last did.
type failure struct {
	retryAt time.Time     // when to try the Project again
	backoff time.Duration // the wait after the next failure
	content int           // how many of the failures were content errors
	count   int           // how many failures there were
	last    error         // the last failure
}

// commitCounts is what a Project's commits stored since the last backlog
// check.
type commitCounts struct {
	commits int           // transactions that stored Logs
	batches int           // Log batches they stored, one per ingest request
	logs    int           // Logs in those batches, before the Log transform
	took    time.Duration // time spent storing them, the commits included
	slowest time.Duration // the longest of those
}

// processAllFailures is how many times in a row ProcessAll lets a Project's
// Logs fail to store before it gives up.
const processAllFailures = 5

// New constructs Log processing, which runs each Project's Log transform with
// transform. It reads the time from clock.
func New(batches *storage.PebbleWriter, store Store, transform Transform, clock clock.Clock) *Processor {
	return &Processor{
		batches:   batches,
		store:     store,
		transform: transform,
		clock:     clock,
		pending:   make(map[uint32]struct{}),
		wake:      make(chan struct{}, 1),
		failing:   make(map[uint32]*failure),
		commits:   make(map[uint32]*commitCounts),
		worker:    newWorker(),
	}
}

// Prepare runs once, before the worker starts and before Log ingest writes. It
// keeps new Log batch ids above every Project's cursor, deletes the Log
// batches a crash left in Pebble after they were stored, and marks every
// Project that has Log batches as pending.
func (p *Processor) Prepare() error {
	cursors, err := p.store.LogProcessingCursors()
	if err != nil {
		return fmt.Errorf("read log processing cursors: %w", err)
	}
	for _, cursor := range cursors {
		p.batches.KeepLogBatchIDsAbove(cursor)
	}
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

// takeDue takes a pending Project that isn't waiting to be retried. If every
// pending Project is waiting, it returns the earliest retry time instead, or
// the zero time if none is pending.
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

// Start runs the worker in its own goroutine.
func (p *Processor) Start() {
	p.start(p.run)
}

// Stop stops the worker and waits for it to exit. Batches not yet committed
// are processed again after a restart, from the cursor.
func (p *Processor) Stop() {
	p.halt()
}

func (p *Processor) run() {
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

// ProcessUntilCaughtUp processes Log batches until no Project has new ones.
// A Project whose Logs fail to store is retried after a backoff, and the
// other Projects are processed while it waits. Store errors are retried until
// they clear, so it only returns ErrStopped.
func (p *Processor) ProcessUntilCaughtUp() error {
	for {
		p.checkBacklog()
		if p.stopped() {
			return ErrStopped
		}
		projectID, ok, retryAt := p.takeDue()
		if ok {
			p.processProject(projectID)
			continue
		}
		if retryAt.IsZero() {
			return nil
		}
		if !p.waitUntil(retryAt) {
			return ErrStopped
		}
	}
}

// ProcessAll processes every Log batch waiting, with no worker running, and
// returns once none is left, for prepare-clickhouse. Unlike
// ProcessUntilCaughtUp, it gives up once a Project's Logs fail to store
// processAllFailures times in a row, and returns the last failure, so that an
// offline run ends.
func (p *Processor) ProcessAll() error {
	for {
		projectID, ok, retryAt := p.takeDue()
		if ok {
			p.processProject(projectID)
			if failed := p.failing[projectID]; failed != nil && failed.count >= processAllFailures {
				return fmt.Errorf("the Logs of Project %d failed to store %d times in a row: %w", projectID, failed.count, failed.last)
			}
			continue
		}
		if retryAt.IsZero() {
			return nil
		}
		if !p.waitUntil(retryAt) {
			return ErrStopped
		}
	}
}

// waitUntil waits until t, or until a Project has new Log batches. It returns
// false if Stop is called meanwhile.
func (p *Processor) waitUntil(t time.Time) bool {
	timer := time.NewTimer(time.Until(t))
	defer timer.Stop()
	select {
	case <-p.stop:
		return false
	case <-p.wake:
	case <-timer.C:
	}
	return true
}

// processProject stores a Project's next Logs. If they fail to store, the
// Project waits to be retried.
func (p *Processor) processProject(projectID uint32) {
	more, err := p.storeLogs(projectID)
	switch {
	case errors.Is(err, ErrStopped):
		p.markPending(projectID)
	case err != nil:
		p.retryLater(projectID, err)
	default:
		delete(p.failing, projectID)
		if more {
			p.markPending(projectID)
		}
	}
}

// retryLater logs a Project's failure and keeps it pending until its
// backoff has passed. The backoff doubles with each failure.
func (p *Processor) retryLater(projectID uint32, err error) {
	failed := p.failing[projectID]
	if failed == nil {
		failed = &failure{backoff: minBackoff}
		p.failing[projectID] = failed
	}
	failed.count++
	failed.last = err
	if isContentError(err) {
		failed.content++
		logger.L.Error("Log processing content error, retrying", "project_id", projectID, "error", err, "retry_in", failed.backoff)
	} else {
		logger.L.Error("Log processing store error, retrying", "project_id", projectID, "error", err, "retry_in", failed.backoff)
	}
	failed.retryAt = time.Now().Add(failed.backoff)
	failed.backoff = min(2*failed.backoff, maxBackoff)
	p.markPending(projectID)
}

// storeLogs stores a Project's Log batches after its cursor, up to about
// maxLogsPerCommit Logs. Once they have failed on their content
// contentRetries times, it stores them without the Logs that fail on their
// own. more reports whether more may be waiting.
func (p *Processor) storeLogs(projectID uint32) (more bool, err error) {
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
	logs := 0
	for _, batch := range batches {
		logs += len(batch.Logs)
	}
	last := batches[len(batches)-1].ID

	// Before the hot/late split, so promotion, the hot table, logs_late and
	// Match monitors all see the Logs the Project's Log transform returns. A
	// batch it leaves empty still moves the cursor.
	batches, err = p.transform.Apply(projectID, batches)
	if err != nil {
		return false, fmt.Errorf("run log transform: %w", err)
	}

	now := p.clock.Now()
	began := time.Now()
	err = p.classify(p.store.AppendLogBatches(projectID, batches, now))
	if failed := p.failing[projectID]; isContentError(err) && failed != nil && failed.content >= contentRetries {
		err = p.storeApart(projectID, batches, now)
	}
	if err != nil {
		return false, fmt.Errorf("store logs: %w", err)
	}
	p.countCommit(projectID, len(batches), logs, time.Since(began))

	if err := p.batches.DeleteLogBatchesUpTo(projectID, last); err != nil {
		// The Logs are stored and the cursor is past these batches, so they
		// go with the next delete, or at startup.
		logger.L.Warn("Deleting stored log batches failed", "project_id", projectID, "error", err)
	}
	return logs >= maxLogsPerCommit, nil
}

// classify marks a failure to store Logs as a content error if DuckDB still
// answers a query and the failure isn't one DuckDB reports for the store
// itself, such as a full disk. A content error is then the Logs' fault.
//
// The appender's errors aren't typed, so a failure that hits only appends
// and isn't reported as one of those also counts as a content error.
func (p *Processor) classify(err error) error {
	if err == nil || storeFailure(err) || p.store.Ping() != nil {
		return err
	}
	return contentError{err}
}

// storeFailure reports whether DuckDB says err is a failure of the store,
// not of what was stored.
func storeFailure(err error) bool {
	var duckErr *duckdb.Error
	if !errors.As(err, &duckErr) {
		return false
	}
	switch duckErr.Type {
	case duckdb.ErrorTypeIO, duckdb.ErrorTypeOutOfMemory, duckdb.ErrorTypeFatal, duckdb.ErrorTypeTransaction:
		return true
	}
	return false
}

// batchedLog is a Log with the id of its Log batch.
type batchedLog struct {
	batchID uuid.UUID
	log     models.Log
}

// rejectedLog is a Log that fails on its own, with how it failed.
type rejectedLog struct {
	batchedLog
	err error
}

// storeApart stores a Project's Log batches, which fail on their content
// together, without the Logs that fail on their own. It finds those by trying
// the Logs in halves without storing them, and skips and logs each. The
// batches the skipped Logs leave empty still move the cursor.
func (p *Processor) storeApart(projectID uint32, batches []storage.LogBatch, now time.Time) error {
	var logs []batchedLog
	for _, batch := range batches {
		for _, log := range batch.Logs {
			logs = append(logs, batchedLog{batch.ID, log})
		}
	}
	rejected, err := p.rejectedLogs(projectID, logs, now)
	if err != nil {
		return err
	}

	skip := make(map[uuid.UUID]bool, len(rejected))
	for _, r := range rejected {
		skip[r.log.ID] = true
	}
	kept := make([]storage.LogBatch, len(batches))
	for i, batch := range batches {
		kept[i] = storage.LogBatch{ID: batch.ID}
		for _, log := range batch.Logs {
			if !skip[log.ID] {
				kept[i].Logs = append(kept[i].Logs, log)
			}
		}
	}
	if err := p.classify(p.store.AppendLogBatches(projectID, kept, now)); err != nil {
		return err
	}

	for _, r := range rejected {
		logger.L.Error("Log processing skipped a Log",
			"project_id", projectID,
			"batch_id", r.batchID.String(),
			"log_id", r.log.ID.String(),
			"error", r.err)
	}
	return nil
}

// rejectedLogs returns the Logs out of logs that fail on their own. It tries
// logs without storing them, and if they fail on their content, it looks in
// each half, down to single Logs. A store error, or Stop, ends the search.
func (p *Processor) rejectedLogs(projectID uint32, logs []batchedLog, now time.Time) ([]rejectedLog, error) {
	if len(logs) == 0 {
		return nil, nil
	}
	if p.stopped() {
		return nil, ErrStopped
	}
	err := p.classify(p.store.TryLogBatches(projectID, batchesOf(logs), now))
	if err == nil || !isContentError(err) {
		return nil, err
	}
	if len(logs) == 1 {
		return []rejectedLog{{logs[0], err}}, nil
	}
	half := len(logs) / 2
	first, err := p.rejectedLogs(projectID, logs[:half], now)
	if err != nil {
		return nil, err
	}
	second, err := p.rejectedLogs(projectID, logs[half:], now)
	if err != nil {
		return nil, err
	}
	return append(first, second...), nil
}

// batchesOf groups Logs back into their Log batches, in order.
func batchesOf(logs []batchedLog) []storage.LogBatch {
	var batches []storage.LogBatch
	for _, l := range logs {
		if n := len(batches); n == 0 || batches[n-1].ID != l.batchID {
			batches = append(batches, storage.LogBatch{ID: l.batchID})
		}
		last := &batches[len(batches)-1]
		last.Logs = append(last.Logs, l.log)
	}
	return batches
}

// countCommit adds a commit that stored a Project's batches, holding logs
// Logs, in took, to the Project's commits since the last backlog check.
func (p *Processor) countCommit(projectID uint32, batches, logs int, took time.Duration) {
	c := p.commits[projectID]
	if c == nil {
		c = &commitCounts{}
		p.commits[projectID] = c
	}
	c.commits++
	c.batches += batches
	c.logs += logs
	c.took += took
	c.slowest = max(c.slowest, took)
}

// checkBacklog runs logBacklog and logCommits at most once per
// backlogInterval by the clock. It is called from the processing loop, which
// also runs while Projects wait to be retried, so a failing worker still
// reports.
func (p *Processor) checkBacklog() {
	now := p.clock.Now()
	if now.Sub(p.lastBacklogCheck) < backlogInterval {
		return
	}
	if !p.lastBacklogCheck.IsZero() {
		p.logCommits(now.Sub(p.lastBacklogCheck))
	}
	clear(p.commits)
	p.lastBacklogCheck = now
	p.logBacklog(now)
}

// logCommits logs, for each Project that stored Logs over window, since the
// last backlog check, how many commits stored how many Log batches and Logs,
// and how long they took. From these a reader works out commits a second and
// Logs a commit.
func (p *Processor) logCommits(window time.Duration) {
	for projectID, c := range p.commits {
		logger.L.Info("Log processing commits",
			"kind", "log_commits",
			"project_id", projectID,
			"seconds", window.Seconds(),
			"commits", c.commits,
			"batches", c.batches,
			"logs", c.logs,
			"took_ms", float64(c.took.Microseconds())/1000,
			"slowest_ms", float64(c.slowest.Microseconds())/1000)
	}
}

// logBacklog logs each Project whose oldest Log batch was written more than
// backlogThreshold before now, with how far behind it is. Batches leave
// Pebble once they are stored, so it reads Pebble alone, and reports even
// while DuckDB is down.
func (p *Processor) logBacklog(now time.Time) {
	oldest, err := p.batches.OldestLogBatches()
	if err != nil {
		logger.L.Error("Log backlog check failed", "error", err)
		return
	}
	for _, batch := range oldest {
		if behind := now.Sub(storage.LogBatchWrittenAt(batch.BatchID)); behind > backlogThreshold {
			logger.L.Warn("Log processing is behind", "project_id", batch.ProjectID, "behind", behind.Round(time.Second).String())
		}
	}
}

func (p *Processor) stopped() bool {
	select {
	case <-p.stop:
		return true
	default:
		return false
	}
}
