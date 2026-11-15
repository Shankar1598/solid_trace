package dispatcher_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// runCount is the count of the Organization's Runs.
func runCount(t *testing.T, env *factorytest.Env) int {
	t.Helper()
	a := env.API(adminToken(t, env), "GET", "/api/v1/runs", nil)
	var runs dispatcherapi.Runs
	a.JSON(t, &runs)
	return len(runs.Runs)
}

// adminToken is the token of an Admin of the Organization.
func adminToken(t *testing.T, env *factorytest.Env) string {
	t.Helper()
	return env.PersonIn("Reader", factorytest.Organization, "admin").Token
}

func shopDelivery() dispatcherapi.GenericWebhook {
	return dispatcherapi.GenericWebhook{Project: "shop", Prompt: "Fix the cart"}
}

func TestADeliveryWithNoValidSignatureOrAnOldTimestampGets401AndStartsNoRun(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	sender, path := env.Sender("Alerts")
	raw := genericBody(t, shopDelivery())
	now := env.Clock.Now()
	ts := func(at time.Time) string { return strconv.FormatInt(at.Unix(), 10) }
	headers := func(timestamp, signature string) http.Header {
		h := http.Header{}
		if timestamp != "" {
			h.Set(dispatcherapi.HeaderTimestamp, timestamp)
		}
		if signature != "" {
			h.Set(dispatcherapi.HeaderSignature, signature)
		}
		h.Set(dispatcherapi.HeaderDelivery, "d-1")
		return h
	}
	bodyOnly := factorytest.GenericSignature(sender.Secret, "", raw)
	cases := map[string]struct {
		header http.Header
		body   []byte
	}{
		"no signature":                     {headers(ts(now), ""), raw},
		"another secret":                   {headers(ts(now), factorytest.GenericSignature("not-the-secret", ts(now), raw)), raw},
		"no sha256= prefix":                {headers(ts(now), strings.TrimPrefix(factorytest.GenericSignature(sender.Secret, ts(now), raw), "sha256=")), raw},
		"the body only signed":             {headers(ts(now), "sha256="+hashOf(t, sender.Secret, raw)), raw},
		"another body":                     {headers(ts(now), factorytest.GenericSignature(sender.Secret, ts(now), raw)), genericBody(t, dispatcherapi.GenericWebhook{Project: "shop", Prompt: "Delete main"})},
		"another timestamp":                {headers(ts(now.Add(time.Second)), factorytest.GenericSignature(sender.Secret, ts(now), raw)), raw},
		"no timestamp":                     {headers("", bodyOnly), raw},
		"a timestamp that is not a number": {headers("soon", factorytest.GenericSignature(sender.Secret, "soon", raw)), raw},
		"5 minutes and 1 second old": {headers(ts(now.Add(-5*time.Minute-time.Second)),
			factorytest.GenericSignature(sender.Secret, ts(now.Add(-5*time.Minute-time.Second)), raw)), raw},
		"5 minutes and 1 second ahead": {headers(ts(now.Add(5*time.Minute+time.Second)),
			factorytest.GenericSignature(sender.Secret, ts(now.Add(5*time.Minute+time.Second)), raw)), raw},
	}
	for name, c := range cases {
		if a := env.Webhook(path, c.header, c.body); a.Status != http.StatusUnauthorized {
			t.Errorf("%s: %d %s, want 401", name, a.Status, a.Body)
		}
	}
	if n := runCount(t, env); n != 0 {
		t.Fatalf("the refused deliveries started %d Runs", n)
	}

	for i, at := range []time.Time{now.Add(-5 * time.Minute), now.Add(5 * time.Minute)} {
		a := env.Webhook(path, factorytest.GenericDelivery(sender.Secret, "edge-"+strconv.Itoa(i), at, raw), raw)
		if a.Status != http.StatusAccepted {
			t.Errorf("a timestamp exactly 5 minutes from the clock (%v): %d %s, want 202", at, a.Status, a.Body)
		}
	}
}

