package dispatcher

import (
	"cmp"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Messages to a Run (R1 to R5). A message to a running Run goes to the
// live Execution as a message command, which the wrapper takes with its
// long poll and writes to the harness's stdin. A message that no live
// Execution can take (the Run is queued, or the wrapper agreed to end) is
// kept on the Run, and the next Execution's task gives it. Each message
// has the events accepted, written and delivered; the harness's replay
// echo with the message's uuid is the proof of delivery. At the end of an
// Execution, each of its messages that the harness did not deliver goes
// back to the Run, and a new Execution gets it ("sent again").

// Message event kinds.
const (
	eventMessageAccepted  = "message_accepted"
	eventMessageWritten   = "message_written"
	eventMessageDelivered = "message_delivered"
	eventMessageSentAgain = "message_sent_again"
)

// maxSends is how many Executions get a message that is not delivered.
// After that, the Run keeps it with no new Execution: a message that no
// Execution can deliver does not start Executions with no end.
const maxSends = 2

// reasonSentAgain is the reason of a Run that a message that was not
// delivered queues again.
const reasonSentAgain = "a message was not delivered: a new Execution gets it"

// messageStates are the states of a Run that take a message now. Ticket
// 59 adds the states that a message continues.
var messageStates = []string{dispatcherapi.StateQueued, dispatcherapi.StateRunning}

// continuedStates are the states that a kept message, which was not
// delivered, changes to queued at the end of an Execution.
var continuedStates = []string{dispatcherapi.StateDone, dispatcherapi.StateFailed, dispatcherapi.StateWaitingOnPerson}

// notMessageable is the error of a message to a Run that takes none now.
type notMessageable struct {
	state  string
	closed bool
}

func (e *notMessageable) Error() string {
	if e.closed {
		return "the Run is closed (its PR is merged or declined, or it had no activity for 30 days): " +
			"a message cannot continue it; start a new Run"
	}
	return "the Run is " + e.state + ": only a queued or running Run takes a message now"
}

// sendMessage is a person's message to a Run.
func (d *Dispatcher) sendMessage(w http.ResponseWriter, r *http.Request, p person) {
	var send dispatcherapi.SendMessage
	if !decode(w, r, &send, "the body must be a JSON object with text") {
		return
	}
	text := strings.TrimSpace(send.Text)
	switch {
	case text == "":
		writeProblem(w, http.StatusBadRequest, "a message needs a text")
		return
	case len(text) > dispatcherapi.MaxMessage:
		writeProblem(w, http.StatusBadRequest, "a message can have at most 64 KiB")
		return
	}
	id := r.PathValue("id")
	by := actor{kind: actorPerson, id: p.id, name: p.name}
	accepted, err := d.store.acceptMessage(p.organization.id, id, by, text, d.now())
	var refused *notMessageable
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "no Run "+id+" in this Organization")
		return
	case errors.As(err, &refused):
		writeProblem(w, http.StatusConflict, refused.Error())
		return
	case err != nil:
		d.serverError(w, "accept the message", err)
		return
	}
	d.commandsAdded.wake()
	writeJSON(w, http.StatusOK, accepted)
}

