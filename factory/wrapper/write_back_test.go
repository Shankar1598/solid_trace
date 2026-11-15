package wrapper_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/faketracker"
)

const trackerSecret = "whsec-for-tests"

var rosa = faketracker.User{ID: "u-7", Name: "Rosa"}

// boardPath is the webhook path of the connection board (the tests of
// this package do not run in parallel).
var boardPath string

// trackerEnv is an Env with Executions, the Project shop, and the
// tracker connection board with the default route to shop.
func trackerEnv(t *testing.T) (*factorytest.Env, *factorytest.Person) {
	t.Helper()
	env, ana := newEnv(t)
	boardPath = env.TrackerConnection("board", trackerSecret)
	routes := []dispatcherapi.Route{{Connection: "board", Match: ""}}
	a := env.Admin().API("PUT", "/api/v1/projects/shop/settings", dispatcherapi.SaveFactorySettings{
		Repository: "bitbucket.org/example/shop", Connection: factorytest.ForgeConnection, Routes: &routes})
	if a.Status != http.StatusOK {
		t.Fatalf("save the route: %d %s", a.Status, a.Body)
	}
	return env, ana
}

// ticketRun puts the ticket T-1 in the ready status, announces it, and
// gives the id of the Run that it started.
func ticketRun(t *testing.T, env *factorytest.Env, ana *factorytest.Person) string {
	t.Helper()
	env.Tracker.Put("board", faketracker.Ticket{ID: "T-1", Title: "Fix the cart total",
		Status: faketracker.ReadyStatus}, rosa)
	if a := env.TrackerWebhook(boardPath, trackerSecret, "d-1", "T-1"); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		a := ana.API("GET", "/api/v1/runs", nil)
		var list dispatcherapi.Runs
		a.JSON(t, &list)
		if len(list.Runs) == 1 {
			return list.Runs[0].ID
		}
		if time.Now().After(deadline) {
			t.Fatalf("the ready ticket started %d Runs, want 1\nDispatcher log:\n%s", len(list.Runs), env.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForWriteBack waits up to 10 s until the ticket T-1 has n comments
// and its last status is status, and gives the comments.
func waitForWriteBack(t *testing.T, env *factorytest.Env, n int, status string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var comments []string
		for _, c := range env.Tracker.Comments("board") {
			comments = append(comments, c.Body)
		}
		if len(comments) >= n && env.Tracker.Status("board", "T-1") == status {
			// Nothing more arrives.
			time.Sleep(300 * time.Millisecond)
			if got := env.Tracker.Comments("board"); len(got) != n {
				t.Fatalf("the ticket has %d comments, want %d: %+v", len(got), n, got)
			}
			return comments
		}
		if time.Now().After(deadline) {
			t.Fatalf("the ticket has the status %q and the comments %q, want %q and %d comments\nDispatcher log:\n%s",
				env.Tracker.Status("board", "T-1"), comments, status, n, env.Logs())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func runLink(env *factorytest.Env, id string) string {
	return env.Config.SolidTraceURL + "/" + factorytest.Organization.Slug + "/runs/" + id
}

func wantStatuses(t *testing.T, env *factorytest.Env, want ...string) {
	t.Helper()
	if got := env.Tracker.Statuses("board", "T-1"); !slices.Equal(got, want) {
		t.Errorf("the Dispatcher set the statuses %q, want %q", got, want)
	}
}

func wantComment(t *testing.T, comment string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(comment, p) {
			t.Errorf("the comment does not have %q:\n%s", p, comment)
		}
	}
}

func TestATicketRunIsAgentWorkingThenInReviewWhenThePROpens(t *testing.T) {
	env, ana := trackerEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, commitStep, shell(`solidtrace pr --title "Fix the cart total"`),
		reportStep("--outcome", "done", "--summary", "The total now takes the discount."),
		reply, result, waitForEnd)

	id := ticketRun(t, env, ana)
	started := waitForWriteBack(t, env, 1, "Agent working")
	wantComment(t, started[0], "started", runLink(env, id))
	open()
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	comments := waitForWriteBack(t, env, 2, "In review")
	wantStatuses(t, env, "Agent working", "In review")
	wantComment(t, comments[1], run.PRURL, runLink(env, id), "The total now takes the discount.", "no Evidence")
	if run.PRURL == "" {
		t.Errorf("the Run has no PR")
	}
}

func TestNeedsAPersonSetsWaitingOnYouWithTheQuestion(t *testing.T) {
	env, ana := trackerEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "needs-a-person", "--summary", "Two fixes.", "--question", "Keep the old cart API?"),
		waitForEnd)
	id := ticketRun(t, env, ana)
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)

	comments := waitForWriteBack(t, env, 2, "Waiting on you")
	wantStatuses(t, env, "Agent working", "Waiting on you")
	wantComment(t, comments[0], runLink(env, id))
	wantComment(t, comments[1], "Keep the old cart API?")
}

func TestAFailedRunSetsWaitingOnYouWithTheReason(t *testing.T) {
	env, ana := trackerEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "failed", "--summary", "The cart service does not build on main"),
		reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	comments := waitForWriteBack(t, env, 2, "Waiting on you")
	wantStatuses(t, env, "Agent working", "Waiting on you")
	wantComment(t, comments[1], "failed", "The cart service does not build on main")
}

