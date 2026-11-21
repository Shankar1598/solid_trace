package dispatcher

import (
	"slices"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A Run whose PR has an outcome is not closed while its Execution runs,
// and is closed after the end.
func TestARunIsNotClosedWhileItsExecutionRuns(t *testing.T) {
	w := newAdminWorld(t)
	d, now := w.d, w.clock()
	startedExecution(t, w)
	if err := d.store.savePROutcome("r-1", dispatcherapi.PRMerged, now); err != nil {
		t.Fatal(err)
	}
	if closed, err := d.store.closeRun("r-1", closePRMerged, now, nil); closed || err != nil {
		t.Fatalf("the close of a Run with a running Execution: %v %v, want not closed", closed, err)
	}
	if err := d.store.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndHarnessExited}, now); err != nil {
		t.Fatal(err)
	}
	if closed, err := d.store.closeRun("r-1", closePRMerged, now, nil); !closed || err != nil {
		t.Fatalf("the close after the end of the Execution: %v %v, want closed", closed, err)
	}
}

// A queued Run whose PR has an outcome gets no new Execution before its
// close: it cannot continue.
func TestAQueuedRunWhosePRHasAnOutcomeGetsNoExecution(t *testing.T) {
	w := newAdminWorld(t)
	d, now := w.d, w.clock()
	run := storedRun{Run: dispatcherapi.Run{ID: "r-1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: now}, organizationID: "7", projectID: "42"}
	if err := d.store.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := d.store.savePROutcome("r-1", dispatcherapi.PRDeclined, now); err != nil {
		t.Fatal(err)
	}
	if ids, err := d.store.queuedRuns(); err != nil || slices.Contains(ids, "r-1") {
		t.Errorf("the queued Runs are %v (%v): the Run whose PR has an outcome is in them", ids, err)
	}
	if ok, err := d.store.addExecution("r-1", "e-1", now); ok || err != nil {
		t.Errorf("add an Execution to a Run whose PR has an outcome: %v %v, want none", ok, err)
	}
}

// A Closed Run with no PR outcome gets no new Execution: the dispatch
// claim reads the close in its own transaction, also when the Run was in
// the queued Runs before the close.
func TestAClosedQueuedRunGetsNoExecution(t *testing.T) {
	w := newAdminWorld(t)
	d, now := w.d, w.clock()
	run := storedRun{Run: dispatcherapi.Run{ID: "r-1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: now}, organizationID: "7", projectID: "42"}
	if err := d.store.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if ids, err := d.store.queuedRuns(); err != nil || !slices.Contains(ids, "r-1") {
		t.Fatalf("the queued Runs are %v (%v), want r-1 in them", ids, err)
	}
	if closed, err := d.store.closeRun("r-1", closeNoActivity, now, nil); !closed || err != nil {
		t.Fatalf("the close of the queued Run: %v %v, want closed", closed, err)
	}
	if ok, err := d.store.addExecution("r-1", "e-1", now); ok || err != nil {
		t.Errorf("add an Execution to a Closed Run: %v %v, want none", ok, err)
	}
}
