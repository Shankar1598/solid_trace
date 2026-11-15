package dispatcher_test

import (
	"strings"
	"testing"
	"time"

)

func TestAStartSendsTheCreatedAndQueuedEventsWithTheRunsSnapshot(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	at := env.Clock.Now()

	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test\n\nIt fails one time in ten.")

	events := env.WaitForRunEvents(id, 2)
	for i, kind := range []string{"created", "queued"} {
		ev := events[i]
		want := map[string]any{
			"event.kind":      kind,
			"run.id":          id,
			"run.seq":         float64(i + 1),
			"time":            at.Format(time.RFC3339Nano),
			"level":           "info",
			"run.state":       "queued",
			"run.reason":      "",
			"run.trigger":     "person",
			"run.title":       "Fix the flaky cart test",
			"run.executions":  float64(0),
			"run.pr.url":      "",
			"run.pr.outcome":  "",
			"factory.project": "checkout",
			"repository":      "bitbucket.org/example/checkout",
			"folder":          "",
			"actor.kind":      "person",
			"actor.id":        ana.Answer.Sub,
			"actor.name":      ana.Answer.Username,
		}
		for k, v := range want {
			if ev[k] != v {
				t.Errorf("event %d %s = %#v, want %#v", i+1, k, ev[k], v)
			}
		}
		if s, _ := ev["event.id"].(string); s == "" {
			t.Errorf("event %d has no event.id", i+1)
		}
		if m, _ := ev["message"].(string); !strings.Contains(m, id) {
			t.Errorf("event %d message %q does not name the Run", i+1, m)
		}
	}
	if events[0]["event.id"] == events[1]["event.id"] {
		t.Errorf("two events have one event.id")
	}
}

func TestRunsChangeStateWhileEventStoreIsDownAndTheEventsArriveInOrderLater(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	env.SolidTrace.EventStoreDown(true)

	first, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the cart")
	second, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "search", "Fix the search")
	// The outbox is in the database: a restart keeps the events.
	env.Restart()
	time.Sleep(200 * time.Millisecond)
	if got := env.SolidTrace.RunEvents(); len(got) != 0 {
		t.Fatalf("EventStore is down, but it has %d events", len(got))
	}

	env.SolidTrace.EventStoreDown(false)
	for _, id := range []string{first, second} {
		events := env.WaitForRunEvents(id, 2)
		if events[0]["event.kind"] != "created" || events[0]["run.seq"] != float64(1) ||
			events[1]["event.kind"] != "queued" || events[1]["run.seq"] != float64(2) {
			t.Errorf("the events of %s: %v", id, events)
		}
	}
}

func TestATriggerForAProjectWithNoSettingsSendsAWarningWithFileDispatcher(t *testing.T) {
	env := newEnv(t)
	env.SolidTraceProject("docs")
	ana := env.Member("Ana")

	got := ana.CLI("start", "--dispatcher", env.URL(), "--project", "docs", "Fix the docs")
	if got.Code == 0 {
		t.Fatalf("start for a Project with no settings: exit 0")
	}

	warning := env.WaitForWarning("no Factory settings")
	if warning["level"] != "warn" || warning["factory.project"] != "docs" || warning["actor.name"] != ana.Answer.Username {
		t.Errorf("the warning: %v", warning)
	}
	if _, ok := warning["run.id"]; ok {
		t.Errorf("a warning of the Dispatcher has a run.id: %v", warning)
	}
	if warning["time"] != env.Clock.Now().Format(time.RFC3339Nano) {
		t.Errorf("the warning's time %v is not the Dispatcher's clock", warning["time"])
	}
}

