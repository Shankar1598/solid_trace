package dispatcher_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakesource"
)

func TestAtStartTheDispatcherReconcilesEachConnection(t *testing.T) {
	env := factorytest.New(t)
	env.WebhookConnection("tickets", secret)
	env.WebhookConnection("other", "")
	reconciledOf(t, env, "tickets", "")
	reconciledOf(t, env, "other", "")

	env.Source.Change("tickets", "T-1") // a change while the Dispatcher is down
	env.Restart()
	r, ok := env.Source.WaitFor(func(r fakesource.Reconcile) bool { return r.Connection == "tickets" && len(r.Acted) > 0 })
	if !ok || r.Entity != "" {
		t.Fatalf("no reconcile step of tickets acted on the change at start: %+v", env.Source.Reconciles())
	}
	if n := stepsOf(env, "other", ""); n != 2 {
		t.Errorf("other has %d reconcile steps of the whole connection, want 2 (one each start)", n)
	}
	settle()
	// The reconcile at start is not a backstop: a change from while the
	// Dispatcher was down gives no warning.
	for _, ev := range env.SolidTrace.RunEvents() {
		if strings.Contains(ev["message"].(string), "backstop") {
			t.Errorf("the reconcile at start warned: %v", ev)
		}
	}
}

func TestABackstopThatFindsAChangeWithNoWebhookWritesAWarningWithTheTimeOfTheLastWebhook(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	reconciledOf(t, env, "tickets", "")

	// A change that a webhook announces gives no warning at the backstop.
	env.Source.Change("tickets", "T-1")
	deliver(t, env, path, "d-1", "T-1")
	reconciledOf(t, env, "tickets", "T-1")
	webhookAt := env.Clock.Now().UTC()
	env.Clock.Advance(fakesource.Backstop)
	waitForSteps(t, env, "tickets", "", 2)
	settle()
	if warned(env) {
		t.Fatalf("a backstop that found nothing new warned")
	}

	// A change that no webhook announces: the next backstop acts on it and
	// warns with the time of the last webhook.
	env.Source.Change("tickets", "T-2")
	env.Clock.Advance(fakesource.Backstop - time.Second)
	settle()
	if n := stepsOf(env, "tickets", ""); n != 2 {
		t.Fatalf("a backstop ran before its interval: %d steps", n)
	}
	env.Clock.Advance(time.Second)
	warning := env.WaitForWarning("found a change that no webhook announced")
	want := webhookAt.Format(time.RFC3339)
	if warning["factory.last_webhook_at"] != want || !strings.Contains(warning["message"].(string), want) ||
		warning["factory.connection"] != "tickets" || warning["factory.entities"] != "T-2" || warning["level"] != "warn" ||
		warning["organization"] != factorytest.Organization.Slug {
		t.Errorf("the warning is %v, want one for tickets of %s and T-2 with the last webhook %s", warning,
			factorytest.Organization.Slug, want)
	}
}

func TestABackstopOfAConnectionWithNoWebhookYetWarnsWithNever(t *testing.T) {
	env := factorytest.New(t)
	env.WebhookConnection("tickets", secret)
	reconciledOf(t, env, "tickets", "")
	env.Source.Change("tickets", "T-9")
	env.Clock.Advance(fakesource.Backstop)
	if w := env.WaitForWarning("found a change that no webhook announced"); w["factory.last_webhook_at"] != "never" {
		t.Errorf("the warning is %v, want the last webhook never", w)
	}
}

func TestAWebhookThatArrivesWhileTheBackstopRunsSuppressesItsWarning(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	reconciledOf(t, env, "tickets", "")
	release := env.Source.Hold()
	defer release()
	env.Source.Change("tickets", "T-1")
	env.Clock.Advance(fakesource.Backstop)
	settle() // the backstop step is queued and waits
	deliver(t, env, path, "d-1", "T-1")
	release()
	waitForSteps(t, env, "tickets", "", 2)
	settle()
	if warned(env) {
		t.Errorf("the backstop warned of a change that a webhook announced while it ran")
	}
}

