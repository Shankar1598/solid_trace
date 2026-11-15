package wrapper_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakeforge"
)

// The Dispatcher checks the PRs of Active Runs every 5 minutes of its
// clock, and closes a Run whose PR is merged or declined, or that had no
// activity for 30 days. A Closed Run keeps its last state.

const (
	prCheckEvery = 5 * time.Minute
	closeAfter   = 30 * 24 * time.Hour
)

// runWithPR runs a Run whose agent opens a PR and reports done, and gives
// the Run id and its PR on the fake forge.
func runWithPR(t *testing.T, env *factorytest.Env, ana *factorytest.Person) (string, fakeforge.PR) {
	t.Helper()
	out := t.TempDir()
	env.Runner.Agent(takeTask, initLine(t), commitStep, recorded(out, "pr", `solidtrace pr --title "Fix the cart"`),
		reportStep("--outcome", "done", "--summary", "Fixed the cart test"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the flaky cart test")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	prs := env.Forge.PRs("example/shop")
	if len(prs) != 1 {
		t.Fatalf("want one PR on the forge, got %+v\npr: %s", prs, readRecorded(t, out, "pr"))
	}
	return id, prs[len(prs)-1]
}

// addWIPRef gives the Run a work-in-progress ref on the fake forge, as a
// save of the Run's work makes.
func addWIPRef(t *testing.T, env *factorytest.Env, id string) string {
	t.Helper()
	main, ok := env.Forge.Ref("example/shop", "refs/heads/main")
	if !ok {
		t.Fatal("the repository has no main branch")
	}
	ref := "refs/agent-wip/" + id
	env.Forge.Git("example/shop", "update-ref", ref, main)
	return ref
}

// waitForKind waits up to 10 s until SolidTrace has a run event of kind
// for the Run, and gives the Run's events.
func waitForKind(t *testing.T, env *factorytest.Env, id, kind string) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		events := env.RunEvents(id)
		if len(ofKind(events, kind)) > 0 {
			return events
		}
		if time.Now().After(deadline) {
			t.Fatalf("SolidTrace has no %s event of the Run after 10 s: %v\nDispatcher log:\n%s", kind, events, env.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ofKind are the events of kind.
func ofKind(events []map[string]any, kind string) []map[string]any {
	var got []map[string]any
	for _, ev := range events {
		if ev["event.kind"] == kind {
			got = append(got, ev)
		}
	}
	return got
}

// notClosed checks that the Run is not closed and has no PR outcome, in
// the API and in its events.
func notClosed(t *testing.T, env *factorytest.Env, ana *factorytest.Person, id, when string) {
	t.Helper()
	run := getRun(t, ana, id)
	if run.ClosedAt != nil || run.PROutcome != "" {
		t.Errorf("%s: the Run is closed at %v with the PR outcome %q", when, run.ClosedAt, run.PROutcome)
	}
	events := env.RunEvents(id)
	if got := append(ofKind(events, "pr_outcome"), ofKind(events, "closed")...); len(got) > 0 {
		t.Errorf("%s: the Run has the events %v", when, got)
	}
}

// closedEvent checks that the Run's last two events are the PR outcome
// (when outcome is not "") and the close, and gives the close.
func closedEvent(t *testing.T, env *factorytest.Env, id, state, outcome, reason string) map[string]any {
	t.Helper()
	events := waitForKind(t, env, id, "closed")
	last := events[len(events)-1]
	if last["event.kind"] != "closed" {
		t.Fatalf("the last event is %v, not closed: %v", last["event.kind"], events)
	}
	if last["run.state"] != state || last["run.pr.outcome"] != outcome || last["run.close_reason"] != reason {
		t.Errorf("the closed event has state %v, PR outcome %v and reason %v; want %s, %q and %s",
			last["run.state"], last["run.pr.outcome"], last["run.close_reason"], state, outcome, reason)
	}
	got := ofKind(events, "pr_outcome")
	switch {
	case outcome == "" && len(got) != 0:
		t.Errorf("a close with no PR outcome has the PR outcome events %v", got)
	case outcome != "" && len(got) != 1:
		t.Errorf("want one PR outcome event, got %v", got)
	case outcome != "":
		if events[len(events)-2]["event.kind"] != "pr_outcome" || got[0]["run.pr.outcome"] != outcome ||
			got[0]["pr.outcome"] != outcome {
			t.Errorf("the PR outcome event is not just before the close, or not %s: %v", outcome, events)
		}
	}
	return last
}

func TestAMergedPRGivesThePROutcomeAtTheNextCheckAndClosesTheRun(t *testing.T) {
	env, ana := newEnv(t)
	id, pr := runWithPR(t, env, ana)
	wip := addWIPRef(t, env, id)
	env.Forge.SetState("example/shop", pr.ID, "MERGED")

	// The Dispatcher checked the PRs when it started, at this clock time:
	// the next check is 5 minutes later.
	env.CheckClosedRuns()
	env.Clock.Advance(prCheckEvery - time.Second)
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "1 s before the next check")
	if _, ok := env.Forge.Ref("example/shop", wip); !ok {
		t.Errorf("the WIP ref of a Run that is not closed was deleted")
	}

	env.Clock.Advance(time.Second)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "merged", "pr_merged")
	run := getRun(t, ana, id)
	if run.PROutcome != "merged" || run.ClosedAt == nil || !run.ClosedAt.Equal(env.Clock.Now()) {
		t.Errorf("the Run has the PR outcome %q and is closed at %v, want merged at %s", run.PROutcome, run.ClosedAt,
			env.Clock.Now())
	}
	if run.State != dispatcherapi.StateDone {
		t.Errorf("the Closed Run is %s, not done: it keeps its last state", run.State)
	}
	if _, ok := env.Forge.Ref("example/shop", wip); ok {
		t.Errorf("the WIP ref %s of the Closed Run is not deleted", wip)
	}
	if got := ana.CLI("runs", "get", id); !strings.Contains(got.Stdout, "merged") || !strings.Contains(got.Stdout, "Closed:") {
		t.Errorf("runs get does not show the PR outcome and the close:\n%s", got.Stdout)
	}

	// A Closed Run is not checked again, and closes one time only.
	count := len(env.RunEvents(id))
	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()
	if events := env.RunEvents(id); len(events) != count {
		t.Errorf("the Closed Run got more events: %v", events[count:])
	}
}

func TestADeclinedPRClosesTheRunAfterAForgeFault(t *testing.T) {
	for _, state := range []string{"DECLINED", "SUPERSEDED"} {
		t.Run(state, func(t *testing.T) {
			env, ana := newEnv(t)
			id, pr := runWithPR(t, env, ana)
			env.Forge.SetState("example/shop", pr.ID, state)

			// The forge fails the check: the Run stays as it is, and the
			// next check records the outcome.
			env.Forge.FailNext(http.StatusInternalServerError, "Something went wrong")
			env.Clock.Advance(prCheckEvery)
			env.CheckClosedRuns()
			notClosed(t, env, ana, id, "after a check that the forge failed")

			env.Clock.Advance(prCheckEvery)
			env.CheckClosedRuns()
			closedEvent(t, env, id, dispatcherapi.StateDone, "declined", "pr_declined")
			if run := getRun(t, ana, id); run.PROutcome != "declined" || run.ClosedAt == nil {
				t.Errorf("the Run has the PR outcome %q, closed at %v; want declined", run.PROutcome, run.ClosedAt)
			}
		})
	}
}

func TestAnOpenPRKeepsTheRunOpenUntil30DaysWithNoActivity(t *testing.T) {
	env, ana := newEnv(t)
	id, _ := runWithPR(t, env, ana)
	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "with an open PR")

	env.Clock.Advance(closeAfter - prCheckEvery)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
}

func TestARunDoneWithNoPRClosesWhen30DaysPassWithNoActivity(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reportStep("--outcome", "done", "--summary", "Nothing to fix"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the flaky cart test")
	waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	// The Execution ends 10 minutes after the Run's start.
	const works = 10 * time.Minute
	env.Clock.Advance(works)
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	wip := addWIPRef(t, env, id)

	// The 30 days count from the end of the Execution, not from the start.
	env.Clock.Advance(closeAfter - works)
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "30 days after the Run's start")

	// The end of the Execution was the last activity.
	env.Clock.Advance(works - time.Second)
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "1 s before 30 days after the end of the Execution")

	env.Clock.Advance(time.Second)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
	run := getRun(t, ana, id)
	if run.ClosedAt == nil || run.State != dispatcherapi.StateDone || run.PROutcome != "" {
		t.Errorf("the Run is %s, closed at %v, PR outcome %q; want done and closed", run.State, run.ClosedAt, run.PROutcome)
	}
	if _, ok := env.Forge.Ref("example/shop", wip); ok {
		t.Errorf("the WIP ref %s of the Closed Run is not deleted", wip)
	}
}

