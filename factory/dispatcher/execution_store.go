package dispatcher

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// errEnded is the error for a call on an Execution that has ended.
var errEnded = errors.New("the Execution has ended")

// storedExecution is an Execution with the Run and Organization that
// scope it.
type storedExecution struct {
	id, runID, organizationID, projectID string
	workload                             string
	state                                string
	// account is the subscription account that the Execution got.
	account string
	// form is "" (headless) or dispatcherapi.FormInteractive (a
	// take-over).
	form string
}

func (e storedExecution) interactive() bool { return e.form == dispatcherapi.FormInteractive }

// queuedRuns are the ids of the queued Runs that are not closed and
// whose PR has no outcome, with no Execution that has not ended, oldest
// first. A Closed Run cannot continue, and a Run whose PR has an outcome
// is closed when its Execution ends.
func (s *store) queuedRuns() ([]string, error) {
	rows, err := s.db.Query(`SELECT r.id FROM runs r WHERE r.state = ? AND r.closed_at IS NULL AND r.pr_outcome = ''
		AND NOT EXISTS
		(SELECT 1 FROM executions e WHERE e.run_id = r.id AND e.state != ?) ORDER BY r.created_at, r.id`,
		dispatcherapi.StateQueued, dispatcherapi.ExecutionEnded)
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

// addExecution adds a dispatched Execution to a Run that is queued and has
// no Execution that has not ended, on the account that pickAccount gives
// at at. It is false when the Run is not so, or when no account is free:
// then the Run stays queued, with the reason on its page.
func (s *store) addExecution(runID, id string, at time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var free bool
	err = tx.QueryRow(`SELECT r.state = ? AND r.closed_at IS NULL AND r.pr_outcome = '' AND NOT EXISTS
		(SELECT 1 FROM executions e WHERE e.run_id = r.id AND e.state != ?) FROM runs r WHERE r.id = ?`,
		dispatcherapi.StateQueued, dispatcherapi.ExecutionEnded, runID).Scan(&free)
	if err != nil || !free {
		return false, ignoreNoRows(err)
	}
	account, reason, err := s.accountFor(tx, runID, at)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE runs SET reason = ? WHERE id = ? AND reason != ?`, reason, runID, reason); err != nil {
		return false, err
	}
	if account == "" {
		return false, tx.Commit()
	}
	// The Run is queued until its wrapper takes the task (startExecution
	// clears this). With no VM free, it stays so, and the page says why.
	if _, err := tx.Exec(`UPDATE runs SET reason = ? WHERE id = ?`, reasonWaitingForVM, runID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE runs SET next_account = '' WHERE id = ?`, runID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO executions (id, run_id, state, dispatched_at, account) VALUES (?, ?, ?, ?, ?)`,
		id, runID, dispatcherapi.ExecutionDispatched, at.UnixMicro(), account); err != nil {
		return false, err
	}
	// The last run event of a Run that waits for a VM must carry the
	// reason: the Console pages read it from the run events.
	details := actor{kind: actorDispatcher, name: "Dispatcher"}.details()
	details["execution.id"] = id
	if err := recordEvent(tx, at, runID, eventQueued, details); err != nil {
		return false, err
	}
	return true, s.commit(tx)
}

func ignoreNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (s *store) setWorkload(id, workload string) error {
	_, err := s.db.Exec(`UPDATE executions SET workload = ? WHERE id = ?`, workload, id)
	return err
}

// setFactoryFile keeps the factory file of a dispatched Execution.
func (s *store) setFactoryFile(id, file string) error {
	_, err := s.db.Exec(`UPDATE executions SET factory_file = ? WHERE id = ?`, file, id)
	return err
}

// factoryFile is the factory file that the Execution's dispatch kept.
func (s *store) factoryFile(id string) (factoryFile, error) {
	var data string
	if err := s.db.QueryRow(`SELECT factory_file FROM executions WHERE id = ?`, id).Scan(&data); err != nil {
		return factoryFile{}, err
	}
	return decodeFactoryFile(data)
}

// checkoutOfRun is what the read of a Run's factory file needs.
type checkoutOfRun struct {
	organizationID, projectID, repository, folder string
}

func (s *store) runCheckout(runID string) (checkoutOfRun, error) {
	var c checkoutOfRun
	err := s.db.QueryRow(`SELECT organization_id, project_id, repository, folder FROM runs WHERE id = ?`, runID).
		Scan(&c.organizationID, &c.projectID, &c.repository, &c.folder)
	return c, err
}

// execution reads one Execution.
func (s *store) execution(id string) (storedExecution, error) {
	var e storedExecution
	err := s.db.QueryRow(`SELECT e.id, e.run_id, r.organization_id, r.project_id, e.workload, e.state, e.account, e.form
		FROM executions e JOIN runs r ON r.id = e.run_id WHERE e.id = ?`, id).
		Scan(&e.id, &e.runID, &e.organizationID, &e.projectID, &e.workload, &e.state, &e.account, &e.form)
	if errors.Is(err, sql.ErrNoRows) {
		return e, errNotFound
	}
	return e, err
}

// startedRun is what the task call needs of the Run.
type startedRun struct {
	prompt, sessionID string
	// project, repository, folder and baseBranch are the Run's, as its
	// start kept them: a change of the Factory settings after the start
	// does not change them.
	project, repository, folder, baseBranch string
	resume                                  bool
	// startedAt is when the Execution started: its limits count from it.
	startedAt time.Time
	// limits are the Execution's limits: the total limit as it was kept
	// at the first task call.
	limits dispatcherapi.Limits
	// messages are the Run's kept messages that the Execution's task
	// gives.
	messages []dispatcherapi.Message
	// form is the Execution's form ("" is headless).
	form string
	// cause is the Issue or the Monitor that started the Run, if one did.
	cause runCause
	// restore is set when the Run's last machines were lost: the wrapper
	// restores their work-in-progress save.
	restore *dispatcherapi.Restore
}

// setPlace keeps the machine of an Execution that has not ended, and the
// command that attaches a person to a take-over there, as the wrapper's
// task call gives them. An empty value changes nothing.
func (s *store) setPlace(id string, place dispatcherapi.TaskRequest) error {
	_, err := s.db.Exec(`UPDATE executions SET vm = CASE WHEN ? != '' THEN ? ELSE vm END,
		attach = CASE WHEN ? != '' THEN ? ELSE attach END WHERE id = ? AND state != ?`,
		place.VM, place.VM, place.Attach, place.Attach, id, dispatcherapi.ExecutionEnded)
	return err
}

// startExecution marks a dispatched Execution running and its Run
// running, keeps its total limit from limits, and gives the Run's prompt
// and Agent session id (made at the Run's first Execution). A second call
// on a running Execution changes nothing. An ended Execution gives
// errEnded.
func (s *store) startExecution(id string, at time.Time, limits dispatcherapi.Limits) (startedRun, error) {
	var run startedRun
	tx, err := s.db.Begin()
	if err != nil {
		return run, err
	}
	defer tx.Rollback()
	var runID, state, takenOverByID, takenOverByName string
	if err := tx.QueryRow(`SELECT run_id, state, form, taken_over_by_id, taken_over_by_name FROM executions WHERE id = ?`,
		id).Scan(&runID, &state, &run.form, &takenOverByID, &takenOverByName); err != nil {
		return run, err
	}
	interactive := run.form == dispatcherapi.FormInteractive
	switch state {
	case dispatcherapi.ExecutionEnded:
		return run, errEnded
	case dispatcherapi.ExecutionDispatched:
		if _, err := tx.Exec(`UPDATE executions SET state = ?, started_at = ?, total_limit = ? WHERE id = ?`,
			dispatcherapi.ExecutionRunning, at.UnixMicro(), int64(limits.Total()/time.Second), id); err != nil {
			return run, err
		}
		// A take-over Execution's Run is taken over already.
		if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = '' WHERE id = ? AND state = ? AND NOT ?`,
			dispatcherapi.StateRunning, runID, dispatcherapi.StateQueued, interactive); err != nil {
			return run, err
		}
		if _, err := tx.Exec(`UPDATE runs SET session_id = ? WHERE id = ? AND session_id = ''`,
			newSessionID(), runID); err != nil {
			return run, err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO run_sessions (session_id, run_id, added_at)
			SELECT session_id, id, ? FROM runs WHERE id = ?`, at.UnixMicro(), runID); err != nil {
			return run, err
		}
	}
	var started int64
	var startedBy, attributes string
	err = tx.QueryRow(`SELECT prompt, session_id, project, repository, folder, base_branch, EXISTS (SELECT 1 FROM executions e WHERE e.run_id = r.id
		AND e.id != ? AND e.started_at IS NOT NULL), (SELECT started_at FROM executions WHERE id = ?),
		(SELECT total_limit FROM executions WHERE id = ?), started_by_kind, attributes FROM runs r WHERE id = ?`, id, id, id, runID).
		Scan(&run.prompt, &run.sessionID, &run.project, &run.repository, &run.folder, &run.baseBranch, &run.resume, &started,
			&run.limits.TotalSeconds, &startedBy, &attributes)
	if err != nil {
		return run, err
	}
	if run.cause, err = causeOf(startedBy, attributes); err != nil {
		return run, err
	}
	if run.restore, err = restoreOf(tx, runID, id); err != nil {
		return run, err
	}
	run.startedAt = time.UnixMicro(started).UTC()
	run.limits.IdleSeconds = limits.IdleSeconds
	// A take-over Execution gets no prompt and no message: the person
	// types in the terminal, and the Run keeps its messages until the
	// take-over ends.
	if interactive {
		run.prompt, run.resume = "", true
	} else if run.messages, err = takeKeptMessages(tx, runID, id, state == dispatcherapi.ExecutionDispatched); err != nil {
		return run, err
	}
	// The task always has the prompt: the wrapper writes it only when it
	// cannot resume the Agent session, or has no message to write.
	if state == dispatcherapi.ExecutionDispatched {
		var account string
		if err := tx.QueryRow(`SELECT account FROM executions WHERE id = ?`, id).Scan(&account); err != nil {
			return run, err
		}
		details := map[string]any{"execution.id": id, "execution.account": account}
		if run.prompt != "" {
			// The task is a message of the factory: its uuid and the hash of
			// its text let the Agent session page say that the factory said
			// it. The event's actor stays the person who caused the Execution.
			details["message.id"] = dispatcherapi.TaskMessageID(id)
			details["message.sha256"] = dispatcherapi.TextSHA256(run.prompt)
		}
		if interactive {
			// Each person message of the take-over Execution is from the
			// person who took the Run over.
			details = actor{kind: actorPerson, id: takenOverByID, name: takenOverByName}.details()
			details["execution.id"] = id
		}
		if err := addPlace(tx, id, details); err != nil {
			return run, err
		}
		if err := recordEvent(tx, at, runID, eventExecutionStarted, details); err != nil {
			return run, err
		}
	}
	return run, s.commit(tx)
}

// saveReport keeps report as the running Execution's last valid report,
// with a report event at at.
func (s *store) saveReport(id string, report dispatcherapi.Report, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID string
	checks, err := jsonList(report.Checks)
	if err != nil {
		return err
	}
	evidence, err := jsonList(report.Evidence)
	if err != nil {
		return err
	}
	err = tx.QueryRow(`UPDATE executions SET report_outcome = ?, report_summary = ?, report_question = ?,
		report_checks = ?, report_evidence = ?, report_delivered = ?, report_cause = ? WHERE id = ? AND state = ?
		RETURNING run_id`,
		report.Outcome, report.Summary, report.Question, checks, evidence, report.DeliveredMessages, report.CauseReport, id,
		dispatcherapi.ExecutionRunning).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return errEnded
	}
	if err != nil {
		return err
	}
	err = recordEvent(tx, at, runID, eventReport, map[string]any{"execution.id": id,
		"report.outcome": report.Outcome, "report.summary": report.Summary, "report.question": report.Question,
		"report.checks": nonNil(report.Checks), "report.evidence": nonNil(report.Evidence),
		"report.delivered_messages": report.DeliveredMessages, "report.cause_report": report.CauseReport})
	if err != nil {
		return err
	}
	return s.commit(tx)
}

// endExecution ends a running Execution at the wrapper's end call, and
// sets its Run's state from the outcome of the last valid report.
func (s *store) endExecution(id string, end dispatcherapi.ExecutionEnd, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID, state, checks, startedBy, attributes, account, form string
	var prID int
	var report dispatcherapi.Report
	err = tx.QueryRow(`SELECT run_id, e.state, report_outcome, report_summary, report_question, report_checks,
		report_cause, r.pr_id, r.started_by_kind, r.attributes, e.account, e.form FROM executions e JOIN runs r ON r.id = e.run_id
		WHERE e.id = ?`, id).
		Scan(&runID, &state, &report.Outcome, &report.Summary, &report.Question, &checks, &report.CauseReport, &prID,
			&startedBy, &attributes, &account, &form)
	if err != nil {
		return err
	}
	if state != dispatcherapi.ExecutionRunning {
		return errEnded
	}
	last := &report
	if report.Outcome == "" {
		last = nil
	}
	// The Run's state that the end changes: running for a headless
	// Execution, taken over for a take-over Execution. A Run in another
	// state (a stop, or a take-over of a headless Execution) keeps it.
	from := dispatcherapi.StateRunning
	runState, reason, question, summary := outcome(last, end)
	// At the usage limit (with no report), the Run goes on another free
	// account at once, or waits on an account limit. A take-over
	// Execution has its own outcome rules, and no switch.
	var nextAccount string
	// A preempted or a stopped machine restarts a headless Execution's
	// Run, also after a report: the end did not finish (V7). A take-over
	// does not start again by itself: the Run waits on the person (V8).
	lost := lostReason(end.Reason)
	inARow := 0
	switch {
	case form == dispatcherapi.FormInteractive:
		from = dispatcherapi.StateTakenOver
		runState, reason, question, summary = takeOverOutcome(last, end)
		if lost != "" {
			runState, reason, question = dispatcherapi.StateWaitingOnPerson, reasonTakeOverEnded+lost, ""
		}
	case lost != "":
		if runState, reason, inARow, err = restart(tx, runID, id, lost); err != nil {
			return err
		}
		question, summary = "", ""
		if runState == dispatcherapi.StateQueued {
			nextAccount = account
		}
	case runState == dispatcherapi.StateWaitingOnAccountLimit:
		if runState, reason, nextAccount, err = s.atUsageLimit(tx, account, at); err != nil {
			return err
		}
	}
	returned, err := settleMessages(tx, runID, id, end.Messages, at)
	if err != nil {
		return err
	}
	// Messages do not queue a Run whose machine went: a restart delivers
	// them, and neither a take-over nor a Run that lost too many machines
	// starts again by itself.
	again := false
	if lost == "" {
		if again, err = continueForMessages(tx, runID, id, from, runState, returned, at); err != nil {
			return err
		}
	}
	if again {
		runState, reason, question, summary = dispatcherapi.StateQueued, reasonSentAgain, "", ""
		if form == dispatcherapi.FormInteractive {
			reason = reasonMessagesAfterTakeOver
		}
	}
	var delivered sql.NullInt64
	if end.TaskDeliveredAt != nil {
		delivered = sql.NullInt64{Int64: end.TaskDeliveredAt.UnixMicro(), Valid: true}
	}
	var exit sql.NullInt64
	if end.ExitCode != nil {
		exit = sql.NullInt64{Int64: int64(*end.ExitCode), Valid: true}
	}
	endReason := end.Reason
	if end.Error != "" {
		endReason += ": " + end.Error
	}
	if _, err := tx.Exec(`UPDATE executions SET state = ?, ended_at = ?, end_reason = ?, exit_code = ?,
		task_delivered_at = ?, background_tasks_killed = ? WHERE id = ?`, dispatcherapi.ExecutionEnded, at.UnixMicro(),
		endReason, exit, delivered, max(end.BackgroundTasksKilled, 0), id); err != nil {
		return err
	}
	if lost != "" {
		if err := markLost(tx, id, lost); err != nil {
			return err
		}
	}
	var flags string
	if runState == dispatcherapi.StateDone {
		if err := unmarshalList(checks, &report.Checks); err != nil {
			return err
		}
		cause, err := causeOf(startedBy, attributes)
		if err != nil {
			return err
		}
		if flags, err = jsonList(cause.doneFlags(report, prID)); err != nil {
			return err
		}
	}
	res, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, question = ?, summary = ?, flags = ?, next_account = ?
		WHERE id = ? AND state = ?`,
		runState, reason, question, summary, flags, nextAccount, runID, from)
	if err != nil {
		return err
	}
	// A stop before the end wins: the Run is not running, and does not
	// switch or restart.
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		nextAccount = ""
	}
	if end.Preemption != nil || lost != "" {
		p := dispatcherapi.Preemption{Reason: end.Reason}
		if end.Preemption != nil {
			p = *end.Preemption
		}
		details := preemptionDetails(id, p)
		if form != dispatcherapi.FormInteractive {
			details["preemption.lost_in_a_row"] = inARow
		}
		details["preemption.restart"] = changed > 0 && runState == dispatcherapi.StateQueued
		if err := recordEvent(tx, at, runID, eventPreemption, details); err != nil {
			return err
		}
	}
	details := map[string]any{"execution.id": id, "execution.reason": endReason, "execution.account": account}
	if err := addPlace(tx, id, details); err != nil {
		return err
	}
	if end.ExitCode != nil {
		details["execution.exit_code"] = *end.ExitCode
	}
	details["execution.background_tasks_killed"] = max(end.BackgroundTasksKilled, 0)
	if err := recordEvent(tx, at, runID, eventExecutionEnded, details); err != nil {
		return err
	}
	// A restart stays on its account: that is no switch.
	if nextAccount != "" && nextAccount != account {
		switched := actor{kind: actorDispatcher, name: "Dispatcher"}.details()
		switched["account.old"], switched["account.new"] = account, nextAccount
		if err := recordEvent(tx, at, runID, eventAccountSwitched, switched); err != nil {
			return err
		}
	}
	return s.commit(tx)
}

