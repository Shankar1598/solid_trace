package dispatcher

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The take-over (ticket 61). A person takes over a running Run: the
// Dispatcher makes the take-over Execution, in the interactive form, in
// the same Runner workload as the running headless Execution, and gives
// that Execution a take-over command. Its wrapper ends the headless
// Execution with the reason "taken over"; the end call's answer names the
// take-over Execution, which the same wrapper then starts on the same
// machine. The Run is taken over at once.
//
// A take-over of a Run with no live Execution (ticket 62: it waits on a
// person, is done, failed or stopped) queues the Run, with the person who
// took it over. The Dispatcher then dispatches its next Execution in the
// interactive form, on a new machine. When that Execution starts, the
// Run is taken over. A Run that waits on an account limit refuses a
// take-over, with the reset time: no account can run it before then.
//
// A take-over Execution's end sets the Run's state from a report made
// during it; with none, the Run waits on a person. Messages to a
// taken-over Run are kept, and queue the Run when the take-over ends.

// eventTakeOver is the take-over event: its actor is the person.
const eventTakeOver = "take_over"

// reasonMessagesAfterTakeOver is the reason of a Run that the messages
// kept during its take-over queue when the take-over ends.
const reasonMessagesAfterTakeOver = "messages came during the take-over: a new Execution gets them"

// reasonQueuedForTakeOver is the reason of a Run that a take-over
// queued, until its take-over Execution starts.
func reasonQueuedForTakeOver(by string) string {
	return "queued for the take-over by " + by + ": a new Execution continues the Agent session in a terminal"
}

// notTakeOverable is the error of a take-over of a Run that cannot be
// taken over now.
type notTakeOverable struct {
	state  string
	closed bool
	// by is the person who took the Run over, for a Run that is taken
	// over already or queued for a take-over.
	by string
	// resetAt is the reset time of the account limit that a Run waits on
	// (nil: not known).
	resetAt *time.Time
}

func (e *notTakeOverable) Error() string {
	switch {
	case e.closed:
		return "the Run is closed (its PR is merged or declined, or it had no activity for 30 days): " +
			"it cannot be taken over; start a new Run"
	case e.state == dispatcherapi.StateTakenOver:
		return "the Run is taken over already, by " + e.by
	case e.state == dispatcherapi.StateQueued && e.by != "":
		return "the Run is queued for a take-over already, by " + e.by
	case e.state == dispatcherapi.StateRunning:
		return "the Run's Execution has not started on its machine yet: take it over when it runs"
	case e.state == dispatcherapi.StateWaitingOnAccountLimit && e.resetAt != nil:
		return "the Run waits on an account limit until " + limitText(*e.resetAt) +
			": no account can run it before then; take it over when it runs again"
	case e.state == dispatcherapi.StateWaitingOnAccountLimit:
		return "the Run waits on an account limit: no account can run it now; take it over when it runs again"
	}
	return "the Run is " + e.state + ": it can be taken over when it runs, waits on a person, is done, failed or stopped"
}

// takeOverRun is a person's take-over of a running Run.
func (d *Dispatcher) takeOverRun(w http.ResponseWriter, r *http.Request, p person) {
	id := r.PathValue("id")
	by := actor{kind: actorPerson, id: p.id, name: p.name}
	queued, err := d.store.takeOver(p.organization.id, id, by, newRunID(), d.now())
	var refused *notTakeOverable
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "no Run "+id+" in this Organization")
		return
	case errors.As(err, &refused):
		writeProblem(w, http.StatusConflict, refused.Error())
		return
	case err != nil:
		d.serverError(w, "take over the Run", err)
		return
	}
	d.commandsAdded.wake()
	if queued {
		d.wakeDispatch()
	}
	run, err := d.store.run(p.organization.id, id)
	if err == nil {
		run.Executions, err = d.store.executionsOf(id)
	}
	if err != nil {
		d.serverError(w, "read the Run", err)
		return
	}
	writeJSON(w, http.StatusOK, d.withURL(run, p))
}