// hashOf is the hex HMAC-SHA256 of the body alone, with no timestamp.
func hashOf(t *testing.T, secret string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestADeliveryForAProjectWithNoFactorySettingsGets422AndAWarningThatNamesTheProject(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("docs")
	sender, path := env.Sender("Alerts")

	a := sendGeneric(t, env, path, sender.Secret, "d-1", dispatcherapi.GenericWebhook{Project: "docs", Prompt: "Fix it"})
	if a.Status != http.StatusUnprocessableEntity || !strings.Contains(string(a.Body), "docs") {
		t.Fatalf("a Project with no Factory settings: %d %s, want 422 that names docs", a.Status, a.Body)
	}
	warning := env.WaitForWarning("has no Factory settings")
	if warning["factory.project"] != "docs" || warning["actor.kind"] != "sender" || warning["actor.name"] != "Alerts" {
		t.Errorf("the warning: %v", warning)
	}

	// The same delivery id is not kept: when an Admin saves the settings,
	// the sender's retry starts the Run.
	if a := admin(t, env).API("PUT", "/api/v1/projects/docs/settings",
		dispatcherapi.SaveFactorySettings{Repository: "bitbucket.org/example/docs"}); a.Status != http.StatusOK {
		t.Fatalf("save the settings of docs: %d %s", a.Status, a.Body)
	}
	accepted(t, sendGeneric(t, env, path, sender.Secret, "d-1", dispatcherapi.GenericWebhook{Project: "docs", Prompt: "Fix it"}))
}

func TestADeliveryForAProjectOfAnotherOrganizationOrNotInTheSendersListIsRefused(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	env.Project("cart")
	env.SolidTrace.SetProject("other", recorder.Project{ID: 900, Slug: "billing", Name: "billing"})
	all, allPath := env.Sender("All Projects")
	shopOnly, shopPath := env.Sender("Shop only", "shop")
	if len(shopOnly.Projects) != 1 || shopOnly.Projects[0] != "shop" || len(all.Projects) != 0 {
		t.Fatalf("the senders' Projects: %v and %v", shopOnly.Projects, all.Projects)
	}

	a := sendGeneric(t, env, allPath, all.Secret, "d-1", dispatcherapi.GenericWebhook{Project: "billing", Prompt: "Fix it"})
	if a.Status != http.StatusUnprocessableEntity {
		t.Errorf("a Project of another Organization: %d %s, want 422", a.Status, a.Body)
	}
	a = sendGeneric(t, env, shopPath, shopOnly.Secret, "d-2", dispatcherapi.GenericWebhook{Project: "cart", Prompt: "Fix it"})
	if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), "cart") {
		t.Errorf("a Project not in the sender's list: %d %s, want 403", a.Status, a.Body)
	}
	if n := runCount(t, env); n != 0 {
		t.Fatalf("the refused deliveries started %d Runs", n)
	}
	accepted(t, sendGeneric(t, env, shopPath, shopOnly.Secret, "d-3", shopDelivery()))
	accepted(t, sendGeneric(t, env, allPath, all.Secret, "d-4", dispatcherapi.GenericWebhook{Project: "cart", Prompt: "Fix it"}))

	// The list keeps the Project's id: a new slug of shop still matches,
	// and a new Project with the old slug does not.
	env.SolidTrace.SetProject(factorytest.Organization.Slug, recorder.Project{ID: 101, Slug: "store", Name: "store"})
	accepted(t, sendGeneric(t, env, shopPath, shopOnly.Secret, "d-5", dispatcherapi.GenericWebhook{Project: "store", Prompt: "Fix it"}))
	env.Project("shop")
	a = sendGeneric(t, env, shopPath, shopOnly.Secret, "d-6", dispatcherapi.GenericWebhook{Project: "shop", Prompt: "Fix the cart again"})
	if a.Status != http.StatusForbidden {
		t.Errorf("a new Project with the old slug: %d %s, want 403", a.Status, a.Body)
	}
}

