package dispatcher_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

func TestStopOnAQueuedRunMakesItStoppedWithNoExecutionAndAStopEventWithThePerson(t *testing.T) {
	// No Runner: the Run stays queued.
	env := newEnv(t)
	ana, ben := env.Member("Ana"), env.Member("Ben")
	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")

	got := ben.CLI("stop", "--dispatcher", env.URL(), id)

	if got.Code != 0 || !strings.Contains(got.Stdout, "Run "+id+" is stopped.") {
		t.Fatalf("solidtrace stop: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	var run dispatcherapi.Run
	a := ana.API("GET", "/api/v1/runs/"+id, nil)
	a.JSON(t, &run)
	if run.State != dispatcherapi.StateStopped || len(run.Executions) != 0 {
		t.Errorf("the Run is %s with Executions %+v, want stopped with none", run.State, run.Executions)
	}
	if !strings.Contains(run.Reason, "ben@example.com") {
		t.Errorf("the reason %q does not name who stopped the Run", run.Reason)
	}
	events := env.WaitForRunEvents(id, 3)
	stop := events[len(events)-1]
	if stop["event.kind"] != "stop" || stop["run.state"] != dispatcherapi.StateStopped {
		t.Fatalf("the last event is %v (%v), want stop (stopped)", stop["event.kind"], stop["run.state"])
	}
	if stop["actor.kind"] != "person" || stop["actor.id"] != ben.Answer.Sub || stop["actor.name"] != "ben@example.com" {
		t.Errorf("the stop's actor is %v %v %v, want person %s ben@example.com", stop["actor.kind"], stop["actor.id"],
			stop["actor.name"], ben.Answer.Sub)
	}
	if stop["run.started_by.name"] != "ana@example.com" {
		t.Errorf("the stop event names %v as the starter", stop["run.started_by.name"])
	}
}

func TestStopOnARunThatEndedIsRefusedWithTheState(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")
	if got := ana.CLI("stop", id); got.Code != 0 {
		t.Fatalf("the first stop: exit %d: %s", got.Code, got.Stderr)
	}

	again := ana.API("POST", "/api/v1/runs/"+id+"/stop", nil)

	if again.Status != http.StatusConflict || !strings.Contains(string(again.Body), "stopped") {
		t.Errorf("a second stop: %d %s, want 409 that names the state", again.Status, again.Body)
	}
	got := ana.CLI("stop", id)
	if got.Code != 1 || !strings.Contains(got.Stderr, "stopped") {
		t.Errorf("the CLI's second stop: exit %d: %s", got.Code, got.Stderr)
	}
	if events := env.WaitForRunEvents(id, 3); len(env.RunEvents(id)) != 3 {
		t.Errorf("a refused stop made an event: %v", events)
	}
}
