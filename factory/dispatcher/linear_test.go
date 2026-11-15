package dispatcher_test

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

func TestALinearWebhookWithAWrongSignatureOrATimestampOutside60SecondsGets401(t *testing.T) {
	env, path := linearEnv(t)
	env.Linear.Put(fakelinear.Issue{ID: "issue-3", Identifier: "ENG-3", Title: "Fix the search", Status: "Ready for agent"},
		rosaOnLinear)
	now := env.Clock.Now()
	signed := func(delivery string, at time.Time) (http.Header, []byte) {
		return fakelinear.Webhook(linearSecret, delivery, "Issue", "issue-3", at)
	}
	noTimestamp := []byte(`{"action":"update","type":"Issue","data":{"id":"issue-3"}}`)
	for _, c := range []struct {
		name   string
		header func() (http.Header, []byte)
	}{
		{"another secret", func() (http.Header, []byte) {
			return fakelinear.Webhook("another-secret", "r-1", "Issue", "issue-3", now)
		}},
		{"no signature", func() (http.Header, []byte) {
			h, b := signed("r-2", now)
			h.Del("Linear-Signature")
			return h, b
		}},
		{"a signature that is not hex", func() (http.Header, []byte) {
			h, b := signed("r-3", now)
			h.Set("Linear-Signature", "zz"+h.Get("Linear-Signature")[2:])
			return h, b
		}},
		{"a body changed after the signature", func() (http.Header, []byte) {
			h, b := signed("r-4", now)
			return h, []byte(strings.Replace(string(b), "issue-3", "issue-9", 1))
		}},
		{"61 seconds old", func() (http.Header, []byte) { return signed("r-5", now.Add(-61*time.Second)) }},
		{"61 seconds ahead", func() (http.Header, []byte) { return signed("r-6", now.Add(61*time.Second)) }},
		{"no webhookTimestamp", func() (http.Header, []byte) {
			return fakelinear.Signed(linearSecret, "r-7", noTimestamp), noTimestamp
		}},
	} {
		header, body := c.header()
		if a := env.Webhook(path, header, body); a.Status != http.StatusUnauthorized {
			t.Errorf("a webhook with %s: %d %s, want 401", c.name, a.Status, a.Body)
		}
	}
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("the refused webhooks started %d Runs", len(list))
	}

	// 60 seconds old is in the window; the same delivery again queues
	// nothing.
	header, body := signed("ok-1", now.Add(-60*time.Second))
	a := env.Webhook(path, header, body)
	if a.Status != http.StatusOK || !strings.Contains(string(a.Body), `"queued":true`) {
		t.Fatalf("a webhook 60 seconds old: %d %s, want 200 queued", a.Status, a.Body)
	}
	waitForRuns(t, env, 1)
	if a := env.Webhook(path, header, body); a.Status != http.StatusOK || !strings.Contains(string(a.Body), `"queued":false`) {
		t.Errorf("the same delivery again: %d %s, want 200 not queued", a.Status, a.Body)
	}
}

// readyTicket puts a ready ticket on the fake Linear and sends its
// webhook.
func readyTicket(t *testing.T, env *factorytest.Env, path, id string) {
	t.Helper()
	env.Linear.Put(fakelinear.Issue{ID: id, Identifier: strings.ToUpper(id), Title: "Fix " + id,
		Status: "Ready for agent"}, rosaOnLinear)
	if a := env.LinearWebhook(path, linearSecret, "d-"+id, "Issue", id); a.Status != http.StatusOK {
		t.Fatalf("the webhook of %s: %d %s", id, a.Status, a.Body)
	}
}

