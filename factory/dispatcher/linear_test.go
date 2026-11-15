package dispatcher_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakelinear"
)

// linearSecret is the webhook secret of the Linear connections.
const linearSecret = "linear-signing-secret"

// rosaOnLinear is a person of the Linear workspace.
var rosaOnLinear = fakelinear.User{ID: "lin-rosa", Name: "Rosa"}

// linearEnv is an Env with the Linear connection "issues" (webhook
// secret linearSecret), and the Project shop with its default route.
func linearEnv(t *testing.T) (*factorytest.Env, string) {
	t.Helper()
	env := factorytest.New(t)
	path := env.LinearConnection("issues", linearSecret)
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("issues", ""))
	settle()
	return env, path
}

func TestALinearTicketMovedToTheReadyStatusStartsARunThroughTheWebhook(t *testing.T) {
	env, path := linearEnv(t)
	env.Linear.Put(fakelinear.Issue{ID: "issue-1", Identifier: "ENG-1", Title: "Fix the cart total",
		Description: "The total skips the discount.", Status: "Backlog"}, rosaOnLinear)
	if a := env.LinearWebhook(path, linearSecret, "d-0", "Issue", "issue-1"); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("a ticket in Backlog started %d Runs", len(list))
	}

	env.Linear.SetStatus("issue-1", "Ready for agent", rosaOnLinear)
	if a := env.LinearWebhook(path, linearSecret, "d-1", "Issue", "issue-1"); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	run := waitForRuns(t, env, 1)[0]
	if run.Project != "shop" || !strings.HasPrefix(run.Prompt, "Fix the cart total\n") ||
		!strings.Contains(run.Prompt, "The total skips the discount.") {
		t.Errorf("the Run is in %s with the prompt %q, want shop with the ticket's title and body", run.Project, run.Prompt)
	}
	if want := fakelinear.IssueURL("ENG-1"); run.Link != want {
		t.Errorf("the Run's link is %q, want %q", run.Link, want)
	}
	if want := (dispatcherapi.Person{Kind: dispatcherapi.ActorTracker, ID: "lin-rosa", Name: "Rosa"}); run.StartedBy != want {
		t.Errorf("the Run was started by %+v, want %+v", run.StartedBy, want)
	}

	// A comment's webhook on the same ticket, and the backstop, start no
	// second Run, and the backstop does not warn: a webhook announced it.
	env.LinearWebhook(path, linearSecret, "d-2", "Comment", "issue-1")
	env.Clock.Advance(5*time.Minute + time.Second)
	settle()
	if list := runs(t, env); len(list) != 1 {
		t.Errorf("the ticket has %d Runs, want 1", len(list))
	}
	if n := warnings(env, "found a change that no webhook announced"); n != 0 {
		t.Errorf("the backstop wrote %d warnings, want none", n)
	}
}

func TestALinearTicketThatNoWebhookAnnouncedStartsARunThroughTheBackstopWithAWarning(t *testing.T) {
	env, _ := linearEnv(t)
	env.Linear.Put(fakelinear.Issue{ID: "issue-2", Identifier: "ENG-2", Title: "Add a dark mode",
		Status: "Ready for agent"}, rosaOnLinear)
	env.Clock.Advance(5*time.Minute + time.Second)
	if run := waitForRuns(t, env, 1)[0]; run.Project != "shop" || run.Link != fakelinear.IssueURL("ENG-2") {
		t.Errorf("the Run is in %s with the link %s, want shop and the ticket ENG-2", run.Project, run.Link)
	}
	w := env.WaitForWarning("found a change that no webhook announced")
	if w["factory.connection"] != "issues" || w["factory.connection_kind"] != "linear" || w["factory.entities"] != "issue-2" {
		t.Errorf("the warning is %v, want the connection issues of kind linear and the ticket issue-2", w)
	}
}

func connection(t *testing.T, env *factorytest.Env, name string) (dispatcherapi.Connection, []byte) {
	t.Helper()
	a := env.Admin().API("GET", "/api/v1/connections", nil)
	var list dispatcherapi.Connections
	a.JSON(t, &list)
	for _, c := range list.Connections {
		if c.Name == name {
			return c, a.Body
		}
	}
	t.Fatalf("no connection %s in %s", name, a.Body)
	return dispatcherapi.Connection{}, nil
}

func TestAnAdminInstallsTheLinearAppWithTheCLIAsTheApp(t *testing.T) {
	env := factorytest.New(t)
	port := env.LinearCallbackPort()
	r := env.Admin().LinearCLI(fakelinear.ClientSecret+"\n", "connections", "add", "linear", "issues",
		"--client-id", fakelinear.ClientID, "--port", port, "--dispatcher", env.URL())
	if r.Code != 0 {
		t.Fatalf("connections add linear: exit %d\n%s%s", r.Code, r.Stdout, r.Stderr)
	}
	if actors := env.Linear.Actors(); !slices.Equal(actors, []string{"app"}) {
		t.Errorf("the approvals asked for the actors %q, want [app]", actors)
	}
	c, body := connection(t, env, "issues")
	if c.Kind != "linear" || !strings.HasPrefix(c.WebhookURL, factorytest.PublicWebhookURL+"/webhooks/linear/") {
		t.Errorf("the connection is %+v, want a linear connection with a webhook URL", c)
	}
	if !strings.Contains(r.Stdout, c.WebhookURL) {
		t.Errorf("the CLI printed %q, want the webhook URL %s", r.Stdout, c.WebhookURL)
	}
	if c.Tracker == nil || c.Tracker.ReadyStatus != "Ready for agent" || c.Tracker.RoutingField != "Project" {
		t.Errorf("the tracker settings are %+v, want Ready for agent and the label group Project", c.Tracker)
	}
	for _, secret := range []string{fakelinear.ClientSecret, "access-", "refresh-"} {
		if strings.Contains(string(body), secret) || strings.Contains(r.Stdout+r.Stderr, secret) || strings.Contains(env.Logs(), secret) {
			t.Errorf("an answer, the CLI or the log has %q", secret)
		}
	}
}

func TestAPlainSaveOfALinearConnectionIsRefused(t *testing.T) {
	env := factorytest.New(t)
	a := env.Admin().API("PUT", "/api/v1/connections/issues", dispatcherapi.SaveConnection{Kind: "linear", Token: "lin_api_x"})
	if a.Status != http.StatusBadRequest || !strings.Contains(string(a.Body), "connections add linear") {
		t.Errorf("the save answered %d %s, want 400 that names 'connections add linear'", a.Status, a.Body)
	}
}
