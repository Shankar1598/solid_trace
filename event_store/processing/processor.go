package processing

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	batchSize    = 10000
	tickInterval = time.Second

	// contentRetries is how often an Event that fails on its content is
	// retried before it is skipped.
	contentRetries = 3

	minBackoff = 100 * time.Millisecond
	maxBackoff = 30 * time.Second

	backlogInterval  = time.Minute
	backlogThreshold = time.Minute
)

// ErrStopped is returned when Stop abandons the batch in progress.
var ErrStopped = errors.New("event processing stopped")

// contentError marks an Event that can't be handled because of what it
// contains. Every other error is a store error.
type contentError struct{ err error }

func (e contentError) Error() string { return e.err.Error() }
func (e contentError) Unwrap() error { return e.err }

func isContentError(err error) bool {
	var content contentError
	return errors.As(err, &content)
}

// Processor is Event processing: one worker that reads each Project's Events
// from Pebble after its processing cursor, and for each batch works out the
// Issues, indexes the Events in DuckDB, tells the Console, then moves the
// cursor. It is the only place that does Issue work.
type Processor struct {
	events  *storage.PebbleWriter
	index   *storage.DuckDBWriter
	console *storage.MessageQueueWriter
	issues  IssueRepository

	// batchLock is shared with the archive job, which moves rows out of
	// events_hot by timestamp. Holding it for a whole batch keeps the archive
	// out of the window between a batch's DuckDB commit and its cursor move.
	batchLock sync.Locker

	mu      sync.Mutex
	pending map[uint32]struct{} // Projects that have new Events
	wake    chan struct{}

	// Used only by the worker.
	known            map[uint32]struct{} // Projects to check for a backlog
	lastBacklogCheck time.Time
	// indexedUpTo holds, per Project, the newest Event that DuckDB already
	// had past the cursor at startup. A crash after a batch's DuckDB commit
	// leaves it there; the replay skips the append up to it but still sends
	// the batch's Console messages.
	indexedUpTo map[uint32]string

	stop     chan struct{}
	stopOnce sync.Once
	started  bool
	done     chan struct{}
}

