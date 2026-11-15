package dispatcher

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A queued Run whose Execution the Runner has, but whose wrapper did not
// take its task: the stop ends the Execution at once. The wrapper's task
// call then gets errEnded, and its exit does not change the Run.
func TestAStopOfAQueuedRunEndsItsDispatchedExecutionAtOnce(t *testing.T) {
	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	run := storedRun{Run: dispatcherapi.Run{ID: "r-1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: at}, organizationID: "7", projectID: "42"}
	if err := s.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.addExecution("r-1", "e-1", at); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}

	by := actor{kind: actorPerson, id: "8", name: "Ben"}
	if err := s.stopRun("7", "r-1", by, "stopped by Ben", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	got, err := s.run("7", "r-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != dispatcherapi.StateStopped || got.Reason != "stopped by Ben" {
		t.Errorf("the Run is %s (%s), want stopped by Ben", got.State, got.Reason)
	}
	executions, err := s.executionsOf("r-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(executions) != 1 || executions[0].State != dispatcherapi.ExecutionEnded ||
		executions[0].EndReason != dispatcherapi.EndStopped {
		t.Fatalf("the Executions are %+v, want e-1 ended with the reason stopped", executions)
	}
	var commands int
	if err := s.db.QueryRow(`SELECT count(*) FROM commands`).Scan(&commands); err != nil || commands != 0 {
		t.Errorf("%d commands (%v): an ended Execution needs none", commands, err)
	}
	body, _, err := s.outboxBatch()
	if err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatal(err)
	}
	var kinds []any
	for _, ev := range events {
		kinds = append(kinds, ev["event.kind"])
	}
	if len(events) < 2 || events[len(events)-2]["event.kind"] != eventStop ||
		events[len(events)-2]["actor.name"] != "Ben" || events[len(events)-1]["event.kind"] != eventExecutionEnded ||
		events[len(events)-1]["execution.reason"] != dispatcherapi.EndStopped {
		t.Errorf("the events are %v, want a stop of Ben and then execution_ended with the reason stopped", kinds)
	}

	// The wrapper starts late: its task call is refused.
	if _, err := s.startExecution("e-1", at.Add(2*time.Second), defaultLimits); !errors.Is(err, errEnded) {
		t.Errorf("the task call after the stop: %v, want errEnded", err)
	}
	// The wrapper then exits, and the Runner says so.
	code := 1
	if ended, err := s.failExecution("e-1", "the wrapper exited with code 1", &code, at.Add(3*time.Second)); ended || err != nil {
		t.Errorf("the wrapper's exit changed the ended Execution: %v %v", ended, err)
	}
	if got, _ := s.run("7", "r-1"); got.State != dispatcherapi.StateStopped {
		t.Errorf("after the wrapper's exit the Run is %s, want stopped", got.State)
	}
	// A second stop is refused: the Run is stopped.
	var refused *notStoppable
	if err := s.stopRun("7", "r-1", by, "stopped by Ben", at.Add(4*time.Second)); !errors.As(err, &refused) {
		t.Errorf("a second stop: %v, want notStoppable", err)
	}
}