// A connection with no webhook secret verifies no webhook (a poll-only
// connection): its backstop finds each change, and that is no sign that
// its webhooks stopped.
func TestABackstopOfAConnectionWithNoWebhookSecretDoesNotWarn(t *testing.T) {
	env := factorytest.New(t)
	env.WebhookConnection("polled", "")
	reconciledOf(t, env, "polled", "")
	env.Source.Change("polled", "T-1")
	env.Clock.Advance(fakesource.Backstop)
	if _, ok := env.Source.WaitFor(func(r fakesource.Reconcile) bool { return r.Connection == "polled" && len(r.Acted) > 0 }); !ok {
		t.Fatalf("the backstop did not act on the change: %+v", env.Source.Reconciles())
	}
	settle()
	if warned(env) {
		t.Errorf("the backstop of a connection with no webhook secret warned")
	}
}

// A webhook announced the change, but its own step failed: the next
// backstop acts on the change and does not warn, because the webhooks
// work. A change after that with no webhook still warns.
func TestABackstopAfterAFailedWebhookStepDoesNotWarnOfTheAnnouncedChange(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	reconciledOf(t, env, "tickets", "")
	env.Clock.Advance(time.Minute)
	env.Source.Change("tickets", "T-1")
	env.Source.FailNext(1)
	deliver(t, env, path, "d-1", "T-1")
	if _, ok := env.Source.WaitFor(func(r fakesource.Reconcile) bool { return r.Entity == "T-1" && r.Failed }); !ok {
		t.Fatalf("the webhook's step did not fail: %+v", env.Source.Reconciles())
	}
	env.Clock.Advance(fakesource.Backstop - time.Minute)
	if _, ok := env.Source.WaitFor(func(r fakesource.Reconcile) bool { return r.Entity == "" && len(r.Acted) > 0 }); !ok {
		t.Fatalf("the backstop did not act on T-1: %+v", env.Source.Reconciles())
	}
	settle()
	if warned(env) {
		t.Fatalf("the backstop warned of a change that a webhook announced before its step failed")
	}

	env.Source.Change("tickets", "T-2")
	env.Clock.Advance(fakesource.Backstop)
	if w := env.WaitForWarning("found a change that no webhook announced"); w["factory.entities"] != "T-2" {
		t.Errorf("the warning is %v, want one for T-2 only", w)
	}
}

// A webhook's step for one entity is not a step of the whole connection:
// it does not move the time from which the next backstop counts the
// announcements. Here T-1's webhook came after the previous whole step
// and its own step failed; a later webhook step for T-2 must not make
// the backstop count from after T-1's announcement.
func TestAWebhookStepForOneEntityDoesNotMoveTheTimeOfTheBackstop(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	reconciledOf(t, env, "tickets", "")
	env.Clock.Advance(time.Minute)
	env.Source.Change("tickets", "T-1")
	env.Source.FailNext(1)
	deliver(t, env, path, "d-1", "T-1")
	if _, ok := env.Source.WaitFor(func(r fakesource.Reconcile) bool { return r.Entity == "T-1" && r.Failed }); !ok {
		t.Fatalf("the webhook's step of T-1 did not fail: %+v", env.Source.Reconciles())
	}
	env.Clock.Advance(time.Minute)
	env.Source.Change("tickets", "T-2")
	deliver(t, env, path, "d-2", "T-2")
	if r := reconciledOf(t, env, "tickets", "T-2"); len(r.Acted) != 1 {
		t.Fatalf("the webhook's step of T-2 acted on %v, want [T-2]", r.Acted)
	}
	env.Clock.Advance(fakesource.Backstop - 2*time.Minute)
	if _, ok := env.Source.WaitFor(func(r fakesource.Reconcile) bool { return r.Entity == "" && len(r.Acted) > 0 }); !ok {
		t.Fatalf("the backstop did not act on T-1: %+v", env.Source.Reconciles())
	}
	settle()
	if warned(env) {
		t.Errorf("the backstop warned of T-1, which a webhook announced after the previous step of the whole connection")
	}
}