// ownStateSQL is true for a Run whose state an Execution that ends with
// no end call sets: a running Run, or a queued Run when the Execution did
// not start (its wrapper made no task call). Its arguments are
// StateRunning, StateQueued and the Execution's "started". A started
// Execution of a queued Run is the Execution of a stopped Run that a
// message queued again: its end leaves the Run queued for the new
// Execution.
const ownStateSQL = `(state = ? OR (state = ? AND NOT ?))`

// preemptionDetails are the Attributes of a preemption event (spec V9):
// the reason, the seconds from the notice to the saved state, what was
// saved, and the age of the last checkpoint.
func preemptionDetails(execution string, p dispatcherapi.Preemption) map[string]any {
	details := map[string]any{
		"execution.id":                execution,
		"preemption.reason":           p.Reason,
		"preemption.notice_at":        p.NoticeAt.UTC().Format(time.RFC3339Nano),
		"preemption.seconds_to_saved": p.SecondsToSaved,
		"preemption.seconds_to_end":   p.SecondsToEnd,
		"preemption.wip_commit":       p.WIPCommit,
		"preemption.skipped":          nonNil(p.Skipped),
		"preemption.problems":         nonNil(p.Problems),
	}
	if p.TranscriptOffset != nil {
		details["preemption.transcript_offset"] = *p.TranscriptOffset
	}
	if p.CheckpointAgeSeconds != nil {
		details["preemption.checkpoint_age_seconds"] = *p.CheckpointAgeSeconds
	}
	return details
}