func TestARunThatWaitsOnAPersonClosesAfter30DaysAndAStopKeepsItsState(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "needs-a-person", "--summary", "Two fixes", "--question", "Which one?"),
		`{"fake":"await_message","eof_code":0}`)
	id := start(t, env, ana, "Fix the flaky cart test")
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)

	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateWaitingOnPerson, "", "no_activity")

	a := ana.API("POST", "/api/v1/runs/"+id+"/stop", nil)
	if a.Status != http.StatusConflict || !strings.Contains(string(a.Body), "closed") {
		t.Errorf("a stop of a Closed Run: %d %s, want 409 that says it is closed", a.Status, a.Body)
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateWaitingOnPerson || run.ClosedAt == nil {
		t.Errorf("the Closed Run is %s, closed at %v; want waiting on a person and closed", run.State, run.ClosedAt)
	}
}

func TestTheWIPRefDeleteIsTriedAgainAtTheNextCheck(t *testing.T) {
	env, ana := newEnv(t)
	id, pr := runWithPR(t, env, ana)
	wip := addWIPRef(t, env, id)
	env.Forge.SetState("example/shop", pr.ID, "MERGED")
	env.Forge.RefusePushes("example/shop", "the forge is read-only now")

	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "merged", "pr_merged")
	if _, ok := env.Forge.Ref("example/shop", wip); !ok {
		t.Fatalf("the forge refused the push, but the WIP ref is gone")
	}

	env.Forge.AcceptPushes("example/shop")
	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	if _, ok := env.Forge.Ref("example/shop", wip); ok {
		t.Errorf("the WIP ref %s is not deleted at the check after the forge accepts pushes again", wip)
	}
}

