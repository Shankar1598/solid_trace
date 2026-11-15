package wrapper_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakeforge"
	"github.com/solidtrace/factory/internal/fakesource"
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

// barrier sends a webhook of the fake source and waits for its reconcile
// step. The reconcile steps run one at a time in order, so each step that
// an earlier webhook queued ran. The fake source calls no forge.
func barrier(t *testing.T, env *factorytest.Env) {
	t.Helper()
	const secret = "the-barrier-secret"
	path := env.WebhookConnection("barrier", secret)
	entity := "b" + strconv.FormatInt(time.Now().UnixNano(), 10)
	raw, _ := json.Marshal(fakesource.Body{Entity: entity})
	if a := env.Webhook(path, factorytest.FakeDelivery(secret, entity, raw), raw); a.Status != 200 || !queued(t, a) {
		t.Fatalf("the barrier webhook: %d %s", a.Status, a.Body)
	}
	if _, ok := env.Source.WaitFor(func(r fakesource.Reconcile) bool {
		return r.Connection == "barrier" && r.Entity == entity
	}); !ok {
		t.Fatalf("no reconcile step of the barrier after 10 s\nDispatcher log:\n%s", env.Logs())
	}
}

func TestARejectedWebhookGivesThePROutcomeDeclined(t *testing.T) {
	env, ana := newEnv(t)
	path := env.ForgeWebhooks(forgeSecret)
	id, pr := runWithPR(t, env, ana)
	env.CheckClosedRuns()

	env.Forge.SetState("example/shop", pr.ID, "DECLINED")
	a := env.ForgeWebhook(path, forgeSecret, "{req-1}", fakeforge.EventPRRejected, env.Forge.PRWebhook("example/shop", pr.ID))
	if a.Status != 200 || !queued(t, a) {
		t.Fatalf("a signed pullrequest:rejected: %d %s, want 200 queued", a.Status, a.Body)
	}
	closedEvent(t, env, id, dispatcherapi.StateDone, "declined", "pr_declined")
	if run := getRun(t, ana, id); run.PROutcome != "declined" || run.ClosedAt == nil {
		t.Errorf("the Run has the PR outcome %q, closed at %v; want declined", run.PROutcome, run.ClosedAt)
	}
}

func TestTheOutcomeIsTheStateThatTheForgeGivesNotTheWebhooksBody(t *testing.T) {
	env, ana := newEnv(t)
	path := env.ForgeWebhooks(forgeSecret)
	id, pr := runWithPR(t, env, ana)
	env.CheckClosedRuns()

	// A signed webhook says that the PR is merged, but the forge has it
	// open: the Run stays open.
	env.Forge.SetState("example/shop", pr.ID, "MERGED")
	body := env.Forge.PRWebhook("example/shop", pr.ID)
	env.Forge.SetState("example/shop", pr.ID, "OPEN")
	if a := env.ForgeWebhook(path, forgeSecret, "{req-1}", fakeforge.EventPRFulfilled, body); a.Status != 200 {
		t.Fatalf("a signed pullrequest:fulfilled: %d %s", a.Status, a.Body)
	}
	barrier(t, env)
	notClosed(t, env, ana, id, "after a webhook of a PR that the forge has open")

	// A webhook of the same PR number in another repository, and one of
	// an absent PR, read nothing from the forge.
	before := env.Forge.APICalls()
	for uuid, raw := range map[string]string{
		"{other-repo}": `{"pullrequest":{"id":` + strconv.Itoa(pr.ID) + `},"repository":{"full_name":"example/other"}}`,
		"{absent-pr}":  `{"pullrequest":{"id":9999},"repository":{"full_name":"example/shop"}}`,
	} {
		if a := env.ForgeWebhook(path, forgeSecret, uuid, fakeforge.EventPRFulfilled, []byte(raw)); a.Status != 200 {
			t.Errorf("%s: %d %s, want 200", uuid, a.Status, a.Body)
		}
	}
	barrier(t, env)
	if got := env.Forge.APICalls() - before; got != 0 {
		t.Errorf("the forge got %d API calls, want none: the webhooks match no Run", got)
	}
	notClosed(t, env, ana, id, "after webhooks of other PRs")
}

