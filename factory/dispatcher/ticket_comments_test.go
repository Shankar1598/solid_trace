package dispatcher_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakelinear"
	"github.com/solidtrace/factory/internal/fakenotion"
	"github.com/solidtrace/factory/internal/faketracker"
)

// Comments on the ticket of a Run are messages to the Run. With no
// Runner, a Run stays queued, and a queued Run keeps a message for its
// next Execution: the accepted event shows each message. The wrapper's
// tests continue a Run that waits on a person.

// acceptedMessages are the message_accepted events of the Run.
func acceptedMessages(env *factorytest.Env, runID string) []map[string]any {
	var list []map[string]any
	for _, ev := range env.RunEvents(runID) {
		if ev["event.kind"] == "message_accepted" {
			list = append(list, ev)
		}
	}
	return list
}

// waitForAccepted waits up to 10 s until the Run has n accepted messages,
// then checks that no more arrive, and gives them.
func waitForAccepted(t *testing.T, env *factorytest.Env, runID string, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(acceptedMessages(env, runID)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("the Run has %d accepted messages after 10 s, want %d: %v\nDispatcher log:\n%s",
				len(acceptedMessages(env, runID)), n, acceptedMessages(env, runID), env.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
	settle()
	list := acceptedMessages(env, runID)
	if len(list) != n {
		t.Fatalf("the Run has %d accepted messages, want %d: %v", len(list), n, list)
	}
	return list
}

// wantMessage checks an accepted message's text and its tracker actor.
func wantMessage(t *testing.T, ev map[string]any, text, id, name string) {
	t.Helper()
	if ev["message.text"] != text || ev["actor.kind"] != "tracker" || ev["actor.id"] != id || ev["actor.name"] != name {
		t.Errorf("the accepted message is %q by %v %v %v, want %q by the tracker user %s %s", ev["message.text"],
			ev["actor.kind"], ev["actor.id"], ev["actor.name"], text, id, name)
	}
}

// boardRun puts the ticket T-1 of board in the ready status, announces
// it, and gives its queued Run's id.
func boardRun(t *testing.T, env *factorytest.Env, path string) string {
	t.Helper()
	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Fix the cart total", Status: faketracker.ReadyStatus}, rosa)
	if a := env.TrackerWebhook(path, secret, "d-ready", "T-1"); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	return waitForRuns(t, env, 1)[0].ID
}

func TestATriggerCommentOnTheTicketIsAMessageToItsRunOneTimeAndAgainAfterAnEdit(t *testing.T) {
	env, path := trackerEnv(t)
	id := boardRun(t, env, path)

	env.Tracker.AddComment("board", "T-1", "Looks good, but see the tax table", rosa)
	env.Tracker.AddComment("board", "T-1", "/agents are fun", rosa)
	env.Tracker.AddComment("board", "T-1", "Please /agent do it", rosa)
	comment := env.Tracker.AddComment("board", "T-1", "  /agent Also fix the tax\n", rosa)
	if a := env.TrackerWebhook(path, secret, "d-c1", "T-1"); a.Status != http.StatusOK {
		t.Fatalf("the comment's webhook: %d %s", a.Status, a.Body)
	}
	ev := waitForAccepted(t, env, id, 1)[0]
	wantMessage(t, ev, "/agent Also fix the tax", "u-7", "Rosa")
	if ev["factory.connection"] != "board" || ev["factory.ticket"] != "T-1" || ev["factory.comment"] != comment ||
		ev["message.source"] != "ticket_comment" {
		t.Errorf("the accepted event does not name the ticket comment: %v", ev)
	}

	// The same comment, announced again and found by the backstop, is
	// not a second message.
	env.TrackerWebhook(path, secret, "d-c2", "T-1")
	env.Clock.Advance(faketracker.Backstop + time.Second)
	settle()
	waitForAccepted(t, env, id, 1)

	// An edit sends it again.
	env.Tracker.EditComment("board", comment, "/agent Also fix the tax, and the shipping")
	env.TrackerWebhook(path, secret, "d-c3", "T-1")
	wantMessage(t, waitForAccepted(t, env, id, 2)[1], "/agent Also fix the tax, and the shipping", "u-7", "Rosa")
}

func TestABotsTriggerCommentDoesNothing(t *testing.T) {
	env, path := trackerEnv(t)
	id := boardRun(t, env, path)
	env.Tracker.AddComment("board", "T-1", "/agent delete the tests", faketracker.User{ID: "b-1", Name: "Ci bot", Bot: true})
	env.Tracker.AddComment("board", "T-1", "/agent Use the second fix", rosa)
	env.TrackerWebhook(path, secret, "d-c1", "T-1")
	wantMessage(t, waitForAccepted(t, env, id, 1)[0], "/agent Use the second fix", "u-7", "Rosa")
	if !strings.Contains(env.Logs(), "cannot steer its Run") || !strings.Contains(env.Logs(), "a bot wrote it") {
		t.Errorf("no log of the refused comment:\n%s", env.Logs())
	}
}

func TestATriggerCommentFromBeforeTheRunIsNotForIt(t *testing.T) {
	env, path := trackerEnv(t)
	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Fix the cart total", Status: "Backlog"}, rosa)
	env.Tracker.AddComment("board", "T-1", "/agent an old note", rosa)
	env.Clock.Advance(2 * time.Minute)
	id := boardRun(t, env, path)
	env.Tracker.AddComment("board", "T-1", "/agent a new note", rosa)
	env.TrackerWebhook(path, secret, "d-c1", "T-1")
	wantMessage(t, waitForAccepted(t, env, id, 1)[0], "/agent a new note", "u-7", "Rosa")
}

func TestWithNoWebhookTheBackstopFindsANewTriggerComment(t *testing.T) {
	env, path := trackerEnv(t)
	id := boardRun(t, env, path)
	settle() // the first reconcile step of the new connection
	env.Tracker.AddComment("board", "T-1", "/agent Also fix the tax", rosa)
	env.Clock.Advance(faketracker.Backstop - time.Second)
	settle()
	if n := len(acceptedMessages(env, id)); n != 0 {
		t.Fatalf("%d accepted messages before the backstop", n)
	}
	env.Clock.Advance(2 * time.Second)
	wantMessage(t, waitForAccepted(t, env, id, 1)[0], "/agent Also fix the tax", "u-7", "Rosa")
}

func TestTheOperatorChangesTheTriggerWord(t *testing.T) {
	env := factorytest.New(t, func(e *factorytest.Env) { e.Config.TriggerWord = "@factory" })
	path := env.TrackerConnection("board", secret)
	env.Project("shop")
	mustSaveRoutes(t, env, "shop", route("board", ""))
	id := boardRun(t, env, path)
	env.Tracker.AddComment("board", "T-1", "/agent Also fix the tax", rosa)
	env.Tracker.AddComment("board", "T-1", "@factory Also fix the shipping", rosa)
	env.TrackerWebhook(path, secret, "d-c1", "T-1")
	wantMessage(t, waitForAccepted(t, env, id, 1)[0], "@factory Also fix the shipping", "u-7", "Rosa")
}

func TestTheTriggerWordIsOneWordAndSlashAgentByDefault(t *testing.T) {
	valid := map[string]string{
		dispatcher.EnvSolidTraceURL:   "https://console.example.com",
		dispatcher.EnvSolidTraceToken: "stk_x",
		dispatcher.EnvDatabase:        "/var/lib/dispatcher/d.sqlite3",
		dispatcher.EnvEncryptionKey:   "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	}
	load := func(value string) (dispatcher.Config, error) {
		env := map[string]string{dispatcher.EnvTriggerWord: value}
		for k, v := range valid {
			env[k] = v
		}
		return dispatcher.LoadConfig("", func(k string) string { return env[k] })
	}
	if c, err := load(" @factory "); err != nil || c.TriggerWord != "@factory" {
		t.Errorf("@factory: %q, %v", c.TriggerWord, err)
	}
	if _, err := load("hey factory"); err == nil || !strings.Contains(err.Error(), dispatcher.EnvTriggerWord) {
		t.Errorf("a word with a space: the error %v does not name %s", err, dispatcher.EnvTriggerWord)
	}
	if dispatcher.DefaultTriggerWord != "/agent" {
		t.Errorf("the default trigger word is %q", dispatcher.DefaultTriggerWord)
	}
}

// Notion.

// notionRun puts a ready page (public or not) and announces it, and gives
// the page and its queued Run's id.
func notionRun(t *testing.T, env *factorytest.Env, db, path string, public bool) (string, string) {
	t.Helper()
	page := fakenotion.NewID()
	env.Notion.Put(db, fakenotion.Page{ID: page, Title: "Fix the cart total", Status: "Ready for agent",
		Select: map[string]string{"Project": "shop"}, Public: public}, rosaN)
	if a := env.NotionWebhook(path, secret, fakenotion.PageEvent(fakenotion.NewID(), "page.properties_updated", page)); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	return page, waitForRuns(t, env, 1)[0].ID
}

func notionComment(t *testing.T, env *factorytest.Env, path, page string) {
	t.Helper()
	if a := env.NotionWebhook(path, secret, fakenotion.CommentEvent(fakenotion.NewID(), page)); a.Status != http.StatusOK {
		t.Fatalf("the comment's webhook: %d %s", a.Status, a.Body)
	}
}

func TestANotionTriggerCommentByAPersonOrAGuestIsAMessageAndByABotIsNot(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	guest := fakenotion.User{ID: "u0000000-0000-4000-8000-0000000g0e57", Name: "Gita (guest)"}
	other := fakenotion.User{ID: "b0000000-0000-4000-8000-0000000000b2", Name: "Other integration", Bot: true}
	env.Notion.AddUser(guest)
	env.Notion.AddUser(other)
	page, id := notionRun(t, env, db, path, false)

	env.Notion.AddComment(page, "/agent from the integration itself", fakenotion.User{ID: fakenotion.BotID})
	env.Notion.AddComment(page, "/agent from another integration", other)
	env.Notion.AddComment(page, "/agent Use the second fix", rosaN)
	env.Notion.AddComment(page, "no trigger word", rosaN)
	env.Notion.AddComment(page, "/agent And the shipping", guest)
	notionComment(t, env, path, page)
	list := waitForAccepted(t, env, id, 2)
	wantMessage(t, list[0], "/agent Use the second fix", rosaN.ID, "Rosa")
	wantMessage(t, list[1], "/agent And the shipping", guest.ID, "Gita (guest)")
}

func TestATriggerCommentOnANotionPagePublishedToTheWebDoesNothing(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	page, id := notionRun(t, env, db, path, true)
	env.Notion.AddComment(page, "/agent Use the second fix", rosaN)
	notionComment(t, env, path, page)
	waitForNotionCalls(t, env, "GET /v1/comments", 2) // the ready page's step, then the comment's
	settle()
	if n := len(acceptedMessages(env, id)); n != 0 {
		t.Errorf("a comment on a public page is %d messages", n)
	}
	if !strings.Contains(env.Logs(), "the page is published to the web") {
		t.Errorf("no log of the refused comment:\n%s", env.Logs())
	}
}

func TestANotionTriggerCommentWhoseAuthorTheIntegrationCannotReadDoesNothing(t *testing.T) {
	env, db, path := notionEnv(t, secret)
	page, id := notionRun(t, env, db, path, false)
	env.Notion.RefuseUsers()
	someone := fakenotion.User{ID: "u0000000-0000-4000-8000-00000000a11e", Name: "Alex"}
	env.Notion.AddUser(someone)
	env.Notion.AddComment(page, "/agent Use the second fix", someone)
	notionComment(t, env, path, page)
	waitForNotionCalls(t, env, "GET /v1/comments", 2)
	settle()
	if n := len(acceptedMessages(env, id)); n != 0 {
		t.Errorf("a comment of an author that the Dispatcher cannot read is %d messages", n)
	}
	if !strings.Contains(env.Logs(), "capability to read user information") {
		t.Errorf("no log that names the capability:\n%s", env.Logs())
	}
}

func TestWithNoWebhookTheNotionPollFindsANewTriggerCommentIn30Seconds(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	page := readyPage(env, db, "Fix the cart total")
	env.Clock.Advance(dispatcher.NotionPoll)
	id := waitForRuns(t, env, 1)[0].ID
	env.Notion.AddComment(page, "/agent Use the second fix", rosaN)
	env.Clock.Advance(dispatcher.NotionPoll)
	wantMessage(t, waitForAccepted(t, env, id, 1)[0], "/agent Use the second fix", rosaN.ID, "Rosa")
}

// Linear.

func linearRun(t *testing.T, env *factorytest.Env, path string) string {
	t.Helper()
	readyTicket(t, env, path, "issue-1")
	return waitForRuns(t, env, 1)[0].ID
}

func TestALinearTriggerCommentByAMemberOrAGuestIsAMessageAndByAnAppABotOrAnExternalUserIsNot(t *testing.T) {
	env, path := linearEnv(t)
	id := linearRun(t, env, path)
	guest := fakelinear.User{ID: "lin-gita", Name: "Gita", Guest: true}
	app := fakelinear.App
	otherApp := fakelinear.User{ID: "app-other", Name: "Other app", App: true}

	env.Linear.AddComment("issue-1", "/agent from the app itself", fakelinear.By{User: &app})
	env.Linear.AddComment("issue-1", "/agent from another app", fakelinear.By{User: &otherApp})
	env.Linear.AddComment("issue-1", "/agent from an integration", fakelinear.By{Bot: "Zapier"})
	env.Linear.AddComment("issue-1", "/agent from a customer", fakelinear.By{External: "Customer"})
	env.Linear.AddComment("issue-1", "/agent Use the second fix", fakelinear.By{User: &rosaOnLinear})
	env.Linear.AddComment("issue-1", "/agent And the shipping", fakelinear.By{User: &guest})
	if a := env.LinearWebhook(path, linearSecret, "d-c1", "Comment", "issue-1"); a.Status != http.StatusOK {
		t.Fatalf("the comment's webhook: %d %s", a.Status, a.Body)
	}
	list := waitForAccepted(t, env, id, 2)
	wantMessage(t, list[0], "/agent Use the second fix", "lin-rosa", "Rosa")
	wantMessage(t, list[1], "/agent And the shipping", "lin-gita", "Gita")
	for _, reason := range []string{"the connection's own identity wrote it", "an app wrote it",
		"a bot or an integration wrote it", "an external user wrote it"} {
		if !strings.Contains(env.Logs(), reason) {
			t.Errorf("no log with %q:\n%s", reason, env.Logs())
		}
	}
}

func TestTheLinearBackstopFindsATriggerCommentThatNoWebhookAnnounced(t *testing.T) {
	env, path := linearEnv(t)
	id := linearRun(t, env, path)
	// A backstop after the ready ticket's webhook: the webhook announced
	// the ticket until then.
	env.Clock.Advance(5*time.Minute + time.Second)
	settle()
	env.Linear.AddComment("issue-1", "/agent Use the second fix", fakelinear.By{User: &rosaOnLinear})
	env.Clock.Advance(5*time.Minute + time.Second)
	wantMessage(t, waitForAccepted(t, env, id, 1)[0], "/agent Use the second fix", "lin-rosa", "Rosa")
	if w := env.WaitForWarning("found a change that no webhook announced"); w["factory.entities"] != "issue-1" {
		t.Errorf("the warning is %v, want the ticket issue-1", w)
	}
}

// In poll mode, the poll every 30 seconds reads the comments of the
// tickets whose Run is Active. A Run that is not Active and not Closed
// (done, failed or stopped) still continues on a trigger comment, but
// the Dispatcher reads its ticket's comments only every 5 minutes, so
// that the poll keeps in Notion's rate limit.
func TestInPollModeTheCommentsOfATicketWhoseRunIsNotActiveAreReadEvery5Minutes(t *testing.T) {
	env, db, _ := notionEnv(t, "")
	page := readyPage(env, db, "Fix the cart total")
	env.Clock.Advance(dispatcher.NotionPoll)
	id := waitForRuns(t, env, 1)[0].ID
	if got := env.Member("Ana").CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	env.Notion.AddComment(page, "/agent Use the second fix", rosaN)
	settle()
	before := notionCalls(env, "GET /v1/comments")
	// 4 minutes and 30 seconds after the first step of the connection.
	for range 8 {
		env.Clock.Advance(dispatcher.NotionPoll)
		settle()
	}
	if n := notionCalls(env, "GET /v1/comments") - before; n != 0 {
		t.Errorf("the polls read the comments of a stopped Run's ticket %d times in 4 minutes", n)
	}
	env.Clock.Advance(dispatcher.NotionPoll)
	wantMessage(t, waitForAccepted(t, env, id, 1)[0], "/agent Use the second fix", rosaN.ID, "Rosa")
}
