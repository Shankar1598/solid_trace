package dispatcher

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// An interrupt goes to the live Execution of a running Run as an
// interrupt command. The wrapper sends the interrupt control request, so
// that the agent's turn (and its current tool call) stops at once. With a
// message, the wrapper writes the message when the turn stopped, and the
// Execution continues. With none, the Execution ends with the reason
// interrupted, and the Run waits on a person. The interrupt's message is a
// message of the Run (written, delivered, sent again), with the interrupt
// event in place of its accepted event: one intervention.

// eventInterrupt is the interrupt event: its actor is the person who
// interrupted the Run.
const eventInterrupt = "interrupt"

// notInterruptible is the error of an interrupt of a Run with no live
// Execution.
type notInterruptible struct{ reason string }

func (e *notInterruptible) Error() string { return e.reason }

// interruptRun is a person's interrupt of a Run, with a message or none.
func (d *Dispatcher) interruptRun(w http.ResponseWriter, r *http.Request, p person) {
	var req dispatcherapi.Interrupt
	// An empty body is an interrupt with no message.
	if r.ContentLength != 0 && !decode(w, r, &req, "the body must be a JSON object with an optional text") {
		return
	}
	text := strings.TrimSpace(req.Text)
	if len(text) > dispatcherapi.MaxMessage {
		writeProblem(w, http.StatusBadRequest, "a message can have at most 64 KiB")
		return
	}
	id := r.PathValue("id")
	by := actor{kind: actorPerson, id: p.id, name: p.name}
	accepted, err := d.store.interruptRun(p.organization.id, id, by, text, d.now())
	var refused *notInterruptible
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "no Run "+id+" in this Organization")
		return
	case errors.As(err, &refused):
		writeProblem(w, http.StatusConflict, refused.Error())
		return
	case err != nil:
		d.serverError(w, "interrupt the Run", err)
		return
	}
	d.commandsAdded.wake()
	writeJSON(w, http.StatusOK, accepted)
}

// interruptRun gives the live Execution of a running Run of the
// Organization an interrupt command, with the interrupt event, in one
// transaction. A text is kept as a message of that Execution.
func (s *store) interruptRun(organizationID, runID string, by actor, text string,
	at time.Time) (dispatcherapi.InterruptAccepted, error) {
	accepted := dispatcherapi.InterruptAccepted{RunID: runID, AcceptedAt: at.UTC()}
	tx, err := s.db.Begin()
	if err != nil {
		return accepted, err
	}
	defer tx.Rollback()
	var state string
	var closed bool
	err = tx.QueryRow(`SELECT state, closed_at IS NOT NULL FROM runs WHERE id = ? AND organization_id = ?`, runID,
		organizationID).Scan(&state, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return accepted, errNotFound
	}
	if err != nil {
		return accepted, err
	}
	if closed {
		return accepted, &notInterruptible{"the Run is closed (its PR is merged or declined, or it had no activity " +
			"for 30 days): it has no live Execution to interrupt; start a new Run"}
	}
	if state != dispatcherapi.StateRunning {
		return accepted, &notInterruptible{"the Run is " + state + ": it has no live Execution to interrupt; " +
			"only a running Run can be interrupted (a message continues a Run that waits or ended)"}
	}
	var live string
	var agreed bool
	err = tx.QueryRow(`SELECT id, end_agreed FROM executions WHERE run_id = ? AND state = ?
		ORDER BY dispatched_at DESC, rowid DESC LIMIT 1`, runID, dispatcherapi.ExecutionRunning).Scan(&live, &agreed)
	if errors.Is(err, sql.ErrNoRows) {
		return accepted, &notInterruptible{"the Run has no live Execution to interrupt: its Execution has not " +
			"started yet; send a message, which its agent gets after the prompt"}
	}
	if err != nil {
		return accepted, err
	}
	if agreed {
		return accepted, &notInterruptible{"the Run's Execution is ending: it has no live Execution to interrupt; " +
			"send a message, which a new Execution gets"}
	}
	accepted.ExecutionID = live
	details := by.details()
	details["execution.id"] = live
	if text != "" {
		accepted.MessageID = newSessionID()
		if _, err := tx.Exec(`INSERT INTO messages (id, run_id, text, actor_kind, actor_id, actor_name, accepted_at,
			execution_id, sends) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`, accepted.MessageID, runID, text, by.kind, by.id,
			by.name, at.UnixMicro(), live); err != nil {
			return accepted, err
		}
		details["message.id"] = accepted.MessageID
		details["message.text"] = text
	}
	if _, err := tx.Exec(`INSERT INTO commands (execution_id, kind, created_at, message_id) VALUES (?, ?, ?, ?)`,
		live, dispatcherapi.CommandInterrupt, at.UnixMicro(), accepted.MessageID); err != nil {
		return accepted, err
	}
	if err := recordEvent(tx, at, runID, eventInterrupt, details); err != nil {
		return accepted, err
	}
	return accepted, s.commit(tx)
}
