package dispatcher_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakenotion"
)

func TestANotionPageThatNoRouteTakesGetsOneCommentForEachEdit(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	page := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: page, Title: "Water the plants", Status: "Ready for agent",
		Select: map[string]string{"Project": "garden"}}, rosaN)
	env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.created", page))
	deadline := time.Now().Add(10 * time.Second)
	for len(env.Notion.Comments(page)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	comments := env.Notion.Comments(page)
	if len(comments) != 1 || !strings.Contains(comments[0].Text, `"Project"`) || !strings.Contains(comments[0].Text, `"shop"`) ||
		comments[0].By != fakenotion.BotID {
		t.Fatalf("the comments on the page are %+v, want one by the integration that names the field Project and the value shop", comments)
	}
	// The same edit again gets no second comment.
	env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.properties_updated", page))
	settle()
	if n := len(env.Notion.Comments(page)); n != 1 {
		t.Fatalf("%d comments after the same edit, want 1", n)
	}

	// Notion's last_edited_time has whole minutes: another value in the
	// same minute is still a new edit, which gets its own comment.
	env.Notion.SetSelect(page, "Project", "orchard", rosaN)
	env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.properties_updated", page))
	deadline = time.Now().Add(10 * time.Second)
	for len(env.Notion.Comments(page)) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(env.Notion.Comments(page)); n != 2 {
		t.Fatalf("%d comments after a new value in the same minute, want 2", n)
	}
	env.Notion.SetSelect(page, "Project", "shop", rosaN)
	env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.properties_updated", page))
	waitForRuns(t, env, 1)
}

// notionKind is the Notion kind against the fake Notion of env, and a
// connection of it with the token of the connection "notion".
func notionKind(env *factorytest.Env, db string) (dispatcher.Tracker, dispatcher.TrackerConnection) {
	kind := dispatcher.NewNotion(env.Notion.URL(), nil)
	settings := kind.Defaults()
	settings.Database = db
	return kind, dispatcher.TrackerConnection{
		WebhookSource: dispatcher.WebhookSource{ID: "c1", Name: "notion", Kind: dispatcher.NotionKind,
			Token: factorytest.NotionToken("notion")},
		Settings: settings,
	}
}

func TestTheStatusAndTheCommentsOfTheNotionKindReachNotionsAPI(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	ctx := context.Background()
	kind, c := notionKind(env, db)
	page := readyPage(env, db, "Fix the cart total")

	if err := kind.SetStatus(ctx, c, page, "Agent working"); err != nil {
		t.Fatalf("set the status: %v", err)
	}
	if got := env.Notion.Status(page); got != "Agent working" {
		t.Errorf("the page's status is %q, want Agent working", got)
	}
	err := kind.SetStatus(ctx, c, page, "Agent wrking")
	if err == nil || !strings.Contains(err.Error(), "Agent wrking") || !strings.Contains(err.Error(), "400") {
		t.Errorf("a status that the database does not have: %v, want Notion's 400 that names it", err)
	}

	// Notion takes at most 2,000 UTF-16 units in each rich text object, and
	// 100 objects: a longer comment is cut into parts, never inside a
	// character.
	long := strings.Repeat("a", 1999) + "😀" + "b"
	for _, body := range []string{"Started: https://solidtrace.example.com/runs/r1", strings.Repeat("x", 2000),
		strings.Repeat("y", 2001), long} {
		if err := kind.Comment(ctx, c, page, body); err != nil {
			t.Fatalf("comment of %d bytes: %v", len(body), err)
		}
	}
	comments := env.Notion.Comments(page)
	wantParts := []int{1, 1, 2, 2}
	for i, comment := range comments {
		if comment.Parts != wantParts[i] {
			t.Errorf("comment %d has %d parts, want %d", i, comment.Parts, wantParts[i])
		}
	}
	if len(comments) != 4 || comments[3].Text != long || comments[2].Text != strings.Repeat("y", 2001) {
		t.Errorf("the comments do not keep their text: %d comments", len(comments))
	}
	if err := kind.Comment(ctx, c, page, strings.Repeat("z", 100*2000+5)); err != nil {
		t.Fatalf("a comment over 100 parts: %v", err)
	}
	comments = env.Notion.Comments(page)
	if last := comments[len(comments)-1]; last.Parts != 100 || !strings.HasSuffix(last.Text, "…") {
		t.Errorf("a comment over 100 parts has %d parts, want 100 that end with …", last.Parts)
	}

	// A page of another database, a trashed page and an id that is not a
	// page are no ticket of the connection.
	elsewhere := fakenotion.NewID()
	env.Notion.Database(elsewhere, statuses, "Project")
	other := fakenotion.NewID()
	env.Notion.Put(elsewhere, fakenotion.Page{ID: other, Title: "Not ours", Status: "Backlog"}, rosaN)
	trashed := readyPage(env, db, "Trashed")
	env.Notion.Trash(trashed)
	for _, id := range []string{other, trashed, fakenotion.NewID(), "../users/me"} {
		if err := kind.SetStatus(ctx, c, id, "Agent working"); !errors.Is(err, dispatcher.ErrNoTicket) {
			t.Errorf("set the status of %s: %v, want ErrNoTicket", id, err)
		}
		if _, err := kind.Ticket(ctx, c, id); !errors.Is(err, dispatcher.ErrNoTicket) {
			t.Errorf("read %s: %v, want ErrNoTicket", id, err)
		}
	}
	if env.Notion.Status(other) != "Backlog" {
		t.Errorf("the page of another database has the status %q", env.Notion.Status(other))
	}
	for _, id := range []string{trashed, fakenotion.NewID(), "../users/me"} {
		if err := kind.Comment(ctx, c, id, "hello"); !errors.Is(err, dispatcher.ErrNoTicket) {
			t.Errorf("comment on %s: %v, want ErrNoTicket", id, err)
		}
	}
}