// takeOver takes over a Run of the Organization in one transaction. A
// running Run gets the take-over Execution newID (dispatched, in the
// interactive form, in the workload of the running headless Execution, on
// its account and with its factory file), the take-over command for the
// headless Execution, and the state taken over. A Run with no live
// Execution (continuingStates) is queued for the take-over of by: queued
// is true. Each take-over has the take-over event of by. It gives
// errNotFound or a *notTakeOverable.
func (s *store) takeOver(organizationID, runID string, by actor, newID string, at time.Time) (queued bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var state, prOutcome, queuedBy string
	var closed bool
	err = tx.QueryRow(`SELECT state, closed_at IS NOT NULL, pr_outcome, take_over_by_name FROM runs
		WHERE id = ? AND organization_id = ?`, runID, organizationID).Scan(&state, &closed, &prOutcome, &queuedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errNotFound
	}
	if err != nil {
		return false, err
	}
	switch {
	// A Run whose PR has an outcome is closed when no Execution runs (as
	// for a message).
	case closed || (prOutcome != "" && state != dispatcherapi.StateRunning && state != dispatcherapi.StateTakenOver):
		return false, &notTakeOverable{state: state, closed: true}
	case state == dispatcherapi.StateTakenOver:
		var name string
		err := tx.QueryRow(`SELECT taken_over_by_name FROM executions WHERE run_id = ? AND form = ?
			ORDER BY dispatched_at DESC, rowid DESC LIMIT 1`, runID, dispatcherapi.FormInteractive).Scan(&name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		return false, &notTakeOverable{state: state, by: name}
	case slices.Contains(continuingStates, state):
		return true, s.queueTakeOver(tx, runID, state, by, at)
	case state == dispatcherapi.StateWaitingOnAccountLimit:
		reset, err := s.waitingResetTime(tx, runID, at)
		if err != nil {
			return false, err
		}
		return false, &notTakeOverable{state: state, resetAt: reset}
	case state != dispatcherapi.StateRunning:
		return false, &notTakeOverable{state: state, by: queuedBy}
	}
	// The headless Execution that runs on a machine: its wrapper took
	// its task (a dispatched one has no machine yet).
	var headless, workload string
	err = tx.QueryRow(`SELECT id, workload FROM executions WHERE run_id = ? AND state = ? AND form = ''
		ORDER BY dispatched_at DESC, rowid DESC LIMIT 1`, runID, dispatcherapi.ExecutionRunning).Scan(&headless, &workload)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && workload == "") {
		return false, &notTakeOverable{state: state}
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO executions (id, run_id, state, dispatched_at, account, workload, factory_file,
		vm, attach, form, taken_over_by_id, taken_over_by_name, after_execution)
		SELECT ?, run_id, ?, ?, account, workload, factory_file, vm, attach, ?, ?, ?, id FROM executions WHERE id = ?`,
		newID, dispatcherapi.ExecutionDispatched, at.UnixMicro(), dispatcherapi.FormInteractive, by.id, by.name,
		headless); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO commands (execution_id, kind, created_at) VALUES (?, ?, ?)`,
		headless, dispatcherapi.CommandTakeOver, at.UnixMicro()); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, question = '', summary = '' WHERE id = ?`,
		dispatcherapi.StateTakenOver, "taken over by "+by.name, runID); err != nil {
		return false, err
	}
	details := by.details()
	details["execution.id"] = newID
	details["take_over.from_execution"] = headless
	if err := addPlace(tx, newID, details); err != nil {
		return false, err
	}
	if err := recordEvent(tx, at, runID, eventTakeOver, details); err != nil {
		return false, err
	}
	return false, s.commit(tx)
}

// queueTakeOver queues a Run with no live Execution, in the state from,
// for the take-over of by, with the take-over event, in tx. The Run's
// next Execution is in the interactive form. The Execution of a stopped
// Run can still be in its end: the queue waits for that end, and the end
// does not change the queued Run (see ownStateSQL).
func (s *store) queueTakeOver(tx *sql.Tx, runID, from string, by actor, at time.Time) error {
	if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, question = '', summary = '', flags = '',
		take_over_by_id = ?, take_over_by_name = ? WHERE id = ?`, dispatcherapi.StateQueued,
		reasonQueuedForTakeOver(by.name), by.id, by.name, runID); err != nil {
		return err
	}
	details := by.details()
	details["run.continued_from"] = from
	if err := recordEvent(tx, at, runID, eventTakeOver, details); err != nil {
		return err
	}
	return s.commit(tx)
}

// waitingResetTime is the reset time after which a Run that waits on an
// account limit can continue at now (nil: not known): with the wait
// setting, the reset time of the Run's own account; else the first reset
// time in the pool.
func (s *store) waitingResetTime(tx *sql.Tx, runID string, now time.Time) (*time.Time, error) {
	if s.waitAtLimit {
		var want string
		if err := tx.QueryRow(`SELECT next_account FROM runs WHERE id = ?`, runID).Scan(&want); err != nil {
			return nil, err
		}
		if want == "" {
			var err error
			if want, err = lastAccount(tx, runID); err != nil {
				return nil, err
			}
		}
		return limitOf(tx, want, now)
	}
	var first sql.NullInt64
	if err := tx.QueryRow(`SELECT MIN(limited_until) FROM accounts WHERE limited_until > ?`, now.UnixMicro()).
		Scan(&first); err != nil {
		return nil, err
	}
	if !first.Valid {
		return nil, nil
	}
	return timeOf(first), nil
}

// addPlace adds the Execution's machine (execution.vm) and the command
// that attaches a person to a take-over there (execution.attach) to an
// event's details, when the wrapper gave them.
func addPlace(tx *sql.Tx, executionID string, details map[string]any) error {
	var vm, attach, form string
	if err := tx.QueryRow(`SELECT vm, attach, form FROM executions WHERE id = ?`, executionID).Scan(&vm, &attach,
		&form); err != nil {
		return err
	}
	if vm != "" {
		details["execution.vm"] = vm
	}
	if form == dispatcherapi.FormInteractive {
		details["execution.form"] = form
		if attach != "" {
			details["execution.attach"] = attach
		}
	}
	return nil
}

// takeOverAfter gives the take-over Execution that follows the Execution
// in its workload and has not started ("" is none): the wrapper's end
// call answers with it.
func (s *store) takeOverAfter(executionID string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM executions WHERE after_execution = ? AND form = ? AND state = ?`,
		executionID, dispatcherapi.FormInteractive, dispatcherapi.ExecutionDispatched).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// executionsOfWorkload gives the Execution and each later Execution in
// its workload (a take-over) that has not ended, oldest first: when the
// workload ends, each of them ends.
func (s *store) executionsOfWorkload(executionID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT e.id FROM executions e JOIN executions first ON first.id = ?
		WHERE (e.id = first.id OR (first.workload != '' AND e.workload = first.workload)) AND e.state != ?
		ORDER BY e.dispatched_at, e.rowid`, executionID, dispatcherapi.ExecutionEnded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
