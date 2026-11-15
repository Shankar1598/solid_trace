package wrapper_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakeforge"
)

// Bitbucket's webhook gives the PR outcome at once: pullrequest:fulfilled
// and pullrequest:rejected queue a reconcile of the PR, which reads the
// PR's state from the forge, records the outcome and closes the Run. The
// 5-minute check stays as the backstop.

const forgeSecret = "the-bitbucket-webhook-secret"

// queued gives the "queued" of a webhook's 200 answer.
func queued(t *testing.T, a factorytest.Answer) bool {
	t.Helper()
	var got struct {
		Queued bool `json:"queued"`
	}
	if err := json.Unmarshal(a.Body, &got); err != nil {
		t.Fatalf("the webhook's answer %s: %v", a.Body, err)
	}
	return got.Queued
}

func TestAFulfilledWebhookGivesThePROutcomeMergedAndClosesTheRunBeforeTheNextCheck(t *testing.T) {
	env, ana := newEnv(t)
	path := env.ForgeWebhooks(forgeSecret)
	id, pr := runWithPR(t, env, ana)
	wip := addWIPRef(t, env, id)
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "before the merge")

	// The clock does not move, so the 5-minute check does not run again:
	// only the webhook can give the outcome.
	env.Forge.SetState("example/shop", pr.ID, "MERGED")
	a := env.ForgeWebhook(path, forgeSecret, "{req-1}", fakeforge.EventPRFulfilled, env.Forge.PRWebhook("example/shop", pr.ID))
	if a.Status != 200 || !queued(t, a) {
		t.Fatalf("a signed pullrequest:fulfilled: %d %s, want 200 queued", a.Status, a.Body)
	}
	closedEvent(t, env, id, dispatcherapi.StateDone, "merged", "pr_merged")
	run := getRun(t, ana, id)
	if run.PROutcome != "merged" || run.ClosedAt == nil || !run.ClosedAt.Equal(env.Clock.Now()) {
		t.Errorf("the Run has the PR outcome %q and is closed at %v, want merged at %s", run.PROutcome, run.ClosedAt,
			env.Clock.Now())
	}
	// The close deletes the WIP ref after the closed event.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, ok := env.Forge.Ref("example/shop", wip); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the WIP ref %s of the Closed Run is not deleted after 10 s", wip)
		}
	}
}
