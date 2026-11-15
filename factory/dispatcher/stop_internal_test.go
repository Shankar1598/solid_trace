package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

// runningExecution makes the Run r-1 of the Organization 7 with the
// running Execution e-1, in s.
func runningExecution(t *testing.T, s *store, at time.Time) {
	t.Helper()
	run := storedRun{Run: dispatcherapi.Run{ID: "r-1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: at}, organizationID: "7", projectID: "42"}
	if err := s.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.addExecution("r-1", "e-1", at); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}
	if _, err := s.startExecution("e-1", at, defaultLimits); err != nil {
		t.Fatal(err)
	}
}

// count gives the count of rows of the query.
func count(t *testing.T, s *store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The store finds the Run of a stop in the person's Organization only.
func TestAStopOfARunOfAnotherOrganizationIsNotFoundAndChangesNothing(t *testing.T) {
	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	runningExecution(t, s, at)
	events := count(t, s, `SELECT count(*) FROM outbox`)

	by := actor{kind: actorPerson, id: "9", name: "Bo"}
	if err := s.stopRun("8", "r-1", by, "stopped by Bo", at.Add(time.Second)); !errors.Is(err, errNotFound) {
		t.Fatalf("a stop from the Organization 8: %v, want errNotFound", err)
	}

	if got, _ := s.run("7", "r-1"); got.State != dispatcherapi.StateRunning {
		t.Errorf("the Run is %s (%s), want running", got.State, got.Reason)
	}
	if n := count(t, s, `SELECT count(*) FROM commands`); n != 0 {
		t.Errorf("%d commands, want none", n)
	}
	if n := count(t, s, `SELECT count(*) FROM outbox`); n != events {
		t.Errorf("%d new events, want none", n-events)
	}
}

// A person's stop and then the backstop's stop of the same Execution make
// one stop command, and one stop event.
func TestTwoStopsOfARunningExecutionMakeOneStopCommand(t *testing.T) {
	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	runningExecution(t, s, at)
	by := actor{kind: actorPerson, id: "8", name: "Ben"}
	if err := s.stopRun("7", "r-1", by, "stopped by Ben", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := s.backstopStop("e-1", at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}

	if n := count(t, s, `SELECT count(*) FROM commands WHERE execution_id = ? AND kind = ?`, "e-1",
		dispatcherapi.CommandStop); n != 1 {
		t.Errorf("%d stop commands for e-1, want 1", n)
	}
	if n := count(t, s, `SELECT count(*) FROM outbox WHERE json_extract(body, '$."event.kind"') = ?`, eventStop); n != 1 {
		t.Errorf("%d stop events, want 1", n)
	}
	if got, _ := s.run("7", "r-1"); got.Reason != "stopped by Ben" {
		t.Errorf("the Run's reason is %q, want stopped by Ben", got.Reason)
	}
}

// An Execution past its total limit plus 15 minutes that has a stop
// command already is not the backstop's: the backstop does nothing and
// logs nothing.
func TestTheBackstopSkipsAnExecutionThatHasAStopCommand(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	var log bytes.Buffer
	d.log = slog.New(slog.NewTextHandler(&log, nil))
	startedExecution(t, w)
	by := actor{kind: actorPerson, id: "8", name: "Ben"}
	if err := d.store.stopRun("7", "r-1", by, "stopped by Ben", w.clock()); err != nil {
		t.Fatal(err)
	}
	w.advance(dispatcherapi.DefaultTotalLimit + backstopMargin + time.Second)

	due, err := d.store.pastTheBackstop(d.now())
	if err != nil || len(due) != 0 {
		t.Errorf("past the backstop: %v %v, want none", due, err)
	}
	d.backstop()

	if strings.Contains(log.String(), "backstop") {
		t.Errorf("the backstop logged:\n%s", log.String())
	}
	if n := count(t, d.store, `SELECT count(*) FROM commands`); n != 1 {
		t.Errorf("%d commands, want the one of the person's stop", n)
	}
}

// fakeRunner records each Stop. The first failures Stop calls fail.
type fakeRunner struct {
	Runner
	mu       sync.Mutex
	stops    []string
	failures int
}

func (r *fakeRunner) Stop(_ context.Context, workload string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops = append(r.stops, workload)
	if len(r.stops) <= r.failures {
		return errors.New("the Runner's API does not answer")
	}
	return nil
}

func (r *fakeRunner) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.stops)
}

// wedgedExecution makes the running Execution e-1 of the workload w-1,
// with a stop command that is stopGrace old.
func wedgedExecution(t *testing.T, w *adminWorld, runner *fakeRunner) {
	t.Helper()
	d := w.d
	d.log = slog.New(slog.DiscardHandler)
	d.runner = runner
	startedExecution(t, w)
	if _, err := d.store.db.Exec(`UPDATE executions SET workload = 'w-1' WHERE id = 'e-1'`); err != nil {
		t.Fatal(err)
	}
	by := actor{kind: actorPerson, id: "8", name: "Ben"}
	if err := d.store.stopRun("7", "r-1", by, "stopped by Ben", w.clock()); err != nil {
		t.Fatal(err)
	}
	w.advance(stopGrace)
}

// The wrapper stays wedged after the Runner's Stop: the next passes of the
// backstop do not stop it again until the Runner's end event.
func TestTheRunnerStopsAWedgedExecutionOnce(t *testing.T) {
	w := newAdminWorld(t)
	runner := &fakeRunner{}
	wedgedExecution(t, w, runner)

	w.d.stopThroughTheRunner(context.Background())
	w.d.stopThroughTheRunner(context.Background())

	if got := runner.calls(); !slices.Equal(got, []string{"w-1"}) {
		t.Errorf("the Runner's Stop calls are %v, want one for w-1", got)
	}
}

// A Stop that failed (an outage of the Runner's API) is tried again at the
// next pass of the backstop.
func TestTheRunnerStopsAWedgedExecutionAgainWhenTheStopFailed(t *testing.T) {
	w := newAdminWorld(t)
	runner := &fakeRunner{failures: 1}
	wedgedExecution(t, w, runner)

	w.d.stopThroughTheRunner(context.Background())
	w.d.stopThroughTheRunner(context.Background())
	w.d.stopThroughTheRunner(context.Background())

	if got := runner.calls(); !slices.Equal(got, []string{"w-1", "w-1"}) {
		t.Errorf("the Runner's Stop calls are %v, want the failed one and one more", got)
	}
}
