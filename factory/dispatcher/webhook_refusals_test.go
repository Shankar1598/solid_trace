package dispatcher_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakesource"
)

func TestAWebhookWithAWrongOrAbsentSignatureGets401BeforeItsBodyIsParsed(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	noSecret := env.WebhookConnection("nosecret", "")
	reconciledOf(t, env, "tickets", "")
	raw := body(t, fakesource.Body{Entity: "T-1"})
	notJSON := []byte("{this is not JSON")
	valid := fakesource.Sign(secret, raw)
	header := func(signature, delivery string) http.Header {
		h := http.Header{}
		if signature != "" {
			h.Set(fakesource.SignatureHeader, signature)
		}
		h.Set(fakesource.DeliveryHeader, delivery)
		return h
	}

	for name, c := range map[string]struct {
		path      string
		signature string
		body      []byte
	}{
		"no signature":                  {path, "", raw},
		"another secret":                {path, fakesource.Sign("another-secret", raw), raw},
		"the signature of another body": {path, fakesource.Sign(secret, []byte(`{"entity":"T-2"}`)), raw},
		"one byte more in the body":     {path, valid, append(append([]byte{}, raw...), ' ')},
		"an empty body":                 {path, valid, nil},
		"a cut signature":               {path, valid[:len(valid)-2], raw},
		"a signature that is not hex":   {path, "sha256=" + strings.Repeat("z", 64), raw},
		"an empty signature":            {path, "sha256=", raw},
		"no sha256= prefix":             {path, strings.TrimPrefix(valid, "sha256="), raw},
		"a body that is not JSON":       {path, fakesource.Sign("another-secret", notJSON), notJSON},
		"a connection with no secret":   {noSecret, fakesource.Sign("", raw), raw},
	} {
		if a := env.Webhook(c.path, header(c.signature, "d-"+name), c.body); a.Status != http.StatusUnauthorized {
			t.Errorf("%s: %d %s, want 401", name, a.Status, a.Body)
		}
	}
	// A valid signature on a body that is not JSON: the body is parsed
	// only after it is verified.
	if a := env.Webhook(path, header(fakesource.Sign(secret, notJSON), "d-x"), notJSON); a.Status != http.StatusBadRequest {
		t.Errorf("a signed body that is not JSON: %d %s, want 400", a.Status, a.Body)
	}
	settle()
	for _, r := range env.Source.Reconciles() {
		if r.Entity != "" {
			t.Errorf("a webhook that is not valid ran the reconcile step %+v", r)
		}
	}
	var list dispatcherapi.Connections
	admin(t, env).API("GET", "/api/v1/connections", nil).JSON(t, &list)
	for _, c := range list.Connections {
		if c.Name == "nosecret" && (c.LastWebhookAt != nil || c.WebhookSecret) {
			t.Errorf("the connection with no secret: %+v", c)
		}
	}
}

func TestADeliveryWithATimeOutsideTheKindsWindowGets401(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	for name, c := range map[string]struct {
		age  time.Duration
		want int
	}{
		"61 s old":   {61 * time.Second, http.StatusUnauthorized},
		"61 s ahead": {-61 * time.Second, http.StatusUnauthorized},
		"60 s old":   {60 * time.Second, http.StatusOK},
	} {
		raw := body(t, fakesource.Body{Entity: "T-1", Timestamp: env.Clock.Now().Add(-c.age).UnixMilli()})
		if a := env.Webhook(path, factorytest.FakeDelivery(secret, "d-"+name, raw), raw); a.Status != c.want {
			t.Errorf("a delivery %s: %d %s, want %d", name, a.Status, a.Body, c.want)
		}
	}
}

func queued(a factorytest.Answer) bool { return strings.Contains(string(a.Body), `"queued":true`) }

func TestARepeatedDeliveryIDWithin7DaysDoesNothing(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	other := env.WebhookConnection("other", secret)
	reconciledOf(t, env, "tickets", "")

	if a := deliver(t, env, path, "d-1", "T-1"); a.Status != http.StatusOK || !queued(a) {
		t.Fatalf("the first delivery: %d %s", a.Status, a.Body)
	}
	reconciledOf(t, env, "tickets", "T-1")
	if a := deliver(t, env, path, "d-1", "T-1"); a.Status != http.StatusOK || queued(a) {
		t.Errorf("a repeat: %d %s, want 200 that queues nothing", a.Status, a.Body)
	}
	// The same id from another connection is not a repeat.
	deliver(t, env, other, "d-1", "T-1")
	reconciledOf(t, env, "other", "T-1")

	env.Clock.Advance(7 * 24 * time.Hour)
	if a := deliver(t, env, path, "d-1", "T-1"); a.Status != http.StatusOK || queued(a) {
		t.Errorf("a repeat 7 days later: %d %s, want a repeat", a.Status, a.Body)
	}
	settle()
	if n := stepsOf(env, "tickets", "T-1"); n != 1 {
		t.Errorf("the repeats ran %d reconcile steps of T-1, want 1 in all", n)
	}
	env.Clock.Advance(time.Second)
	if a := deliver(t, env, path, "d-1", "T-1"); !queued(a) {
		t.Errorf("the same id more than 7 days later: %d %s, want a new delivery", a.Status, a.Body)
	}
	env.Restart()
	if a := deliver(t, env, path, "d-1", "T-1"); a.Status != http.StatusOK || queued(a) {
		t.Errorf("a repeat after a restart: %d %s, want a repeat", a.Status, a.Body)
	}
}

