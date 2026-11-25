package agentprocessing

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/storage"
)

// The activity of an Agent session (agent_sessions.first_time and
// last_time) is the times of its lines that have a timestamp. Claude Code
// writes many lines with no timestamp (ai-title, last-prompt, mode,
// permission-mode, cost-state…). Such a line gets a time for its Log, but
// it is not activity: else a unit that starts with such lines, as in an
// old Agent session uploaded again, makes the Agent session active at the
// upload time.

var activitySession = uuid.MustParse("3c5e7a9b-1d2f-4a6b-8c0d-2e4f6a8b0c1d")

// untimed are lines that Claude Code writes with no timestamp.
var untimed = []string{
	`{"type":"mode","mode":"default","sessionId":"x"}`,
	`{"type":"permission-mode","permissionMode":"default","sessionId":"x"}`,
	`{"type":"last-prompt","lastPrompt":"Fix the cart","sessionId":"x"}`,
	aiTitle("Cart fix"),
}

func timedPerson(at, text string) string {
	return `{"type":"user","uuid":"p","timestamp":"` + at + `","message":{"role":"user","content":` + string(quote(text)) + `}}`
}

func activityUnit(offset uint64, stored time.Time, lines ...string) storage.AgentFileUnit {
	u := unitOf("session.jsonl", offset, lines...)
	u.SessionID, u.StoredAt = activitySession, stored
	return u
}

// daysOf parses the ranges in order and gives the rows of the main
// transcript.
func daysOf(ranges ...[]storage.AgentFileUnit) []storage.AgentSessionDay {
	var state parseState
	var all []storage.AgentSessionDay
	for _, units := range ranges {
		var days []storage.AgentSessionDay
		_, days, _, state = parseUnits(units, 180, state)
		all = append(all, days...)
	}
	return all
}

func TestALineWithNoTimestampIsNotActivity(t *testing.T) {
	stored := time.Date(2026, 11, 23, 12, 2, 54, 355220000, time.UTC)

	t.Run("untimed lines at the start of a unit do not give the stored time", func(t *testing.T) {
		lines := append(append([]string{}, untimed...),
			timedPerson("2026-11-17T16:12:50Z", "Fix the cart"),
			timedPerson("2026-11-17T16:20:00Z", "And the checkout"),
		)
		lines = append(lines, untimed...)
		days := daysOf([]storage.AgentFileUnit{activityUnit(0, stored, lines...)})
		if len(days) != 1 {
			t.Fatalf("Expected one row, got %+v", days)
		}
		want := []time.Time{time.Date(2026, 11, 17, 16, 12, 50, 0, time.UTC), time.Date(2026, 11, 17, 16, 20, 0, 0, time.UTC)}
		if !days[0].First.Equal(want[0]) || !days[0].Last.Equal(want[1]) {
			t.Errorf("Expected the times of the timed lines, %s to %s, got %s to %s", want[0], want[1], days[0].First, days[0].Last)
		}
	})

	t.Run("a unit of untimed lines only has no activity", func(t *testing.T) {
		first := activityUnit(0, stored.Add(-time.Hour), timedPerson("2026-11-23T11:00:00Z", "Fix the cart"))
		second := activityUnit(uint64(len(first.Bytes)), stored, untimed...)
		days := daysOf([]storage.AgentFileUnit{first}, []storage.AgentFileUnit{second})
		if len(days) != 2 {
			t.Fatalf("Expected a row for each range, got %+v", days)
		}
		row := days[1]
		if !row.Last.Equal(storage.NoActivity) {
			t.Errorf("Expected no activity (last %s) for the untimed lines, got last %s", storage.NoActivity, row.Last)
		}
		// The first time is the stored time: it is never before the lines
		// that were stored, so it does not move the Agent session's start.
		if !row.First.Equal(stored) {
			t.Errorf("Expected the stored time %s as the first time, got %s", stored, row.First)
		}
		if row.Title.Text != "Cart fix" {
			t.Errorf("Expected the untimed ai-title to give the title, got %+v", row.Title)
		}
	})

	t.Run("an untimed line's Log still has a time", func(t *testing.T) {
		logs, _, _, _ := parseUnits([]storage.AgentFileUnit{activityUnit(0, stored, untimed[0], timedPerson("2026-11-23T12:00:00Z", "Go"), untimed[1])}, 180, parseState{})
		if len(logs[0]) != 3 {
			t.Fatalf("Expected 3 Logs, got %d", len(logs[0]))
		}
		if got := logs[0][0].Time; !got.Equal(stored) {
			t.Errorf("Expected the first untimed line's Log at the stored time %s, got %s", stored, got)
		}
		if got, want := logs[0][2].Time, time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC); !got.Equal(want) {
			t.Errorf("Expected the untimed line after a timed one at %s, got %s", want, got)
		}
	})

	t.Run("a line time that is not believable still gives the stored time", func(t *testing.T) {
		days := daysOf([]storage.AgentFileUnit{activityUnit(0, stored, timedPerson("2999-01-01T00:00:00Z", "Go"))})
		if !days[0].First.Equal(stored) || !days[0].Last.Equal(stored) {
			t.Errorf("Expected the stored time, got %s to %s", days[0].First, days[0].Last)
		}
	})
}
