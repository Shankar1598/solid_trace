package dispatcher_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// genericBody is the JSON of a generic webhook body.
func genericBody(t *testing.T, b dispatcherapi.GenericWebhook) []byte {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// sendGeneric sends a signed delivery of b at the Dispatcher's clock time.
func sendGeneric(t *testing.T, env *factorytest.Env, path, secret, delivery string, b dispatcherapi.GenericWebhook) factorytest.Answer {
	t.Helper()
	raw := genericBody(t, b)
	return env.Webhook(path, factorytest.GenericDelivery(secret, delivery, env.Clock.Now(), raw), raw)
}

// accepted checks that the answer is 202 and gives it.
func accepted(t *testing.T, a factorytest.Answer) dispatcherapi.GenericWebhookAnswer {
	t.Helper()
	if a.Status != http.StatusAccepted {
		t.Fatalf("the delivery got %d %s, want 202", a.Status, a.Body)
	}
	var answer dispatcherapi.GenericWebhookAnswer
	a.JSON(t, &answer)
	if answer.RunID == "" {
		t.Fatalf("the 202 gives no Run: %s", a.Body)
	}
	return answer
}

func TestASignedDeliveryStartsARunWithItsPromptTitleLinkAndAttributes(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	sender, path := env.Sender("Checkout alerts")
	ana := member(t, env)

	answer := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-1", dispatcherapi.GenericWebhook{
		Project:    "shop",
		Prompt:     "Find why the cart test is flaky.\nIt fails one time in ten.",
		Branch:     "release/2.0",
		Title:      "Flaky cart test",
		Link:       "https://tracker.example.com/T-12",
		Attributes: map[string]string{"ticket": "T-12", "priority": "high"},
	}))
	if !answer.Started || answer.URL != env.SolidTrace.URL()+"/example/runs/"+answer.RunID {
		t.Errorf("the answer: %+v, want a started Run with its Console link", answer)
	}

	a := ana.API("GET", "/api/v1/runs/"+answer.RunID, nil)
	var run dispatcherapi.Run
	a.JSON(t, &run)
	if a.Status != http.StatusOK || run.State != dispatcherapi.StateQueued || run.Project != "shop" ||
		run.Prompt != "Find why the cart test is flaky.\nIt fails one time in ten." || run.Title != "Flaky cart test" ||
		run.Link != "https://tracker.example.com/T-12" || len(run.Attributes) != 2 || run.Attributes["ticket"] != "T-12" ||
		run.Attributes["priority"] != "high" {
		t.Errorf("the Run: %d %+v", a.Status, run)
	}
	if want := (dispatcherapi.Person{Kind: dispatcherapi.ActorSender, ID: sender.ID, Name: "Checkout alerts"}); run.StartedBy != want {
		t.Errorf("the Run's starter is %+v, want the sender %+v", run.StartedBy, want)
	}

	got := mustCLI(t, ana, "runs", "get", answer.RunID)
	for _, want := range []string{"Flaky cart test", "by the sender Checkout alerts", "https://tracker.example.com/T-12",
		"\n  ticket: T-12\n"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("runs get has no %q:\n%s", want, got.Stdout)
		}
	}

	events := env.WaitForRunEvents(answer.RunID, 2)
	created := events[0]
	for name, want := range map[string]any{"actor.kind": "sender", "actor.id": sender.ID, "actor.name": "Checkout alerts",
		"run.started_by.kind": "sender", "run.title": "Flaky cart test", "run.base_branch": "release/2.0",
		"run.link": "https://tracker.example.com/T-12", "run.attributes.ticket": "T-12", "run.trigger": "webhook"} {
		if created[name] != want {
			t.Errorf("the created event has %s = %v, want %v", name, created[name], want)
		}
	}
}
