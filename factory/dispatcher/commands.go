package dispatcher

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Commands go to a wrapper by its long poll (the commands call), so no
// inbound path to an agent machine is needed. The stop verb and the
// backstop add a stop command for the Run's live Execution.

// defaultLimits are the limits of an Execution whose factory file sets
// none.
var defaultLimits = dispatcherapi.Limits{
	IdleSeconds:  int64(dispatcherapi.DefaultIdleLimit / time.Second),
	TotalSeconds: int64(dispatcherapi.DefaultTotalLimit / time.Second),
}

// backstopMargin: the Dispatcher stops an Execution that passes its total
// limit by more than this. The wrapper's own timer ends it at the limit.
const backstopMargin = 15 * time.Minute

// stopGrace is how long a wrapper has to end its Execution after a stop
// command (the interrupt, the push, the uploads and the end call). After
// it, the backstop stops the workload through the Runner.
const stopGrace = 5 * time.Minute

// runnerStopWait is the limit of one Runner.Stop call.
const runnerStopWait = 30 * time.Second

// endStoppedByRunner is the end reason of an Execution whose wrapper did
// not act on its stop command, so that the Runner stopped it.
const endStoppedByRunner = dispatcherapi.EndStopped + ": the wrapper did not end the Execution after its stop command, so the Runner stopped it"

// backstopEvery is how often the backstop looks for such Executions
// (Options.BackstopEvery replaces it).
const backstopEvery = time.Minute

// pollWait is the longest time that a commands call waits for a command.
// It is shorter than the wrapper's 30 s client timeout.
const pollWait = 20 * time.Second

// eventStop is the stop event: its actor is the person (or the
// Dispatcher) who stopped the Run.
const eventStop = "stop"

// actorDispatcher is the actor kind of the backstop's stop.
const actorDispatcher = "dispatcher"

// stoppable are the states that a stop changes to stopped.
var stoppable = []string{dispatcherapi.StateQueued, dispatcherapi.StateRunning, dispatcherapi.StateWaitingOnPerson,
	dispatcherapi.StateWaitingOnAccountLimit, dispatcherapi.StateTakenOver}

// actor is who caused an intervention.
type actor struct{ kind, id, name string }

func (a actor) details() map[string]any {
	return map[string]any{"actor.kind": a.kind, "actor.id": a.id, "actor.name": a.name}
}

// notStoppable is the error of a stop of a Run in a state that a stop
// does not change.
type notStoppable struct {
	state  string
	closed bool
}

func (e *notStoppable) Error() string {
	if e.closed {
		return "the Run is closed (its PR is merged or declined, or it had no activity for 30 days): " +
			"it keeps its state " + e.state + "; start a new Run"
	}
	return "the Run is " + e.state + ": only a queued, running, taken-over or waiting Run can be stopped"
}

// signal wakes each waiting commands call when a command is added.
type signal struct {
	mu sync.Mutex
	ch chan struct{}
}

// wait gives a channel that closes at the next wake.
func (s *signal) wait() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		s.ch = make(chan struct{})
	}
	return s.ch
}

func (s *signal) wake() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch != nil {
		close(s.ch)
		s.ch = nil
	}
}

// stopRun is the stop verb of a person: the Run is stopped at once, with
// a stop event that names the person. A live Execution gets the stop
// command; one that the Runner has but whose wrapper did not take its
// task ends at once.
func (d *Dispatcher) stopRun(w http.ResponseWriter, r *http.Request, p person) {
	id := r.PathValue("id")
	by := actor{kind: actorPerson, id: p.id, name: p.name}
	err := d.store.stopRun(p.organization.id, id, by, "stopped by "+p.name, d.now())
	var refused *notStoppable
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "no Run "+id+" in this Organization")
		return
	case errors.As(err, &refused):
		writeProblem(w, http.StatusConflict, refused.Error())
		return
	case err != nil:
		d.serverError(w, "stop the Run", err)
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

// stopRun stops a Run of the Organization in a stoppable state, in one
// transaction. It gives errNotFound or a *notStoppable.
func (s *store) stopRun(organizationID, runID string, by actor, reason string, at time.Time) error {
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
	if !slices.Contains(stoppable, state) {
		return &notStoppable{state: state}
	}
	// A Closed Run keeps its last state. It has no Execution queued or
	// live: the close waits for the end of the Execution.
	if closed {
		return &notStoppable{state: state, closed: true}
	}
	if err := stopInTx(tx, runID, by, reason, true, at); err != nil {
		return err
	}
	return s.commit(tx)
}

