package dispatcher_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakenotion"
)

// notionConnection reads the Notion connection "notion" as the Admin.
func notionConnection(t *testing.T, env *factorytest.Env) dispatcherapi.Connection {
	t.Helper()
	a := env.Admin().API("GET", "/api/v1/connections", nil)
	var list dispatcherapi.Connections
	a.JSON(t, &list)
	for _, c := range list.Connections {
		if c.Name == "notion" {
			return c
		}
	}
	t.Fatalf("no connection notion: %s", a.Body)
	return dispatcherapi.Connection{}
}

func TestANotionWebhookWithNoValidSignatureGets401AndStartsNoRun(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	page := readyPage(env, db, "Fix the cart total")
	body := fakenotion.PageEvent(fakenotion.NewID(), "page.properties_updated", page)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil))
	other := fakenotion.PageEvent(fakenotion.NewID(), "page.properties_updated", page)

	for name, signature := range map[string]string{
		"no signature":                  "",
		"another secret":                fakenotion.Sign("whsec-of-someone-else", body),
		"the signature of another body": fakenotion.Sign(secret, other),
		"no sha256= prefix":             good,
		"sha1= prefix":                  "sha1=" + good,
		"upper-case prefix":             "SHA256=" + good,
		"not hex":                       "sha256=" + strings.Repeat("z", 64),
		"empty":                         "sha256=",
		"a cut signature":               "sha256=" + good[:62],
		"one more byte":                 "sha256=" + good + "00",
		"the secret as signature":       "sha256=" + hex.EncodeToString([]byte(secret)),
	} {
		h := http.Header{"Content-Type": {"application/json"}}
		if signature != "" {
			h.Set(fakenotion.SignatureHeader, signature)
		}
		if a := env.Webhook(path, h, body); a.Status != http.StatusUnauthorized {
			t.Errorf("%s: %d %s, want 401", name, a.Status, a.Body)
		}
	}
	// The signature in another service's header.
	h := http.Header{"Fake-Signature": {"sha256=" + good}}
	if a := env.Webhook(path, h, body); a.Status != http.StatusUnauthorized {
		t.Errorf("a signature in Fake-Signature: %d %s, want 401", a.Status, a.Body)
	}
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("refused webhooks started %d Runs", len(list))
	}
	// A refused webhook is not a verified webhook: the poll stays at 30 s.
	if c := notionConnection(t, env); c.LastWebhookAt != nil {
		t.Errorf("the connection's last webhook is %v after refused webhooks only, want none", c.LastWebhookAt)
	}

	if a := env.NotionWebhook(path, secret, body); a.Status != http.StatusOK || !strings.Contains(string(a.Body), `"queued":true`) {
		t.Fatalf("the signed webhook: %d %s", a.Status, a.Body)
	}
	waitForRuns(t, env, 1)
	if c := notionConnection(t, env); c.LastWebhookAt == nil {
		t.Errorf("the connection has no last webhook after a verified one")
	}
}

func TestANotionConnectionWithNoWebhookSecretRefusesEachWebhook(t *testing.T) {
	env, db, path := notionEnv(t, "")
	page := readyPage(env, db, "Fix the cart total")
	body := fakenotion.PageEvent(fakenotion.NewID(), "page.properties_updated", page)
	for _, key := range []string{"", secret} {
		if a := env.NotionWebhook(path, key, body); a.Status != http.StatusUnauthorized {
			t.Errorf("a webhook signed with %q to a connection with no secret: %d %s, want 401", key, a.Status, a.Body)
		}
	}
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Errorf("refused webhooks started %d Runs", len(list))
	}
}

func TestNotionsVerificationRequestGivesTheTokenToTheOperatorOnly(t *testing.T) {
	env, db, path := notionEnv(t, "")
	const token = "secret_Vb9xQ2mN7pL4kR8sT1wY6zA3cE5gH0jU"
	h, body := fakenotion.Verification(token)
	a := env.Webhook(path, h, body)
	if a.Status != http.StatusOK || strings.Contains(string(a.Body), token) {
		t.Fatalf("the verification request: %d %s, want 200 with no token", a.Status, a.Body)
	}
	if !strings.Contains(env.Logs(), "verification_token="+token) || !strings.Contains(env.Logs(), "webhook-secret notion") {
		t.Errorf("the Dispatcher's log does not give the token and the command that saves it:\n%s", env.Logs())
	}
	settle()
	for _, ev := range env.SolidTrace.RunEvents() {
		for _, v := range ev {
			if s, _ := v.(string); strings.Contains(s, token) {
				t.Errorf("a Log at SolidTrace holds the verification token: %v", ev)
			}
		}
	}
	// It verifies nothing: the poll stays at 30 s, and nothing is queued.
	if c := notionConnection(t, env); c.LastWebhookAt != nil || c.WebhookSecret {
		t.Errorf("after the verification request the connection has last webhook %v and secret %v, want none",
			c.LastWebhookAt, c.WebhookSecret)
	}

	// A body that is not only a verification token is a webhook, and it
	// has no valid signature.
	for name, body := range map[string]string{
		"another field":        `{"verification_token":"secret_abc","id":"e-1"}`,
		"two objects":          `{"verification_token":"secret_abc"}{"verification_token":"secret_def"}`,
		"a space in the token": `{"verification_token":"secret abc"}`,
		"a new line":           `{"verification_token":"secret_abc\nlevel=ERROR"}`,
		"no token":             `{"verification_token":""}`,
		"a token over 1 KiB":   `{"verification_token":"` + strings.Repeat("a", 1025) + `"}`,
	} {
		if a := env.Webhook(path, h, []byte(body)); a.Status != http.StatusUnauthorized {
			t.Errorf("%s: %d %s, want 401", name, a.Status, a.Body)
		}
	}
	signed := fakenotion.Delivery(secret, body)
	if a := env.Webhook(path, signed, body); a.Status != http.StatusUnauthorized {
		t.Errorf("a signed verification request to a connection with no secret: %d %s, want 401", a.Status, a.Body)
	}

	// The Admin saves the token as the webhook secret: Notion's events,
	// which it signs with the token, are verified.
	if a := env.Admin().API("PUT", "/api/v1/connections/notion/webhook-secret",
		dispatcherapi.SaveWebhookSecret{Secret: token}); a.Status != http.StatusOK {
		t.Fatalf("save the webhook secret: %d %s", a.Status, a.Body)
	}
	page := readyPage(env, db, "Fix the cart total")
	if a := env.NotionWebhook(path, token, fakenotion.PageEvent(fakenotion.NewID(), "page.created", page)); a.Status != http.StatusOK {
		t.Fatalf("an event signed with the verification token: %d %s", a.Status, a.Body)
	}
	waitForRuns(t, env, 1)
}