func TestTheNotionKindSendsTheConnectionsTokenAndSaysWhatToDoWhenNotionRefusesIt(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	ctx := context.Background()
	kind, c := notionKind(env, db)
	page := readyPage(env, db, "Fix the cart total")
	c.Token = "ntn_revoked_token_value"
	_, err := kind.Ticket(ctx, c, page)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "connections set") ||
		strings.Contains(err.Error(), c.Token) {
		t.Errorf("a refused token: %v, want Notion's 401 with the command that saves a new token, and no token", err)
	}

	_, c = notionKind(env, db)
	env.Notion.FailNext("POST /v1/databases/", http.StatusTooManyRequests, 1)
	if _, err := kind.ReadyTickets(ctx, c); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("Notion's rate limit: %v, want an error with 429", err)
	}
	if tickets, err := kind.ReadyTickets(ctx, c); err != nil || len(tickets) != 1 {
		t.Errorf("the next poll: %d tickets, %v", len(tickets), err)
	}
	c.Settings.Database = ""
	if _, err := kind.ReadyTickets(ctx, c); err == nil || !strings.Contains(err.Error(), "names no database") {
		t.Errorf("a connection with no database: %v", err)
	}
}

func TestTheNotionKindReadsEachPageOfTheListAndTheContentOfATicket(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	ctx := context.Background()
	kind, c := notionKind(env, db)
	env.Notion.PageSize = 2
	for range 5 {
		readyPage(env, db, "Ready")
	}
	env.Notion.Put(db, fakenotion.Page{ID: fakenotion.NewID(), Title: "Not ready", Status: "Backlog"}, rosaN)
	tickets, err := kind.ReadyTickets(ctx, c)
	if err != nil || len(tickets) != 5 {
		t.Fatalf("ReadyTickets gave %d tickets and %v, want the 5 ready pages over 3 lists", len(tickets), err)
	}

	// A ticket has no body: the reconcile step reads it only for the
	// Run that it starts. Only a ready ticket has the user's name.
	page := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: page, Title: "Later", Status: "Backlog",
		Blocks: []fakenotion.Block{{Type: "paragraph", Text: "Some day"}}}, rosaN)
	ticket, err := kind.Ticket(ctx, c, strings.ReplaceAll(page, "-", ""))
	if err != nil || ticket.ID != page || ticket.Title != "Later" || ticket.Status != "Backlog" || ticket.Route != "" ||
		ticket.Body != "" || ticket.StatusBy != (dispatcher.TrackerUser{ID: rosaN.ID}) {
		t.Errorf("the ticket is %+v (%v)", ticket, err)
	}
	ready := readyPage(env, db, "Ready")
	if ticket, err := kind.Ticket(ctx, c, ready); err != nil || ticket.Route != "shop" ||
		ticket.StatusBy != (dispatcher.TrackerUser{ID: rosaN.ID, Name: "Rosa"}) {
		t.Errorf("the ready ticket is %+v (%v)", ticket, err)
	}
}

// promptsByPage waits until there are n Runs, and gives the prompt of
// each Run by its page.
func promptsByPage(t *testing.T, env *factorytest.Env, n int) map[string]string {
	t.Helper()
	prompts := map[string]string{}
	for _, run := range waitForRuns(t, env, n) {
		prompts[run.Link] = run.Prompt
	}
	return prompts
}

