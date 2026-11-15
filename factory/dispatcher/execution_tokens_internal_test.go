package dispatcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The wrapper's token call and the end call can run at the same time: the
// end step can read the Execution's tokens before the token call keeps
// its new one. Through the API the order is not under the test's control,
// so this test ends the Execution between the token call's start and the
// make.
func TestAChildTokenMadeAfterTheEndIsRevokedAtOnce(t *testing.T) {
	w := newAdminWorld(t)
	d, now := w.d, w.clock()
	e, started := startedExecution(t, w)
	// The end step ran: it revoked no token, as none was kept yet.
	if err := d.store.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndHarnessExited}, now); err != nil {
		t.Fatal(err)
	}

	if _, err := d.sessionToken(context.Background(), e, started); !errors.Is(err, errEnded) {
		t.Fatalf("the token of an ended Execution: %v, want errEnded", err)
	}
	tokens := w.solidTrace.ChildTokens()
	if len(tokens) != 1 {
		t.Fatalf("want the one child token that the make gave, got %+v", tokens)
	}
	if !tokens[0].Revoked {
		t.Error("the child token made after the end is not revoked: it lives until its expiry")
	}
	if ids, err := d.store.tokensToRevoke("e-1"); err != nil || len(ids) != 0 {
		t.Errorf("the ended Execution keeps tokens %v (%v)", ids, err)
	}
}

// startedExecution is the world's Run r-1 with its Execution e-1 running.
func startedExecution(t *testing.T, w *adminWorld) (storedExecution, time.Time) {
	t.Helper()
	d, now := w.d, w.clock()
	run := storedRun{Run: dispatcherapi.Run{ID: "r-1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: now}, organizationID: "7", projectID: "42"}
	if err := d.store.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.store.addExecution(run.ID, "e-1", now); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}
	started, err := d.store.startExecution("e-1", now)
	if err != nil {
		t.Fatal(err)
	}
	e, err := d.store.execution("e-1")
	if err != nil {
		t.Fatal(err)
	}
	return e, started.startedAt
}

// The token call reads that the Execution runs, then adds the Agent
// session id. The end call can come between the two: the add must see
// the end itself, so that no id is added to the Run after its end step.
func TestAnAgentSessionIDIsNotAddedToTheRunOfAnEndedExecution(t *testing.T) {
	w := newAdminWorld(t)
	d, now := w.d, w.clock()
	startedExecution(t, w)
	before, _, err := d.store.runSessions("r-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.store.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndHarnessExited}, now); err != nil {
		t.Fatal(err)
	}

	const id = "0b5d2c84-7e1f-4a3b-9c6d-2f8e1a7b3c40"
	if err := d.store.addRunSession("e-1", id, now); !errors.Is(err, errEnded) {
		t.Fatalf("add an Agent session id to an ended Execution's Run: %v, want errEnded", err)
	}
	after, _, err := d.store.runSessions("r-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("the Run's Agent session ids are %v after the end, want %v", after, before)
	}
}
