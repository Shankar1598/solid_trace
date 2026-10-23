package processing

import (
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
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

// errStoppedBeforeRetry is ErrStopped from attempt, when Stop came while it
// waited to retry an Event's content error. Unlike a store error, nothing
// else is failing, so the batch can still finish without that Event.
var errStoppedBeforeRetry = fmt.Errorf("%w before retrying an event", ErrStopped)

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
// Issues, stores the Events' rows in ClickHouse, tells the Console, then
// moves the cursor. It is the only place that does Issue work.
//
// Each batch is stored exactly once: its range is recorded in Pebble before
// its insert, which carries a token made from the range. After a crash before
// the cursor moved, the recorded range is processed again, and ClickHouse
// drops the repeated insert.
type Processor struct {
	events  *storage.PebbleWriter
	index   *storage.ClickHouse
	console *storage.MessageQueueWriter
	issues  IssueRepository

	// batchLock is shared with the Event cleanup, so that a deleted Project's
	// rows never go between a batch's insert and its cursor move.
	batchLock sync.Locker

	mu      sync.Mutex
	pending map[uint32]struct{} // Projects that have new Events
	wake    chan struct{}

	// Used only by the worker.
	known            map[uint32]struct{} // Projects to check for a backlog
	lastBacklogCheck time.Time

	stop     chan struct{}
	stopOnce sync.Once
	started  bool
	done     chan struct{}
}

// New constructs Event processing. IssueRepository is the seam to Issue
// persistence; storage.SQLiteWriter is the production adapter.
func New(events *storage.PebbleWriter, index *storage.ClickHouse, console *storage.MessageQueueWriter, issues IssueRepository, batchLock sync.Locker) *Processor {
	return &Processor{
		events:    events,
		index:     index,
		console:   console,
		issues:    issues,
		batchLock: batchLock,
		pending:   make(map[uint32]struct{}),
		wake:      make(chan struct{}, 1),
		known:     make(map[uint32]struct{}),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Prepare runs once, before the worker starts. It marks every Project that
// has Events as having new ones.
func (p *Processor) Prepare() error {
	projects, err := p.events.ProjectsWithEvents()
	if err != nil {
		return fmt.Errorf("list projects with events: %w", err)
	}
	for _, projectID := range projects {
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
// cursor. full reports whether more may be waiting.
//
// A Project with a recorded Event range gets that range again, all of it:
// its insert may have been stored before the cursor moved.
//
// Stop abandons a batch while a step is retrying a store error, and a range
// processed again. Otherwise the batch finishes with the Events whose Issue
// work is done, so a graceful stop never loses an issue_created.
func (p *Processor) processBatch(projectID uint32) (full bool, err error) {
	if p.stopped() {
		return false, ErrStopped
	}
	p.batchLock.Lock()
	defer p.batchLock.Unlock()

	var events []models.Event
	var recorded storage.EventRange
	again := false
	err = p.retry("read events", func() error {
		cursor, err := p.events.ProcessingCursor(projectID)
		if err != nil {
			return err
		}
		var ok bool
		if recorded, ok, err = p.events.RecordedEventRange(projectID); err != nil {
			return err
		}
		if events, err = p.events.EventsAfter(projectID, cursor, batchSize); err != nil {
			return err
		}
		again = ok && recorded.After == cursor
		if again {
			events = slices.DeleteFunc(events, func(event models.Event) bool { return event.EventUUID > recorded.Through })
		} else {
			recorded = storage.EventRange{ProjectID: projectID, After: cursor}
		}
		return nil
	})
	if err != nil || len(events) == 0 {
		return false, err
	}

	// 1. SQLite: the Issue of each Event.
	full = again || len(events) == batchSize
	rows := make([]models.Event, 0, len(events))
	for i, event := range events {
		if p.stopped() {
			if again {
				return false, ErrStopped
			}
			events, full = events[:i], false
			break
		}
		var row models.Event
		skipped, err := p.attempt("issue work", event, func() error {
			var err error
			row, err = p.issueWork(event)
			return err
		})
		if errors.Is(err, errStoppedBeforeRetry) && !again {
			events, full = events[:i], false
			break
		}
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

	// 2. Pebble: record the range, unless it is the recorded one.
	r := recorded
	if !again {
		r.Through = events[len(events)-1].EventUUID
		if err := p.retry("record event range", func() error { return p.events.RecordEventRange(r) }); err != nil {
			return false, err
		}
	}

	// 3. ClickHouse: insert the rows, with the range's token.
	stored, err := p.storeRows(r, rows)
	if err != nil {
		return false, err
	}

	// 4. SQLite: count the Events stored on their Issues.
	if len(stored) > 0 {
		if err := p.retry("record seen events", func() error { return p.issues.RecordSeenEvents(projectID, stored, r.Through) }); err != nil {
			return false, err
		}
	}

	// 5. Console messages, only after the rows can be queried.
	if err := p.retry("enqueue console messages", func() error { return p.enqueueMessages(rows) }); err != nil {
		return false, err
	}

	// 6. Pebble: move the cursor past the range.
	if err := p.retry("move processing cursor", func() error { return p.events.SetProcessingCursor(projectID, r.Through) }); err != nil {
		return false, err
	}

	return full, nil
}

// parseEvent parses an Event's JSON as encoding/json does. The v2 API parses
// into a map in about half the time, but rejects duplicate names and invalid
// UTF-8, which encoding/json accepts. So an Event it rejects is parsed again
// by encoding/json, which keeps a name's last value and replaces an invalid
// byte, as before.
func parseEvent(raw []byte) (map[string]interface{}, error) {
	var payload map[string]interface{}
	if jsonv2.Unmarshal(raw, &payload) == nil {
		return payload, nil
	}
	payload = nil
	err := json.Unmarshal(raw, &payload)
	return payload, err
}

// issueWork derives an Event's row from its raw payload, finding or
// creating its Issue and reopening the Issue if it is resolved.
func (p *Processor) issueWork(event models.Event) (models.Event, error) {
	payload, err := parseEvent(event.RawJSON)
	if err != nil {
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

// storeRows inserts the range's rows that the events table can hold, in one
// insert, and returns them. A row it can't hold is skipped and logged; that
// depends on the row alone, so a range processed again skips the same rows.
// While ClickHouse fails, the insert is retried with backoff and the same
// token, and no row is skipped.
func (p *Processor) storeRows(r storage.EventRange, rows []models.Event) ([]models.Event, error) {
	stored := make([]models.Event, 0, len(rows))
	for _, row := range rows {
		if err := storage.EventRowError(row); err != nil {
			p.skip("store event", row, err)
			continue
		}
		stored = append(stored, row)
	}
	if err := p.retry("store events", func() error { return p.index.InsertEvents(r, stored) }); err != nil {
		return nil, err
	}
	return stored, nil
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
// A content error is retried contentRetries times with backoff, then the Event
// is skipped and logged with its key. The raw payload stays in Pebble. It
// returns ErrStopped if Stop is called meanwhile, as errStoppedBeforeRetry
// while waiting to retry a content error.
func (p *Processor) attempt(step string, event models.Event, fn func() error) (skipped bool, err error) {
	backoff := minBackoff
	for try := 0; ; try++ {
		err := p.retry(step, fn)
		if !isContentError(err) {
			return false, err
		}
		if try == contentRetries {
			p.skip(step, event, err)
			return true, nil
		}
		if !p.wait(&backoff) {
			return false, errStoppedBeforeRetry
		}
	}
}

// skip logs an Event that a step skips for its content, with its key.
func (p *Processor) skip(step string, event models.Event, err error) {
	logger.L.Error("Event processing skipped an event",
		"step", step,
		"project_id", event.ProjectID,
		"event_uuid", event.EventUUID,
		"key", hex.EncodeToString(storage.KeyForEvent(event.ProjectID, event.EventUUID)),
		"error", err)
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
		if !p.wait(&backoff) {
			return ErrStopped
		}
	}
}

// wait sleeps for backoff, then doubles it up to maxBackoff. It returns false
// if Stop is called meanwhile.
func (p *Processor) wait(backoff *time.Duration) bool {
	select {
	case <-p.stop:
		return false
	case <-time.After(*backoff):
	}
	*backoff = min(2**backoff, maxBackoff)
	return true
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