func TestTheRunOfANotionPageHasThePagesContentAsItsPrompt(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	content := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: content, Title: "Content", Status: "Ready for agent",
		Select: map[string]string{"Project": "shop"}, Blocks: []fakenotion.Block{
			{Type: "heading_2", Text: "Steps"},
			{Type: "numbered_list_item", Text: "Open the cart"},
			{Type: "to_do", Text: "Add a coupon", Checked: true},
			{Type: "to_do", Text: "Pay"},
			{Type: "image"},
			{Type: "code", Text: "total := sum - discount"},
			{Type: "quote", Text: "It is wrong"},
			{Type: "paragraph", Text: "Level 0", Children: []fakenotion.Block{
				{Type: "paragraph", Text: "Level 1", Children: []fakenotion.Block{
					{Type: "paragraph", Text: "Level 2", Children: []fakenotion.Block{
						{Type: "paragraph", Text: "Level 3"}}}}}}},
		}}, rosaN)

	// A body over 64 KiB is cut.
	big := fakenotion.NewID()
	var blocks []fakenotion.Block
	for range 70 {
		blocks = append(blocks, fakenotion.Block{Type: "paragraph", Text: strings.Repeat("é", 600)})
	}
	env.Notion.Put(db, fakenotion.Page{ID: big, Title: "Big", Status: "Ready for agent",
		Select: map[string]string{"Project": "shop"}, Blocks: blocks}, rosaN)

	env.Clock.Advance(dispatcher.NotionPoll)
	prompts := promptsByPage(t, env, 2)
	want := "Content\n\n## Steps\n1. Open the cart\n[x] Add a coupon\n[ ] Pay\n```\ntotal := sum - discount\n```\n> It is wrong\n" +
		"Level 0\n  Level 1\n    Level 2\n\n(The page has more content: read the rest in Notion.)"
	if got := prompts[fakenotion.PageURL(content)]; got != want {
		t.Errorf("the prompt is\n%s\nwant\n%s", got, want)
	}
	if got := prompts[fakenotion.PageURL(big)]; len(got) > 64<<10+100 || !strings.HasSuffix(got, "read the rest in Notion.)") ||
		!strings.HasPrefix(got, "Big\n\néé") {
		t.Errorf("the prompt of a big body has %d bytes, want at most 64 KiB and the note", len(got))
	}
}

func TestANotionStatusPropertyWithAnotherNameAndAUserWhoseNameTheIntegrationCannotRead(t *testing.T) {
	env := factorytest.New(t)
	db := fakenotion.NewID()
	env.Notion.DatabaseWith(db, "Stage", []string{"Backlog", "Agent ready"}, "App")
	env.Notion.RefuseUsers()
	env.NotionConnection("notion", "", db)
	if a := env.Admin().API("PUT", "/api/v1/connections/notion/tracker", dispatcherapi.TrackerSettings{
		ReadyStatus: "Agent ready", RoutingField: "App", Database: db}); a.Status != http.StatusOK {
		t.Fatalf("save the tracker settings: %d %s", a.Status, a.Body)
	}
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("notion", "shop"))
	page := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: page, Title: "Fix it", Status: "Agent ready",
		Select: map[string]string{"App": "shop"}}, rosaN)
	env.Clock.Advance(dispatcher.NotionPoll)
	run := waitForRuns(t, env, 1)[0]
	if run.StartedBy.ID != rosaN.ID || run.StartedBy.Name != rosaN.ID {
		t.Errorf("the Run was started by %+v, want Rosa's id as her name", run.StartedBy)
	}
}

func TestAnAdminGivesTheNotionDatabaseAsItsIDOrItsLink(t *testing.T) {
	env := factorytest.New(t)
	db := fakenotion.NewID()
	env.Notion.Database(db, statuses, "Project")
	env.NotionConnection("notion", "", db)
	compact := strings.ReplaceAll(db, "-", "")
	for _, given := range []string{compact, strings.ToUpper(compact), "https://www.notion.so/acme/Tickets-" + compact + "?v=" +
		strings.Repeat("f", 32), "https://www.notion.so/" + compact} {
		a := env.Admin().API("PUT", "/api/v1/connections/notion/tracker", dispatcherapi.TrackerSettings{Database: given})
		var c dispatcherapi.Connection
		a.JSON(t, &c)
		if a.Status != http.StatusOK || c.Tracker == nil || c.Tracker.Database != db {
			t.Errorf("the database %q: %d, %s, want it kept as %s", given, a.Status, a.Body, db)
		}
	}
	for _, given := range []string{"", "tickets", compact[:31], "https://www.notion.so/acme/Tickets?v=" + compact,
		"ftp://www.notion.so/" + compact, db + "0"} {
		if a := env.Admin().API("PUT", "/api/v1/connections/notion/tracker",
			dispatcherapi.TrackerSettings{Database: given}); a.Status != http.StatusBadRequest {
			t.Errorf("the database %q: %d %s, want 400", given, a.Status, a.Body)
		}
	}
	// Another tracker kind has no database.
	env.TrackerConnection("board", "")
	if a := env.Admin().API("PUT", "/api/v1/connections/board/tracker",
		dispatcherapi.TrackerSettings{Database: compact}); a.Status != http.StatusBadRequest {
		t.Errorf("a database for a fake tracker connection: %d %s, want 400", a.Status, a.Body)
	}
}

