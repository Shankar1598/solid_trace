package dispatcher

import (
	"context"
	"errors"
	"testing"

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
	// The end step ran: it revoked no token, as none was kept yet.
	if err := d.store.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndHarnessExited}, now); err != nil {
		t.Fatal(err)
	}

	if _, err := d.sessionToken(context.Background(), e, started.startedAt); !errors.Is(err, errEnded) {
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