func TestAnExpiredLinearAccessTokenIsRefreshedWithTheRotatingRefreshToken(t *testing.T) {
	env, path := linearEnv(t)
	// The access token lasts 24 hours.
	env.Clock.Advance(25 * time.Hour)
	readyTicket(t, env, path, "issue-4")
	waitForRuns(t, env, 1)
	if n := env.Linear.Refreshes(); n != 1 {
		t.Fatalf("Linear gave %d refreshes, want 1", n)
	}

	// The next refresh, after a restart, uses the new refresh token: the
	// fake refuses the old one.
	env.Restart()
	env.Clock.Advance(25 * time.Hour)
	readyTicket(t, env, path, "issue-5")
	waitForRuns(t, env, 2)
	if n := env.Linear.Refreshes(); n != 2 {
		t.Errorf("Linear gave %d refreshes, want 2", n)
	}
}

func TestALinearAccessTokenThatLinearRefusesBeforeItsExpiryIsRefreshedOneTime(t *testing.T) {
	env, path := linearEnv(t)
	env.Linear.RevokeAccessTokens()
	readyTicket(t, env, path, "issue-6")
	waitForRuns(t, env, 1)
	if n := env.Linear.Refreshes(); n != 1 {
		t.Errorf("Linear gave %d refreshes, want 1", n)
	}
}

func TestALinearCallThatGets401AfterARefreshStopsWithNoSecondRefresh(t *testing.T) {
	env, _ := linearEnv(t)
	env.Linear.RefuseAccessTokens()
	// The backstop refreshes the expired token first; Linear refuses the
	// new access token too.
	env.Clock.Advance(25 * time.Hour)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(env.Logs(), "a reconcile step failed") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	settle()
	if logs := env.Logs(); !strings.Contains(logs, "a reconcile step failed") {
		t.Fatalf("the reconcile step did not fail:\n%s", logs)
	}
	if n := env.Linear.Refreshes(); n != 1 {
		t.Errorf("Linear gave %d refreshes, want 1: a second refresh uses one more rotating refresh token", n)
	}
}

func TestALinearAccessTokenIsRefreshedWhenItExpiresInLessThan5Minutes(t *testing.T) {
	for _, c := range []struct {
		left      time.Duration
		refreshes int
	}{{4 * time.Minute, 1}, {6 * time.Minute, 0}} {
		t.Run(c.left.String(), func(t *testing.T) {
			env, path := linearEnv(t)
			// The Dispatcher's token expires fakelinear.TokenLife - 1 s
			// after the install.
			env.Clock.Advance(fakelinear.TokenLife - time.Second - c.left)
			readyTicket(t, env, path, "issue-40")
			waitForRuns(t, env, 1)
			if n := env.Linear.Refreshes(); n != c.refreshes {
				t.Errorf("a token that expires in %s: Linear gave %d refreshes, want %d", c.left, n, c.refreshes)
			}
		})
	}
}

func TestALinearConnectionWhoseRefreshTokenLinearRefusesStartsNoRunAndLogsWhatToDo(t *testing.T) {
	env, path := linearEnv(t)
	env.Linear.RefuseRefresh()
	env.Clock.Advance(25 * time.Hour)
	readyTicket(t, env, path, "issue-7")
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Errorf("a connection with no valid token started %d Runs", len(list))
	}
	if logs := env.Logs(); !strings.Contains(logs, "solidtrace connections add linear issues") {
		t.Errorf("the log does not say to install the app again:\n%s", logs)
	}
}

