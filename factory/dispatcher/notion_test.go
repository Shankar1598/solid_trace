package dispatcher_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakenotion"
)

var (
	rosaN    = fakenotion.User{ID: "u0000000-0000-4000-8000-00000000r05a", Name: "Rosa"}
	statuses = []string{"Backlog", "Ready for agent", "Agent working", "In review", "Waiting on you"}
)

// notionEnv is an Env with the Notion connection "notion" for a database
// of the fake Notion (webhook secret secret, "" for none), and the
// Project shop with the route "shop" of the select property Project.
func notionEnv(t *testing.T, webhookSecret string) (*factorytest.Env, string, string) {
	t.Helper()
	env := factorytest.New(t)
	db := fakenotion.NewID()
	env.Notion.Database(db, statuses, "Project")
	env.Notion.AddUser(rosaN)
	path := env.NotionConnection("notion", webhookSecret, db)
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("notion", "shop"))
	settle() // the reconcile step of the new connection
	return env, db, path
}

func TestANotionPageSetToTheReadyStatusStartsARunThroughTheWebhook(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	page := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: page, Title: "Fix the cart total", Status: "Backlog",
		Select: map[string]string{"Project": "shop"}, Blocks: []fakenotion.Block{
			{Type: "paragraph", Text: "The total skips the discount."},
			{Type: "bulleted_list_item", Text: "Check the coupon code", Children: []fakenotion.Block{
				{Type: "paragraph", Text: "and the gift card"}}},
		}}, rosaN)
	if a := env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.created", page)); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("a page in Backlog started %d Runs", len(list))
	}

	env.Notion.SetStatus(page, "Ready for agent", rosaN)
	if a := env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.properties_updated", page)); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	run := waitForRuns(t, env, 1)[0]
	if run.Project != "shop" || run.Link != fakenotion.PageURL(page) {
		t.Errorf("the Run is in %s with the link %q, want shop and the page's URL", run.Project, run.Link)
	}
	want := "Fix the cart total\n\nThe total skips the discount.\n- Check the coupon code\n  and the gift card"
	if run.Prompt != want {
		t.Errorf("the Run's prompt is %q, want %q", run.Prompt, want)
	}
	if want := (dispatcherapi.Person{Kind: dispatcherapi.ActorTracker, ID: rosaN.ID, Name: "Rosa"}); run.StartedBy != want {
		t.Errorf("the Run was started by %+v, want %+v", run.StartedBy, want)
	}
	if !strings.Contains(env.Logs(), "a ready ticket started a Run") {
		t.Errorf("no log of the start:\n%s", env.Logs())
	}
}
