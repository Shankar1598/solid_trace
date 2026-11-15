package dispatcher_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakesource"
)

const secret = "whsec-for-tests"

func body(t *testing.T, b fakesource.Body) []byte {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// deliver sends a signed delivery of entity with the delivery id.
func deliver(t *testing.T, env *factorytest.Env, path, delivery, entity string) factorytest.Answer {
	t.Helper()
	raw := body(t, fakesource.Body{Entity: entity})
	return env.Webhook(path, factorytest.FakeDelivery(secret, delivery, raw), raw)
}

// reconciledOf waits for a reconcile step of the connection's entity.
func reconciledOf(t *testing.T, env *factorytest.Env, connection, entity string) fakesource.Reconcile {
	t.Helper()
	r, ok := env.Source.WaitFor(func(r fakesource.Reconcile) bool {
		return r.Connection == connection && r.Entity == entity
	})
	if !ok {
		t.Fatalf("no reconcile step of %s %q after 10 s: %+v", connection, entity, env.Source.Reconciles())
	}
	return r
}

// stepsOf counts the reconcile steps of the connection's entity so far.
func stepsOf(env *factorytest.Env, connection, entity string) int {
	n := 0
	for _, r := range env.Source.Reconciles() {
		if r.Connection == connection && r.Entity == entity {
			n++
		}
	}
	return n
}

// settle gives the Dispatcher's loops time to act on what a test sent.
func settle() { time.Sleep(200 * time.Millisecond) }

func TestAValidWebhookAnswers200InLessThanASecondAndTheReconcileStepRunsAfterIt(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	reconciledOf(t, env, "tickets", "") // the reconcile at start
	env.Source.Change("T-1")
	release := env.Source.Hold()
	defer release()

	began := time.Now()
	a := deliver(t, env, path, "d-1", "T-1")
	if took := time.Since(began); a.Status != http.StatusOK || took >= time.Second {
		t.Fatalf("a valid webhook: %d %s in %v, want 200 in less than 1 s", a.Status, a.Body, took)
	}
	if n := stepsOf(env, "tickets", "T-1"); n != 0 {
		t.Fatalf("the reconcile step ran before the answer, %d times", n)
	}
	release()
	if r := reconciledOf(t, env, "tickets", "T-1"); len(r.Acted) != 1 || r.Acted[0] != "T-1" {
		t.Errorf("the reconcile step after the webhook acted on %v, want [T-1]", r.Acted)
	}
}

func TestThePrivateListenerServesTheSameWebhookHandler(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	raw := body(t, fakesource.Body{Entity: "T-1"})
	post := func(header http.Header) int {
		req, err := http.NewRequest("POST", env.URL()+path, strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header = header
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if status := post(http.Header{}); status != http.StatusUnauthorized {
		t.Errorf("no signature on the private listener: %d, want 401", status)
	}
	if status := post(factorytest.FakeDelivery(secret, "d-1", raw)); status != http.StatusOK {
		t.Errorf("a signed delivery on the private listener: %d, want 200", status)
	}
	reconciledOf(t, env, "tickets", "T-1")
}

func TestTheWebhookListenerAnswers404ToEachOtherPathAndMethodAndServesNoAPI(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	dana := admin(t, env)
	raw := body(t, fakesource.Body{Entity: "T-1"})
	signed := factorytest.FakeDelivery(secret, "d-1", raw)
	signed.Set("Authorization", "Bearer "+dana.Token)

	for _, c := range []struct{ method, path string }{
		{"GET", path}, {"PUT", path}, {"DELETE", path}, {"HEAD", path}, {"PATCH", path},
		{"POST", path + "/"}, {"POST", path + "/more"}, {"POST", "/webhooks/fake"}, {"POST", "/webhooks/fake/"},
		{"POST", "/webhooks"}, {"POST", "/webhooks/"}, {"POST", "/webhooks/notakind/" + path[len("/webhooks/fake/"):]},
		{"POST", "/webhooks/fake/nosuchconnection"}, {"POST", "/webhooks/bitbucket/" + path[len("/webhooks/fake/"):]},
		{"POST", "/webhooks/FAKE/" + path[len("/webhooks/fake/"):]}, {"POST", "/webhooks/fake/..%2F..%2Fapi%2Fv1%2Fruns"},
		{"POST", "/api/v1/runs"}, {"GET", "/api/v1/runs"}, {"GET", "/api/v1/connections"}, {"GET", "/api/v1/status"},
		{"PUT", "/api/v1/connections/x"}, {"POST", "/api/v1/executions/x/task"}, {"GET", "/"}, {"POST", "/"},
	} {
		a := env.WebhookRequest(c.method, c.path, signed, strings.NewReader(string(raw)))
		if a.Status != http.StatusNotFound {
			t.Errorf("%s %s on the webhook listener: %d %s, want 404", c.method, c.path, a.Status, a.Body)
		}
		if strings.Contains(string(a.Body), "example") || strings.Contains(string(a.Body), "runs\"") {
			t.Errorf("%s %s on the webhook listener gave data: %s", c.method, c.path, a.Body)
		}
	}
	if n := stepsOf(env, "tickets", "T-1"); n != 0 {
		t.Errorf("a request to another path ran %d reconcile steps", n)
	}
	if a := env.Webhook(path, signed, raw); a.Status != http.StatusOK {
		t.Errorf("POST %s: %d %s, want 200", path, a.Status, a.Body)
	}
}
