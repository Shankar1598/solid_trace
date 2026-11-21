package dispatcher

import (
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// vmWaitWarning is how long a Run waits for a VM before the Dispatcher
// writes a warning Log. A cluster that is at its maximum, or a group that
// cannot get Spot capacity, makes Runs wait in the queue (the operator
// guide, "Scaling").
const vmWaitWarning = 10 * time.Minute

// reasonWaitingForVM is the reason of a queued Run whose Execution is
// with the Runner and has no machine yet.
const reasonWaitingForVM = "waiting for a VM: the Runner has not placed the Execution on a machine yet"

// warnLongVMWaits writes one warning Log for each Execution that has
// waited more than vmWaitWarning, at the Dispatcher's clock, for the
// Runner to give it a machine.
func (d *Dispatcher) warnLongVMWaits() {
	waits, err := d.store.longVMWaits(d.now().Add(-vmWaitWarning))
	if err != nil {
		d.log.Error("could not read the Executions that wait for a VM", "error", err)
		return
	}
	for _, w := range waits {
		warned, err := d.store.markVMWaitWarned(w.execution, d.now())
		if err != nil {
			d.log.Error("could not keep the warning of an Execution that waits for a VM", "error", err)
			continue
		}
		if !warned {
			continue
		}
		d.warn("a Run waited more than 10 minutes for a VM: the Runner has not placed its Execution on a machine. "+
			"The node pool may be at its maximum, or its group may have no capacity",
			"factory.run", w.run, "factory.execution", w.execution)
	}
}

type vmWait struct{ run, execution string }

// longVMWaits are the Executions that the Runner holds with no machine,
// that were dispatched at or before cutoff, and that have no warning yet. A take-over
// Execution (form interactive) runs in the allocation of the Execution before it: it waits for
// no VM.
func (s *store) longVMWaits(cutoff time.Time) ([]vmWait, error) {
	rows, err := s.db.Query(`SELECT run_id, id FROM executions WHERE state = ? AND form = '' AND placed_at IS NULL
		AND wait_warned_at IS NULL AND dispatched_at <= ? ORDER BY dispatched_at, id`,
		dispatcherapi.ExecutionDispatched, cutoff.UnixMicro())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var waits []vmWait
	for rows.Next() {
		var w vmWait
		if err := rows.Scan(&w.run, &w.execution); err != nil {
			return nil, err
		}
		waits = append(waits, w)
	}
	return waits, rows.Err()
}

// markVMWaitWarned records the warning of an Execution. It is false when
// the Execution has one already, or got a machine meanwhile.
func (s *store) markVMWaitWarned(id string, at time.Time) (bool, error) {
	res, err := s.db.Exec(`UPDATE executions SET wait_warned_at = ? WHERE id = ? AND state = ? AND placed_at IS NULL
		AND wait_warned_at IS NULL`, at.UnixMicro(), id, dispatcherapi.ExecutionDispatched)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// markPlaced records that the Runner gave the Execution a machine.
func (s *store) markPlaced(id string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE executions SET placed_at = ? WHERE id = ? AND placed_at IS NULL`, at.UnixMicro(), id)
	return err
}