// An announcement counts only from the previous step of the whole
// connection on: a webhook of T-1 before that step does not hide a later
// change of T-1 that no webhook announced.
func TestAnAnnouncementBeforeThePreviousBackstopDoesNotHideALaterChange(t *testing.T) {
	env := factorytest.New(t)
	path := env.WebhookConnection("tickets", secret)
	reconciledOf(t, env, "tickets", "")
	env.Source.Change("tickets", "T-1")
	deliver(t, env, path, "d-1", "T-1")
	reconciledOf(t, env, "tickets", "T-1")
	env.Clock.Advance(fakesource.Backstop)
	waitForSteps(t, env, "tickets", "", 2)
	settle()
	if warned(env) {
		t.Fatalf("a backstop that found nothing new warned")
	}

	env.Source.Change("tickets", "T-1") // the webhooks stopped
	env.Clock.Advance(fakesource.Backstop)
	if w := env.WaitForWarning("found a change that no webhook announced"); w["factory.entities"] != "T-1" {
		t.Errorf("the warning is %v, want one for T-1", w)
	}
}

func waitForSteps(t *testing.T, env *factorytest.Env, connection, entity string, n int) {
	t.Helper()
	if _, ok := env.Source.WaitFor(func(fakesource.Reconcile) bool { return stepsOf(env, connection, entity) >= n }); !ok {
		t.Fatalf("%s %q has %d reconcile steps after 10 s, want %d", connection, entity, stepsOf(env, connection, entity), n)
	}
}

func warned(env *factorytest.Env) bool {
	for _, ev := range env.SolidTrace.RunEvents() {
		if m, _ := ev["message"].(string); strings.Contains(m, "no webhook announced") {
			return true
		}
	}
	return false
}

func TestTheCLIListOfConnectionsShowsEachWebhookURLAndTheTimeOfItsLastWebhook(t *testing.T) {
	env := factorytest.New(t)
	dana, ana := admin(t, env), member(t, env)
	path := env.WebhookConnection("tickets", secret)
	mustCLIIn(t, dana, "forge-token\n", "connections", "set", "forge", "--kind", "bitbucket")

	list := mustCLI(t, ana, "connections", "list")
	url := factorytest.PublicWebhookURL + path
	if !strings.Contains(list.Stdout, url) || !strings.Contains(list.Stdout, "never") {
		t.Errorf("connections list before a webhook has no %s and never:\n%s", url, list.Stdout)
	}

	env.Clock.Advance(3 * time.Minute)
	deliver(t, env, path, "d-1", "T-1")
	at := env.Clock.Now().UTC()
	list = mustCLI(t, ana, "connections", "list")
	for _, line := range strings.Split(list.Stdout, "\n") {
		switch {
		case strings.HasPrefix(line, "tickets"):
			if !strings.Contains(line, at.Format("2006-01-02 15:04")) || !strings.Contains(line, url) {
				t.Errorf("the line of tickets has no last webhook at %s and URL %s: %q", at, url, line)
			}
		case strings.HasPrefix(line, "forge"):
			if strings.Contains(line, "/webhooks/") {
				t.Errorf("a connection whose kind sends no webhooks has a webhook URL: %q", line)
			}
		}
	}
	var connections dispatcherapi.Connections
	ana.API("GET", "/api/v1/connections", nil).JSON(t, &connections)
	for _, c := range connections.Connections {
		if c.Name == "tickets" && (c.LastWebhookAt == nil || !c.LastWebhookAt.Equal(at) || c.WebhookURL != url || !c.WebhookSecret) {
			t.Errorf("the API gives %+v, want the last webhook at %s and the URL %s", c, at, url)
		}
	}
}

