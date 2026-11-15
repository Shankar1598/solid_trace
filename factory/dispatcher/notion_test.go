package dispatcher_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
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

// readyPage puts a page of the database in the ready status, routed to
// shop, as Rosa. It sends no webhook.
func readyPage(env *factorytest.Env, db, title string) string {
	page := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: page, Title: title, Status: "Ready for agent",
		Select: map[string]string{"Project": "shop"}}, rosaN)
	return page
}

func TestWithNoWebhookTheDispatcherPollsNotionEvery30Seconds(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	page := readyPage(env, db, "Fix the cart total")

	env.Clock.Advance(dispatcher.NotionPoll - time.Second)
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("%d Runs before the poll", len(list))
	}
	env.Clock.Advance(time.Second)
	run := waitForRuns(t, env, 1)[0]
	if run.Link != fakenotion.PageURL(page) {
		t.Errorf("the Run's link is %q, want the page's URL", run.Link)
	}
	// A connection with no webhook secret gets no webhooks: its poll
	// does not warn.
	if n := warnings(env, "no webhook announced"); n != 0 {
		t.Errorf("the poll of a connection with no webhook secret wrote %d warnings", n)
	}
}

func TestAfterTheFirstVerifiedWebhookTheBackstopIsEvery5Minutes(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	// A page that no route takes: it announces the webhook and starts no
	// Run.
	other := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: other, Title: "Other", Status: "Backlog"}, rosaN)
	if a := env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.created", other)); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	// The poll that was due before the webhook runs one more time; the
	// next one is 5 minutes after it.
	env.Clock.Advance(dispatcher.NotionPoll)
	settle()

	readyPage(env, db, "Fix the cart total")
	env.Clock.Advance(dispatcher.NotionPoll + time.Second)
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("a poll 30 s after a verified webhook started %d Runs, want none until 5 minutes", len(list))
	}
	env.Clock.Advance(dispatcher.NotionBackstop - dispatcher.NotionPoll - 2*time.Second)
	settle()
	if list := runs(t, env); len(list) != 0 {
		t.Fatalf("%d Runs before the 5-minute backstop", len(list))
	}
	env.Clock.Advance(2 * time.Second)
	waitForRuns(t, env, 1)
	// The backstop found a ready page that no webhook announced.
	env.WaitForWarning("no webhook announced")
}

// notionCalls is how many calls to the fake Notion so far start with
// prefix ("METHOD /path").
func notionCalls(env *factorytest.Env, prefix string) int {
	n := 0
	for _, call := range env.Notion.Calls() {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

// waitForNotionCalls waits up to 10 s until the fake Notion has at least n
// calls that start with prefix.
func waitForNotionCalls(t *testing.T, env *factorytest.Env, prefix string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for notionCalls(env, prefix) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d calls %s after 10 s, want %d", notionCalls(env, prefix), prefix, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheDispatcherReadsANotionPagesContentOnlyForTheRunThatItStarts(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	later := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: later, Title: "Some day", Status: "Backlog",
		Select: map[string]string{"Project": "shop"}, Blocks: []fakenotion.Block{{Type: "paragraph", Text: "Not now"}}}, rosaN)
	env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.content_updated", later))
	waitForNotionCalls(t, env, "GET /v1/pages/"+later, 1)
	settle()
	if n := notionCalls(env, "GET /v1/blocks/") + notionCalls(env, "GET /v1/users/"); n != 0 {
		t.Errorf("a webhook for a page in Backlog read %d lists of blocks or users, want none", n)
	}

	page := readyPage(env, db, "Fix the cart total")
	query := "POST /v1/databases/" + db + "/query"
	polls := notionCalls(env, query)
	env.Clock.Advance(dispatcher.NotionPoll) // the poll that was due before the webhook
	waitForRuns(t, env, 1)
	if n := notionCalls(env, "GET /v1/blocks/"+page); n != 1 {
		t.Errorf("the start of the Run read the page's content %d times, want 1", n)
	}
	// The page is still in the ready status, and its edit has a Run: the
	// next poll does not read its content again.
	blocks := notionCalls(env, "GET /v1/blocks/")
	polls = notionCalls(env, query)
	env.Clock.Advance(dispatcher.NotionBackstop)
	waitForNotionCalls(t, env, query, polls+1)
	settle()
	if n := notionCalls(env, "GET /v1/blocks/"); n != blocks {
		t.Errorf("a poll read %d lists of blocks of a page whose edit has a Run, want none", n-blocks)
	}
}

func TestANotionPageWhoseContentNotionDoesNotGiveDoesNotStopTheOtherReadyPages(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	first := readyPage(env, db, "Fix the cart total")
	second := readyPage(env, db, "Fix the coupon")
	env.Notion.FailNext("GET /v1/blocks/"+second, http.StatusInternalServerError, 1)
	env.Clock.Advance(dispatcher.NotionPoll)
	if run := waitForRuns(t, env, 1)[0]; run.Link != fakenotion.PageURL(first) {
		t.Errorf("the Run's link is %q, want the page whose content Notion gave", run.Link)
	}
	env.Clock.Advance(dispatcher.NotionPoll)
	waitForRuns(t, env, 2)
}