func TestASenderOfAnOrganizationThatTheDispatcherNoLongerServesGets403AndStartsNoRun(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	sender, path := env.Sender("Alerts")

	// The operator gives the Dispatcher a token of another Organization,
	// which also has a Project shop with Factory settings.
	other := recorder.Organization{ID: 8, Slug: "other", Name: "Other"}
	env.SolidTrace.SetProject(other.Slug, recorder.Project{ID: 900, Slug: "shop", Name: "shop"})
	env.SolidTrace.Token(factorytest.DispatcherToken, recorder.Answer{
		Sub: "1", Username: "dana@example.com", Scope: "run_events:write issues:comment tokens:create mcp:read",
		Organizations: []recorder.Organization{{ID: other.ID, Slug: other.Slug, Name: other.Name, Role: "admin"}},
	})
	env.Restart()
	otherAdmin := env.PersonIn("Olga", other, "admin")
	if a := otherAdmin.API("PUT", "/api/v1/projects/shop/settings",
		dispatcherapi.SaveFactorySettings{Repository: "bitbucket.org/other/shop"}); a.Status != http.StatusOK {
		t.Fatalf("save the settings of the other Organization's shop: %d %s", a.Status, a.Body)
	}

	a := sendGeneric(t, env, path, sender.Secret, "d-1", shopDelivery())
	if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), "no longer serves") {
		t.Fatalf("a sender of the Organization that the Dispatcher no longer serves: %d %s, want 403", a.Status, a.Body)
	}
	var runs dispatcherapi.Runs
	otherAdmin.API("GET", "/api/v1/runs", nil).JSON(t, &runs)
	if len(runs.Runs) != 0 {
		t.Fatalf("the refused delivery started %d Runs in the other Organization", len(runs.Runs))
	}

	// The operator gives the first token back: the sender starts Runs again,
	// and the refused delivery started none there.
	env.SolidTrace.Token(factorytest.DispatcherToken, recorder.Answer{
		Sub: "1", Username: "dana@example.com", Scope: "run_events:write issues:comment tokens:create mcp:read",
		Organizations: []recorder.Organization{{ID: factorytest.Organization.ID, Slug: factorytest.Organization.Slug,
			Name: factorytest.Organization.Name, Role: "admin"}},
	})
	env.Restart()
	if n := runCount(t, env); n != 0 {
		t.Fatalf("the refused delivery started %d Runs in the sender's Organization", n)
	}
	accepted(t, sendGeneric(t, env, path, sender.Secret, "d-2", shopDelivery()))
}

func TestASenderCannotListAProjectThatSolidTraceDoesNotHave(t *testing.T) {
	env := factorytest.New(t)
	dana := admin(t, env)
	a := dana.API("POST", "/api/v1/senders", dispatcherapi.SaveSender{Name: "Alerts", Projects: []string{"nope"}})
	if a.Status != http.StatusUnprocessableEntity {
		t.Errorf("a sender with an unknown Project: %d %s, want 422", a.Status, a.Body)
	}
}

func TestWhileAnActiveRunHasTheDedupeKeyADeliveryGivesThatRunAndStartsNone(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	sender, path := env.Sender("Alerts")
	other, otherPath := env.Sender("Other alerts")
	keyed := func(key string) dispatcherapi.GenericWebhook {
		b := shopDelivery()
		b.DedupeKey = key
		return b
	}

	first := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-1", keyed("issue-42")))
	again := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-2", keyed("issue-42")))
	if again.Started || again.RunID != first.RunID || again.URL != first.URL {
		t.Errorf("the same dedupe_key: %+v, want the Run %s and no start", again, first.RunID)
	}
	if next := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-3", keyed("issue-43"))); !next.Started || next.RunID == first.RunID {
		t.Errorf("another dedupe_key: %+v, want a new Run", next)
	}
	if theirs := accepted(t, sendGeneric(t, env, otherPath, other.Secret, "d-4", keyed("issue-42"))); !theirs.Started {
		t.Errorf("the same dedupe_key from another sender: %+v, want a new Run", theirs)
	}
	if n := runCount(t, env); n != 3 {
		t.Errorf("%d Runs, want 3", n)
	}
}

func TestARepeatedDeliveryIDOrSignatureGivesTheFirstRunFor7Days(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	sender, path := env.Sender("Alerts")
	raw := genericBody(t, shopDelivery())

	first := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-1", shopDelivery()))
	repeat := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-1", dispatcherapi.GenericWebhook{Project: "shop", Prompt: "Another task"}))
	if repeat.Started || repeat.RunID != first.RunID {
		t.Errorf("a repeated delivery id: %+v, want the Run %s and no start", repeat, first.RunID)
	}
	// The delivery id is not signed: the same signed request under a new
	// delivery id is a replay.
	replay := factorytest.GenericDelivery(sender.Secret, "d-1", env.Clock.Now(), raw)
	replay.Set(dispatcherapi.HeaderDelivery, "d-other")
	if a := accepted(t, env.Webhook(path, replay, raw)); a.Started || a.RunID != first.RunID {
		t.Errorf("a replay under a new delivery id: %+v, want the Run %s and no start", a, first.RunID)
	}

	env.Clock.Advance(7 * 24 * time.Hour)
	if a := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-1", shopDelivery())); a.Started {
		t.Errorf("the delivery id exactly 7 days later: %+v, want the first Run", a)
	}
	env.Clock.Advance(time.Second)
	if a := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-1", shopDelivery())); !a.Started || a.RunID == first.RunID {
		t.Errorf("the delivery id 7 days and 1 second later: %+v, want a new Run", a)
	}
}