// New constructs Event processing. IssueRepository is the seam to Issue
// persistence; storage.SQLiteWriter is the production adapter.
func New(events *storage.PebbleWriter, index *storage.DuckDBWriter, console *storage.MessageQueueWriter, issues IssueRepository, batchLock sync.Locker) *Processor {
	return &Processor{
		events:      events,
		index:       index,
		console:     console,
		issues:      issues,
		batchLock:   batchLock,
		pending:     make(map[uint32]struct{}),
		wake:        make(chan struct{}, 1),
		known:       make(map[uint32]struct{}),
		indexedUpTo: make(map[uint32]string),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// Prepare runs once, before the worker and the archive job start. It records
// how far DuckDB already got past each Project's cursor, and marks every
// Project that has Events as having new ones.
func (p *Processor) Prepare() error {
	projects, err := p.events.ProjectsWithEvents()
	if err != nil {
		return fmt.Errorf("list projects with events: %w", err)
	}
	for _, projectID := range projects {
		cursor, err := p.events.ProcessingCursor(projectID)
		if err != nil {
			return fmt.Errorf("read processing cursor of project %d: %w", projectID, err)
		}
		newest, err := p.index.NewestHotUUIDAfter(projectID, cursor)
		if err != nil {
			return fmt.Errorf("find newest indexed event of project %d: %w", projectID, err)
		}
		if newest != "" {
			p.indexedUpTo[projectID] = newest
		}
		p.Notify(projectID)
	}
	return nil
}

// Notify marks a Project as having new Events and wakes the worker.
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
	go p.run()
}

// Stop stops the worker and waits for it to exit. A batch in progress is
// abandoned, which is safe: its Events are handled again from the cursor.
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
		if err := p.ProcessUntilCaughtUp(); err != nil {
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

// ProcessUntilCaughtUp processes batches until no Project has new Events.
// Store errors are retried until they clear, so it only returns ErrStopped.
func (p *Processor) ProcessUntilCaughtUp() error {
	for {
		p.checkBacklog()
		projectID, ok := p.takePending()
		if !ok {
			return nil
		}
		p.known[projectID] = struct{}{}

		full, err := p.processBatch(projectID)
		if err != nil {
			p.Notify(projectID)
			return err
		}
		if full {
			p.Notify(projectID)
		}
	}
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

// processBatch handles up to batchSize of a Project's Events after its
// cursor. full reports whether the batch was full, so more may be waiting.
//
// Stop abandons a batch only while a step is retrying a store error. Otherwise
// the batch finishes with the Events whose Issue work is done, so a graceful
// stop never loses an issue_created.
func (p *Processor) processBatch(projectID uint32) (full bool, err error) {
	if p.stopped() {
		return false, ErrStopped
	}
	p.batchLock.Lock()
	defer p.batchLock.Unlock()

	var events []models.Event
	err = p.retry("read events", func() error {
		cursor, err := p.events.ProcessingCursor(projectID)
		if err != nil {
			return err
		}
		events, err = p.events.EventsAfter(projectID, cursor, batchSize)
		return err
	})
	if err != nil || len(events) == 0 {
		return false, err
	}

	// 1. SQLite: the Issue of each Event.
	full = len(events) == batchSize
	rows := make([]models.Event, 0, len(events))
	for i, event := range events {
		if p.stopped() {
			events, full = events[:i], false
			break
		}
		var row models.Event
		skipped, err := p.attempt("issue work", event, func() error {
			var err error
			row, err = p.issueWork(event)
			return err
		})
		if err != nil {
			return false, err
		}
		if !skipped {
			rows = append(rows, row)
		}
	}
	if len(events) == 0 {
		return false, ErrStopped
	}

	// 2. DuckDB: index the rows it doesn't already hold.
	if err := p.indexRows(projectID, rows); err != nil {
		return false, err
	}

	// 3. Console messages, only after the rows can be queried.
	if err := p.retry("enqueue console messages", func() error { return p.enqueueMessages(rows) }); err != nil {
		return false, err
	}

	// 4. Pebble: move the cursor.
	last := events[len(events)-1].EventUUID
	if err := p.retry("move processing cursor", func() error { return p.events.SetProcessingCursor(projectID, last) }); err != nil {
		return false, err
	}
	if mark, ok := p.indexedUpTo[projectID]; ok && mark <= last {
		delete(p.indexedUpTo, projectID)
	}

	return full, nil
}

// issueWork derives an Event's DuckDB row from its raw payload, finding or
// creating its Issue and reopening the Issue if it is resolved.
func (p *Processor) issueWork(event models.Event) (models.Event, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(event.RawJSON, &payload); err != nil {
		return models.Event{}, contentError{fmt.Errorf("parse event: %w", err)}
	}
	if payload == nil {
		return models.Event{}, contentError{errors.New("event payload is null")}
	}

	issue := classifyIssue(payload)

	issueID, fingerprintID, issueStatus, found, err := p.issues.FindIssueByFingerprint(event.ProjectID, issue.fingerprint)
	if err != nil {
		return models.Event{}, fmt.Errorf("find issue by fingerprint: %w", err)
	}

	isNewIssue := false
	if !found {
		issueID, fingerprintID, issueStatus, isNewIssue, err = p.issues.FindOrCreateIssue(event.ProjectID, issue.fingerprint, issue.title, issue.culprit, issue.kind)
		if err != nil {
			return models.Event{}, fmt.Errorf("find or create issue: %w", err)
		}
	}
	if !isNewIssue && issueStatus == IssueStatusResolved {
		// Domain rule: new Events reopen resolved Issues.
		if err := p.issues.ReopenIssue(issueID); err != nil {
			return models.Event{}, fmt.Errorf("reopen issue: %w", err)
		}
	}

	receivedAt := storage.ReceiveTimeForEventKey(storage.KeyForEvent(event.ProjectID, event.EventUUID))
	return models.Event{
		ProjectID:          event.ProjectID,
		EventUUID:          event.EventUUID,
		Timestamp:          extractTimestamp(payload, receivedAt),
		Tags:               extractTags(payload),
		Environment:        extractString(payload, "environment"),
		ServerName:         extractString(payload, "server_name"),
		Release:            extractString(payload, "release"),
		Level:              extractString(payload, "level"),
		IssueFingerprintID: fingerprintID,
		IssueID:            issueID,
		IsNewIssue:         isNewIssue,
	}, nil
}

// indexRows appends the rows DuckDB doesn't already hold, in one transaction.
//
// If the append fails, the rows are appended one at a time. A row that fails
// while others succeed is bad: it is retried, then skipped. If every row fails,
// DuckDB itself is failing, which is a store error: the rows still missing are
// retried with backoff, and none is skipped.
func (p *Processor) indexRows(projectID uint32, rows []models.Event) error {
	if mark, ok := p.indexedUpTo[projectID]; ok {
		rows = slices.DeleteFunc(slices.Clone(rows), func(row models.Event) bool { return row.EventUUID <= mark })
	}

	var bad []models.Event
	err := p.retry("index events", func() error {
		if len(rows) == 0 {
			return nil
		}
		err := p.index.WriteBatch(rows)
		if err == nil {
			rows = nil
			return nil
		}
		var failed []models.Event
		for _, row := range rows {
			if p.index.WriteBatch([]models.Event{row}) != nil {
				failed = append(failed, row)
			}
		}
		if len(failed) == len(rows) {
			return err
		}
		rows, bad = nil, failed
		return nil
	})
	if err != nil {
		return err
	}

	for _, row := range bad {
		if _, err := p.attempt("index event", row, func() error {
			err := p.index.WriteBatch([]models.Event{row})
			if err != nil && p.index.Ping() == nil {
				return contentError{err}
			}
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// enqueueMessages sends issue_created for the Issues created in this batch and
// issue_received_event for the other Issues that got Events.
func (p *Processor) enqueueMessages(rows []models.Event) error {
	newIssues := make(map[int64]bool)
	existingIssues := make(map[int64]bool)
	for _, row := range rows {
		if row.IsNewIssue {
			newIssues[row.IssueID] = true
		} else {
			existingIssues[row.IssueID] = true
		}
	}
	// An Issue created in this batch gets only issue_created.
	for id := range newIssues {
		delete(existingIssues, id)
	}

	for _, message := range []struct {
		messageType string
		issues      map[int64]bool
	}{
		{"issue_created", newIssues},
		{"issue_received_event", existingIssues},
	} {
		if len(message.issues) == 0 {
			continue
		}
		ids := slices.Sorted(maps.Keys(message.issues))
		if err := p.console.EnqueueMessage(message.messageType, map[string]interface{}{"issue_ids": ids}); err != nil {
			return fmt.Errorf("enqueue %s: %w", message.messageType, err)
		}
	}
	return nil
}

// attempt runs a step for one Event, retrying store errors until they clear.
// A content error is retried contentRetries times, then the Event is skipped
// and logged with its key. The raw payload stays in Pebble.
func (p *Processor) attempt(step string, event models.Event, fn func() error) (skipped bool, err error) {
	for try := 0; ; try++ {
		err := p.retry(step, fn)
		if !isContentError(err) {
			return false, err
		}
		if try == contentRetries {
			logger.L.Error("Event processing skipped an event",
				"step", step,
				"project_id", event.ProjectID,
				"event_uuid", event.EventUUID,
				"key", hex.EncodeToString(storage.KeyForEvent(event.ProjectID, event.EventUUID)),
				"error", err)
			return true, nil
		}
	}
}

// retry runs fn until it succeeds or returns a content error, backing off
// after each store error. It returns ErrStopped if Stop is called meanwhile.
func (p *Processor) retry(step string, fn func() error) error {
	backoff := minBackoff
	for {
		err := fn()
		if err == nil || isContentError(err) {
			return err
		}
		logger.L.Error("Event processing store error, retrying", "step", step, "error", err, "retry_in", backoff)
		p.checkBacklog()
		select {
		case <-p.stop:
			return ErrStopped
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, maxBackoff)
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

// checkBacklog runs logBacklog at most once per backlogInterval. It is called
// from the processing loop, so a busy or failing worker still reports.
func (p *Processor) checkBacklog() {
	if time.Since(p.lastBacklogCheck) < backlogInterval {
		return
	}
	p.lastBacklogCheck = time.Now()
	p.logBacklog()
}

// logBacklog logs each Project whose oldest unprocessed Event is older than
// backlogThreshold, with how far behind it is.
func (p *Processor) logBacklog() {
	for projectID := range p.known {
		cursor, err := p.events.ProcessingCursor(projectID)
		if err != nil {
			logger.L.Error("Backlog check failed", "project_id", projectID, "error", err)
			continue
		}
		oldest, err := p.events.EventsAfter(projectID, cursor, 1)
		if err != nil {
			logger.L.Error("Backlog check failed", "project_id", projectID, "error", err)
			continue
		}
		if len(oldest) == 0 {
			continue
		}
		receivedAt := storage.ReceiveTimeForEventKey(storage.KeyForEvent(projectID, oldest[0].EventUUID))
		if behind := time.Since(receivedAt); behind > backlogThreshold {
			logger.L.Warn("Event processing is behind", "project_id", projectID, "behind", behind.Round(time.Second).String())
		}
	}
}