func TestALinearTicketGoesToTheProjectOfItsLabelInTheProjectLabelGroup(t *testing.T) {
	env, path := linearEnv(t)
	env.Project("admin")
	mustSaveRoutes(t, env, "admin", route("issues", "Admin app"))
	env.Linear.Put(fakelinear.Issue{ID: "issue-8", Identifier: "ENG-8", Title: "Fix the login", Status: "Ready for agent",
		Labels: []fakelinear.Label{{Name: "Bug"}, {Name: "Admin app", Group: "Project"}}}, rosaOnLinear)
	// A label of that name in another group is not the routing field.
	env.Linear.Put(fakelinear.Issue{ID: "issue-9", Identifier: "ENG-9", Title: "Fix the cart", Status: "Ready for agent",
		Labels: []fakelinear.Label{{Name: "Admin app", Group: "Area"}}}, rosaOnLinear)
	env.LinearWebhook(path, linearSecret, "d-8", "Issue", "issue-8")
	env.LinearWebhook(path, linearSecret, "d-9", "Issue", "issue-9")
	projects := map[string]string{}
	for _, run := range waitForRuns(t, env, 2) {
		projects[run.Link] = run.Project
	}
	if projects[fakelinear.IssueURL("ENG-8")] != "admin" || projects[fakelinear.IssueURL("ENG-9")] != "shop" {
		t.Errorf("the Runs went to %v, want ENG-8 to admin and ENG-9 to shop", projects)
	}
}

func TestTheNoRouteCommentAndAStatusChangeShowAsTheAppOnLinear(t *testing.T) {
	env := factorytest.New(t)
	path := env.LinearConnection("issues", linearSecret)
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("issues", "Shop"))
	settle()
	env.Linear.Put(fakelinear.Issue{ID: "issue-10", Identifier: "ENG-10", Title: "Fix the cart", Status: "Ready for agent"},
		rosaOnLinear)
	// The comment comes later than the ticket's last change.
	env.Clock.Advance(time.Second)
	env.LinearWebhook(path, linearSecret, "d-10", "Issue", "issue-10")
	deadline := time.Now().Add(10 * time.Second)
	for len(env.Linear.Comments()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	comments := env.Linear.Comments()
	if len(comments) != 1 || comments[0].Issue != "issue-10" || comments[0].Author != fakelinear.App ||
		!strings.Contains(comments[0].Body, "Project") {
		t.Fatalf("the comments are %+v, want one by the app on issue-10 that names the label group Project", comments)
	}
	// The app's own comment is not a new edit of the ticket: the
	// backstops post no second comment.
	for range 2 {
		env.Clock.Advance(5*time.Minute + time.Second)
		settle()
	}
	if n := len(env.Linear.Comments()); n != 1 {
		t.Errorf("the ticket has %d comments, want 1", n)
	}
	if list := runs(t, env); len(list) != 0 {
		t.Errorf("a ticket with no route started %d Runs", len(list))
	}

	if err := env.Dispatcher().SetTicketStatus(t.Context(), "issues", "issue-10", "Agent working"); err != nil {
		t.Fatal(err)
	}
	if status, by := env.Linear.Status("issue-10"); status != "Agent working" || by != fakelinear.App {
		t.Errorf("the ticket is %q, set by %+v, want Agent working by the app", status, by)
	}
	if err := env.Dispatcher().SetTicketStatus(t.Context(), "issues", "issue-10", "No such status"); err == nil ||
		!strings.Contains(err.Error(), "no status") {
		t.Errorf("a status that the team does not have: %v, want an error that says so", err)
	}
}

// addLinear runs 'connections add linear issues' as p with the client
// secret, on the callback port, with the flags.
func addLinear(env *factorytest.Env, p *factorytest.Person, clientSecret, port string, flags ...string) factorytest.Result {
	return p.LinearCLI(clientSecret, append([]string{"connections", "add", "linear", "issues", "--client-id",
		fakelinear.ClientID, "--port", port, "--dispatcher", env.URL()}, flags...)...)
}

// noConnections checks that the Organization has no connection.
func noConnections(t *testing.T, env *factorytest.Env) {
	t.Helper()
	var list dispatcherapi.Connections
	env.Admin().API("GET", "/api/v1/connections", nil).JSON(t, &list)
	if len(list.Connections) != 0 {
		t.Errorf("the Organization has the connections %+v, want none", list.Connections)
	}
}