func TestANotionEventIDThatTheDispatcherGotIsARepeat(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	page := readyPage(env, db, "Fix the cart total")
	event := fakenotion.NewID()
	first := fakenotion.PageEvent(event, "page.properties_updated", page)
	if a := env.NotionWebhook(path, secret, first); !strings.Contains(string(a.Body), `"queued":true`) {
		t.Fatalf("the first delivery: %d %s", a.Status, a.Body)
	}
	// Notion sends the event again (its retry), and a second event with
	// the same id and another body.
	for _, body := range [][]byte{first, fakenotion.CommentEvent(event, page)} {
		if a := env.NotionWebhook(path, secret, body); a.Status != http.StatusOK || !strings.Contains(string(a.Body), `"queued":false`) {
			t.Errorf("a repeat of the event id: %d %s, want 200 and not queued", a.Status, a.Body)
		}
	}
	waitForRuns(t, env, 1)
}

func TestASignedNotionEventThatTheDispatcherCannotReadGets400(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	page := readyPage(env, db, "Fix the cart total")
	for name, body := range map[string][]byte{
		"not JSON":              []byte("page.created " + page),
		"no event id":           fakenotion.PageEvent("", "page.created", page),
		"an event id over 256":  fakenotion.PageEvent(strings.Repeat("e", 257), "page.created", page),
		"a page that is a path": fakenotion.PageEvent(fakenotion.NewID(), "page.created", "../../users/me"),
		"a page id of 31 hex":   fakenotion.PageEvent(fakenotion.NewID(), "page.created", strings.Repeat("a", 31)),
		"a comment on a path":   fakenotion.Event(fakenotion.NewID(), "comment.created", "comment", fakenotion.NewID(), map[string]any{"page_id": "x/../y"}),
	} {
		if a := env.NotionWebhook(path, secret, body); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	// A page id with no dashes, as in a page's link, is the same page.
	if a := env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.created",
		strings.ReplaceAll(page, "-", ""))); a.Status != http.StatusOK {
		t.Fatalf("a page id with no dashes: %d %s", a.Status, a.Body)
	}
	run := waitForRuns(t, env, 1)[0]
	if run.Link != fakenotion.PageURL(page) {
		t.Errorf("the Run's link is %q", run.Link)
	}
}

func TestACommentOnAPageAndAChangeOfTheDatabaseReconcileAtOnce(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	page := readyPage(env, db, "Fix the cart total")
	// comment.created names the comment as its entity, and the page in
	// data.page_id.
	if a := env.NotionWebhook(path, secret, fakenotion.CommentEvent(fakenotion.NewID(), page)); a.Status != http.StatusOK {
		t.Fatalf("the comment event: %d %s", a.Status, a.Body)
	}
	waitForRuns(t, env, 1)

	// An event of the database announces the whole connection.
	readyPage(env, db, "Fix the tax total")
	if a := env.NotionWebhook(path, secret, fakenotion.Event(fakenotion.NewID(), "database.content_updated",
		"database", db, nil)); a.Status != http.StatusOK {
		t.Fatalf("the database event: %d %s", a.Status, a.Body)
	}
	waitForRuns(t, env, 2)
}

func TestAPageOfAnotherDatabaseIsNotATicketOfTheConnection(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	elsewhere := fakenotion.NewID()
	env.Notion.Database(elsewhere, statuses, "Project")
	page := fakenotion.NewID()
	env.Notion.Put(elsewhere, fakenotion.Page{ID: page, Title: "Not ours", Status: "Ready for agent",
		Select: map[string]string{"Project": "shop"}}, rosaN)
	if a := env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.created", page)); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	env.Clock.Advance(dispatcher.NotionBackstop)
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("a page of another database started %d Runs", len(list))
	}
	// A trashed page of the database is not a ticket either.
	trashed := readyPage(env, db, "Trashed")
	env.Notion.Trash(trashed)
	if a := env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.deleted", trashed)); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("a trashed page started %d Runs", len(list))
	}
}
