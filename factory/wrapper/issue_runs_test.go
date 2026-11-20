package wrapper_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// The attributes that Console's 'Factory' Integration puts on a delivery
// (Notifiers::FactoryNotifier).
func issueAttributes(number string) map[string]string {
	return map[string]string{"solidtrace.notification": "issue_created", "solidtrace.issue_number": number}
}

func monitorAttributes(id string) map[string]string {
	return map[string]string{"solidtrace.notification": "monitor_firing", "solidtrace.monitor_id": id,
		"solidtrace.monitor_group": "service=api"}
}

// factoryDelivery sends a signed delivery for the Project shop with the
// attributes, as Console's 'Factory' Integration does, and gives the
// Run's id.
func factoryDelivery(t *testing.T, env *factorytest.Env, path, secret, delivery string, attributes map[string]string) string {
	t.Helper()
	raw, _ := json.Marshal(dispatcherapi.GenericWebhook{Project: "shop", Prompt: "SolidTrace Issue #7: the cart total is wrong",
		Title: "The cart total is wrong", DedupeKey: "solid-trace-" + delivery, Attributes: attributes})
	a := env.Webhook(path, factorytest.GenericDelivery(secret, delivery, env.Clock.Now(), raw), raw)
	if a.Status != http.StatusAccepted {
		t.Fatalf("the delivery %s: %d %s, want 202", delivery, a.Status, a.Body)
	}
	var answer dispatcherapi.GenericWebhookAnswer
	a.JSON(t, &answer)
	return answer.RunID
}

// standingTask is a sentence of the standing task that the Dispatcher
// adds to an Issue or a Monitor Run's first message.
const standingTask = "Open a PR only when a check proves the fix"

// firstMessage is the transcript that the Run's first Execution uploaded.
func uploadedTranscript(t *testing.T, env *factorytest.Env) []byte {
	t.Helper()
	session := firstToken(t, env).AgentSessionIDs[0]
	return env.SolidTrace.Stored(session, session+".jsonl")
}

func TestAnIssueOrAMonitorDeliveryStartsARunWhoseFirstMessageHasTheStandingTask(t *testing.T) {
	for name, attributes := range map[string]map[string]string{
		"Issue": issueAttributes("7"), "Monitor": monitorAttributes("12"),
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			sender, path := env.Sender("Factory")
			env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Looked"), reply, result,
				waitForEnd)
			id := factoryDelivery(t, env, path, sender.Secret, "d-1", attributes)
			waitFor(t, env, ana, id, dispatcherapi.StateDone)
			transcript := uploadedTranscript(t, env)
			if !bytes.Contains(transcript, []byte(standingTask)) || !bytes.Contains(transcript, []byte("the cart total is wrong")) {
				t.Errorf("the first message has no standing task, or not the delivery's prompt:\n%s", transcript)
			}
		})
	}
}

func TestADeliveryWithNoIssueOrMonitorGetsNoStandingTask(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Alerts")
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd)
	// The notification attribute alone names no Issue and no Monitor.
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", map[string]string{"solidtrace.notification": "issue_created"})
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if transcript := uploadedTranscript(t, env); bytes.Contains(transcript, []byte(standingTask)) {
		t.Errorf("a plain delivery got the standing task:\n%s", transcript)
	}
}