func TestADeliveryWithNoIDOrAnIDThatIsTooLongIsRefused(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	for name, id := range map[string]string{"no id": "", "an id of 257 bytes": strings.Repeat("a", 257)} {
		if a := deliver(t, env, path, id, "T-1"); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	if a := deliver(t, env, path, strings.Repeat("a", 256), "T-1"); a.Status != http.StatusOK {
		t.Errorf("an id of 256 bytes: %d %s, want 200", a.Status, a.Body)
	}
}

func TestABodyOver1MiBGets413(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	sized := func(n int) []byte {
		raw := body(t, fakesource.Body{Entity: "T-1", Padding: "x"})
		raw = body(t, fakesource.Body{Entity: "T-1", Padding: strings.Repeat("x", n-len(raw)+1)})
		if len(raw) != n {
			t.Fatalf("a body of %d bytes, want %d", len(raw), n)
		}
		return raw
	}
	exact, over := sized(dispatcher.MaxWebhookBody), sized(dispatcher.MaxWebhookBody+1)
	if a := env.Webhook(path, factorytest.FakeDelivery(secret, "d-1", exact), exact); a.Status != http.StatusOK {
		t.Errorf("a body of 1 MiB: %d %s, want 200", a.Status, a.Body)
	}
	if a := env.Webhook(path, factorytest.FakeDelivery(secret, "d-2", over), over); a.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("a body of 1 MiB and 1 byte: %d %s, want 413", a.Status, a.Body)
	}
	// With no Content-Length (chunked), the limit is on the bytes read.
	chunked := struct{ io.Reader }{bytes.NewReader(over)}
	if a := env.WebhookRequest("POST", path, factorytest.FakeDelivery(secret, "d-3", over), chunked); a.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("a chunked body of 1 MiB and 1 byte: %d %s, want 413", a.Status, a.Body)
	}
	if a := env.Webhook(path, http.Header{}, over); a.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("an unsigned body over 1 MiB: %d, want 413", a.Status)
	}
}

func TestAConnectionOverItsRateLimitGets429UntilTheNextMinute(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	other := env.WebhookConnection("other", secret)
	raw := body(t, fakesource.Body{Entity: "T-1"})
	// Webhooks with no valid signature do not count.
	for i := range dispatcher.WebhookRateLimit + 10 {
		if a := env.Webhook(path, factorytest.FakeDelivery("wrong", fmt.Sprint("x-", i), raw), raw); a.Status != http.StatusUnauthorized {
			t.Fatalf("a webhook with a wrong signature: %d, want 401", a.Status)
		}
	}
	for i := range dispatcher.WebhookRateLimit {
		if a := deliver(t, env, path, fmt.Sprint("d-", i), "T-1"); a.Status != http.StatusOK {
			t.Fatalf("webhook %d in the minute: %d %s, want 200", i+1, a.Status, a.Body)
		}
	}
	env.Clock.Advance(20 * time.Second)
	a := deliver(t, env, path, "d-over", "T-1")
	if a.Status != http.StatusTooManyRequests || a.Header.Get("Retry-After") != "40" {
		t.Errorf("webhook %d in the minute: %d Retry-After %q, want 429 with 40", dispatcher.WebhookRateLimit+1,
			a.Status, a.Header.Get("Retry-After"))
	}
	if a := deliver(t, env, other, "d-0", "T-1"); a.Status != http.StatusOK {
		t.Errorf("another connection in the same minute: %d, want 200", a.Status)
	}
	env.Clock.Advance(40 * time.Second)
	// The refused delivery was not kept, so the sender's retry is new.
	if a := deliver(t, env, path, "d-over", "T-1"); a.Status != http.StatusOK || !queued(a) {
		t.Errorf("the retry in the next minute: %d %s, want 200 that queues", a.Status, a.Body)
	}
}
