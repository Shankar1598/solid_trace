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
	refused := map[string]dispatcherapi.TrackerSettings{
		"a working status that is the ready status": {WorkingStatus: faketracker.ReadyStatus},
		"a review status that is the ready status":  {ReviewStatus: faketracker.ReadyStatus},
		"a waiting status that is the ready status": {WaitingStatus: faketracker.ReadyStatus},
		"a ready status that is the default working status": {ReadyStatus: "Agent working",
			WaitingStatus: "Blocked"},
		"a working status of 201 characters":        {WorkingStatus: strings.Repeat("a", 201)},
		"a review status with a new line":           {ReviewStatus: "In\nreview"},
		"a waiting status with a control character": {WaitingStatus: "Wait\x07ing"},
	}
	for name, settings := range refused {
		if a := env.Admin().API("PUT", "/api/v1/connections/board/tracker", settings); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	if a := env.Admin().API("PUT", "/api/v1/connections/board/tracker", dispatcherapi.TrackerSettings{
		WorkingStatus: strings.Repeat("a", 200), ReviewStatus: "Review", WaitingStatus: "Blocked"}); a.Status != http.StatusOK {
		t.Errorf("a working status of 200 characters: %d %s, want 200", a.Status, a.Body)
	}
	a = env.Admin().API("PUT", "/api/v1/connections/board/tracker", dispatcherapi.TrackerSettings{
		WorkingStatus: "Doing", ReviewStatus: "Review", WaitingStatus: "Blocked"})
	if a.Status != http.StatusOK {
		t.Fatalf("save the statuses again: %d %s", a.Status, a.Body)
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

func TestATicketThatRefusesCommentsDoesNotHoldBackAnotherRun(t *testing.T) {
	env, ana := trackerEnv(t)
	env.Tracker.RefuseComments("board", "T-1")
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "The total was right."),
		reply, result, waitForEnd)
	first := ticketRun(t, env, ana)
	waitFor(t, env, ana, first, dispatcherapi.StateDone)
	// The waits of T-1's started step become seconds long.
	time.Sleep(4500 * time.Millisecond)

	env.Tracker.Put("board", faketracker.Ticket{ID: "T-2", Title: "Fix the tax", Status: faketracker.ReadyStatus},
		rosa)
	if a := env.TrackerWebhook(boardPath, trackerSecret, "d-2", "T-2"); a.Status != http.StatusOK {
		t.Fatalf("the webhook: %d %s", a.Status, a.Body)
	}
	started := time.Now()
	for {
		var got []faketracker.Comment
		for _, c := range env.Tracker.Comments("board") {
			if c.Ticket == "T-2" {
				got = append(got, c)
			}
		}
		if len(got) > 0 {
			wantComment(t, got[0].Body, "started")
			break
		}
		if time.Since(started) > 1500*time.Millisecond {
			t.Fatalf("T-2 has no started comment 1.5 s after its webhook\nDispatcher log:\n%s", env.Logs())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := env.Tracker.Comments("board"); len(got) != 1 {
		t.Errorf("the comments are %+v, want only the started comment of T-2", got)
	}
}

// waitForLog waits up to 10 s until the Dispatcher's log has part, and
// gives the log.
func waitForLog(t *testing.T, env *factorytest.Env, part string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if logs := env.Logs(); strings.Contains(logs, part) {
			return logs
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Dispatcher's log does not have %q:\n%s", part, env.Logs())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

const gaveUp = "the Dispatcher gave it up"

func TestAStepOfADeletedTicketIsGivenUpAtOnce(t *testing.T) {
	env, ana := trackerEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reportStep("--outcome", "done", "--summary", "The total was right."),
		reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	waitForWriteBack(t, env, 1, "Agent working")

	env.Tracker.Delete("board", "T-1")
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	logs := waitForLog(t, env, gaveUp)
	// The waits of a retry are 250 ms and more: a retry comes in this
	// time.
	time.Sleep(600 * time.Millisecond)
	logs = env.Logs()
	if n := strings.Count(logs, gaveUp); n != 1 || strings.Contains(logs, "tries again") {
		t.Errorf("the step of the deleted ticket was given up %d times, want 1 and no retry:\n%s", n, logs)
	}
	if got := env.Tracker.Comments("board"); len(got) != 1 {
		t.Errorf("the comments are %+v, want only the started comment", got)
	}
}

func TestAStepThatFailsForADayIsGivenUp(t *testing.T) {
	env, ana := trackerEnv(t)
	env.Tracker.RefuseComments("board", "T-1")
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "The total was right."),
		reply, result, waitForEnd)
	id := ticketRun(t, env, ana)
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	waitForLog(t, env, "tries again")
	if strings.Contains(env.Logs(), gaveUp) {
		t.Fatalf("a step was given up before a day:\n%s", env.Logs())
	}

	env.Clock.Advance(24 * time.Hour)
	waitForLog(t, env, gaveUp)
	env.Tracker.AcceptComments("board", "T-1")
	time.Sleep(1500 * time.Millisecond)
	if got := env.Tracker.Comments("board"); len(got) != 0 {
		t.Errorf("a given-up step was sent: %+v", got)
	}
}
