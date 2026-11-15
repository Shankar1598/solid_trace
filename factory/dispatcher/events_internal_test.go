package dispatcher

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// A Run's title is the first line of its prompt, at most maxTitle runes:
// a longer line is cut to maxTitle-1 runes and an ellipsis, at a rune
// boundary.
func TestTheTitleIsTheFirstLineOfThePromptAtMostMaxTitleRunes(t *testing.T) {
	exact := strings.Repeat("a", maxTitle)
	cases := []struct{ name, prompt, want string }{
		{"short", "Fix the cart", "Fix the cart"},
		{"first line only", "  Fix the cart  \nThen the checkout\n", "Fix the cart"},
		{"exactly maxTitle runes", exact, exact},
		{"one rune too many", exact + "b", strings.Repeat("a", maxTitle-1) + "…"},
		{"multi-byte", strings.Repeat("é", maxTitle+20) + "\nmore", strings.Repeat("é", maxTitle-1) + "…"},
	}
	for _, c := range cases {
		got := title(c.prompt)
		if got != c.want {
			t.Errorf("%s: title is %q, want %q", c.name, got, c.want)
		}
		if n := utf8.RuneCountInString(got); n > maxTitle || !utf8.ValidString(got) {
			t.Errorf("%s: title has %d runes (valid UTF-8 %v), want at most %d", c.name, n, utf8.ValidString(got), maxTitle)
		}
	}
}

// The outbox sends its Logs in batches of at most maxBatchBytes (or one
// Log that is larger alone), so EventStore's request limit never refuses
// a batch, and sent removes exactly the Logs of the batch.
func TestTheOutboxSendsBatchesOfAtMostMaxBatchBytesAndRemovesTheSentLogs(t *testing.T) {
	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	const logs = 40
	big := strings.Repeat("x", 100<<10) // 40 Logs of 100 KiB: about 4 MiB in all.
	for i := 0; i < logs; i++ {
		if err := s.addWarning(at, fmt.Sprintf("warning %d", i), map[string]any{"body": big}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.addWarning(at, "oversize", map[string]any{"body": strings.Repeat("y", maxBatchBytes+1)}); err != nil {
		t.Fatal(err)
	}
	if err := s.addWarning(at, "after the oversize Log", nil); err != nil {
		t.Fatal(err)
	}

	var got []string
	for batches := 0; ; batches++ {
		if batches > logs+2 {
			t.Fatal("the outbox does not become empty")
		}
		body, last, err := s.outboxBatch()
		if err != nil {
			t.Fatal(err)
		}
		if body == nil {
			break
		}
		var events []map[string]any
		if err := json.Unmarshal(body, &events); err != nil {
			t.Fatalf("batch %d is not a JSON array: %v", batches+1, err)
		}
		if len(body) > maxBatchBytes && len(events) != 1 {
			t.Errorf("batch %d has %d bytes in %d Logs, want at most %d bytes or one Log", batches+1, len(body),
				len(events), maxBatchBytes)
		}
		var lastID int64
		if err := s.db.QueryRow(`SELECT id FROM outbox ORDER BY id LIMIT 1 OFFSET ?`, len(events)-1).Scan(&lastID); err != nil {
			t.Fatal(err)
		}
		if last != lastID {
			t.Errorf("batch %d gives last %d, want %d, the id of its last Log", batches+1, last, lastID)
		}
		for _, ev := range events {
			got = append(got, ev["message"].(string))
		}
		if err := s.sent(last); err != nil {
			t.Fatal(err)
		}
		var left int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM outbox WHERE id <= ?`, last).Scan(&left); err != nil || left != 0 {
			t.Fatalf("after batch %d, the outbox keeps %d sent Logs (%v), want 0", batches+1, left, err)
		}
	}
	if len(got) != logs+2 {
		t.Fatalf("the batches hold %d Logs, want %d", len(got), logs+2)
	}
	for i := 0; i < logs; i++ {
		if got[i] != fmt.Sprintf("warning %d", i) {
			t.Fatalf("Log %d is %q, want warning %d: the batches lose or repeat Logs", i, got[i], i)
		}
	}
	if got[logs] != "oversize" || got[logs+1] != "after the oversize Log" {
		t.Errorf("the last Logs are %q, %q", got[logs], got[logs+1])
	}
}
