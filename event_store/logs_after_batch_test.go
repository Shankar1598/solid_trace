package main_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// nilBatch is the ingest_cursor of a Project Log processing has stored
// nothing for: the nil UUID.
const nilBatch = "00000000-0000-0000-0000-000000000000"

// ingestedPage is the answer to a run with after_batch.
type ingestedPage struct {
	Logs         []listedLog `json:"logs"`
	NextCursor   *string     `json:"next_cursor"`
	IngestCursor string      `json:"ingest_cursor"`
	Total        int         `json:"total"`
}

// runAfterBatch runs a Query of Project 123 for the Logs ingested after the
// Log batch afterBatch, "" to arm a Monitor, with any more of the request in
// extra.
func runAfterBatch(t *testing.T, env *TestEnv, query, afterBatch string, extra map[string]any) ingestedPage {
	t.Helper()
	body := map[string]any{"query": query, "after_batch": afterBatch}
	for name, value := range extra {
		body[name] = value
	}
	answer := answerOf(t, env, "/api/123/logs/query", body, 200)
	if elapsed := string(answer["elapsed_ms"]); !wholeNumber.MatchString(elapsed) {
		t.Errorf("Expected elapsed_ms, a whole number of milliseconds, got %q", elapsed)
	}
	var page ingestedPage
	if err := json.Unmarshal(mustMarshal(t, answer), &page); err != nil {
		t.Fatalf("Decode %s: %v", mustMarshal(t, answer), err)
	}
	if page.NextCursor != nil {
		t.Errorf("Expected no next_cursor after a batch, got %q", *page.NextCursor)
	}
	if page.IngestCursor == "" {
		t.Errorf("Expected an ingest_cursor, got %s", mustMarshal(t, answer))
	}
	return page
}

// matchMonitor runs a Query as a Match monitor does: armed with an empty
// after_batch, then each run reads the Logs ingested after the last run's
// ingest_cursor, and keeps the new one.
type matchMonitor struct {
	t      *testing.T
	env    *TestEnv
	query  string
	cursor string
}

func armMonitor(t *testing.T, env *TestEnv, query string) *matchMonitor {
	t.Helper()
	page := runAfterBatch(t, env, query, "", nil)
	if len(page.Logs) != 0 || page.Total != 0 {
		t.Errorf("Expected arming to read no Logs, got %d (total %d)", len(page.Logs), page.Total)
	}
	return &matchMonitor{t: t, env: env, query: query, cursor: page.IngestCursor}
}

// run runs the Monitor's Query once, with any more of the request in extra,
// and returns the messages of the Logs it read, newest first.
func (m *matchMonitor) run(extra map[string]any) []string {
	m.t.Helper()
	page := runAfterBatch(m.t, m.env, m.query, m.cursor, extra)
	m.cursor = page.IngestCursor
	return messages(page.Logs)
}

func TestAMatchMonitorReadsTheLogsIngestedSinceItsLastRun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(minute int) time.Time { return testNow.Add(time.Duration(minute) * time.Minute) }

	// Armed before Log processing has stored anything for the Project, a
	// Monitor goes from the nil batch, and reads everything stored after.
	first := armMonitor(t, env, "")
	if first.cursor != nilBatch {
		t.Errorf("Expected a Project with nothing stored to arm at the nil batch, got %q", first.cursor)
	}
	postAndProcess(t, env, logJSON(t, "before", at(-30), nil))

	second := armMonitor(t, env, "")
	if second.cursor == nilBatch {
		t.Fatalf("Expected arming after a Log batch to give its id")
	}
	postAndProcess(t, env, logJSON(t, "one", at(-20), nil), logJSON(t, "two", at(-10), nil))
	postAndProcess(t, env, logJSON(t, "three", at(-15), nil))

	if got, want := first.run(nil), []string{"two", "three", "one", "before"}; !slices.Equal(got, want) {
		t.Errorf("Expected the first Monitor to read %v, got %v", want, got)
	}
	armed := second.cursor
	if got, want := second.run(nil), []string{"two", "three", "one"}; !slices.Equal(got, want) {
		t.Errorf("Expected the second Monitor to read %v, got %v", want, got)
	}
	if second.cursor == armed || second.cursor != first.cursor {
		t.Errorf("Expected both Monitors to move to the same new cursor, got %q and %q (armed at %q)", first.cursor, second.cursor, armed)
	}

	// Nothing new: the next run reads none, and keeps the cursor.
	cursor := second.cursor
	if got := second.run(nil); len(got) != 0 {
		t.Errorf("Expected a run with nothing new to read none, got %v", got)
	}
	if second.cursor != cursor {
		t.Errorf("Expected the cursor kept at %q, got %q", cursor, second.cursor)
	}

	postAndProcess(t, env, logJSON(t, "four", at(-5), nil))
	if got := second.run(nil); !slices.Equal(got, []string{"four"}) {
		t.Errorf("Expected only the new Log, got %v", got)
	}
}
