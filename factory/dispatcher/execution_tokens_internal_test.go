package dispatcher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/recorder"
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
	started, err := d.store.startExecution("e-1", now, defaultLimits)
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

// A new current Agent session (the resume was not possible) joins the
// running Execution's Run and becomes its current one. An ended
// Execution's Run gets none.
func TestANewCurrentAgentSessionJoinsTheRunOfARunningExecutionOnly(t *testing.T) {
	w := newAdminWorld(t)
	d, now := w.d, w.clock()
	startedExecution(t, w)
	before, old, err := d.store.runSessions("r-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.store.newCurrentSession("e-1", now); err != nil {
		t.Fatal(err)
	}
	ids, current, err := d.store.runSessions("r-1")
	if err != nil {
		t.Fatal(err)
	}
	if current == old || !sessionIDPattern.MatchString(current) || len(ids) != len(before)+1 ||
		ids[len(ids)-1] != current || ids[0] != old {
		t.Errorf("the Run's Agent sessions are %v, current %q; before %v, current %q", ids, current, before, old)
	}

	if err := d.store.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndHarnessExited}, now); err != nil {
		t.Fatal(err)
	}
	if err := d.store.newCurrentSession("e-1", now); !errors.Is(err, errEnded) {
		t.Fatalf("a new current Agent session for an ended Execution: %v, want errEnded", err)
	}
	if after, still, err := d.store.runSessions("r-1"); err != nil || len(after) != len(ids) || still != current {
		t.Errorf("after the end: %v, current %q (%v)", after, still, err)
	}
}

// The end step revokes only the tokens that are not revoked yet: an end
// step that runs again, or a token that a first end step revoked, sends
// no second revoke to SolidTrace.
func TestTheEndStepRevokesOnlyTheTokensThatAreNotRevoked(t *testing.T) {
	w := newAdminWorld(t)
	d, now := w.d, w.clock()
	e, started := startedExecution(t, w)
	for range 2 {
		if _, err := d.sessionToken(context.Background(), e, started); err != nil {
			t.Fatal(err)
		}
	}
	tokens := w.solidTrace.ChildTokens()
	if len(tokens) != 2 {
		t.Fatalf("want two child tokens, got %+v", tokens)
	}
	// The first token is revoked already (by an earlier end step).
	if err := d.store.tokenRevoked(tokens[0].ID, now); err != nil {
		t.Fatal(err)
	}

	revokes := func() []string {
		var paths []string
		for _, c := range w.solidTrace.Calls() {
			if c.Method == "DELETE" && strings.HasPrefix(c.Path, recorder.ChildTokensPath+"/") {
				paths = append(paths, c.Path)
			}
		}
		return paths
	}
	d.revokeTokens("e-1")
	want := recorder.ChildTokensPath + "/" + tokens[1].ID
	if got := revokes(); len(got) != 1 || got[0] != want {
		t.Fatalf("the end step revoked %v, want only %s", got, want)
	}
	d.revokeTokens("e-1")
	if got := revokes(); len(got) != 1 {
		t.Errorf("a second end step revoked again: %v", got)
	}
}
