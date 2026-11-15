package factorytest

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/fakesource"
	"github.com/solidtrace/factory/internal/faketracker"
)

// PublicWebhookURL is the Dispatcher's public webhook URL in each Env (the
// name that Caddy serves). Nothing listens there: a test sends to
// WebhookListenerURL with the path of a connection's webhook URL.
const PublicWebhookURL = "https://factory.example.com"

// reconcileTick is how often the Dispatcher looks for a due backstop, so
// that a test sees a backstop soon after it moves the clock.
const reconcileTick = 20 * time.Millisecond

// WebhookConnection makes a connection of the fake source with name, as
// an Admin, with secret as its webhook secret ("" is none). It returns
// the path of its webhook URL, such as /webhooks/fake/<id>.
func (e *Env) WebhookConnection(name, secret string) string {
	e.t.Helper()
	return e.WebhookConnectionOf(fakesource.Kind, name, secret)
}

// WebhookConnectionOf is WebhookConnection for a connection of the kind,
// fakesource.Kind or fakesource.OtherKind.
func (e *Env) WebhookConnectionOf(kind, name, secret string) string {
	e.t.Helper()
	if e.admin == nil {
		e.admin = e.PersonIn("factorytest-admin", Organization, "admin")
	}
	a := e.admin.API("PUT", "/api/v1/connections/"+name, dispatcherapi.SaveConnection{Kind: kind,
		Token: "fake-token-" + name})
	if a.Status != http.StatusOK {
		e.t.Fatalf("save the connection %s: %d %s", name, a.Status, a.Body)
	}
	var c dispatcherapi.Connection
	a.JSON(e.t, &c)
	if secret != "" {
		a = e.admin.API("PUT", "/api/v1/connections/"+name+"/webhook-secret", dispatcherapi.SaveWebhookSecret{Secret: secret})
		if a.Status != http.StatusOK {
			e.t.Fatalf("save the webhook secret of %s: %d %s", name, a.Status, a.Body)
		}
		a.JSON(e.t, &c)
	}
	path, ok := strings.CutPrefix(c.WebhookURL, PublicWebhookURL)
	if !ok || !strings.HasPrefix(path, "/webhooks/") {
		e.t.Fatalf("the connection %s has the webhook URL %q, not one under %s/webhooks/", name, c.WebhookURL, PublicWebhookURL)
	}
	return path
}

// Webhook sends one POST to the webhook listener at path with the
// headers and the raw body.
func (e *Env) Webhook(path string, header http.Header, body []byte) Answer {
	e.t.Helper()
	return e.WebhookRequest("POST", path, header, bytes.NewReader(body))
}

// WebhookRequest sends one request to the webhook listener. A body that
// is not a *bytes.Reader goes with no Content-Length (chunked).
func (e *Env) WebhookRequest(method, path string, header http.Header, body io.Reader) Answer {
	e.t.Helper()
	req, err := http.NewRequest(method, e.WebhookListenerURL()+path, body)
	if err != nil {
		e.t.Fatal(err)
	}
	for name, values := range header {
		req.Header[name] = values
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	return Answer{resp.StatusCode, resp.Header, answer}
}

// FakeDelivery is the headers of a delivery of the fake source with the
// delivery id, signed with secret.
func FakeDelivery(secret, delivery string, body []byte) http.Header {
	h := http.Header{}
	h.Set(fakesource.SignatureHeader, fakesource.Sign(secret, body))
	h.Set(fakesource.DeliveryHeader, delivery)
	h.Set("Content-Type", "application/json")
	return h
}

// TrackerConnection makes a connection of the fake tracker with name, as
// an Admin, with secret as its webhook secret ("" is none). Its token is
// fake-token-<name>. It returns the path of its webhook URL.
func (e *Env) TrackerConnection(name, secret string) string {
	e.t.Helper()
	return e.WebhookConnectionOf(faketracker.Kind, name, secret)
}

// TrackerWebhook sends a delivery of the fake tracker to path that
// announces a change of the ticket, signed with secret.
func (e *Env) TrackerWebhook(path, secret, delivery, ticket string) Answer {
	e.t.Helper()
	header, body := faketracker.Delivery(secret, delivery, ticket)
	return e.Webhook(path, header, body)
}

// Admin is the Admin of Organization that the Env's helpers act as.
func (e *Env) Admin() *Person {
	if e.admin == nil {
		e.admin = e.PersonIn("factorytest-admin", Organization, "admin")
	}
	return e.admin
}