func TestASixthRunQueuedOrLiveGets429AndADedupeHitDoesNot(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	sender, path := env.Sender("Alerts")
	other, otherPath := env.Sender("Other alerts")
	var keyed dispatcherapi.GenericWebhookAnswer
	for i := range 5 {
		b := shopDelivery()
		b.DedupeKey = "key-" + strconv.Itoa(i)
		keyed = accepted(t, sendGeneric(t, env, path, sender.Secret, "d-"+strconv.Itoa(i), b))
	}
	a := sendGeneric(t, env, path, sender.Secret, "d-6", shopDelivery())
	if a.Status != http.StatusTooManyRequests || a.Header.Get("Retry-After") != "60" {
		t.Fatalf("a 6th Run queued: %d %s Retry-After %q, want 429 with 60", a.Status, a.Body, a.Header.Get("Retry-After"))
	}
	b := shopDelivery()
	b.DedupeKey = "key-4"
	if got := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-7", b)); got.Started || got.RunID != keyed.RunID {
		t.Errorf("a dedupe hit at the limit: %+v, want the Run %s", got, keyed.RunID)
	}
	accepted(t, sendGeneric(t, env, otherPath, other.Secret, "d-8", shopDelivery()))
	// The refused delivery was not kept: its retry is refused again by
	// the limit, not given a Run.
	if a := sendGeneric(t, env, path, sender.Secret, "d-6", shopDelivery()); a.Status != http.StatusTooManyRequests {
		t.Errorf("the retry of the refused delivery: %d %s, want 429", a.Status, a.Body)
	}
}

func TestTheEleventhStartInAMinuteGets429WithRetryAfter(t *testing.T) {
	env := factorytest.New(t, func(e *factorytest.Env) { e.Config.SenderActiveRuns = 50 })
	env.Project("shop")
	sender, path := env.Sender("Alerts")
	for i := range 10 {
		accepted(t, sendGeneric(t, env, path, sender.Secret, "d-"+strconv.Itoa(i), shopDelivery()))
		env.Clock.Advance(time.Second)
	}
	// The first start was 10 s ago: it leaves the minute in 50 s.
	a := sendGeneric(t, env, path, sender.Secret, "d-10", shopDelivery())
	if a.Status != http.StatusTooManyRequests || a.Header.Get("Retry-After") != "50" {
		t.Fatalf("the 11th start: %d %s Retry-After %q, want 429 with 50", a.Status, a.Body, a.Header.Get("Retry-After"))
	}
	env.Clock.Advance(49 * time.Second)
	if a := sendGeneric(t, env, path, sender.Secret, "d-10", shopDelivery()); a.Status != http.StatusTooManyRequests ||
		a.Header.Get("Retry-After") != "1" {
		t.Errorf("1 s before the first start leaves the minute: %d Retry-After %q, want 429 with 1", a.Status,
			a.Header.Get("Retry-After"))
	}
	env.Clock.Advance(time.Second)
	accepted(t, sendGeneric(t, env, path, sender.Secret, "d-10", shopDelivery()))
}

func TestTheOperatorChangesTheSenderLimits(t *testing.T) {
	env := factorytest.New(t, func(e *factorytest.Env) { e.Config.SenderActiveRuns = 1 })
	env.Project("shop")
	sender, path := env.Sender("Alerts")
	accepted(t, sendGeneric(t, env, path, sender.Secret, "d-1", shopDelivery()))
	if a := sendGeneric(t, env, path, sender.Secret, "d-2", dispatcherapi.GenericWebhook{Project: "shop", Prompt: "Fix the cart again"}); a.Status != http.StatusTooManyRequests {
		t.Errorf("a second Run with a limit of 1: %d %s, want 429", a.Status, a.Body)
	}
}