func TestAWebhookWithNoValidSignatureGets401AndDoesNothing(t *testing.T) {
	env, ana := newEnv(t)
	var connections dispatcherapi.Connections
	ana.API("GET", "/api/v1/connections", nil).JSON(t, &connections)
	var path string
	for _, c := range connections.Connections {
		if c.Name == factorytest.ForgeConnection {
			path = strings.TrimPrefix(c.WebhookURL, factorytest.PublicWebhookURL)
		}
	}
	if !strings.HasPrefix(path, "/webhooks/bitbucket/") {
		t.Fatalf("the forge connection has no Bitbucket webhook URL: %+v", connections.Connections)
	}
	id, pr := runWithPR(t, env, ana)
	env.CheckClosedRuns()
	env.Forge.SetState("example/shop", pr.ID, "MERGED")
	body := env.Forge.PRWebhook("example/shop", pr.ID)

	// With no webhook secret, the connection verifies nothing.
	if a := env.ForgeWebhook(path, forgeSecret, "{no-secret}", fakeforge.EventPRFulfilled, body); a.Status != 401 {
		t.Errorf("a webhook to a connection with no secret: %d %s, want 401", a.Status, a.Body)
	}
	if env.ForgeWebhooks(forgeSecret) != path {
		t.Fatalf("the webhook secret changed the webhook URL")
	}

	signed := func(edit func(string) string) http.Header {
		h := fakeforge.Delivery(forgeSecret, "{req}", fakeforge.EventPRFulfilled, body)
		h.Set("X-Hub-Signature", edit(h.Get("X-Hub-Signature")))
		return h
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-2] = ' '
	for name, c := range map[string]struct {
		header http.Header
		body   []byte
	}{
		"another secret":      {fakeforge.Delivery("another-secret", "{req}", fakeforge.EventPRFulfilled, body), body},
		"a changed body":      {fakeforge.Delivery(forgeSecret, "{req}", fakeforge.EventPRFulfilled, body), tampered},
		"no signature":        {signed(func(string) string { return "" }), body},
		"an empty signature":  {signed(func(string) string { return "sha256=" }), body},
		"no sha256= prefix":   {signed(func(s string) string { return strings.TrimPrefix(s, "sha256=") }), body},
		"a sha1 signature":    {signed(func(s string) string { return "sha1=" + strings.TrimPrefix(s, "sha256=") }), body},
		"a signature not hex": {signed(func(s string) string { return "sha256=zz" + s[9:] }), body},
		"a cut signature":     {signed(func(s string) string { return s[:len(s)-2] }), body},
		"a longer signature":  {signed(func(s string) string { return s + "00" }), body},
	} {
		if a := env.Webhook(path, c.header, c.body); a.Status != 401 {
			t.Errorf("%s: %d %s, want 401", name, a.Status, a.Body)
		}
	}
	notClosed(t, env, ana, id, "after the refused webhooks")
	// A refused webhook keeps no delivery id: the same request UUID,
	// signed, is not a repeat.
	if a := env.ForgeWebhook(path, forgeSecret, "{req}", fakeforge.EventPRFulfilled, body); a.Status != 200 || !queued(t, a) {
		t.Errorf("the signed webhook after the refusals: %d %s, want 200 queued", a.Status, a.Body)
	}
	closedEvent(t, env, id, dispatcherapi.StateDone, "merged", "pr_merged")
}