// recordPreempting records the wrapper's preempting call on the running
// Execution's Run.
func (s *store) recordPreempting(id string, p dispatcherapi.Preempting, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID string
	err = tx.QueryRow(`SELECT run_id FROM executions WHERE id = ? AND state = ?`, id,
		dispatcherapi.ExecutionRunning).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return errEnded
	}
	if err != nil {
		return err
	}
	err = recordEvent(tx, at, runID, eventPreempting, map[string]any{"execution.id": id,
		"preemption.reason": p.Reason, "preemption.notice_at": p.NoticeAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	return s.commit(tx)
}

// reasonTakeOverEnded starts the reason of a Run whose take-over ended
// with no end call, or with its machine: it waits on a person (V8).
const reasonTakeOverEnded = "the take-over ended: "

// reasonMachineLost is the end reason of an Execution whose machine went
// with no notice (the Runner's lost event).
const reasonMachineLost = "the machine was lost"

// failExecution ends an Execution that has not ended, with reason, and
// fails its Run if the Execution sets its state (ownStateSQL). exitCode is the
// wrapper's exit code, or nil. It is false when the Execution had ended
// (or is not known).
func (s *store) failExecution(id, reason string, exitCode *int, at time.Time) (bool, error) {
	return s.endWithNoCall(id, reason, exitCode, "", at)
}

// loseMachine ends an Execution whose machine went with no notice, and
// restarts its Run if the Execution sets its state (V7): the Run is queued
// again, or fails after too many lost machines in a row. A take-over's
// Run waits on a person. It is false when the Execution had ended.
func (s *store) loseMachine(id string, at time.Time) (bool, error) {
	return s.endWithNoCall(id, reasonMachineLost, nil, dispatcherapi.MachineLost, at)
}

// endWithNoCall ends an Execution that has not ended and had no end call.
// lost is "" or dispatcherapi.MachineLost.
func (s *store) endWithNoCall(id, reason string, exitCode *int, lost string, at time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var runID string
	var started bool
	var exit sql.NullInt64
	if exitCode != nil {
		exit = sql.NullInt64{Int64: int64(*exitCode), Valid: true}
	}
	err = tx.QueryRow(`UPDATE executions SET state = ?, ended_at = ?, end_reason = ?, exit_code = ?, machine_lost = ?
		WHERE id = ? AND state != ? RETURNING run_id, started_at IS NOT NULL`, dispatcherapi.ExecutionEnded,
		at.UnixMicro(), reason, exit, lost, id, dispatcherapi.ExecutionEnded).Scan(&runID, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// With no end call, no message of the Execution is delivered but the
	// ones that the wrapper's message calls named.
	returned, err := settleMessages(tx, runID, id, nil, at)
	if err != nil {
		return false, err
	}
	var form, account string
	if err := tx.QueryRow(`SELECT form, account FROM executions WHERE id = ?`, id).Scan(&form, &account); err != nil {
		return false, err
	}
	var restarted bool
	inARow := 0
	if form == dispatcherapi.FormInteractive {
		// A take-over that ends with no end call (the machine went, or
		// the wrapper failed) does not start again by itself: the Run
		// waits on a person, with the reason (V8).
		if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, question = '', summary = '' WHERE id = ?
			AND state = ?`, dispatcherapi.StateWaitingOnPerson, reasonTakeOverEnded+reason, runID,
			dispatcherapi.StateTakenOver); err != nil {
			return false, err
		}
	} else {
		runState, runReason, nextAccount := dispatcherapi.StateFailed, reason, ""
		if lost != "" {
			if runState, runReason, inARow, err = restart(tx, runID, id, lost); err != nil {
				return false, err
			}
			if runState == dispatcherapi.StateQueued {
				nextAccount = account
			}
		} else {
			again, err := continueForMessages(tx, runID, id, dispatcherapi.StateRunning, dispatcherapi.StateFailed,
				returned, at)
			if err != nil {
				return false, err
			}
			if again {
				runState, runReason = dispatcherapi.StateQueued, reasonSentAgain
			}
		}
		res, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, question = '', summary = '',
			next_account = CASE WHEN ? != '' THEN ? ELSE next_account END WHERE id = ? AND `+ownStateSQL,
			runState, runReason, nextAccount, nextAccount, runID, dispatcherapi.StateRunning, dispatcherapi.StateQueued,
			started)
		if err != nil {
			return false, err
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		restarted = changed > 0 && lost != "" && runState == dispatcherapi.StateQueued
	}
	if lost != "" {
		details := map[string]any{"execution.id": id, "machine_lost.reason": lost, "machine_lost.restart": restarted}
		if form != dispatcherapi.FormInteractive {
			details["machine_lost.in_a_row"] = inARow
		}
		if err := recordEvent(tx, at, runID, eventMachineLost, details); err != nil {
			return false, err
		}
	}
	details := map[string]any{"execution.id": id, "execution.reason": reason}
	if err := addPlace(tx, id, details); err != nil {
		return false, err
	}
	if exitCode != nil {
		details["execution.exit_code"] = *exitCode
	}
	if err := recordEvent(tx, at, runID, eventExecutionEnded, details); err != nil {
		return false, err
	}
	return true, s.commit(tx)
}

// requeueExecution ends an Execution that has not ended, with reason, and
// puts its Run back in the queue if the Execution sets its state
// (ownStateSQL): the
// next dispatch gives the Run a new Execution. It is false when the
// Execution had ended (or is not known).
func (s *store) requeueExecution(id, reason string, at time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var runID string
	var started bool
	err = tx.QueryRow(`UPDATE executions SET state = ?, ended_at = ?, end_reason = ? WHERE id = ? AND state != ?
		RETURNING run_id, started_at IS NOT NULL`, dispatcherapi.ExecutionEnded, at.UnixMicro(), reason, id,
		dispatcherapi.ExecutionEnded).Scan(&runID, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ? WHERE id = ? AND `+ownStateSQL,
		dispatcherapi.StateQueued, reason, runID, dispatcherapi.StateRunning, dispatcherapi.StateQueued,
		started); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// executionsOf reads a Run's Executions, oldest first. The fake clock can give two
// Executions the same dispatch time: rowid keeps the order in which they were made.
func (s *store) executionsOf(runID string) ([]dispatcherapi.Execution, error) {
	rows, err := s.db.Query(`SELECT id, state, dispatched_at, started_at, ended_at, task_delivered_at, end_reason,
		exit_code, background_tasks_killed, account, form, vm, attach, taken_over_by_id, taken_over_by_name
		FROM executions WHERE run_id = ? ORDER BY dispatched_at, rowid`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []dispatcherapi.Execution
	for rows.Next() {
		var e dispatcherapi.Execution
		var dispatched int64
		var started, ended, delivered, exit sql.NullInt64
		var byID, byName string
		if err := rows.Scan(&e.ID, &e.State, &dispatched, &started, &ended, &delivered, &e.EndReason, &exit,
			&e.BackgroundTasksKilled, &e.Account, &e.Form, &e.VM, &e.Attach, &byID, &byName); err != nil {
			return nil, err
		}
		if e.Form == "" {
			e.Form = dispatcherapi.FormHeadless
		}
		if e.Form == dispatcherapi.FormInteractive {
			e.TakenOverBy = &dispatcherapi.Person{Kind: actorPerson, ID: byID, Name: byName}
		}
		e.DispatchedAt = time.UnixMicro(dispatched).UTC()
		e.StartedAt, e.EndedAt, e.TaskDeliveredAt = timeOf(started), timeOf(ended), timeOf(delivered)
		if exit.Valid {
			code := int(exit.Int64)
			e.ExitCode = &code
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func timeOf(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.UnixMicro(v.Int64).UTC()
	return &t
}

// newSessionID is a random UUID (version 4): the Agent session id that the
// Dispatcher gives the harness with --session-id.
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
