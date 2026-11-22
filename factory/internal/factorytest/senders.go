package factorytest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Sender makes a sender of the generic webhook with name, as an Admin,
// that can start Runs in the Projects with the slugs projects (none is
// each Project). It returns the sender and the path of its webhook URL,
// such as /webhooks/generic/<id>.
func (e *Env) Sender(name string, projects ...string) (dispatcherapi.NewSender, string) {
	e.t.Helper()
	return e.makeSender(dispatcherapi.SaveSender{Name: name, Projects: projects})
}

// FactorySender makes the sender of a 'Factory' Integration with name, as
// Console does for an Admin: it can start Issue and Monitor Runs in each
// Project. It returns the sender and the path of its webhook URL.
func (e *Env) FactorySender(name string) (dispatcherapi.NewSender, string) {
	e.t.Helper()
	return e.makeSender(dispatcherapi.SaveSender{Name: name, Factory: true})
}

func (e *Env) makeSender(save dispatcherapi.SaveSender) (dispatcherapi.NewSender, string) {
	e.t.Helper()
	name := save.Name
	if e.admin == nil {
		e.admin = e.PersonIn("factorytest-admin", Organization, "admin")
	}
	a := e.admin.API("POST", "/api/v1/senders", save)
	if a.Status != http.StatusCreated {
		e.t.Fatalf("make the sender %s: %d %s", name, a.Status, a.Body)
	}
	var sender dispatcherapi.NewSender
	a.JSON(e.t, &sender)
	path, ok := strings.CutPrefix(sender.WebhookURL, PublicWebhookURL)
	if !ok || path != "/webhooks/generic/"+sender.ID {
		e.t.Fatalf("the sender %s has the webhook URL %q, not %s/webhooks/generic/%s", name, sender.WebhookURL,
			PublicWebhookURL, sender.ID)
	}
	return sender, path
}

// GenericSignature is the SolidTrace-Signature of a generic webhook
// delivery with the timestamp and the raw body, signed with secret.
func GenericSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// GenericDelivery is the headers of a generic webhook delivery with the
// delivery id, sent at, and signed with secret.
func GenericDelivery(secret, delivery string, at time.Time, body []byte) http.Header {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	h := http.Header{}
	h.Set(dispatcherapi.HeaderTimestamp, timestamp)
	h.Set(dispatcherapi.HeaderDelivery, delivery)
	h.Set(dispatcherapi.HeaderSignature, GenericSignature(secret, timestamp, body))
	h.Set("Content-Type", "application/json")
	return h
}