func TestALinearInstallThatFailsSavesNoConnection(t *testing.T) {
	t.Run("the approval is denied", func(t *testing.T) {
		env := factorytest.New(t)
		env.Linear.Deny()
		r := addLinear(env, env.Admin(), fakelinear.ClientSecret, env.LinearCallbackPort())
		if r.Code != 1 || !strings.Contains(r.Stderr, "denied") {
			t.Errorf("exit %d, %q, want 1 and a message that the install was denied", r.Code, r.Stderr)
		}
		noConnections(t, env)
	})
	t.Run("Linear refuses the client secret", func(t *testing.T) {
		env := factorytest.New(t)
		r := addLinear(env, env.Admin(), "a-wrong-secret", env.LinearCallbackPort())
		if r.Code != 1 || !strings.Contains(r.Stderr, "Linear refused the approval") {
			t.Errorf("exit %d, %q, want 1 and Linear's refusal", r.Code, r.Stderr)
		}
		if strings.Contains(env.Logs(), "a-wrong-secret") {
			t.Error("the log has the client secret")
		}
		noConnections(t, env)
	})
	t.Run("a member is not an Admin", func(t *testing.T) {
		env := factorytest.New(t)
		r := addLinear(env, env.Member("Bob"), fakelinear.ClientSecret, env.LinearCallbackPort())
		if r.Code != 1 || !strings.Contains(r.Stderr, "only an Admin") {
			t.Errorf("exit %d, %q, want 1 and only an Admin", r.Code, r.Stderr)
		}
		noConnections(t, env)
	})
	t.Run("no client secret on stdin", func(t *testing.T) {
		env := factorytest.New(t)
		r := addLinear(env, env.Admin(), "", env.LinearCallbackPort())
		if r.Code != 2 || !strings.Contains(r.Stderr, "client secret") || len(env.Linear.Actors()) != 0 {
			t.Errorf("exit %d, %q, %d approvals, want 2, a message that names the client secret, and no approval",
				r.Code, r.Stderr, len(env.Linear.Actors()))
		}
	})
	t.Run("the callback port is in use", func(t *testing.T) {
		env := factorytest.New(t)
		port := env.LinearCallbackPort()
		busy, err := net.Listen("tcp4", "127.0.0.1:"+port)
		if err != nil {
			t.Fatal(err)
		}
		defer busy.Close()
		r := addLinear(env, env.Admin(), fakelinear.ClientSecret, port)
		if r.Code != 1 || !strings.Contains(r.Stderr, "--port") {
			t.Errorf("exit %d, %q, want 1 and a message that names --port", r.Code, r.Stderr)
		}
	})
}

func TestARedirectWithAWrongStateDoesNotEndTheLinearInstall(t *testing.T) {
	env := factorytest.New(t)
	env.Linear.Forged()
	r := addLinear(env, env.Admin(), fakelinear.ClientSecret, env.LinearCallbackPort())
	if r.Code != 0 {
		t.Fatalf("exit %d: %s%s", r.Code, r.Stdout, r.Stderr)
	}
	if c, _ := connection(t, env, "issues"); c.Kind != "linear" {
		t.Errorf("the connection is %+v, want a linear connection", c)
	}
}

func TestAnInstallOfLinearAgainKeepsTheWebhookSecretAndUsesTheNewTokens(t *testing.T) {
	env, path := linearEnv(t)
	if r := addLinear(env, env.Admin(), fakelinear.ClientSecret, env.LinearCallbackPort()); r.Code != 0 {
		t.Fatalf("install again: exit %d: %s%s", r.Code, r.Stdout, r.Stderr)
	}
	if c, _ := connection(t, env, "issues"); !c.WebhookSecret {
		t.Error("the connection lost its webhook secret")
	}
	readyTicket(t, env, path, "issue-11")
	waitForRuns(t, env, 1)
}

// design is a second team of the workspace.
var design = fakelinear.Team{ID: "team-des", Key: "DES", Name: "Design"}

