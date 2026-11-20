package dispatcher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

// The close of a Run. A Closed Run cannot continue: its PR is merged or
// declined, or closeAfter passed since its last activity. It keeps its
// last state (Closed is not a state). The close loop checks the PRs of
// the Runs that are not closed every prCheckEvery of the Dispatcher's
// clock, as the backstop of the forge's webhook, and records a merged or
// declined outcome with its PR outcome event. A Run with an Execution
// queued or live is not closed: its Execution can still save a
// work-in-progress ref. The Dispatcher gives it no new Execution, and the
// first pass after the end of its Execution closes it. Each pass also
// closes each Run with no activity for closeAfter. At the close, the
// closed event goes to the outbox, and the Dispatcher deletes the Run's
// work-in-progress ref.

const (
	// prCheckEvery is the time of the Dispatcher's clock between two
	// checks of the PRs of the Runs that are not closed.
	prCheckEvery = 5 * time.Minute
	// closeAfter is the time with no activity that closes a Run.
	closeAfter = 30 * 24 * time.Hour
	// closeEvery is the wait between two passes of the close loop.
	closeEvery = 30 * time.Second
	// wipDeleteFor is how long after the close the Dispatcher tries to
	// delete the Run's work-in-progress ref, at each PR check. Then it
	// gives up with a warning.
	wipDeleteFor = 24 * time.Hour
	// forgeCallBudget is the longest time of one forge call of the loop.
	forgeCallBudget = time.Minute
)

// Close reasons (run.close_reason of the closed event).
const (
	closePRMerged   = "pr_merged"
	closePRDeclined = "pr_declined"
	closeNoActivity = "no_activity"
)

// activeRunSQL is the SQL test of an Active Run in the runs table, with
// the arguments activeRunArgs: the Run is not closed, and it is in a
// state of activeStates, or has a PR (a Run whose PR has an outcome is
// closed when its Execution ends).
func activeRunSQL() string {
	return `(closed_at IS NULL AND (state IN ` + inList(len(activeStates)) + ` OR pr_id != 0))`
}

// noLiveExecutionSQL is the SQL test that the Run of the runs table has
// no Execution queued or live, with the argument ExecutionEnded.
const noLiveExecutionSQL = `NOT EXISTS (SELECT 1 FROM executions e WHERE e.run_id = runs.id AND e.state != ?)`

func activeRunArgs() []any { return anys(activeStates) }

// closeLoop does a pass at once, then every closeEvery. A pass checks the
// PRs and deletes the due work-in-progress refs when prCheckEvery passed
// since the last check (and in the first pass after a start), then closes
// the Runs whose PR has an outcome and the Runs with no activity for
// closeAfter, when they have no Execution queued or live.
func (d *Dispatcher) closeLoop(ctx context.Context) {
	var nextCheck time.Time
	for {
		now := d.now()
		if !now.Before(nextCheck) {
			d.checkPRs(ctx, now)
			d.deleteDueWIPRefs(ctx, now)
			nextCheck = now.Add(prCheckEvery)
		}
		d.closeDecidedRuns(ctx, now)
		d.closeIdleRuns(ctx, now)
		if d.closeChecked != nil {
			d.closeChecked(now)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.closeEvery):
		}
	}
}

// closingRun is a Run that the close loop reads: where its PR and its
// work-in-progress ref are.
type closingRun struct {
	id, organizationID, projectID, repository, prOutcome string
	prID                                                 int
	closedAt                                             time.Time
}

func (d *Dispatcher) checkPRs(ctx context.Context, now time.Time) {
	runs, err := d.store.closingRuns(`closed_at IS NULL AND pr_id != 0 AND pr_outcome = ''`)
	if err != nil {
		d.log.Error("could not read the Runs with an open PR", "error", err)
		return
	}
	for _, r := range runs {
		if ctx.Err() != nil {
			return
		}
		outcome, err := d.prOutcome(ctx, r)
		if err != nil {
			d.log.Warn("could not check the PR of a Run: the next check tries again", "run", r.id,
				"pr", r.prID, "error", err)
			continue
		}
		if outcome == "" {
			continue
		}
		if err := d.store.savePROutcome(r.id, outcome, now); err != nil {
			d.log.Error("could not record the outcome of a Run's PR", "run", r.id, "error", err)
			continue
		}
		d.log.Info("the PR of a Run has an outcome", "run", r.id, "outcome", outcome)
	}
}