func mustCLIIn(t *testing.T, p *factorytest.Person, stdin string, args ...string) factorytest.Result {
	t.Helper()
	r := p.RunCLI("", stdin, args...)
	if r.Code != 0 {
		t.Fatalf("solidtrace %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), r.Code, r.Stdout, r.Stderr)
	}
	return r
}

func TestAnAdminSetsAWebhookSecretWithTheCLIAndNoAnswerGivesIt(t *testing.T) {
	env := factorytest.New(t)
	dana, ana := admin(t, env), member(t, env)
	mustCLIIn(t, dana, "fake-token\n", "connections", "set", "tickets", "--kind", fakesource.Kind)

	set := mustCLIIn(t, dana, "the-secret-from-the-service\n", "connections", "webhook-secret", "tickets")
	if !strings.Contains(set.Stdout, factorytest.PublicWebhookURL+"/webhooks/fake/") {
		t.Errorf("webhook-secret does not print the webhook URL:\n%s", set.Stdout)
	}
	list := mustCLI(t, ana, "connections", "list")
	path := "/webhooks/fake/" + strings.Fields(list.Stdout[strings.Index(list.Stdout, "/webhooks/fake/")+len("/webhooks/fake/"):])[0]
	raw := body(t, fakesource.Body{Entity: "T-1"})
	if a := env.Webhook(path, factorytest.FakeDelivery("the-secret-from-the-service", "d-1", raw), raw); a.Status != http.StatusOK {
		t.Errorf("a webhook signed with the secret that the CLI set: %d %s", a.Status, a.Body)
	}
	a := ana.API("GET", "/api/v1/connections", nil)
	if strings.Contains(string(a.Body), "the-secret-from-the-service") || strings.Contains(list.Stdout, "the-secret-from-the-service") {
		t.Errorf("the list gives the secret: %s", a.Body)
	}

	if r := ana.RunCLI("", "mine\n", "connections", "webhook-secret", "tickets"); r.Code != 1 || !strings.Contains(r.Stderr, "Admin") {
		t.Errorf("a member's webhook-secret: exit %d, stderr %q", r.Code, r.Stderr)
	}
	if a := ana.API("PUT", "/api/v1/connections/tickets/webhook-secret", dispatcherapi.SaveWebhookSecret{Secret: "x"}); a.Status != http.StatusForbidden {
		t.Errorf("a member's PUT of a webhook secret: %d, want 403", a.Status)
	}
	if r := dana.RunCLI("", "", "connections", "webhook-secret", "tickets"); r.Code == 0 {
		t.Errorf("webhook-secret with no secret on stdin: exit 0")
	}
	for name, c := range map[string]struct {
		connection, secret string
		want               int
	}{
		"an absent connection":          {"absent", "s", http.StatusNotFound},
		"an empty secret":               {"tickets", "  ", http.StatusBadRequest},
		"a secret over 1 KiB":           {"tickets", strings.Repeat("s", 1025), http.StatusBadRequest},
		"a kind that sends no webhooks": {"forge", "s", http.StatusUnprocessableEntity},
	} {
		if name == "a kind that sends no webhooks" {
			mustCLIIn(t, dana, "forge-token\n", "connections", "set", "forge", "--kind", "bitbucket")
		}
		a := dana.API("PUT", "/api/v1/connections/"+c.connection+"/webhook-secret", dispatcherapi.SaveWebhookSecret{Secret: c.secret})
		if a.Status != c.want {
			t.Errorf("%s: %d %s, want %d", name, a.Status, a.Body, c.want)
		}
	}
	// A new token keeps the webhook secret.
	mustCLIIn(t, dana, "fake-token-2\n", "connections", "set", "tickets", "--kind", fakesource.Kind)
	if a := env.Webhook(path, factorytest.FakeDelivery("the-secret-from-the-service", "d-2", raw), raw); a.Status != http.StatusOK {
		t.Errorf("a webhook after a new token: %d %s, want 200", a.Status, a.Body)
	}
}