// acceptMessage keeps a message to a Run of the Organization, with its
// accepted event, in one transaction. The live Execution of a running Run
// gets it as a message command, unless its wrapper agreed to end; else
// the Run keeps it for its next Execution.
func (s *store) acceptMessage(organizationID, runID string, by actor, text string,
	at time.Time) (dispatcherapi.MessageAccepted, error) {
	accepted := dispatcherapi.MessageAccepted{ID: newSessionID(), RunID: runID, AcceptedAt: at.UTC()}
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
		return accepted, &notMessageable{state: state, closed: true}
	}
	if !slices.Contains(messageStates, state) {
		return accepted, &notMessageable{state: state}
	}
	var live string
	err = tx.QueryRow(`SELECT id FROM executions WHERE run_id = ? AND state = ? AND end_agreed = 0`, runID,
		dispatcherapi.ExecutionRunning).Scan(&live)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return accepted, err
	}
	sends := 0
	if live != "" {
		sends = 1
	}
	if _, err := tx.Exec(`INSERT INTO messages (id, run_id, text, actor_kind, actor_id, actor_name, accepted_at,
		execution_id, sends) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, accepted.ID, runID, text, by.kind, by.id, by.name,
		at.UnixMicro(), live, sends); err != nil {
		return accepted, err
	}
	details := by.details()
	details["message.id"] = accepted.ID
	details["message.text"] = text
	details["message.sha256"] = dispatcherapi.TextSHA256(text)
	if live != "" {
		if _, err := tx.Exec(`INSERT INTO commands (execution_id, kind, created_at, message_id) VALUES (?, ?, ?, ?)`,
			live, dispatcherapi.CommandMessage, at.UnixMicro(), accepted.ID); err != nil {
			return accepted, err
		}
		details["execution.id"] = live
	}
	if err := recordEvent(tx, at, runID, eventMessageAccepted, details); err != nil {
		return accepted, err
	}
	return accepted, s.commit(tx)
}

// messageEvent is the wrapper's message call: it wrote a message of its
// Execution, or the harness delivered it.
func (d *Dispatcher) messageEvent(w http.ResponseWriter, r *http.Request, e storedExecution) {
	var ev dispatcherapi.MessageEvent
	if !decode(w, r, &ev, "the body must be a JSON object with id, event and at") {
		return
	}
	if ev.Event != dispatcherapi.MessageWritten && ev.Event != dispatcherapi.MessageDelivered {
		writeProblem(w, http.StatusBadRequest, "the event must be written or delivered")
		return
	}
	err := d.store.markMessage(e.id, ev, d.now())
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "the Execution has no message "+ev.ID)
	case errors.Is(err, errEnded):
		writeProblem(w, http.StatusConflict, errEnded.Error())
	case err != nil:
		d.serverError(w, "record the message event", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// markMessage records a message event of a running Execution. A repeat
// changes nothing.
func (s *store) markMessage(executionID string, ev dispatcherapi.MessageEvent, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID, state string
	if err := tx.QueryRow(`SELECT run_id, state FROM executions WHERE id = ?`, executionID).Scan(&runID,
		&state); err != nil {
		return err
	}
	if state != dispatcherapi.ExecutionRunning {
		return errEnded
	}
	m := dispatcherapi.MessageState{ID: ev.ID}
	wrapperAt := ev.At.UTC()
	m.WrittenAt = &wrapperAt
	if ev.Event == dispatcherapi.MessageDelivered {
		m.DeliveredAt = &wrapperAt
	}
	found, err := settleMessage(tx, runID, executionID, m, at)
	if err != nil {
		return err
	}
	if !found {
		return errNotFound
	}
	return s.commit(tx)
}

// messageDetails are the Attributes that name a message in an event: its
// id (the uuid that the wrapper writes), the hash of its text and the
// person who sent it as the event's actor, so that the Agent session page
// can say who said it.
func messageDetails(tx *sql.Tx, id string) (map[string]any, error) {
	var text, kind, actorID, name string
	if err := tx.QueryRow(`SELECT text, actor_kind, actor_id, actor_name FROM messages WHERE id = ?`, id).
		Scan(&text, &kind, &actorID, &name); err != nil {
		return nil, err
	}
	details := actor{kind: kind, id: actorID, name: name}.details()
	details["message.id"] = id
	details["message.sha256"] = dispatcherapi.TextSHA256(text)
	return details, nil
}

// settleMessage records what the wrapper did with a message of its
// Execution: the written event and the delivered event, each one time.
// A delivered message was written. It is false when the Execution has no
// such message.
func settleMessage(tx *sql.Tx, runID, executionID string, m dispatcherapi.MessageState, at time.Time) (bool, error) {
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM messages WHERE id = ? AND execution_id = ?)`, m.ID,
		executionID).Scan(&exists); err != nil || !exists {
		return false, err
	}
	written := m.WrittenAt
	if written == nil {
		written = m.DeliveredAt
	}
	steps := []struct {
		column, kind string
		at           *time.Time
	}{{"written_at", eventMessageWritten, written}, {"delivered_at", eventMessageDelivered, m.DeliveredAt}}
	for _, step := range steps {
		if step.at == nil {
			continue
		}
		result, err := tx.Exec(`UPDATE messages SET `+step.column+` = ? WHERE id = ? AND `+step.column+` IS NULL`,
			step.at.UnixMicro(), m.ID)
		if err != nil {
			return false, err
		}
		if n, err := result.RowsAffected(); err != nil || n == 0 {
			if err != nil {
				return false, err
			}
			continue
		}
		details, err := messageDetails(tx, m.ID)
		if err != nil {
			return false, err
		}
		details["execution.id"] = executionID
		details["message.at"] = step.at.UTC().Format(time.RFC3339Nano)
		if err := recordEvent(tx, at, runID, step.kind, details); err != nil {
			return false, err
		}
	}
	return true, nil
}

