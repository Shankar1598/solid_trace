package dispatcher

import (
	"database/sql"
	"errors"
	"net/http"
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
// A take-over Execution's end sets the Run's state from a report made
// during it; with none, the Run waits on a person. Messages to a
// taken-over Run are kept, and queue the Run when the take-over ends.

// eventTakeOver is the take-over event: its actor is the person.
const eventTakeOver = "take_over"

// reasonMessagesAfterTakeOver is the reason of a Run that the messages
// kept during its take-over queue when the take-over ends.
const reasonMessagesAfterTakeOver = "messages came during the take-over: a new Execution gets them"

// notTakeOverable is the error of a take-over of a Run that cannot be
// taken over now.
type notTakeOverable struct {
	state  string
	closed bool
	// by is the person who took the Run over, for a Run that is taken
	// over already.
	by string
}

func (e *notTakeOverable) Error() string {
	switch {
	case e.closed:
		return "the Run is closed (its PR is merged or declined, or it had no activity for 30 days): " +
			"it cannot be taken over; start a new Run"
	case e.state == dispatcherapi.StateTakenOver:
		return "the Run is taken over already, by " + e.by
	case e.state == dispatcherapi.StateRunning:
		return "the Run's Execution has not started on its machine yet: take it over when it runs"
	}
	return "the Run is " + e.state + ": only a running Run can be taken over"
}

// takeOverRun is a person's take-over of a running Run.
func (d *Dispatcher) takeOverRun(w http.ResponseWriter, r *http.Request, p person) {
	id := r.PathValue("id")
	by := actor{kind: actorPerson, id: p.id, name: p.name}
	err := d.store.takeOver(p.organization.id, id, by, newRunID(), d.now())
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

// takeOver takes over a running Run of the Organization in one
// transaction: the take-over Execution newID (dispatched, in the
// interactive form, in the workload of the running headless Execution, on
// its account and with its factory file), the take-over command for the
// headless Execution, the Run's state taken over, and the take-over event
// of by. It gives errNotFound or a *notTakeOverable.
func (s *store) takeOver(organizationID, runID string, by actor, newID string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	var closed bool
	err = tx.QueryRow(`SELECT state, closed_at IS NOT NULL FROM runs WHERE id = ? AND organization_id = ?`, runID,
		organizationID).Scan(&state, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	switch {
	case closed:
		return &notTakeOverable{state: state, closed: true}
	case state == dispatcherapi.StateTakenOver:
		var name string
		err := tx.QueryRow(`SELECT taken_over_by_name FROM executions WHERE run_id = ? AND form = ?
			ORDER BY dispatched_at DESC, rowid DESC LIMIT 1`, runID, dispatcherapi.FormInteractive).Scan(&name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return &notTakeOverable{state: state, by: name}
	case state != dispatcherapi.StateRunning:
		return &notTakeOverable{state: state}
	}
	// The headless Execution that runs on a machine: its wrapper took
	// its task (a dispatched one has no machine yet).
	var headless, workload string
	err = tx.QueryRow(`SELECT id, workload FROM executions WHERE run_id = ? AND state = ? AND form = ''
		ORDER BY dispatched_at DESC, rowid DESC LIMIT 1`, runID, dispatcherapi.ExecutionRunning).Scan(&headless, &workload)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && workload == "") {
		return &notTakeOverable{state: state}
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO executions (id, run_id, state, dispatched_at, account, workload, factory_file,
		vm, attach, form, taken_over_by_id, taken_over_by_name, after_execution)
		SELECT ?, run_id, ?, ?, account, workload, factory_file, vm, attach, ?, ?, ?, id FROM executions WHERE id = ?`,
		newID, dispatcherapi.ExecutionDispatched, at.UnixMicro(), dispatcherapi.FormInteractive, by.id, by.name,
		headless); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO commands (execution_id, kind, created_at) VALUES (?, ?, ?)`,
		headless, dispatcherapi.CommandTakeOver, at.UnixMicro()); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, question = '', summary = '' WHERE id = ?`,
		dispatcherapi.StateTakenOver, "taken over by "+by.name, runID); err != nil {
		return err
	}
	details := by.details()
	details["execution.id"] = newID
	details["take_over.from_execution"] = headless
	if err := addPlace(tx, newID, details); err != nil {
		return err
	}
	if err := recordEvent(tx, at, runID, eventTakeOver, details); err != nil {
		return err
	}
	return s.commit(tx)
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