func TestADeliveryWithABodyThatIsNotValidGets400(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	sender, path := env.Sender("Alerts")
	attributes := map[string]string{}
	for i := range 21 {
		attributes["a"+strconv.Itoa(i)] = "v"
	}
	for name, b := range map[string]dispatcherapi.GenericWebhook{
		"no project":           {Prompt: "Fix it"},
		"no prompt":            {Project: "shop", Prompt: "  "},
		"a prompt over 64 KiB": {Project: "shop", Prompt: strings.Repeat("a", 64<<10+1)},
		"a branch with ..":     {Project: "shop", Prompt: "Fix it", Branch: "main/../x"},
		"a javascript: link":   {Project: "shop", Prompt: "Fix it", Link: "javascript:alert(1)"},
		// These links have a host, so only the scheme check refuses them.
		"a javascript:// link":   {Project: "shop", Prompt: "Fix it", Link: "javascript://example.com/%0Aalert(1)"},
		"an ftp:// link":         {Project: "shop", Prompt: "Fix it", Link: "ftp://example.com/x"},
		"a file:// link":         {Project: "shop", Prompt: "Fix it", Link: "file://host/x"},
		"a data:// link":         {Project: "shop", Prompt: "Fix it", Link: "data://x/y"},
		"a link over 2048 bytes": {Project: "shop", Prompt: "Fix it", Link: linkOf(2049)},
		"21 attributes":          {Project: "shop", Prompt: "Fix it", Attributes: attributes},
		"an attribute name of /": {Project: "shop", Prompt: "Fix it", Attributes: map[string]string{"a/b": "v"}},
		"a long dedupe_key":      {Project: "shop", Prompt: "Fix it", DedupeKey: strings.Repeat("k", 257)},
	} {
		if a := sendGeneric(t, env, path, sender.Secret, "d-"+name, b); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	for name, raw := range map[string][]byte{
		"an unknown field": []byte(`{"project":"shop","prompt":"Fix it","promt":"x"}`),
		"not JSON":         []byte(`project=shop`),
	} {
		h := factorytest.GenericDelivery(sender.Secret, "d-"+name, env.Clock.Now(), raw)
		if a := env.Webhook(path, h, raw); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	raw := genericBody(t, shopDelivery())
	h := factorytest.GenericDelivery(sender.Secret, "", env.Clock.Now(), raw)
	if a := env.Webhook(path, h, raw); a.Status != http.StatusBadRequest {
		t.Errorf("no delivery id: %d %s, want 400", a.Status, a.Body)
	}
	if n := runCount(t, env); n != 0 {
		t.Fatalf("the refused deliveries started %d Runs", n)
	}

	at := accepted(t, sendGeneric(t, env, path, sender.Secret, "d-64k", dispatcherapi.GenericWebhook{Project: "shop",
		Prompt: strings.Repeat("a", 64<<10)}))
	if !at.Started {
		t.Errorf("a prompt of exactly 64 KiB: %+v, want a Run", at)
	}
	at = accepted(t, sendGeneric(t, env, path, sender.Secret, "d-2048", dispatcherapi.GenericWebhook{Project: "shop",
		Prompt: "Fix the link", Link: linkOf(2048)}))
	if !at.Started {
		t.Errorf("a link of exactly 2048 bytes: %+v, want a Run", at)
	}
}

// linkOf gives an https:// link of n bytes.
func linkOf(n int) string {
	const prefix = "https://example.com/"
	return prefix + strings.Repeat("a", n-len(prefix))
}

func TestAGenericWebhookPathOfNoSenderOrAnotherMethodGets404AndABigBodyGets413(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	sender, path := env.Sender("Alerts")
	raw := genericBody(t, shopDelivery())
	h := factorytest.GenericDelivery(sender.Secret, "d-1", env.Clock.Now(), raw)
	if a := env.Webhook("/webhooks/generic/nosuchsender", h, raw); a.Status != http.StatusNotFound {
		t.Errorf("no such sender: %d, want 404", a.Status)
	}
	if a := env.WebhookRequest("GET", path, h, nil); a.Status != http.StatusNotFound {
		t.Errorf("GET: %d, want 404", a.Status)
	}
	big := bytes.Repeat([]byte("a"), 1<<20+1)
	if a := env.Webhook(path, factorytest.GenericDelivery(sender.Secret, "d-2", env.Clock.Now(), big), big); a.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("a body over 1 MiB: %d, want 413", a.Status)
	}
	if a := env.Webhook(path, h, raw); a.Status != http.StatusAccepted {
		t.Fatalf("a valid delivery: %d %s", a.Status, a.Body)
	}
	// The private listener serves the same handler.
	if a := env.API("", "POST", path, raw); a.Status != http.StatusUnauthorized {
		t.Errorf("the private listener with no signature: %d, want 401", a.Status)
	}
}