// settleMessages is the messages' part of the end of an Execution, in its
// transaction: it records what the wrapper did with each message (states,
// nil when the wrapper made no end call), and gives each message of the
// Execution that the harness did not deliver back to the Run. It gives
// the ids of those messages.
func settleMessages(tx *sql.Tx, runID, executionID string, states []dispatcherapi.MessageState,
	at time.Time) ([]string, error) {
	for _, m := range states {
		if _, err := settleMessage(tx, runID, executionID, m, at); err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query(`UPDATE messages SET execution_id = '', in_task = 0, written_at = NULL
		WHERE execution_id = ? AND delivered_at IS NULL RETURNING id, seq`, executionID)
	if err != nil {
		return nil, err
	}
	type returned struct {
		id  string
		seq int64
	}
	var list []returned
	for rows.Next() {
		var m returned
		if err := rows.Scan(&m.id, &m.seq); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// RETURNING gives no order.
	slices.SortFunc(list, func(a, b returned) int { return cmp.Compare(a.seq, b.seq) })
	ids := make([]string, len(list))
	for i, m := range list {
		ids[i] = m.id
	}
	return ids, nil
}

// continueForMessages decides, in the end's transaction, if the Run's
// kept messages queue it for a new Execution: the Run is still running
// (a stop wins), its new state is done, failed or waiting on a person, it
// is not closed, its PR has no outcome, and a kept message went to fewer
// than maxSends Executions. Then it gives the sent-again event of each
// message of returned (of the Execution that ended) that goes again.
func continueForMessages(tx *sql.Tx, runID, executionID, state string, returned []string, at time.Time) (bool, error) {
	if !slices.Contains(continuedStates, state) {
		return false, nil
	}
	var due bool
	err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM messages m JOIN runs r ON r.id = m.run_id WHERE m.run_id = ?
		AND m.execution_id = '' AND m.delivered_at IS NULL AND m.sends < ? AND r.closed_at IS NULL
		AND r.pr_outcome = '' AND r.state = ?)`, runID, maxSends, dispatcherapi.StateRunning).Scan(&due)
	if err != nil || !due {
		return false, err
	}
	for _, id := range returned {
		var sends int
		if err := tx.QueryRow(`SELECT sends FROM messages WHERE id = ?`, id).Scan(&sends); err != nil {
			return false, err
		}
		if sends >= maxSends {
			continue
		}
		details, err := messageDetails(tx, id)
		if err != nil {
			return false, err
		}
		details["execution.id"] = executionID
		if err := recordEvent(tx, at, runID, eventMessageSentAgain, details); err != nil {
			return false, err
		}
	}
	return true, nil
}

// takeKeptMessages gives an Execution that starts the Run's kept messages
// that it can still send, oldest first, in its task call's transaction.
// A second task call gives the same messages.
func takeKeptMessages(tx *sql.Tx, runID, executionID string, starting bool) ([]dispatcherapi.Message, error) {
	if starting {
		if _, err := tx.Exec(`UPDATE messages SET execution_id = ?, in_task = 1, sends = sends + 1 WHERE run_id = ?
			AND execution_id = '' AND delivered_at IS NULL AND sends < ?`, executionID, runID, maxSends); err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query(`SELECT id, text FROM messages WHERE execution_id = ? AND in_task = 1 ORDER BY seq`,
		executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []dispatcherapi.Message
	for rows.Next() {
		var m dispatcherapi.Message
		if err := rows.Scan(&m.ID, &m.Text); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// compareEnd is the wrapper's compare-and-end call (R3, R4): the wrapper
// asks to end its Execution, with the seq of the last command that it
// got. With a later message command, the Dispatcher gives it, and the
// wrapper writes it. With none, it agrees, and the Execution gets no more
// messages: a later message waits for a new Execution.
func (d *Dispatcher) compareEnd(w http.ResponseWriter, r *http.Request, e storedExecution) {
	var req dispatcherapi.CompareEnd
	if !decode(w, r, &req, "the body must be a JSON object with after") {
		return
	}
	answer, err := d.store.compareEnd(e.id, req.After)
	if errors.Is(err, errEnded) {
		writeProblem(w, http.StatusConflict, errEnded.Error())
		return
	}
	if err != nil {
		d.serverError(w, "compare the end", err)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

func (s *store) compareEnd(executionID string, after int64) (dispatcherapi.CompareEndAnswer, error) {
	answer := dispatcherapi.CompareEndAnswer{Commands: []dispatcherapi.Command{}}
	tx, err := s.db.Begin()
	if err != nil {
		return answer, err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRow(`SELECT state FROM executions WHERE id = ?`, executionID).Scan(&state); err != nil {
		return answer, err
	}
	if state != dispatcherapi.ExecutionRunning {
		return answer, errEnded
	}
	later, err := commandsIn(tx, executionID, after, dispatcherapi.CommandMessage)
	if err != nil {
		return answer, err
	}
	if len(later) > 0 {
		answer.Commands = later
		return answer, nil
	}
	if _, err := tx.Exec(`UPDATE executions SET end_agreed = 1 WHERE id = ?`, executionID); err != nil {
		return answer, err
	}
	answer.Agreed = true
	return answer, tx.Commit()
}

// querier is a *sql.DB or a *sql.Tx.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// commandsIn gives the Execution's commands after seq after, oldest
// first, with the message of each message command. A kind limits them to
// that kind.
func commandsIn(q querier, executionID string, after int64, kind string) ([]dispatcherapi.Command, error) {
	rows, err := q.Query(`SELECT c.seq, c.kind, COALESCE(m.id, ''), COALESCE(m.text, '') FROM commands c
		LEFT JOIN messages m ON c.message_id != '' AND m.id = c.message_id
		WHERE c.execution_id = ? AND c.seq > ? AND (? = '' OR c.kind = ?) ORDER BY c.seq`,
		executionID, after, kind, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []dispatcherapi.Command
	for rows.Next() {
		var c dispatcherapi.Command
		var m dispatcherapi.Message
		if err := rows.Scan(&c.Seq, &c.Kind, &m.ID, &m.Text); err != nil {
			return nil, err
		}
		if c.Kind == dispatcherapi.CommandMessage {
			c.Message = &m
		}
		list = append(list, c)
	}
	return list, rows.Err()
}