func TestASignedWebhookWithNoRequestUUIDOrNoPRGets400(t *testing.T) {
	env, ana := newEnv(t)
	path := env.ForgeWebhooks(forgeSecret)
	id, pr := runWithPR(t, env, ana)
	env.CheckClosedRuns()
	env.Forge.SetState("example/shop", pr.ID, "MERGED")
	good := env.Forge.PRWebhook("example/shop", pr.ID)

	if a := env.Webhook(path, fakeforge.Delivery(forgeSecret, "", fakeforge.EventPRFulfilled, good), good); a.Status != 400 {
		t.Errorf("a signed webhook with no X-Request-UUID: %d %s, want 400", a.Status, a.Body)
	}
	for name, raw := range map[string]string{
		"not JSON":                  `{"pullrequest":`,
		"no pullrequest":            `{"repository":{"full_name":"example/shop"}}`,
		"a PR id of 0":              `{"pullrequest":{"id":0},"repository":{"full_name":"example/shop"}}`,
		"a PR id as a string":       `{"pullrequest":{"id":"1"},"repository":{"full_name":"example/shop"}}`,
		"no repository":             `{"pullrequest":{"id":1}}`,
		"a repository with no slug": `{"pullrequest":{"id":1},"repository":{"full_name":"example"}}`,
		"a repository of 3 parts":   `{"pullrequest":{"id":1},"repository":{"full_name":"example/shop/x"}}`,
	} {
		if a := env.ForgeWebhook(path, forgeSecret, "{"+name+"}", fakeforge.EventPRFulfilled, []byte(raw)); a.Status != 400 {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "after the refused webhooks")
}

func TestAPushToTheWIPRefAndOtherEventsDoNothing(t *testing.T) {
	env, ana := newEnv(t)
	path := env.ForgeWebhooks(forgeSecret)
	id, pr := runWithPR(t, env, ana)
	wip := addWIPRef(t, env, id)
	commit, _ := env.Forge.Ref("example/shop", wip)
	env.CheckClosedRuns()

	before := env.Forge.APICalls()
	for name, c := range map[string]struct {
		event string
		body  []byte
	}{
		"a push to the WIP ref":        {fakeforge.EventPush, fakeforge.PushWebhook("example/shop", wip, commit)},
		"a push to a WIP branch":       {fakeforge.EventPush, fakeforge.PushWebhook("example/shop", "refs/heads/agent-wip/"+id, commit)},
		"a push to the Run's branch":   {fakeforge.EventPush, fakeforge.PushWebhook("example/shop", "refs/heads/agent/"+id, commit)},
		"a PR that opens":              {fakeforge.EventPRCreated, env.Forge.PRWebhook("example/shop", pr.ID)},
		"an event with no event key":   {"", env.Forge.PRWebhook("example/shop", pr.ID)},
		"a push whose body is no JSON": {fakeforge.EventPush, []byte("not json")},
	} {
		a := env.ForgeWebhook(path, forgeSecret, "{"+name+"}", c.event, c.body)
		if a.Status != 200 || queued(t, a) {
			t.Errorf("%s: %d %s, want 200 with nothing queued", name, a.Status, a.Body)
		}
	}
	barrier(t, env)
	if got := env.Forge.APICalls() - before; got != 0 {
		t.Errorf("the forge got %d API calls, want none", got)
	}
	notClosed(t, env, ana, id, "after the pushes")
	if got, ok := env.Forge.Ref("example/shop", wip); !ok || got != commit {
		t.Errorf("the WIP ref is %q (%v) after its push webhook, want %s", got, ok, commit)
	}
	var connections dispatcherapi.Connections
	ana.API("GET", "/api/v1/connections", nil).JSON(t, &connections)
	for _, c := range connections.Connections {
		if c.Name == factorytest.ForgeConnection && (c.LastWebhookAt == nil || !c.LastWebhookAt.Equal(env.Clock.Now().UTC())) {
			t.Errorf("the forge connection's last webhook is %v, want %s", c.LastWebhookAt, env.Clock.Now().UTC())
		}
	}
}

func TestARepeatedRequestUUIDDoesNothingAndTheBackstopGivesTheOutcome(t *testing.T) {
	env, ana := newEnv(t)
	path := env.ForgeWebhooks(forgeSecret)
	id, pr := runWithPR(t, env, ana)
	env.CheckClosedRuns()

	// The first delivery reads the PR while it is open.
	body := env.Forge.PRWebhook("example/shop", pr.ID)
	if a := env.ForgeWebhook(path, forgeSecret, "{req-1}", fakeforge.EventPRFulfilled, body); a.Status != 200 || !queued(t, a) {
		t.Fatalf("the first delivery: %d %s, want 200 queued", a.Status, a.Body)
	}
	barrier(t, env)

	// Bitbucket sends it again with the same request UUID after the merge:
	// a repeat, which queues nothing.
	env.Forge.SetState("example/shop", pr.ID, "MERGED")
	before := env.Forge.APICalls()
	if a := env.ForgeWebhook(path, forgeSecret, "{req-1}", fakeforge.EventPRFulfilled, body); a.Status != 200 || queued(t, a) {
		t.Fatalf("the repeat: %d %s, want 200 with nothing queued", a.Status, a.Body)
	}
	env.CheckClosedRuns()
	if got := env.Forge.APICalls(); got != before {
		t.Errorf("the repeat made %d forge calls", got-before)
	}
	notClosed(t, env, ana, id, "after the repeat")

	// The 5-minute check is the backstop.
	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "merged", "pr_merged")
}

func TestWithNoWebhookTheFiveMinuteCheckGivesTheOutcomeOfAConnectionWithASecret(t *testing.T) {
	env, ana := newEnv(t)
	env.ForgeWebhooks(forgeSecret)
	id, pr := runWithPR(t, env, ana)
	env.CheckClosedRuns()
	env.Forge.SetState("example/shop", pr.ID, "DECLINED")

	env.Clock.Advance(prCheckEvery - time.Second)
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "1 s before the check")
	env.Clock.Advance(time.Second)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "declined", "pr_declined")
}
