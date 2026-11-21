package dispatcher

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The restart after a lost machine (the spec's V7). A preempted, a
// stopped and a lost machine queue the Run again, at most
// dispatcherapi.MaxLostInARow times in a row; the next one fails the
// Run. An Execution that ends for another reason starts the count again.
// The new Execution gets a Restore in its task (V6), so that its wrapper
// puts the saved work back. The limits of an Execution count from its
// start, so the wait for a machine in the queue is not in them.

// eventMachineLost is the run event of a machine that went with no
// notice to the wrapper (the Runner's lost event): machine_lost.reason,
// machine_lost.in_a_row and machine_lost.restart. A preemption and a
// stopped machine have the preemption event, from the wrapper's end call.
const eventMachineLost = "machine_lost"

// lostReason is why the machine of an Execution that ended with
// endReason went ("" when it did not go).
func lostReason(endReason string) string {
	switch endReason {
	case dispatcherapi.EndPreempted, dispatcherapi.EndMachineStopped:
		return endReason
	}
	return ""
}

// lostMachines is what a Run's ended Executions say about its lost
// machines.
type lostMachines struct {
	// inARow counts the lost machines in a row before now.
	inARow int
	// restore are the lost Executions whose save a new Execution
	// restores, newest first.
	restore []string
	// reason and at are the newest lost machine's reason and end.
	reason string
	at     time.Time
}

// lostMachinesOf reads the Run's ended Executions but except, newest
// first. A lost machine counts, and its save can be restored. An Execution that did
// not start did nothing to the work, and is passed over. A setup that
// failed (such as a restore that could not fetch the save) changed no
// saved work, so its Run's save can still be restored; but it ends the
// count. Each other end ends both: after it, the WIP ref holds no work
// that the Run lost.
func lostMachinesOf(tx *sql.Tx, runID, except string) (lostMachines, error) {
	var l lostMachines
	rows, err := tx.Query(`SELECT id, started_at IS NOT NULL, machine_lost, end_reason, COALESCE(ended_at, 0)
		FROM executions WHERE run_id = ? AND id != ? AND state = ? ORDER BY dispatched_at DESC, rowid DESC`, runID, except,
		dispatcherapi.ExecutionEnded)
	if err != nil {
		return l, err
	}
	defer rows.Close()
	counting := true
	for rows.Next() {
		var id, lost, endReason string
		var started bool
		var ended int64
		if err := rows.Scan(&id, &started, &lost, &endReason, &ended); err != nil {
			return l, err
		}
		switch {
		case lost != "":
			if counting {
				l.inARow++
			}
			l.restore = append(l.restore, id)
			if l.reason == "" {
				l.reason, l.at = lost, time.UnixMicro(ended).UTC()
			}
		case !started:
		case endReason == dispatcherapi.EndSetupFailed || strings.HasPrefix(endReason, dispatcherapi.EndSetupFailed+": "):
			counting = false
		default:
			return l, rows.Err()
		}
	}
	return l, rows.Err()
}

// restoreOf is the Restore of a new Execution of the Run (nil: its last
// machines were not lost).
func restoreOf(tx *sql.Tx, runID, execution string) (*dispatcherapi.Restore, error) {
	l, err := lostMachinesOf(tx, runID, execution)
	if err != nil || len(l.restore) == 0 {
		return nil, err
	}
	return &dispatcherapi.Restore{Reason: l.reason, LostAt: l.at, Executions: l.restore}, nil
}

// restart is the Run's state after the headless Execution id lost its
// machine for lost: queued on the same account, or failed after too many
// lost machines in a row. n is the lost machines in a row with this one
// (the count does not read the row of id).
func restart(tx *sql.Tx, runID, id, lost string) (state, reason string, n int, err error) {
	l, err := lostMachinesOf(tx, runID, id)
	if err != nil {
		return "", "", 0, err
	}
	n = l.inARow + 1
	if n > dispatcherapi.MaxLostInARow {
		return dispatcherapi.StateFailed, dispatcherapi.ReasonLostTooOften, n, nil
	}
	return dispatcherapi.StateQueued, fmt.Sprintf("%s: a new Execution continues the Run (%d of %d lost machines in a row)",
		lost, n, dispatcherapi.MaxLostInARow), n, nil
}

// markLost keeps why the machine of the ended Execution id went.
func markLost(tx *sql.Tx, id, lost string) error {
	_, err := tx.Exec(`UPDATE executions SET machine_lost = ? WHERE id = ?`, lost, id)
	return err
}