func TestTheDispatcherStopsTryingToDeleteAWIPRefADayAfterTheClose(t *testing.T) {
	env, ana := newEnv(t)
	id, pr := runWithPR(t, env, ana)
	wip := addWIPRef(t, env, id)
	env.Forge.SetState("example/shop", pr.ID, "DECLINED")
	env.Forge.RefusePushes("example/shop", "the forge is read-only now")
	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "declined", "pr_declined")

	env.Clock.Advance(24 * time.Hour)
	env.CheckClosedRuns()
	warning := env.WaitForWarning("could not delete the work-in-progress ref")
	if message, _ := warning["message"].(string); !strings.Contains(message, id) {
		t.Errorf("the warning does not name the Run %s: %v", id, warning)
	}

	env.Forge.AcceptPushes("example/shop")
	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	if _, ok := env.Forge.Ref("example/shop", wip); !ok {
		t.Errorf("the Dispatcher still deletes the WIP ref after it gave up")
	}
}

// An open PR keeps the Run active, so its dedupe_key starts no Run; when
// the PR is merged, the Run is closed and a delivery starts a new Run.
func TestARunWithAnOpenPRKeepsItsDedupeKeyUntilThePRIsMerged(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Alerts")
	out := t.TempDir()
	env.Runner.Agent(takeTask, initLine(t), commitStep, recorded(out, "pr", `solidtrace pr --title "Fix the cart"`),
		reportStep("--outcome", "done", "--summary", "Fixed the cart test"), reply, result, waitForEnd)
	first := deliver(t, env, path, sender.Secret, "d-1", "issue-1")
	waitFor(t, env, ana, first.RunID, dispatcherapi.StateDone)
	prs := env.Forge.PRs("example/shop")
	if len(prs) != 1 {
		t.Fatalf("want one PR, got %+v\npr: %s", prs, readRecorded(t, out, "pr"))
	}
	if again := deliver(t, env, path, sender.Secret, "d-2", "issue-1"); again.Started || again.RunID != first.RunID {
		t.Errorf("the dedupe_key of a done Run with an open PR: %+v, want the Run %s", again, first.RunID)
	}

	env.Forge.SetState("example/shop", prs[0].ID, "MERGED")
	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	waitForKind(t, env, first.RunID, "closed")
	if next := deliver(t, env, path, sender.Secret, "d-3", "issue-1"); !next.Started || next.RunID == first.RunID {
		t.Errorf("the dedupe_key of a Closed Run: %+v, want a new Run", next)
	}
}