func TestADoneRunWithNoPRSetsWaitingOnYouWithTheSummary(t *testing.T) {
	env, ana := trackerEnv(t)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "The total was right."),
		reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	comments := waitForWriteBack(t, env, 2, "Waiting on you")
	wantStatuses(t, env, "Agent working", "Waiting on you")
	wantComment(t, comments[1], "no PR", "The total was right.")
}

func TestAStopChangesNoStatusAndPostsNoComment(t *testing.T) {
	env, ana := trackerEnv(t)
	wait, _ := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	waitForWriteBack(t, env, 1, "Agent working")

	if a := ana.API("POST", "/api/v1/runs/"+id+"/stop", nil); a.Status != http.StatusOK {
		t.Fatalf("stop: %d %s", a.Status, a.Body)
	}
	waitFor(t, env, ana, id, dispatcherapi.StateStopped)
	waitForWriteBack(t, env, 1, "Agent working")
	wantStatuses(t, env, "Agent working")
}

func TestTheWriteBackStatusesAreSettingsOfTheConnection(t *testing.T) {
	env, ana := trackerEnv(t)
	a := env.Admin().API("PUT", "/api/v1/connections/board/tracker", dispatcherapi.TrackerSettings{
		WorkingStatus: "Doing", ReviewStatus: "Review", WaitingStatus: "Blocked"})
	var c dispatcherapi.Connection
	a.JSON(t, &c)
	if a.Status != http.StatusOK || c.Tracker == nil || c.Tracker.WaitingStatus != "Blocked" ||
		c.Tracker.ReadyStatus != faketracker.ReadyStatus {
		t.Fatalf("the save of the statuses: %d %s", a.Status, a.Body)
	}
	a = env.Admin().API("PUT", "/api/v1/connections/board/tracker", dispatcherapi.TrackerSettings{
		WaitingStatus: faketracker.ReadyStatus})
	if a.Status != http.StatusBadRequest {
		t.Errorf("a waiting status that is the ready status: %d %s, want 400", a.Status, a.Body)
	}

	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Nothing to fix."),
		reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	waitForWriteBack(t, env, 2, "Blocked")
	wantStatuses(t, env, "Doing", "Blocked")
}

func TestACommentThatFailsIsSentAgainInOrder(t *testing.T) {
	env, ana := trackerEnv(t)
	env.Tracker.RefuseComments("board", "T-1")
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "The total was right."),
		reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	time.Sleep(300 * time.Millisecond)
	if got := env.Tracker.Comments("board"); len(got) != 0 {
		t.Fatalf("refused comments arrived: %+v", got)
	}

	env.Tracker.AcceptComments("board", "T-1")
	comments := waitForWriteBack(t, env, 2, "Waiting on you")
	wantComment(t, comments[0], "started")
	wantComment(t, comments[1], "The total was right.")
	wantStatuses(t, env, "Agent working", "Waiting on you")
}

// prThen is an agent that opens a PR and then runs end: the steps after
// the pr call.
func prThen(t *testing.T, env *factorytest.Env, end ...string) {
	t.Helper()
	steps := append([]string{takeTask, initLine(t), commitStep, shell(`solidtrace pr --title "Fix the cart total"`)},
		end...)
	env.Runner.Agent(steps...)
}

func TestAPRThatNeedsAPersonGetsItsPROpenedComment(t *testing.T) {
	env, ana := trackerEnv(t)
	prThen(t, env, reportStep("--outcome", "needs-a-person", "--summary", "Two fixes.", "--question",
		"Keep the old cart API?"), waitForEnd)
	id := ticketRun(t, env, ana)
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)

	comments := waitForWriteBack(t, env, 3, "Waiting on you")
	wantStatuses(t, env, "Agent working", "In review", "Waiting on you")
	wantComment(t, comments[1], "opened a PR", run.PRURL, runLink(env, id), "Two fixes.")
	wantComment(t, comments[2], "Keep the old cart API?")
}

func TestAPRThatFailsGetsItsPROpenedCommentThenTheReason(t *testing.T) {
	env, ana := trackerEnv(t)
	prThen(t, env, reportStep("--outcome", "failed", "--summary", "The cart service does not build on main"),
		reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	comments := waitForWriteBack(t, env, 3, "Waiting on you")
	wantStatuses(t, env, "Agent working", "In review", "Waiting on you")
	wantComment(t, comments[1], "opened a PR", run.PRURL, runLink(env, id))
	wantComment(t, comments[2], "failed", "The cart service does not build on main")
}

func TestAStopAfterThePRPostsThePROpenedCommentAndNoStatus(t *testing.T) {
	env, ana := trackerEnv(t)
	wait, _ := gate(t)
	prThen(t, env, wait, reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	waitForWriteBack(t, env, 1, "In review")

	if a := ana.API("POST", "/api/v1/runs/"+id+"/stop", nil); a.Status != http.StatusOK {
		t.Fatalf("stop: %d %s", a.Status, a.Body)
	}
	run := waitFor(t, env, ana, id, dispatcherapi.StateStopped)
	comments := waitForWriteBack(t, env, 2, "In review")
	wantStatuses(t, env, "Agent working", "In review")
	wantComment(t, comments[1], "opened a PR", run.PRURL, runLink(env, id))
}
