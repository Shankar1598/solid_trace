package factorytest

import (
	"net/http"
	"strings"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/fakenotion"
)

// NotionToken is the integration token of the Notion connection with
// name.
func NotionToken(name string) string { return "ntn_fake_" + strings.ReplaceAll(name, "-", "_") }

// NotionConnection makes a Notion connection with name for the database
// of the fake Notion, as an Admin, with secret as its webhook secret (""
// is none). Its token is NotionToken(name), which the fake Notion accepts.
// It returns the path of its webhook URL, such as /webhooks/notion/<id>.
func (e *Env) NotionConnection(name, secret, database string) string {
	e.t.Helper()
	e.Notion.Token(NotionToken(name))
	admin := e.Admin()
	a := admin.API("PUT", "/api/v1/connections/"+name, dispatcherapi.SaveConnection{Kind: "notion",
		Token: NotionToken(name)})
	if a.Status != http.StatusOK {
		e.t.Fatalf("save the Notion connection %s: %d %s", name, a.Status, a.Body)
	}
	a = admin.API("PUT", "/api/v1/connections/"+name+"/tracker", dispatcherapi.TrackerSettings{Database: database})
	if a.Status != http.StatusOK {
		e.t.Fatalf("save the database of %s: %d %s", name, a.Status, a.Body)
	}
	var c dispatcherapi.Connection
	a.JSON(e.t, &c)
	if secret != "" {
		a = admin.API("PUT", "/api/v1/connections/"+name+"/webhook-secret", dispatcherapi.SaveWebhookSecret{Secret: secret})
		if a.Status != http.StatusOK {
			e.t.Fatalf("save the webhook secret of %s: %d %s", name, a.Status, a.Body)
		}
	}
	path, ok := strings.CutPrefix(c.WebhookURL, PublicWebhookURL)
	if !ok || !strings.HasPrefix(path, "/webhooks/notion/") {
		e.t.Fatalf("the connection %s has the webhook URL %q, not one under %s/webhooks/notion/", name, c.WebhookURL,
			PublicWebhookURL)
	}
	return path
}

// NotionWebhook sends a Notion event to path, signed with secret.
func (e *Env) NotionWebhook(path, secret string, body []byte) Answer {
	e.t.Helper()
	return e.Webhook(path, fakenotion.Delivery(secret, body), body)
}