func TestTheNotionTokenIsStoredEncrypted(t *testing.T) {
	env := factorytest.New(t)
	const token = "ntn_4217750512345678abcdEFGHijklMNOPqrstUVWX"
	env.Notion.Token(token)
	dana := admin(t, env)
	if r := dana.RunCLI("", token+"\n", "connections", "set", "notion", "--kind", "notion"); r.Code != 0 {
		t.Fatalf("connections set: exit %d: %s", r.Code, r.Stderr)
	}
	r := dana.CLI("connections", "list")
	if r.Code != 0 || !strings.Contains(r.Stdout, "notion") || strings.Contains(r.Stdout, token) {
		t.Errorf("connections list: exit %d\n%s", r.Code, r.Stdout)
	}
	assertNoSecretInDatabase(t, env, token)

	// The Dispatcher opens it for Notion's API.
	db := fakenotion.NewID()
	env.Notion.Database(db, statuses, "Project")
	if a := env.Admin().API("PUT", "/api/v1/connections/notion/tracker", dispatcherapi.TrackerSettings{Database: db}); a.Status != http.StatusOK {
		t.Fatalf("save the database: %d %s", a.Status, a.Body)
	}
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("notion", ""))
	readyPage(env, db, "Fix the cart total")
	env.Clock.Advance(dispatcher.NotionPoll)
	waitForRuns(t, env, 1)
}

// assertNoSecretInDatabase fails when a file of the Dispatcher's database
// holds secret.
func assertNoSecretInDatabase(t *testing.T, env *factorytest.Env, secret string) {
	t.Helper()
	files, _ := filepath.Glob(env.Config.Database + "*")
	if len(files) == 0 {
		t.Fatal("no database file")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("%s holds the secret", filepath.Base(file))
		}
	}
}

func TestThePromptOfANotionPageHasTheTextInItsColumnsSyncedBlocksAndTables(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	shop := map[string]string{"Project": "shop"}
	layout := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: layout, Title: "Layout", Status: "Ready for agent", Select: shop,
		Blocks: []fakenotion.Block{
			{Type: "paragraph", Text: "Intro"},
			{Type: "column_list", Children: []fakenotion.Block{
				{Type: "column", Children: []fakenotion.Block{{Type: "paragraph", Text: "In a column"}}},
				{Type: "column", Children: []fakenotion.Block{{Type: "bulleted_list_item", Text: "In the other column"}}},
			}},
			{Type: "synced_block", Children: []fakenotion.Block{{Type: "paragraph", Text: "Synced"}}},
			{Type: "toggle", Children: []fakenotion.Block{{Type: "paragraph", Text: "In a toggle"}}},
			{Type: "table", Children: []fakenotion.Block{
				{Type: "table_row", Cells: []string{"Given", "Then"}},
				{Type: "table_row", Cells: []string{"a cart", "the total"}},
			}},
			// A child page is another ticket.
			{Type: "child_page", Children: []fakenotion.Block{{Type: "paragraph", Text: "Another ticket"}}},
		}}, rosaN)
	// The text of a block type that the kind cannot read gets the note.
	unknown := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: unknown, Title: "Template", Status: "Ready for agent", Select: shop,
		Blocks: []fakenotion.Block{{Type: "paragraph", Text: "Seen"}, {Type: "template", Text: "Not seen"}}}, rosaN)

	env.Clock.Advance(dispatcher.NotionPoll)
	prompts := promptsByPage(t, env, 2)
	want := "Layout\n\nIntro\nIn a column\n- In the other column\nSynced\nIn a toggle\n| Given | Then |\n| a cart | the total |"
	if got := prompts[fakenotion.PageURL(layout)]; got != want {
		t.Errorf("the prompt is\n%s\nwant\n%s", got, want)
	}
	want = "Template\n\nSeen\n\n(The page has more content: read the rest in Notion.)"
	if got := prompts[fakenotion.PageURL(unknown)]; got != want {
		t.Errorf("the prompt is\n%s\nwant\n%s", got, want)
	}
}