func TestALinearConnectionIsOfOneTeamAndATicketOfAnotherTeamStartsNoRun(t *testing.T) {
	env := factorytest.New(t)
	env.Linear.AddTeam(design)
	port := env.LinearCallbackPort()
	// The app reads two teams: the install must name one.
	r := addLinear(env, env.Admin(), fakelinear.ClientSecret+"\n", port)
	if r.Code != 1 || !strings.Contains(r.Stderr, "--team") || !strings.Contains(r.Stderr, "ENG") ||
		!strings.Contains(r.Stderr, "DES") {
		t.Fatalf("an install with no team: exit %d, %q, want 1 and a message that names --team, ENG and DES", r.Code, r.Stderr)
	}
	noConnections(t, env)
	r = addLinear(env, env.Admin(), fakelinear.ClientSecret+"\n", port, "--team", "OPS")
	if r.Code != 1 || !strings.Contains(r.Stderr, "OPS") || !strings.Contains(r.Stderr, "ENG") {
		t.Fatalf("an install with a team that the app cannot read: exit %d, %q, want 1 and a message that names OPS "+
			"and the teams", r.Code, r.Stderr)
	}
	noConnections(t, env)
	if r = addLinear(env, env.Admin(), fakelinear.ClientSecret+"\n", port, "--team", "eng"); r.Code != 0 {
		t.Fatalf("an install for the team eng: exit %d\n%s%s", r.Code, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "ENG") {
		t.Errorf("the CLI printed %q, want the team ENG", r.Stdout)
	}
	c, _ := connection(t, env, "issues")
	path, _ := strings.CutPrefix(c.WebhookURL, factorytest.PublicWebhookURL)
	if a := env.Admin().API("PUT", "/api/v1/connections/issues/webhook-secret",
		dispatcherapi.SaveWebhookSecret{Secret: linearSecret}); a.Status != http.StatusOK {
		t.Fatalf("save the webhook secret: %d %s", a.Status, a.Body)
	}
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("issues", ""))
	settle()

	// A ready ticket of Design, through its webhook and the backstop, and
	// a ready ticket of Engineering through the backstop.
	env.Linear.Put(fakelinear.Issue{ID: "issue-20", Identifier: "DES-1", Team: "DES", Title: "Draw the icons",
		Status: "Ready for agent"}, rosaOnLinear)
	env.LinearWebhook(path, linearSecret, "d-20", "Issue", "issue-20")
	env.Linear.Put(fakelinear.Issue{ID: "issue-21", Identifier: "ENG-21", Title: "Fix the cart", Status: "Ready for agent"},
		rosaOnLinear)
	env.Clock.Advance(5*time.Minute + time.Second)
	if run := waitForRuns(t, env, 1)[0]; run.Link != fakelinear.IssueURL("ENG-21") {
		t.Errorf("the Run is of %s, want ENG-21", run.Link)
	}
	env.Clock.Advance(5*time.Minute + time.Second)
	settle()
	if list := runs(t, env); len(list) != 1 {
		t.Errorf("the tickets have %d Runs, want 1 (none of the team Design)", len(list))
	}
	for _, comment := range env.Linear.Comments() {
		if comment.Issue == "issue-20" {
			t.Errorf("the factory commented on the Design ticket: %+v", comment)
		}
	}
}

func TestACopyOfTheDatabaseFileShowsNoLinearSecretAfterARefresh(t *testing.T) {
	env, path := linearEnv(t)
	env.Clock.Advance(25 * time.Hour)
	readyTicket(t, env, path, "issue-30")
	waitForRuns(t, env, 1)
	if n := env.Linear.Refreshes(); n != 1 {
		t.Fatalf("Linear gave %d refreshes, want 1", n)
	}

	files, _ := filepath.Glob(env.Config.Database + "*")
	if len(files) == 0 {
		t.Fatal("no database file")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{fakelinear.ClientSecret, "app-access-", "app-refresh-"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Errorf("%s holds %q", filepath.Base(file), secret)
			}
		}
	}
}
