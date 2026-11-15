package dispatcher_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/faketracker"
	"github.com/solidtrace/factory/internal/recorder"
)

var rosa = faketracker.User{ID: "u-7", Name: "Rosa"}

// saveRoutes saves the Factory settings of the Project as the Admin, with
// the routes, and gives the answer.
func saveRoutes(env *factorytest.Env, slug string, routes ...dispatcherapi.Route) factorytest.Answer {
	if routes == nil {
		routes = []dispatcherapi.Route{}
	}
	return env.Admin().API("PUT", "/api/v1/projects/"+slug+"/settings", dispatcherapi.SaveFactorySettings{
		Repository: "bitbucket.org/example/" + slug, Routes: &routes})
}

func mustSaveRoutes(t *testing.T, env *factorytest.Env, slug string, routes ...dispatcherapi.Route) {
	t.Helper()
	if a := saveRoutes(env, slug, routes...); a.Status != http.StatusOK {
		t.Fatalf("save the routes of %s: %d %s", slug, a.Status, a.Body)
	}
}

func route(connection, match string) dispatcherapi.Route {
	return dispatcherapi.Route{Connection: connection, Match: match}
}

// runs lists the Organization's Runs as the Admin.
func runs(t *testing.T, env *factorytest.Env) []dispatcherapi.Run {
	t.Helper()
	a := env.Admin().API("GET", "/api/v1/runs", nil)
	if a.Status != http.StatusOK {
		t.Fatalf("list the Runs: %d %s", a.Status, a.Body)
	}
	var list dispatcherapi.Runs
	a.JSON(t, &list)
	return list.Runs
}

// waitForRuns waits up to 10 s until the Organization has n Runs.
func waitForRuns(t *testing.T, env *factorytest.Env, n int) []dispatcherapi.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		list := runs(t, env)
		if len(list) >= n {
			return list
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d Runs after 10 s, want %d\nDispatcher log:\n%s", len(list), n, env.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForComments waits up to 10 s until the connection's tickets have n
// comments.
func waitForComments(t *testing.T, env *factorytest.Env, connection string, n int) []faketracker.Comment {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		comments := env.Tracker.Comments(connection)
		if len(comments) >= n {
			return comments
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d comments after 10 s, want %d\nDispatcher log:\n%s", len(comments), n, env.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// warnings counts the warning Logs whose message has text.
func warnings(env *factorytest.Env, text string) int {
	n := 0
	for _, ev := range env.SolidTrace.RunEvents() {
		if message, _ := ev["message"].(string); ev["run.id"] == nil && strings.Contains(message, text) {
			n++
		}
	}
	return n
}

// trackerEnv is an Env with the tracker connection "board" (webhook
// secret secret), and the Project shop with the default route of board.
func trackerEnv(t *testing.T) (*factorytest.Env, string) {
	t.Helper()
	env := factorytest.New(t)
	path := env.TrackerConnection("board", secret)
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("board", ""))
	return env, path
}

func TestATicketSetToTheReadyStatusStartsOneRunOnItsRoutesProject(t *testing.T) {
	env, path := trackerEnv(t)
	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Fix the cart total",
		Body: "The total skips the discount.", Status: "Backlog"}, rosa)
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("a ticket in Backlog started %d Runs", len(list))
	}

	env.Tracker.SetStatus("board", "T-1", faketracker.ReadyStatus, rosa)
	if a := env.TrackerWebhook(path, secret, "d-1", "T-1"); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	run := waitForRuns(t, env, 1)[0]
	if run.Project != "shop" || run.State != dispatcherapi.StateQueued {
		t.Errorf("the Run is in %s, %s, want shop, queued", run.Project, run.State)
	}
	if !strings.HasPrefix(run.Prompt, "Fix the cart total\n") || !strings.Contains(run.Prompt, "The total skips the discount.") {
		t.Errorf("the Run's prompt is %q, want the ticket's title and body", run.Prompt)
	}
	if want := faketracker.TicketURL("board", "T-1"); run.Link != want {
		t.Errorf("the Run's link is %q, want %q", run.Link, want)
	}
	if want := (dispatcherapi.Person{Kind: dispatcherapi.ActorTracker, ID: "u-7", Name: "Rosa"}); run.StartedBy != want {
		t.Errorf("the Run was started by %+v, want %+v", run.StartedBy, want)
	}
	events := env.WaitForRunEvents(run.ID, 2)
	if events[0]["actor.kind"] != "tracker" || events[0]["actor.name"] != "Rosa" || events[0]["run.trigger"] != "tracker" ||
		events[0]["run.title"] != "Fix the cart total" || events[0]["run.link"] != run.Link {
		t.Errorf("the created event is %v, want the tracker user Rosa as its actor, and the ticket's title and link", events[0])
	}

	// The same ticket announced again, and the backstop, start no second Run.
	env.TrackerWebhook(path, secret, "d-2", "T-1")
	env.Clock.Advance(faketracker.Backstop + time.Second)
	settle()
	if list := runs(t, env); len(list) != 1 {
		t.Errorf("the ticket has %d Runs, want 1", len(list))
	}
}

