package wrapper_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// A trigger comment on the ticket continues a Run that waits on a
// person. The operator's trigger word here is "SolidTrace", so that the
// Dispatcher's own write-back comments ("SolidTrace Factory needs a
// person ...") start with it too: they must not continue the Run.
func TestATriggerCommentOnTheTicketContinuesARunThatWaitsOnAPersonAndTheDispatchersOwnDoesNot(t *testing.T) {
	env := factorytest.New(t, factorytest.Executions, func(e *factorytest.Env) { e.Config.TriggerWord = "SolidTrace" })
	env.Project("shop")
	ana := env.Member("Ana")
	boardPath = env.TrackerConnection("board", trackerSecret)
	routes := []dispatcherapi.Route{{Connection: "board", Match: ""}}
	if a := env.Admin().API("PUT", "/api/v1/projects/shop/settings", dispatcherapi.SaveFactorySettings{
		Repository: "bitbucket.org/example/shop", Connection: factorytest.ForgeConnection, Routes: &routes}); a.Status != http.StatusOK {
		t.Fatalf("save the route: %d %s", a.Status, a.Body)
	}
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "needs-a-person", "--summary", "Two fixes", "--question", "Which one?"),
		`{"fake":"await_message","eof_code":0}`)
	env.Runner.Agent(takeTask, done("Used the second fix"), reply, result, waitForEnd)

	id := ticketRun(t, env, ana)
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	comments := waitForWriteBack(t, env, 2, "Waiting on you")
	if !strings.HasPrefix(comments[1], "SolidTrace ") {
		t.Fatalf("the Dispatcher's comment does not start with the trigger word: %q", comments[1])
	}
	if a := env.TrackerWebhook(boardPath, trackerSecret, "d-own", "T-1"); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	time.Sleep(300 * time.Millisecond)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateWaitingOnPerson || len(eventsOf(env, id, "message_accepted")) != 0 {
		t.Fatalf("the Dispatcher's own comment steered the Run: %s, %v", run.State, eventsOf(env, id, "message_accepted"))
	}

	env.Tracker.AddComment("board", "T-1", "SolidTrace Use the second fix", rosa)
	if a := env.TrackerWebhook(boardPath, trackerSecret, "d-rosa", "T-1"); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	waitForExecutions(t, env, ana, id, 2)
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Used the second fix" || len(run.Executions) != 2 {
		t.Fatalf("the Run is done with %q in %d Executions", run.Summary, len(run.Executions))
	}
	list := eventsOf(env, id, "message_accepted")
	if len(list) != 1 {
		t.Fatalf("%d accepted messages, want 1: %v", len(list), list)
	}
	ev := list[0]
	if ev["message.text"] != "SolidTrace Use the second fix" || ev["actor.kind"] != "tracker" || ev["actor.name"] != "Rosa" ||
		ev["run.continued_from"] != dispatcherapi.StateWaitingOnPerson {
		t.Errorf("the accepted event is %v, want Rosa's text from the tracker, continued from waiting on a person", ev)
	}
}