// stopInTx stops the Run in tx: with run, its state becomes stopped with
// a stop event of by. Each Execution of the Run that has not ended stops
// too: a running one gets one stop command, and a dispatched one (its
// wrapper has not taken its task) ends at once with the reason stopped.
func stopInTx(tx *sql.Tx, runID string, by actor, reason string, run bool, at time.Time) error {
	if run {
		if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, question = '', summary = '' WHERE id = ?`,
			dispatcherapi.StateStopped, reason, runID); err != nil {
			return err
		}
		if err := recordEvent(tx, at, runID, eventStop, by.details()); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT id, state FROM executions WHERE run_id = ? AND state != ? ORDER BY dispatched_at, rowid`,
		runID, dispatcherapi.ExecutionEnded)
	if err != nil {
		return err
	}
	type live struct{ id, state string }
	var executions []live
	for rows.Next() {
		var e live
		if err := rows.Scan(&e.id, &e.state); err != nil {
			rows.Close()
			return err
		}
		executions = append(executions, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range executions {
		if e.state == dispatcherapi.ExecutionDispatched {
			if _, err := tx.Exec(`UPDATE executions SET state = ?, ended_at = ?, end_reason = ? WHERE id = ?`,
				dispatcherapi.ExecutionEnded, at.UnixMicro(), dispatcherapi.EndStopped, e.id); err != nil {
				return err
			}
			err := recordEvent(tx, at, runID, eventExecutionEnded, map[string]any{"execution.id": e.id,
				"execution.reason": dispatcherapi.EndStopped})
			if err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(`INSERT INTO commands (execution_id, kind, created_at) SELECT ?, ?, ?
			WHERE NOT EXISTS (SELECT 1 FROM commands WHERE execution_id = ? AND kind = ?)`,
			e.id, dispatcherapi.CommandStop, at.UnixMicro(), e.id, dispatcherapi.CommandStop); err != nil {
			return err
		}
	}
	return nil
}

// commands is the wrapper's commands call, a long poll: it answers with
// the Execution's commands after the request's After as soon as there is
// one, or with none after pollWait. An ended Execution gets 409, so its
// wrapper stops asking.
func (d *Dispatcher) commands(w http.ResponseWriter, r *http.Request, e storedExecution) {
	var req dispatcherapi.CommandsRequest
	if !decode(w, r, &req, "the body must be a JSON object with after") {
		return
	}
	timeout := time.NewTimer(pollWait)
	defer timeout.Stop()
	for {
		added := d.commandsAdded.wait()
		list, ended, err := d.store.commandsAfter(e.id, req.After)
		if err != nil {
			d.serverError(w, "read the Execution's commands", err)
			return
		}
		if len(list) > 0 {
			writeJSON(w, http.StatusOK, dispatcherapi.Commands{Commands: list})
			return
		}
		if ended {
			writeProblem(w, http.StatusConflict, errEnded.Error())
			return
		}
		select {
		case <-added:
		case <-timeout.C:
			writeJSON(w, http.StatusOK, dispatcherapi.Commands{Commands: []dispatcherapi.Command{}})
			return
		case <-r.Context().Done():
			return
		case <-d.closing:
			writeProblem(w, http.StatusServiceUnavailable, "the Dispatcher is stopping")
			return
		}
	}
}

// commandsAfter gives the Execution's commands after seq after, oldest
// first, and whether the Execution has ended.
func (s *store) commandsAfter(executionID string, after int64) ([]dispatcherapi.Command, bool, error) {
	var state string
	if err := s.db.QueryRow(`SELECT state FROM executions WHERE id = ?`, executionID).Scan(&state); err != nil {
		return nil, false, err
	}
	list, err := commandsIn(s.db, executionID, after)
	return list, state == dispatcherapi.ExecutionEnded, err
}

// executionBackstopLoop stops each Execution that passed its total limit plus
// backstopMargin: its wrapper did not end it at the limit. It also stops
// through the Runner each Execution whose wrapper did not act on its stop
// command in stopGrace: a wrapper that hangs takes no command.
func (d *Dispatcher) executionBackstopLoop(ctx context.Context) {
	for {
		d.backstop()
		d.stopThroughTheRunner(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.backstopEvery):
		}
	}
}

func (d *Dispatcher) backstop() {
	due, err := d.store.pastTheBackstop(d.now())
	if err != nil {
		d.log.Error("could not read the Executions past their limit", "error", err)
		return
	}
	for _, e := range due {
		if err := d.store.backstopStop(e, d.now()); err != nil {
			d.log.Error("could not stop an Execution past its limit", "execution", e, "error", err)
			continue
		}
		d.log.Warn("the backstop stopped an Execution that passed its total limit plus 15 minutes", "execution", e)
	}
	if len(due) > 0 {
		d.commandsAdded.wake()
	}
}

// stopThroughTheRunner stops the workload of each running Execution whose
// stop command is stopGrace old or older, once. The Runner's end event then
// ends the Execution (watchLoop) with endStoppedByRunner.
func (d *Dispatcher) stopThroughTheRunner(ctx context.Context) {
	due, err := d.store.unansweredStops(d.now().Add(-stopGrace))
	if err != nil {
		d.log.Error("could not read the Executions with a stop command", "error", err)
		return
	}
	for _, e := range due {
		// Before the call: the end event can come before Stop returns.
		if _, done := d.runnerStopped.LoadOrStore(e.id, true); done {
			continue
		}
		stop, cancel := context.WithTimeout(ctx, runnerStopWait)
		err := d.runner.Stop(stop, e.workload)
		cancel()
		if err != nil {
			d.runnerStopped.Delete(e.id)
			d.log.Error("the Runner could not stop an Execution", "execution", e.id, "error", err)
			continue
		}
		d.log.Warn("the Runner stopped an Execution whose wrapper did not act on its stop command", "execution", e.id)
	}
}

// unansweredStops gives the running Executions that have a workload and a
// stop command made at or before before.
func (s *store) unansweredStops(before time.Time) ([]storedExecution, error) {
	rows, err := s.db.Query(`SELECT DISTINCT e.id, e.workload FROM executions e
		JOIN commands c ON c.execution_id = e.id WHERE e.state = ? AND e.workload != '' AND c.kind = ?
		AND c.created_at <= ?`, dispatcherapi.ExecutionRunning, dispatcherapi.CommandStop, before.UnixMicro())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []storedExecution
	for rows.Next() {
		var e storedExecution
		if err := rows.Scan(&e.id, &e.workload); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

// pastTheBackstop gives the running Executions that started more than
// their total limit plus backstopMargin before now, and have no stop
// command yet.
func (s *store) pastTheBackstop(now time.Time) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM executions e WHERE state = ? AND started_at IS NOT NULL
		AND started_at + (CASE WHEN total_limit > 0 THEN total_limit ELSE ? END) * 1000000 + ? < ?
		AND NOT EXISTS (SELECT 1 FROM commands c WHERE c.execution_id = e.id AND c.kind = ?)`,
		dispatcherapi.ExecutionRunning, defaultLimits.TotalSeconds, backstopMargin.Microseconds(), now.UnixMicro(),
		dispatcherapi.CommandStop)
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

// backstopStop stops the Run of an Execution past the backstop, as the
// Dispatcher. A Run that is not in a stoppable state keeps its state, and
// its live Executions get the stop command all the same.
func (s *store) backstopStop(executionID string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID, state string
	if err := tx.QueryRow(`SELECT r.id, r.state FROM executions e JOIN runs r ON r.id = e.run_id WHERE e.id = ?`,
		executionID).Scan(&runID, &state); err != nil {
		return err
	}
	by := actor{kind: actorDispatcher, name: "Dispatcher"}
	reason := "stopped by the Dispatcher: the Execution passed its total limit plus 15 minutes"
	if err := stopInTx(tx, runID, by, reason, slices.Contains(stoppable, state), at); err != nil {
		return err
	}
	return s.commit(tx)
}

// totalLimit is the total limit that the Execution keeps (the default
// before its task call).
func (s *store) totalLimit(executionID string) (time.Duration, error) {
	var seconds int64
	if err := s.db.QueryRow(`SELECT total_limit FROM executions WHERE id = ?`, executionID).Scan(&seconds); err != nil {
		return 0, err
	}
	return dispatcherapi.Limits{TotalSeconds: seconds}.Total(), nil
}