// A PR merged while an Execution runs gives the PR outcome at the next
// check, but the Run stays open until the Execution ends: a WIP ref
// that the Execution saves after the check is deleted at the close.
func TestAPRMergedWhileAnExecutionRunsClosesTheRunWhenTheExecutionEnds(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), commitStep, recorded(out, "pr", `solidtrace pr --title "Fix the cart"`),
		wait, reportStep("--outcome", "done", "--summary", "Fixed the cart test"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the flaky cart test")
	deadline := time.Now().Add(30 * time.Second)
	for len(env.Forge.PRs("example/shop")) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the agent opened no PR after 30 s\npr: %s", readRecorded(t, out, "pr"))
		}
		time.Sleep(20 * time.Millisecond)
	}
	pr := env.Forge.PRs("example/shop")[0]
	env.Forge.SetState("example/shop", pr.ID, "MERGED")

	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	events := waitForKind(t, env, id, "pr_outcome")
	if got := ofKind(events, "pr_outcome"); len(got) != 1 || got[0]["pr.outcome"] != "merged" {
		t.Errorf("want one merged PR outcome event, got %v", got)
	}
	if got := ofKind(events, "closed"); len(got) != 0 {
		t.Errorf("the Run is closed while its Execution runs: %v", got)
	}
	run := getRun(t, ana, id)
	if run.State != dispatcherapi.StateRunning || run.ClosedAt != nil || run.PROutcome != "merged" {
		t.Errorf("the Run is %s, closed at %v, PR outcome %q; want running, not closed, merged", run.State,
			run.ClosedAt, run.PROutcome)
	}
	// The Execution saves its work after the check.
	wip := addWIPRef(t, env, id)

	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	env.Clock.Advance(time.Second)
	env.CheckClosedRuns()
	closed := waitForKind(t, env, id, "closed")
	if last := closed[len(closed)-1]; last["event.kind"] != "closed" || last["run.state"] != dispatcherapi.StateDone ||
		last["run.pr.outcome"] != "merged" || last["run.close_reason"] != "pr_merged" {
		t.Errorf("the last event is not the close of the done Run with its merged PR: %v", last)
	}
	if got := ofKind(closed, "pr_outcome"); len(got) != 1 {
		t.Errorf("want one PR outcome event, got %v", got)
	}
	run = getRun(t, ana, id)
	if run.State != dispatcherapi.StateDone || run.ClosedAt == nil {
		t.Errorf("the Run is %s, closed at %v; want done and closed", run.State, run.ClosedAt)
	}
	if _, ok := env.Forge.Ref("example/shop", wip); ok {
		t.Errorf("the WIP ref %s that the Execution saved is not deleted at the close", wip)
	}
}
