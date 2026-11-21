package dispatcher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The write-back. The Dispatcher tells a Run's tracker ticket what the
// Run does: it sets the ticket's status and posts one comment at each
// step. recordEvent queues each step in the ticket_writes outbox, in the
// same transaction as the state change, and the write-back loop sends
// the outbox to the tracker with retries. So a tracker that is down
// never blocks a Run, and the steps of a ticket arrive in order.
//
// The steps, by the run event that queues them:
//
//   - execution_started of the first Execution: "started", the working
//     status and a comment with the Run's link.
//   - pr_opened: "in_review", the review status and no comment.
//   - execution_ended of a done Run with a PR: "pr_opened", the review
//     status and, for the first time of that PR, a comment with the PR's
//     link, the Run's link, the summary and the pass/fail list.
//   - execution_ended of a done Run with no PR: "done", the waiting
//     status and a comment with the summary.
//   - execution_ended of a Run that waits on a person: "needs_a_person",
//     the waiting status and a comment with the question.
//   - execution_ended of a failed Run: "failed", the waiting status and
//     a comment with the reason.
//   - stop: no step of its own, and no status.
//
// Each PR gets its PR-opened comment: when a Run with a PR waits on a
// person, fails or stops before a done, the Dispatcher first queues a
// "pr_opened" step with the comment and no status. Each other event
// writes nothing back.

// Write-back steps (ticket_writes.step).
const (
	stepStarted      = "started"
	stepInReview     = "in_review"
	stepPROpened     = "pr_opened"
	stepNeedsAPerson = "needs_a_person"
	stepDone         = "done"
	stepFailed       = "failed"
)

// Write-back statuses (ticket_writes.status). The sender reads their
// names from the connection's tracker settings.
const (
	statusWorking = "working"
	statusReview  = "review"
	statusWaiting = "waiting"
)

// The write-back loop's waits after a failed send: from
// minWriteBackRetry, two times longer each time, up to
// maxWriteBackRetry. A step that still fails writeBackFor after it was
// queued is given up, with a warning.
const (
	minWriteBackRetry = 250 * time.Millisecond
	maxWriteBackRetry = 5 * time.Minute
	writeBackFor      = 24 * time.Hour
	// writeBackTimeout is the longest time of one call to the tracker.
	writeBackTimeout = 30 * time.Second
)