// closeDecidedRuns closes each Run that is not closed, whose PR has an
// outcome, and that has no Execution queued or live.
func (d *Dispatcher) closeDecidedRuns(ctx context.Context, now time.Time) {
	runs, err := d.store.closingRuns(`closed_at IS NULL AND pr_outcome != '' AND `+noLiveExecutionSQL,
		dispatcherapi.ExecutionEnded)
	if err != nil {
		d.log.Error("could not read the Runs whose PR has an outcome", "error", err)
		return
	}
	for _, r := range runs {
		if ctx.Err() != nil {
			return
		}
		reason := closePRDeclined
		if r.prOutcome == dispatcherapi.PRMerged {
			reason = closePRMerged
		}
		d.close(ctx, r, reason, now)
	}
}

// prOutcome asks the forge for the state of the Run's PR: merged,
// declined, or "" while it is open.
func (d *Dispatcher) prOutcome(ctx context.Context, r closingRun) (string, error) {
	f, repository, err := d.runForge(r)
	if err != nil {
		return "", err
	}
	return prOutcomeOn(ctx, f, repository, r.prID)
}

// prOutcomeOn asks f for the state of the PR with id in repository.
func prOutcomeOn(ctx context.Context, f forge.Forge, repository string, id int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, forgeCallBudget)
	defer cancel()
	pr, err := f.PR(ctx, repository, id)
	if err != nil {
		return "", err
	}
	switch pr.State {
	case forge.PRMerged:
		return dispatcherapi.PRMerged, nil
	case forge.PRDeclined, forge.PRSuperseded:
		return dispatcherapi.PRDeclined, nil
	}
	return "", nil
}

// closeIdleRuns closes each Run that is not closed and had no activity
// for closeAfter, unless it has an Execution queued or live.
func (d *Dispatcher) closeIdleRuns(ctx context.Context, now time.Time) {
	runs, err := d.store.idleRuns(now.Add(-closeAfter))
	if err != nil {
		d.log.Error("could not read the Runs with no activity", "error", err)
		return
	}
	for _, r := range runs {
		if ctx.Err() != nil {
			return
		}
		d.close(ctx, r, closeNoActivity, now)
	}
}

// close closes the Run with the reason, then deletes its work-in-progress
// ref. A Run that is closed already, or has an Execution queued or live,
// stays as it is.
func (d *Dispatcher) close(ctx context.Context, r closingRun, reason string, now time.Time) {
	closed, err := d.store.closeRun(r.id, reason, now)
	if err != nil {
		d.log.Error("could not close a Run", "run", r.id, "error", err)
		return
	}
	if !closed {
		return
	}
	d.log.Info("a Run is closed", "run", r.id, "reason", reason)
	r.closedAt = now
	d.deleteWIPRef(ctx, r, now)
}

func (d *Dispatcher) deleteDueWIPRefs(ctx context.Context, now time.Time) {
	runs, err := d.store.closingRuns(`wip_delete_due = 1`)
	if err != nil {
		d.log.Error("could not read the Runs whose work-in-progress ref is to delete", "error", err)
		return
	}
	for _, r := range runs {
		if ctx.Err() != nil {
			return
		}
		d.deleteWIPRef(ctx, r, now)
	}
}

// deleteWIPRef deletes the Closed Run's work-in-progress ref on the
// forge. A ref that is not there is no error. On a failure, the next PR
// check tries again, until wipDeleteFor after the close: then a warning
// tells the operator to delete it.
func (d *Dispatcher) deleteWIPRef(ctx context.Context, r closingRun, now time.Time) {
	f, repository, err := d.runForge(r)
	if err == nil {
		call, cancel := context.WithTimeout(ctx, forgeCallBudget)
		err = f.DeleteWIP(call, repository, r.id)
		cancel()
	}
	if err == nil {
		if err := d.store.wipDeleteDone(r.id); err != nil {
			d.log.Error("could not record the delete of a work-in-progress ref", "run", r.id, "error", err)
		}
		return
	}
	if ctx.Err() != nil {
		return
	}
	if now.Sub(r.closedAt) < wipDeleteFor {
		d.log.Warn("could not delete the work-in-progress ref of a Closed Run: the next PR check tries again",
			"run", r.id, "error", err)
		return
	}
	d.warn(fmt.Sprintf("could not delete the work-in-progress ref of the Closed Run %s for a day: %v; "+
		"the Dispatcher does not try again, so delete it on the forge by hand", r.id, err))
	if err := d.store.wipDeleteDone(r.id); err != nil {
		d.log.Error("could not record the end of the deletes of a work-in-progress ref", "run", r.id, "error", err)
	}
}

