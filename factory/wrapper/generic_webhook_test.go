package wrapper_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// deliver sends a signed generic webhook delivery for the Project shop
// with the dedupe key, and gives the 202 answer.
func deliver(t *testing.T, env *factorytest.Env, path, secret, delivery, key string) dispatcherapi.GenericWebhookAnswer {
	t.Helper()
	raw, _ := json.Marshal(dispatcherapi.GenericWebhook{Project: "shop", Prompt: "Fix " + delivery, DedupeKey: key})
	a := env.Webhook(path, factorytest.GenericDelivery(secret, delivery, env.Clock.Now(), raw), raw)
	if a.Status != http.StatusAccepted {
		t.Fatalf("the delivery %s: %d %s, want 202", delivery, a.Status, a.Body)
	}
	var answer dispatcherapi.GenericWebhookAnswer
	a.JSON(t, &answer)
	return answer
}

// A Run that waits on a person is active, so it keeps its dedupe_key, but
// it has no Execution queued or live, so it does not count to the
// sender's limit of live Runs. A done Run frees its dedupe_key.
func TestADoneRunFreesItsDedupeKeyAndARunThatWaitsOnAPersonIsNotLive(t *testing.T) {
	env := factorytest.New(t, factorytest.Executions, func(e *factorytest.Env) { e.Config.SenderActiveRuns = 1 })
	env.Project("shop")
	ana := env.Member("Ana")
	sender, path := env.Sender("Alerts")

	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "needs-a-person", "--summary", "Two fixes", "--question", "Which one?"),
		`{"fake":"await_message","eof_code":0}`)
	waiting := deliver(t, env, path, sender.Secret, "d-1", "issue-1")
	waitFor(t, env, ana, waiting.RunID, dispatcherapi.StateWaitingOnPerson)
	if again := deliver(t, env, path, sender.Secret, "d-2", "issue-1"); again.Started || again.RunID != waiting.RunID {
		t.Errorf("the dedupe_key of a Run that waits on a person: %+v, want the Run %s", again, waiting.RunID)
	}

	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd)
	first := deliver(t, env, path, sender.Secret, "d-3", "issue-2")
	if !first.Started {
		t.Fatalf("a Run with a limit of 1 live Run, while the other Run waits on a person: %+v", first)
	}
	waitFor(t, env, ana, first.RunID, dispatcherapi.StateDone)
	if next := deliver(t, env, path, sender.Secret, "d-4", "issue-2"); !next.Started || next.RunID == first.RunID {
		t.Errorf("the dedupe_key of a done Run: %+v, want a new Run", next)
	}
}