// queueWriteBack queues the write-back step of the run event kind, in tx,
// when the Run has a tracker ticket.
func queueWriteBack(tx *sql.Tx, at time.Time, runID, kind string, details map[string]any) error {
	if kind != eventExecutionStarted && kind != eventPROpened && kind != eventExecutionEnded && kind != eventStop {
		return nil
	}
	var connectionID, ticketID, state, reason, question, summary, prURL string
	err := tx.QueryRow(`SELECT ticket_connection_id, ticket_id, state, reason, question, summary, pr_url
		FROM runs WHERE id = ?`, runID).Scan(&connectionID, &ticketID, &state, &reason, &question, &summary, &prURL)
	if err != nil {
		return fmt.Errorf("read the Run for its write-back: %w", err)
	}
	if ticketID == "" {
		return nil
	}
	executionID, _ := details["execution.id"].(string)
	var step, status, text string
	comment := true
	switch kind {
	case eventExecutionStarted:
		// Only the first Execution says that the Run started.
		if done, err := hasStep(tx, runID, stepStarted, ""); err != nil || done {
			return err
		}
		step, status = stepStarted, statusWorking
	case eventPROpened:
		step, status, comment = stepInReview, statusReview, false
	case eventStop:
		// A stop changes no status; only a PR with no comment gets one.
		return queuePROpenedComment(tx, at, runID, executionID, connectionID, ticketID, summary, prURL)
	default:
		switch state {
		case dispatcherapi.StateDone:
			if prURL == "" {
				step, status, text = stepDone, statusWaiting, summary
				break
			}
			// One PR-opened comment for each PR; a later done of the same
			// PR sets the review status again only.
			posted, err := hasStep(tx, runID, stepPROpened, prURL)
			if err != nil {
				return err
			}
			step, status, text, comment = stepPROpened, statusReview, summary, !posted
		case dispatcherapi.StateWaitingOnPerson:
			step, status, text = stepNeedsAPerson, statusWaiting, question
		case dispatcherapi.StateFailed:
			step, status, text = stepFailed, statusWaiting, reason
		default:
			return nil
		}
		if step != stepPROpened {
			if err := queuePROpenedComment(tx, at, runID, executionID, connectionID, ticketID, summary,
				prURL); err != nil {
				return err
			}
		}
	}
	// A status that the ticket got from the Run's last status step is not
	// set again.
	var last string
	err = tx.QueryRow(`SELECT status FROM ticket_writes WHERE run_id = ? AND status != '' ORDER BY id DESC LIMIT 1`,
		runID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if status == last {
		status = ""
	}
	if status == "" && !comment {
		return nil
	}
	_, err = tx.Exec(`INSERT INTO ticket_writes (run_id, execution_id, connection_id, ticket_id, step, status,
		comment, text, pr_url, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, runID, executionID, connectionID,
		ticketID, step, status, comment, text, prURL, at.UnixMicro())
	return err
}

// queuePROpenedComment queues the PR-opened comment, with no status, when
// the Run has a PR that has no PR-opened comment.
func queuePROpenedComment(tx *sql.Tx, at time.Time, runID, executionID, connectionID, ticketID, summary,
	prURL string) error {
	if prURL == "" {
		return nil
	}
	if posted, err := hasStep(tx, runID, stepPROpened, prURL); err != nil || posted {
		return err
	}
	_, err := tx.Exec(`INSERT INTO ticket_writes (run_id, execution_id, connection_id, ticket_id, step, status,
		comment, text, pr_url, created_at) VALUES (?, ?, ?, ?, ?, '', 1, ?, ?, ?)`, runID, executionID, connectionID,
		ticketID, stepPROpened, summary, prURL, at.UnixMicro())
	return err
}

// hasStep is true when the Run has a write-back step with a comment (for
// prURL "", of any PR).
func hasStep(tx *sql.Tx, runID, step, prURL string) (bool, error) {
	var has bool
	err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM ticket_writes WHERE run_id = ? AND step = ? AND comment = 1
		AND (? = '' OR pr_url = ?))`, runID, step, prURL, prURL).Scan(&has)
	return has, err
}

// ticketWrite is one write-back step that is not sent.
type ticketWrite struct {
	id                                   int64
	runID, executionID, organizationSlug string
	connectionID, ticketID, step, status string
	text, prURL                          string
	comment, statusSet                   bool
	failures                             int
	createdAt                            time.Time
}

// pendingWrites reads the write-back steps that are not sent, oldest
// first.
func (s *store) pendingWrites() ([]ticketWrite, error) {
	rows, err := s.db.Query(`SELECT w.id, w.run_id, w.execution_id, r.organization_slug, w.connection_id,
		w.ticket_id, w.step, w.status, w.text, w.pr_url, w.comment, w.status_set, w.failures, w.created_at
		FROM ticket_writes w JOIN runs r ON r.id = w.run_id WHERE w.sent_at IS NULL ORDER BY w.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var writes []ticketWrite
	for rows.Next() {
		var w ticketWrite
		var created int64
		if err := rows.Scan(&w.id, &w.runID, &w.executionID, &w.organizationSlug, &w.connectionID, &w.ticketID,
			&w.step, &w.status, &w.text, &w.prURL, &w.comment, &w.statusSet, &w.failures, &created); err != nil {
			return nil, err
		}
		w.createdAt = time.UnixMicro(created).UTC()
		writes = append(writes, w)
	}
	return writes, rows.Err()
}

func (s *store) writeStatusSet(id int64) error {
	_, err := s.db.Exec(`UPDATE ticket_writes SET status_set = 1 WHERE id = ?`, id)
	return err
}

func (s *store) writeSent(id int64, at time.Time, gaveUp bool) error {
	_, err := s.db.Exec(`UPDATE ticket_writes SET sent_at = ?, gave_up = ? WHERE id = ?`, at.UnixMicro(), gaveUp, id)
	return err
}

func (s *store) writeFailed(id int64) error {
	_, err := s.db.Exec(`UPDATE ticket_writes SET failures = failures + 1 WHERE id = ?`, id)
	return err
}

func (s *store) wakeWriteBack() {
	select {
	case s.writeBackWake <- struct{}{}:
	default:
	}
}

// writeBackLoop sends the write-back outbox to the trackers. A failure
// leaves the step in the outbox, and the loop tries that Run again
// later. Each Run has its own wait, so a ticket that fails holds back
// only its own Run's steps.
func (d *Dispatcher) writeBackLoop(ctx context.Context) {
	backoff := map[string]*writeBackRetry{}
	for {
		next, err := d.sendWriteBacks(ctx, backoff)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			d.log.Error("could not read the write-back outbox", "error", err)
			next = time.Now().Add(minWriteBackRetry)
		}
		var timer *time.Timer
		var due <-chan time.Time
		if !next.IsZero() {
			timer = time.NewTimer(max(time.Until(next), 0))
			due = timer.C
		}
		select {
		case <-ctx.Done():
		case <-d.store.writeBackWake:
		case <-due:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// writeBackRetry is the wait of a Run whose step failed: the loop tries
// the step again at next. The waits use the wall clock, not d.now: they
// are real waits of the loop's timer.
type writeBackRetry struct {
	wait time.Duration
	next time.Time
}

// sendWriteBacks sends each step that is not sent, in order. After a
// step of a Run fails, the later steps of that Run wait, so a ticket
// gets its steps in order; the Run's wait in backoff starts at
// minWriteBackRetry and is two times longer after each failure, up to
// maxWriteBackRetry. A Run whose wait has not ended is skipped. It gives
// the earliest end of a wait, or zero when no step waits.
func (d *Dispatcher) sendWriteBacks(ctx context.Context, backoff map[string]*writeBackRetry) (time.Time, error) {
	writes, err := d.store.pendingWrites()
	if err != nil {
		return time.Time{}, err
	}
	held := map[string]bool{}
	for _, w := range writes {
		if ctx.Err() != nil {
			return time.Time{}, nil
		}
		if held[w.runID] {
			continue
		}
		r := backoff[w.runID]
		if r != nil && time.Now().Before(r.next) {
			held[w.runID] = true
			continue
		}
		if err := d.sendWriteBack(ctx, w); err != nil {
			if r == nil {
				r = &writeBackRetry{}
				backoff[w.runID] = r
			}
			r.wait = min(max(2*r.wait, minWriteBackRetry), maxWriteBackRetry)
			r.next = time.Now().Add(r.wait)
			held[w.runID] = true
			continue
		}
		delete(backoff, w.runID)
	}
	var next time.Time
	for id, r := range backoff {
		if !held[id] {
			// The Run has no step that waits.
			delete(backoff, id)
			continue
		}
		if next.IsZero() || r.next.Before(next) {
			next = r.next
		}
	}
	return next, nil
}

// errGiveUp is a write-back step that can never be sent.
var errGiveUp = errors.New("the step cannot be sent")

// sendWriteBack sends one step: the status, then the comment. It gives
// up the step when the connection, its kind or the ticket is gone, or
// when the step fails for writeBackFor; each give-up is a warning.
func (d *Dispatcher) sendWriteBack(ctx context.Context, w ticketWrite) error {
	err := d.writeBack(ctx, w)
	if err == nil {
		return d.store.writeSent(w.id, d.now().UTC(), false)
	}
	args := []string{"organization", w.organizationSlug, "factory.run_id", w.runID,
		"factory.connection_id", w.connectionID, "factory.ticket", w.ticketID, "factory.write_back", w.step,
		"error", err.Error()}
	if errors.Is(err, errGiveUp) || errors.Is(err, ErrNoTicket) || d.now().Sub(w.createdAt) >= writeBackFor {
		d.warn("the write-back to a Run's ticket failed, and the Dispatcher gave it up", args...)
		if err := d.store.writeSent(w.id, d.now().UTC(), true); err != nil {
			return err
		}
		return nil
	}
	if w.failures == 0 {
		d.warn("the write-back to a Run's ticket failed: the Dispatcher tries again", args...)
	} else {
		d.log.Warn("the write-back to a Run's ticket failed again", "run", w.runID, "step", w.step, "error", err)
	}
	if err := d.store.writeFailed(w.id); err != nil {
		d.log.Error("could not count a failed write-back", "error", err)
	}
	return err
}

// writeBack sets the step's status (one time) and posts its comment.
func (d *Dispatcher) writeBack(ctx context.Context, w ticketWrite) error {
	if w.connectionID == issueConnection {
		return d.commentOnIssue(ctx, w)
	}
	source, err := d.store.withToken(w.connectionID)
	if errors.Is(err, errNotFound) {
		return fmt.Errorf("%w: the connection is deleted", errGiveUp)
	}
	if err != nil {
		return err
	}
	t := d.trackers[source.Kind]
	if t == nil {
		return fmt.Errorf("%w: the connection's kind %s is not a tracker of this Dispatcher", errGiveUp, source.Kind)
	}
	saved, err := d.store.trackerSettings(w.connectionID)
	if err != nil {
		return err
	}
	c := TrackerConnection{WebhookSource: source, Settings: trackerSettings(t, saved)}
	if w.status != "" && !w.statusSet {
		name := map[string]string{statusWorking: c.Settings.WorkingStatus, statusReview: c.Settings.ReviewStatus,
			statusWaiting: c.Settings.WaitingStatus}[w.status]
		call, cancel := context.WithTimeout(ctx, writeBackTimeout)
		err := t.SetStatus(call, c, w.ticketID, name)
		cancel()
		if err != nil {
			return fmt.Errorf("set the status %s: %w", quote(name), err)
		}
		if err := d.store.writeStatusSet(w.id); err != nil {
			return err
		}
	}
	if !w.comment {
		return nil
	}
	body, err := d.writeBackComment(w)
	if err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, writeBackTimeout)
	defer cancel()
	if err := t.Comment(call, c, w.ticketID, body); err != nil {
		return fmt.Errorf("post the comment: %w", err)
	}
	return nil
}

// checkLine is one check of an Execution's last report, for the
// pass/fail list.
type checkLine struct {
	name   string
	passed bool
}

// reportChecks gives the checks of the Execution's last valid report,
// for the pass/fail list (none: the Execution has no report).
func (s *store) reportChecks(executionID string) ([]checkLine, error) {
	var stored string
	err := s.db.QueryRow(`SELECT report_checks FROM executions WHERE id = ?`, executionID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var checks []dispatcherapi.CheckResult
	if err := unmarshalList(stored, &checks); err != nil {
		return nil, err
	}
	lines := make([]checkLine, 0, len(checks))
	for _, c := range checks {
		name := c.Name
		if name == "" {
			name = c.Command
		}
		lines = append(lines, checkLine{name: name, passed: c.Passed})
	}
	return lines, nil
}

// writeBackComment is the comment of a step.
func (d *Dispatcher) writeBackComment(w ticketWrite) (string, error) {
	run := "Run: " + d.config.SolidTraceURL + "/" + w.organizationSlug + "/runs/" + w.runID
	switch w.step {
	case stepStarted:
		return "SolidTrace Factory started a Run for this ticket.\n" + run, nil
	case stepPROpened:
		checks, err := d.store.reportChecks(w.executionID)
		if err != nil {
			return "", err
		}
		return prOpenedComment("this ticket", w.prURL, run, w.text, checks), nil
	case stepNeedsAPerson:
		return "SolidTrace Factory needs a person to continue the Run.\n" + run + "\n\nQuestion:\n" + w.text, nil
	case stepDone:
		return "SolidTrace Factory finished the Run with no PR.\n" + run + "\n\nSummary:\n" + w.text, nil
	case stepFailed:
		return "SolidTrace Factory: the Run failed.\n" + run + "\n\nReason:\n" + w.text, nil
	}
	return "", fmt.Errorf("%w: the step %s has no comment", errGiveUp, w.step)
}

// prOpenedComment is the comment of a PR that opened: the PR's link, the
// Run's link, the summary and the pass/fail list of the checks. A PR with
// no passed check has no Evidence.
func prOpenedComment(what, prURL, run, summary string, checks []checkLine) string {
	var b strings.Builder
	b.WriteString("SolidTrace Factory opened a PR for " + what + ".\nPR: " + prURL + "\n" + run + "\n\n")
	if summary != "" {
		b.WriteString("Summary:\n" + summary + "\n\n")
	}
	b.WriteString("Checks:\n")
	passed := false
	for _, c := range checks {
		result := "failed"
		if c.passed {
			result, passed = "passed", true
		}
		b.WriteString("- " + result + ": " + c.name + "\n")
	}
	if !passed {
		b.WriteString("no Evidence: no check passed.\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}
