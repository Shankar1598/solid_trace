package dispatcher

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The rows of P4 that Seam 3 cannot reach before the states exist
// (waiting on an account limit, ticket 63; taken over, ticket 61), and
// the boundaries of a message that queues a stopped Run whose Execution
// still ends.

var messenger = actor{kind: actorPerson, id: "1", name: "ana@example.com"}

func runState(t *testing.T, s *store, id string) dispatcherapi.Run {
	t.Helper()
	run, err := s.run("7", id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func setState(t *testing.T, s *store, id, state string) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE runs SET state = ? WHERE id = ?`, state, id); err != nil {
		t.Fatal(err)
	}
}

func TestAMessageToARunThatWaitsOnAnAccountLimitIsKeptUntilTheRunContinues(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	startedExecution(t, w)
	if err := d.store.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndHarnessExited},
		w.clock()); err != nil {
		t.Fatal(err)
	}
	setState(t, d.store, "r-1", dispatcherapi.StateWaitingOnAccountLimit)

	// 20 days later a message comes. It does not continue the Run.
	w.advance(20 * 24 * time.Hour)
	accepted, err := d.store.acceptMessage("7", "r-1", messenger, "Also fix the tax", w.clock())
	if err != nil || accepted.Continued {
		t.Fatalf("a message to a Run that waits on an account limit: %+v %v, want accepted and kept", accepted, err)
	}
	if run := runState(t, d.store, "r-1"); run.State != dispatcherapi.StateWaitingOnAccountLimit {
		t.Errorf("the Run is %s, want it to wait on the account limit still", run.State)
	}
	if ids, err := d.store.queuedRuns(); err != nil || slices.Contains(ids, "r-1") {
		t.Errorf("the queued Runs are %v (%v): the message queued the Run", ids, err)
	}

	// The message is activity: the 30 days count from it.
	idle := func(at time.Time) bool {
		runs, err := d.store.idleRuns(at.Add(-closeAfter))
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(runs, func(r closingRun) bool { return r.id == "r-1" })
	}
	if idle(w.clock().Add(29 * 24 * time.Hour)) {
		t.Errorf("the Run has no activity 29 days after the message")
	}
	if !idle(w.clock().Add(closeAfter)) {
		t.Errorf("the Run has activity 30 days after the message")
	}

	// When the Run continues (ticket 63 queues it), its next Execution
	// gets the message and resumes the Agent session.
	setState(t, d.store, "r-1", dispatcherapi.StateQueued)
	if ok, err := d.store.addExecution("r-1", "e-2", w.clock()); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}
	started, err := d.store.startExecution("e-2", w.clock(), defaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !started.resume || len(started.messages) != 1 || started.messages[0].ID != accepted.ID ||
		started.prompt != "Fix the cart" {
		t.Errorf("the task: resume %v, messages %+v, prompt %q; want a resume with the message and the prompt",
			started.resume, started.messages, started.prompt)
	}
}

func TestAMessageToATakenOverRunIsKeptAndNotTypedIntoTheTerminal(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	startedExecution(t, w)
	setState(t, d.store, "r-1", dispatcherapi.StateTakenOver)

	accepted, err := d.store.acceptMessage("7", "r-1", messenger, "Also fix the tax", w.clock())
	if err != nil || accepted.Continued {
		t.Fatalf("a message to a taken over Run: %+v %v, want accepted and kept", accepted, err)
	}
	commands, err := commandsIn(d.store.db, "e-1", 0, "")
	if err != nil || len(commands) != 0 {
		t.Errorf("the take-over Execution got the commands %+v (%v), want none", commands, err)
	}
	if run := runState(t, d.store, "r-1"); run.State != dispatcherapi.StateTakenOver {
		t.Errorf("the Run is %s, want taken over", run.State)
	}
	var kept bool
	if err := d.store.db.QueryRow(`SELECT execution_id = '' FROM messages WHERE id = ?`, accepted.ID).
		Scan(&kept); err != nil || !kept {
		t.Errorf("the message is not kept on the Run: %v", err)
	}
}

// A stop ends a running Execution with a stop command: the Execution can
// still be in its end when a message queues the Run. That end, in each
// form, leaves the Run queued, with the message, for a new Execution.
func TestTheEndOfAStoppedExecutionLeavesTheRunThatAMessageQueued(t *testing.T) {
	for name, end := range map[string]func(s *store, at time.Time) error{
		"the end call": func(s *store, at time.Time) error {
			return s.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndStopped}, at)
		},
		"a lost machine": func(s *store, at time.Time) error {
			_, err := s.failExecution("e-1", "the machine is lost", nil, at)
			return err
		},
		"a requeue": func(s *store, at time.Time) error {
			_, err := s.requeueExecution("e-1", "preempted", at)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newAdminWorld(t)
			d := w.d
			startedExecution(t, w)
			if err := d.store.stopRun("7", "r-1", messenger, "", w.clock()); err != nil {
				t.Fatal(err)
			}
			accepted, err := d.store.acceptMessage("7", "r-1", messenger, "Go on with the tax", w.clock())
			if err != nil || !accepted.Continued {
				t.Fatalf("a message to the stopped Run: %+v %v, want it continued", accepted, err)
			}
			// The ending Execution does not get the message.
			if commands, err := commandsIn(d.store.db, "e-1", 0, dispatcherapi.CommandMessage); err != nil ||
				len(commands) != 0 {
				t.Errorf("the stopped Execution got the message commands %+v (%v)", commands, err)
			}
			if ids, _ := d.store.queuedRuns(); slices.Contains(ids, "r-1") {
				t.Errorf("the Run gets a new Execution before the stopped one ended")
			}

			if err := end(d.store, w.clock()); err != nil {
				t.Fatal(err)
			}
			run := runState(t, d.store, "r-1")
			if run.State != dispatcherapi.StateQueued || run.Reason != reasonContinued {
				t.Errorf("after the end the Run is %s (%q), want queued by the message", run.State, run.Reason)
			}
			if ids, err := d.store.queuedRuns(); err != nil || !slices.Contains(ids, "r-1") {
				t.Fatalf("the queued Runs are %v (%v), want r-1", ids, err)
			}
			if ok, err := d.store.addExecution("r-1", "e-2", w.clock()); !ok || err != nil {
				t.Fatalf("add the Execution: %v %v", ok, err)
			}
			started, err := d.store.startExecution("e-2", w.clock(), defaultLimits)
			if err != nil || len(started.messages) != 1 || started.messages[0].ID != accepted.ID {
				t.Errorf("the new Execution's messages: %+v (%v), want the message", started.messages, err)
			}
		})
	}
}

func TestAMessageToADoneRunWhosePRHasAnOutcomeIsRefusedAsClosed(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	startedExecution(t, w)
	if err := d.store.savePROutcome("r-1", dispatcherapi.PRMerged, w.clock()); err != nil {
		t.Fatal(err)
	}
	// The running Execution still takes a message.
	if _, err := d.store.acceptMessage("7", "r-1", messenger, "One more test", w.clock()); err != nil {
		t.Fatalf("a message to the running Execution: %v", err)
	}
	if err := d.store.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndHarnessExited},
		w.clock()); err != nil {
		t.Fatal(err)
	}
	// Before the close pass: the Run cannot continue.
	_, err := d.store.acceptMessage("7", "r-1", messenger, "Also fix the tax", w.clock())
	var refused *notMessageable
	if !errors.As(err, &refused) || !refused.closed {
		t.Fatalf("a message after the PR outcome: %v, want the closed refusal", err)
	}
	if run := runState(t, d.store, "r-1"); run.State == dispatcherapi.StateQueued {
		t.Errorf("the refused message queued the Run")
	}
}