func TestAReadyTicketStartsARunThroughTheBackstopWithNoWebhook(t *testing.T) {
	env, _ := trackerEnv(t)
	settle()
	env.Tracker.Put("board", faketracker.Ticket{ID: "T-2", Title: "Add a dark mode", Status: faketracker.ReadyStatus}, rosa)
	env.Clock.Advance(faketracker.Backstop + time.Second)
	if run := waitForRuns(t, env, 1)[0]; run.Project != "shop" {
		t.Errorf("the Run is in %s, want shop", run.Project)
	}
}

func TestARouteWithAMatchValueWinsOverTheEmptyRouteOfTheSameSource(t *testing.T) {
	env, path := trackerEnv(t)
	env.Project("admin")
	mustSaveRoutes(t, env, "admin", route("board", "Admin app"))

	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Admin ticket", Route: "Admin app",
		Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-1", "T-1")
	if run := waitForRuns(t, env, 1)[0]; run.Project != "admin" {
		t.Errorf("a ticket with the routing value Admin app runs in %s, want admin", run.Project)
	}
	env.Tracker.Put("board", faketracker.Ticket{ID: "T-2", Title: "Other ticket", Route: "Docs",
		Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-2", "T-2")
	for _, run := range waitForRuns(t, env, 2) {
		if strings.HasPrefix(run.Prompt, "Other ticket") && run.Project != "shop" {
			t.Errorf("a ticket that no match value takes runs in %s, want shop, the default", run.Project)
		}
	}
}

func TestTheRoutingValueOfATicketIsTrimmedBeforeTheRouteLookup(t *testing.T) {
	env, path := trackerEnv(t)
	env.Project("admin")
	mustSaveRoutes(t, env, "admin", route("board", "Admin app"))

	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Admin ticket", Route: "  Admin app  ",
		Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-1", "T-1")
	if run := waitForRuns(t, env, 1)[0]; run.Project != "admin" {
		t.Errorf("a ticket with the routing value '  Admin app  ' runs in %s, want admin", run.Project)
	}
}

func TestAConnectionOfAnotherOrganizationThanTheDispatchersTokenStartsNoRun(t *testing.T) {
	env := factorytest.New(t)
	path := env.TrackerConnection("board", secret)
	shop := env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("board", ""))

	// The Dispatcher's token is now of another Organization, which has a
	// Project with the same id.
	other := recorder.Organization{ID: 8, Slug: "other", Name: "Other", Role: "admin"}
	env.SolidTrace.SetProject(other.Slug, recorder.Project{ID: shop.ID, Slug: "web", Name: "web"})
	env.SolidTrace.Token(factorytest.DispatcherToken, recorder.Answer{Sub: "1", Username: "dana@example.com",
		Scope: "run_events:write issues:comment tokens:create mcp:read", Organizations: []recorder.Organization{other}})
	env.Restart()

	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Fix it", Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-1", "T-1")
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(env.Logs(), "another Organization than the Dispatcher's token") {
		if time.Now().After(deadline) {
			t.Fatalf("no refusal of the connection of another Organization after 10 s\nDispatcher log:\n%s", env.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if logs := env.Logs(); strings.Contains(logs, "a ready ticket started a Run") || strings.Contains(logs, "no longer has") {
		t.Errorf("the ticket of a connection of another Organization acted:\n%s", logs)
	}
}

func TestASaveOfASecondRouteWithTheSamePairIsRefused(t *testing.T) {
	env, _ := trackerEnv(t)
	env.Project("admin")
	mustSaveRoutes(t, env, "admin", route("board", "Admin app"))

	a := saveRoutes(env, "shop", route("board", ""), route("board", " Admin app "))
	if a.Status != http.StatusConflict || !strings.Contains(string(a.Body), "admin") {
		t.Errorf("a second route with the pair (board, Admin app): %d %s, want 409 that names admin", a.Status, a.Body)
	}
	a = saveRoutes(env, "admin", route("board", ""))
	if a.Status != http.StatusConflict || !strings.Contains(string(a.Body), "shop") {
		t.Errorf("a second empty route of board: %d %s, want 409 that names shop", a.Status, a.Body)
	}
	a = env.Admin().API("GET", "/api/v1/projects/shop/settings", nil)
	var shop dispatcherapi.FactorySettings
	a.JSON(t, &shop)
	if len(shop.Routes) != 1 || shop.Routes[0] != route("board", "") {
		t.Errorf("after the refused save, shop has the routes %+v, want only (board, \"\")", shop.Routes)
	}
}

func TestRoutesAreSavedByAnAdminAndReadByEachMember(t *testing.T) {
	env, _ := trackerEnv(t)
	member := env.Member("Mo")
	a := member.API("PUT", "/api/v1/projects/shop/settings", dispatcherapi.SaveFactorySettings{
		Repository: "bitbucket.org/example/shop", Routes: &[]dispatcherapi.Route{}})
	if a.Status != http.StatusForbidden {
		t.Errorf("a member's save of the routes: %d %s, want 403", a.Status, a.Body)
	}
	a = member.API("GET", "/api/v1/projects/shop/settings", nil)
	var shop dispatcherapi.FactorySettings
	a.JSON(t, &shop)
	if a.Status != http.StatusOK || len(shop.Routes) != 1 || shop.Routes[0] != route("board", "") {
		t.Errorf("a member reads %d %+v, want the route (board, \"\")", a.Status, shop.Routes)
	}

	// A save with no routes keeps them.
	a = env.Admin().API("PUT", "/api/v1/projects/shop/settings", map[string]string{"repository": "bitbucket.org/example/shop"})
	a.JSON(t, &shop)
	if len(shop.Routes) != 1 {
		t.Errorf("a save with no routes left %+v, want the route kept", shop.Routes)
	}
}

func TestARouteNeedsATrackerConnectionOfTheOrganization(t *testing.T) {
	env, _ := trackerEnv(t)
	env.Admin().API("PUT", "/api/v1/connections/code", dispatcherapi.SaveConnection{Kind: "bitbucket", Token: "t"})
	for _, r := range []dispatcherapi.Route{route("code", ""), route("nothing", "")} {
		if a := saveRoutes(env, "shop", r); a.Status != http.StatusUnprocessableEntity {
			t.Errorf("a route of the connection %s: %d %s, want 422", r.Connection, a.Status, a.Body)
		}
	}
}

func TestAReadyTicketThatNoRouteMatchesGetsOneCommentAndOneWarningForEachEdit(t *testing.T) {
	env := factorytest.New(t)
	path := env.TrackerConnection("board", secret)
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("board", "Shop"))

	env.Tracker.Put("board", faketracker.Ticket{ID: "T-9", Title: "Lost ticket", Route: "Shopp",
		Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-1", "T-9")
	comments := waitForComments(t, env, "board", 1)
	if c := comments[0]; c.Ticket != "T-9" || !strings.Contains(c.Body, `"Project"`) || !strings.Contains(c.Body, "Shop") {
		t.Errorf("the comment is %+v, want one on T-9 that names the field Project and the value Shop", c)
	}
	env.WaitForWarning("no route")

	// The same edit again: no comment, no warning.
	env.TrackerWebhook(path, secret, "d-2", "T-9")
	env.Clock.Advance(faketracker.Backstop + time.Second)
	settle()
	if n := len(env.Tracker.Comments("board")); n != 1 {
		t.Errorf("the same edit gave %d comments, want 1", n)
	}
	if n := warnings(env, "no route"); n != 1 {
		t.Errorf("the same edit gave %d warnings, want 1", n)
	}
	if list := runs(t, env); len(list) != 0 {
		t.Errorf("a ticket with no route started %d Runs", len(list))
	}

	// A new edit that still matches no route: one more of each.
	env.Tracker.SetRoute("board", "T-9", "Shoop")
	env.TrackerWebhook(path, secret, "d-3", "T-9")
	waitForComments(t, env, "board", 2)

	// The right value starts the Run.
	env.Tracker.SetRoute("board", "T-9", "Shop")
	env.TrackerWebhook(path, secret, "d-4", "T-9")
	if run := waitForRuns(t, env, 1)[0]; run.Project != "shop" {
		t.Errorf("the Run is in %s, want shop", run.Project)
	}
}

func TestARouteSavedAfterTheNoRouteCommentStartsTheRunOfTheSameEdit(t *testing.T) {
	env := factorytest.New(t)
	path := env.TrackerConnection("board", secret)
	env.Project("shop")
	mustSaveRoutes(t, env, "shop")

	env.Tracker.Put("board", faketracker.Ticket{ID: "T-9", Title: "Lost ticket", Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-1", "T-9")
	comments := waitForComments(t, env, "board", 1)
	if c := comments[0].Body; !strings.Contains(c, `"Project"`) || !strings.Contains(c, "board") {
		t.Errorf("the comment is %q, want one that names the field Project and the connection board", c)
	}
	env.WaitForWarning("no route")

	// The Admin adds the route; the backstop starts the Run with no new edit.
	mustSaveRoutes(t, env, "shop", route("board", ""))
	env.Clock.Advance(faketracker.Backstop + time.Second)
	if run := waitForRuns(t, env, 1)[0]; run.Project != "shop" {
		t.Errorf("the Run is in %s, want shop", run.Project)
	}
	if n := len(env.Tracker.Comments("board")); n != 1 {
		t.Errorf("the ticket got %d comments, want 1", n)
	}
}

func TestATicketWhoseCommentFailsDoesNotStopTheOtherReadyTickets(t *testing.T) {
	env := factorytest.New(t)
	env.TrackerConnection("board", secret)
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("board", "Shop"))
	settle()

	// A-1 comes first in the list, matches no route, and its comment fails.
	env.Tracker.Put("board", faketracker.Ticket{ID: "A-1", Title: "Lost", Route: "Nope",
		Status: faketracker.ReadyStatus}, rosa)
	env.Tracker.RefuseComments("board", "A-1")
	env.Tracker.Put("board", faketracker.Ticket{ID: "B-2", Title: "Fix it", Route: "Shop",
		Status: faketracker.ReadyStatus}, rosa)
	env.Clock.Advance(faketracker.Backstop + time.Second)
	if run := waitForRuns(t, env, 1)[0]; run.Link != faketracker.TicketURL("board", "B-2") {
		t.Errorf("the Run is of %s, want B-2", run.Link)
	}
	warning := env.WaitForWarning("comment on it failed")
	if warning["factory.ticket"] != "A-1" || warning["error"] == nil {
		t.Errorf("the warning is %v, want one about A-1 with the error", warning)
	}
}

func TestATicketWithAnActiveRunStartsNoSecondRun(t *testing.T) {
	env, path := trackerEnv(t)
	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Fix it", Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-1", "T-1")
	first := waitForRuns(t, env, 1)[0]

	// A new edit of the ticket in the ready status, while the Run is queued.
	env.Tracker.SetStatus("board", "T-1", "Backlog", rosa)
	env.Tracker.SetStatus("board", "T-1", faketracker.ReadyStatus, rosa)
	env.TrackerWebhook(path, secret, "d-2", "T-1")
	settle()
	if list := runs(t, env); len(list) != 1 || list[0].ID != first.ID {
		t.Errorf("the ticket with an Active Run has the Runs %+v, want only %s", list, first.ID)
	}
	if n := len(env.Tracker.Comments("board")); n != 0 {
		t.Errorf("the ticket got %d comments", n)
	}
}

func TestARouteToAProjectThatSolidTraceNoLongerHasStartsNoRunAndWarnsOneTime(t *testing.T) {
	env := factorytest.New(t)
	path := env.TrackerConnection("board", secret)
	shop := env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("board", ""))
	env.SolidTrace.DeleteProject(factorytest.Organization.Slug, shop.ID)

	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Fix it", Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-1", "T-1")
	warning := env.WaitForWarning("no longer has")
	if message, _ := warning["message"].(string); warning["factory.ticket"] != "T-1" || !strings.Contains(message, "orphaned") {
		t.Errorf("the warning is %v, want one about T-1 that says the settings are marked orphaned", warning)
	}
	env.TrackerWebhook(path, secret, "d-2", "T-1")
	settle()
	if n := warnings(env, "the route of a ready ticket"); n != 1 {
		t.Errorf("the same edit gave %d warnings, want 1", n)
	}
	if list := runs(t, env); len(list) != 0 {
		t.Errorf("a route to a deleted Project started %d Runs", len(list))
	}
}

func TestTheReadyStatusIsASettingOfTheConnection(t *testing.T) {
	env, path := trackerEnv(t)
	a := env.Member("Mo").API("PUT", "/api/v1/connections/board/tracker", dispatcherapi.TrackerSettings{ReadyStatus: "Go"})
	if a.Status != http.StatusForbidden {
		t.Errorf("a member's save of the tracker settings: %d %s, want 403", a.Status, a.Body)
	}
	a = env.Admin().API("PUT", "/api/v1/connections/board/tracker", dispatcherapi.TrackerSettings{ReadyStatus: "Go"})
	var c dispatcherapi.Connection
	a.JSON(t, &c)
	if a.Status != http.StatusOK || c.Tracker == nil || *c.Tracker != (dispatcherapi.TrackerSettings{ReadyStatus: "Go",
		RoutingField: faketracker.RoutingField}) {
		t.Fatalf("the save of the tracker settings: %d %s, want Go and the default routing field", a.Status, a.Body)
	}

	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Old ready", Status: faketracker.ReadyStatus}, rosa)
	env.TrackerWebhook(path, secret, "d-1", "T-1")
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("a ticket in the default status started %d Runs, but the ready status is Go", len(list))
	}
	env.Tracker.SetStatus("board", "T-1", "Go", rosa)
	env.TrackerWebhook(path, secret, "d-2", "T-1")
	waitForRuns(t, env, 1)

	a = env.Admin().API("PUT", "/api/v1/connections/code", dispatcherapi.SaveConnection{Kind: "bitbucket", Token: "t"})
	a = env.Admin().API("PUT", "/api/v1/connections/code/tracker", dispatcherapi.TrackerSettings{ReadyStatus: "Go"})
	if a.Status != http.StatusUnprocessableEntity {
		t.Errorf("tracker settings of a forge connection: %d %s, want 422", a.Status, a.Body)
	}
}
