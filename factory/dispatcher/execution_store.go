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
}

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
	return true, tx.Commit()
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
	err := s.db.QueryRow(`SELECT e.id, e.run_id, r.organization_id, r.project_id, e.workload, e.state, e.account
		FROM executions e JOIN runs r ON r.id = e.run_id WHERE e.id = ?`, id).
		Scan(&e.id, &e.runID, &e.organizationID, &e.projectID, &e.workload, &e.state, &e.account)
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
	// cause is the Issue or the Monitor that started the Run, if one did.
	cause runCause
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
	var runID, state string
	if err := tx.QueryRow(`SELECT run_id, state FROM executions WHERE id = ?`, id).Scan(&runID, &state); err != nil {
		return run, err
	}
	switch state {
	case dispatcherapi.ExecutionEnded:
		return run, errEnded
	case dispatcherapi.ExecutionDispatched:
		if _, err := tx.Exec(`UPDATE executions SET state = ?, started_at = ?, total_limit = ? WHERE id = ?`,
			dispatcherapi.ExecutionRunning, at.UnixMicro(), int64(limits.Total()/time.Second), id); err != nil {
			return run, err
		}
		if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = '' WHERE id = ? AND state = ?`,
			dispatcherapi.StateRunning, runID, dispatcherapi.StateQueued); err != nil {
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
	run.startedAt = time.UnixMicro(started).UTC()
	run.limits.IdleSeconds = limits.IdleSeconds
	if run.messages, err = takeKeptMessages(tx, runID, id, state == dispatcherapi.ExecutionDispatched); err != nil {
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
	var runID, state, checks, startedBy, attributes, account string
	var prID int
	var report dispatcherapi.Report
	err = tx.QueryRow(`SELECT run_id, e.state, report_outcome, report_summary, report_question, report_checks,
		report_cause, r.pr_id, r.started_by_kind, r.attributes, e.account FROM executions e JOIN runs r ON r.id = e.run_id
		WHERE e.id = ?`, id).
		Scan(&runID, &state, &report.Outcome, &report.Summary, &report.Question, &checks, &report.CauseReport, &prID,
			&startedBy, &attributes, &account)
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
	runState, reason, question, summary := outcome(last, end)
	// At the usage limit (with no report), the Run goes on another free
	// account at once, or waits on an account limit.
	var nextAccount string
	if runState == dispatcherapi.StateWaitingOnAccountLimit {
		if runState, reason, nextAccount, err = s.atUsageLimit(tx, account, at); err != nil {
			return err
		}
	}
	returned, err := settleMessages(tx, runID, id, end.Messages, at)
	if err != nil {
		return err
	}
	again, err := continueForMessages(tx, runID, id, runState, returned, at)
	if err != nil {
		return err
	}
	if again {
		runState, reason, question, summary = dispatcherapi.StateQueued, reasonSentAgain, "", ""
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
		runState, reason, question, summary, flags, nextAccount, runID, dispatcherapi.StateRunning)
	if err != nil {
		return err
	}
	// A stop before the end wins: the Run is not running, and does not
	// switch.
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		nextAccount = ""
	}
	if end.Preemption != nil {
		if err := recordEvent(tx, at, runID, eventPreemption, preemptionDetails(id, *end.Preemption)); err != nil {
			return err
		}
	}
	details := map[string]any{"execution.id": id, "execution.reason": endReason, "execution.account": account}
	if end.ExitCode != nil {
		details["execution.exit_code"] = *end.ExitCode
	}
	details["execution.background_tasks_killed"] = max(end.BackgroundTasksKilled, 0)
	if err := recordEvent(tx, at, runID, eventExecutionEnded, details); err != nil {
		return err
	}
	if nextAccount != "" {
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

// failExecution ends an Execution that has not ended, with reason, and
// fails its Run if the Execution sets its state (ownStateSQL). exitCode is the
// wrapper's exit code, or nil. It is false when the Execution had ended
// (or is not known).
func (s *store) failExecution(id, reason string, exitCode *int, at time.Time) (bool, error) {
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
	err = tx.QueryRow(`UPDATE executions SET state = ?, ended_at = ?, end_reason = ?, exit_code = ? WHERE id = ?
		AND state != ? RETURNING run_id, started_at IS NOT NULL`, dispatcherapi.ExecutionEnded, at.UnixMicro(), reason,
		exit, id, dispatcherapi.ExecutionEnded).Scan(&runID, &started)
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
	again, err := continueForMessages(tx, runID, id, dispatcherapi.StateFailed, returned, at)
	if err != nil {
		return false, err
	}
	runState, runReason := dispatcherapi.StateFailed, reason
	if again {
		runState, runReason = dispatcherapi.StateQueued, reasonSentAgain
	}
	if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, question = '', summary = '' WHERE id = ?
		AND `+ownStateSQL, runState, runReason, runID, dispatcherapi.StateRunning, dispatcherapi.StateQueued,
		started); err != nil {
		return false, err
	}
	details := map[string]any{"execution.id": id, "execution.reason": reason}
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
		exit_code, background_tasks_killed, account FROM executions WHERE run_id = ? ORDER BY dispatched_at, rowid`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []dispatcherapi.Execution
	for rows.Next() {
		var e dispatcherapi.Execution
		var dispatched int64
		var started, ended, delivered, exit sql.NullInt64
		if err := rows.Scan(&e.ID, &e.State, &dispatched, &started, &ended, &delivered, &e.EndReason, &exit,
			&e.BackgroundTasksKilled, &e.Account); err != nil {
			return nil, err
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
