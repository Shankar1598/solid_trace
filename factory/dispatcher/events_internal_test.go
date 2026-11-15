package dispatcher

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// An intervention names its own actor in actor.*, but each event's
// snapshot keeps the person who started the Run in run.started_by.*, so
// the Runs list (the last event of each Run) shows who started it.
func TestAnEventWithAnotherActorKeepsWhoStartedTheRunInItsSnapshot(t *testing.T) {
	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	run := storedRun{Run: dispatcherapi.Run{ID: "run1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, StartedBy: dispatcherapi.Person{ID: "7", Name: "Ana"}, CreatedAt: at},
		organizationID: "1", projectID: "2", repository: "bitbucket.org/example/shop", trigger: triggerPerson}
	if err := s.insertRun(run); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = recordEvent(tx, at, "run1", "message_accepted", map[string]any{"actor.kind": actorPerson, "actor.id": "8",
		"actor.name": "Bo", "run.started_by.name": "Bo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.commit(tx); err != nil {
		t.Fatal(err)
	}

	body, _, err := s.outboxBatch()
	if err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	if err := json.Unmarshal(body, &events); err != nil || len(events) != 3 {
		t.Fatalf("the outbox has %s, %v; want 3 events", body, err)
	}
	for i, ev := range events {
		if ev["run.started_by.kind"] != "person" || ev["run.started_by.id"] != "7" || ev["run.started_by.name"] != "Ana" {
			t.Errorf("event %d names %v %v %v as the starter, want person 7 Ana", i+1, ev["run.started_by.kind"],
				ev["run.started_by.id"], ev["run.started_by.name"])
		}
	}
	if last := events[2]; last["actor.id"] != "8" || last["actor.name"] != "Bo" {
		t.Errorf("the intervention's actor is %v %v, want 8 Bo", last["actor.id"], last["actor.name"])
	}
	if first := events[0]; first["actor.id"] != "7" || first["actor.name"] != "Ana" {
		t.Errorf("the created event's actor is %v %v, want 7 Ana", first["actor.id"], first["actor.name"])
	}
}
