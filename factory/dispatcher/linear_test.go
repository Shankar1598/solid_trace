package dispatcher_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakelinear"
)

// linearSecret is the webhook secret of the Linear connections.
const linearSecret = "linear-signing-secret"

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