// runForge gives the Forge of the Run's Project and the Run's
// repository.
func (d *Dispatcher) runForge(r closingRun) (forge.Forge, string, error) {
	settings, err := d.store.settings(r.organizationID, r.projectID)
	if errors.Is(err, errNotFound) {
		return nil, "", errors.New("the Run's Project has no Factory settings")
	}
	if err != nil {
		return nil, "", err
	}
	repository := r.repository
	if repository == "" {
		repository = settings.Repository
	}
	f, err := d.projectForge(r.organizationID, settings.Connection)
	return f, repository, err
}

const closingColumns = `id, organization_id, project_id, repository, pr_id, pr_outcome, closed_at FROM runs`

// closingRuns reads the Runs that match where, oldest first.
func (s *store) closingRuns(where string, args ...any) ([]closingRun, error) {
	rows, err := s.db.Query(`SELECT `+closingColumns+` WHERE `+where+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []closingRun
	for rows.Next() {
		var r closingRun
		var closed sql.NullInt64
		if err := rows.Scan(&r.id, &r.organizationID, &r.projectID, &r.repository, &r.prID, &r.prOutcome,
			&closed); err != nil {
			return nil, err
		}
		if closed.Valid {
			r.closedAt = time.UnixMicro(closed.Int64)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// idleRuns are the Runs that are not closed, with no Execution queued or
// live, whose last activity is at before or earlier. The last activity is
// the end of the Run's last Execution or the last message that it
// accepted, or the Run's start when it has neither.
func (s *store) idleRuns(before time.Time) ([]closingRun, error) {
	return s.closingRuns(`closed_at IS NULL AND state NOT IN `+inList(len(liveStates))+`
		AND MAX(created_at, COALESCE((SELECT MAX(e.ended_at) FROM executions e WHERE e.run_id = runs.id), 0),
		COALESCE((SELECT MAX(m.accepted_at) FROM messages m WHERE m.run_id = runs.id), 0)) <= ?
		AND `+noLiveExecutionSQL,
		append(anys(liveStates), before.UnixMicro(), dispatcherapi.ExecutionEnded)...)
}

// savePROutcome records the outcome of the Run's PR at at, with its PR
// outcome event, in one transaction. A Run that is closed, or whose PR
// has an outcome already, stays as it is. The close comes after it.
func (s *store) savePROutcome(runID, outcome string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE runs SET pr_outcome = ? WHERE id = ? AND closed_at IS NULL AND pr_outcome = ''`,
		outcome, runID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return err
	}
	if err := recordEvent(tx, at, runID, eventPROutcome, map[string]any{"pr.outcome": outcome}); err != nil {
		return err
	}
	return s.commit(tx)
}

// closeRun closes the Run at at with the reason, in one transaction with
// its closed event. It is false when the Run is closed already, or has
// an Execution queued or live: a Closed Run has no Execution that can
// save a work-in-progress ref after the close.
func (s *store) closeRun(runID, reason string, at time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE runs SET closed_at = ?, wip_delete_due = 1
		WHERE id = ? AND closed_at IS NULL AND `+noLiveExecutionSQL, at.UnixMicro(), runID,
		dispatcherapi.ExecutionEnded)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	if err := recordEvent(tx, at, runID, eventClosed, map[string]any{"run.close_reason": reason}); err != nil {
		return false, err
	}
	return true, s.commit(tx)
}

// wipDeleteDone records that the Dispatcher does not delete the Run's
// work-in-progress ref again.
func (s *store) wipDeleteDone(runID string) error {
	_, err := s.db.Exec(`UPDATE runs SET wip_delete_due = 0 WHERE id = ?`, runID)
	return err
}
