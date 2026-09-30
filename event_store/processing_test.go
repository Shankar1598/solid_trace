package main_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/msgpacker"
	"github.com/solidtrace/event_store/storage"
)

// storeStatus sends a payload to the ingest app's store endpoint.
func storeStatus(env *TestEnv, payload string) (int, error) {
	req, _ := http.NewRequest("POST", "/api/123/store?sentry_key=test_public_key", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.App.Test(req, 2000)
	if err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}

// postEvent stores a payload through the ingest app and returns its UUID.
func postEvent(t *testing.T, env *TestEnv, payload string) string {
	t.Helper()
	if status, err := storeStatus(env, payload); err != nil || status != 200 {
		t.Fatalf("Expected 200, got %d %v", status, err)
	}
	uuids := env.EventWriter.UUIDs()
	return uuids[len(uuids)-1]
}

// listEvents returns the Project's Events from the query app.
func listEvents(t *testing.T, env *TestEnv) []models.Event {
	t.Helper()
	req, _ := http.NewRequest("GET", "/api/123/events", nil)
	resp, err := env.QueryApp.Test(req, 2000)
	if err != nil {
		t.Fatalf("List request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("List expected 200, got %d", resp.StatusCode)
	}
	var events []models.Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("Decode list: %v", err)
	}
	return events
}

func eventUUIDs(events []models.Event) []string {
	uuids := make([]string, len(events))
	for i, event := range events {
		uuids[i] = event.EventUUID
	}
	slices.Sort(uuids)
	return uuids
}

type consoleMessage struct {
	Type     string
	IssueIDs []int64
}

// consoleMessages returns the messages EventStore enqueued for the Console,
// oldest first.
func consoleMessages(t *testing.T, env *TestEnv) []consoleMessage {
	t.Helper()
	db, err := sql.Open("sqlite3", env.MQPath)
	if err != nil {
		t.Fatalf("Open MQ DB: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT message_type, payload FROM event_store_messages ORDER BY id")
	if err != nil {
		t.Fatalf("Query messages: %v", err)
	}
	defer rows.Close()

	packer := msgpacker.New(msgpacker.ModeZstd)
	var messages []consoleMessage
	for rows.Next() {
		var message consoleMessage
		var payload []byte
		if err := rows.Scan(&message.Type, &payload); err != nil {
			t.Fatalf("Scan message: %v", err)
		}
		var body struct {
			IssueIDs []int64 `msgpack:"issue_ids"`
		}
		if err := packer.Unpack(payload, &body); err != nil {
			t.Fatalf("Unpack message: %v", err)
		}
		message.IssueIDs = body.IssueIDs
		messages = append(messages, message)
	}
	return messages
}

// sqliteQuery runs a single-value query against the Console database.
func sqliteQuery(t *testing.T, env *TestEnv, dest interface{}, query string, args ...interface{}) {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Open SQLite: %v", err)
	}
	defer db.Close()
	if err := db.QueryRow(query, args...).Scan(dest); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func issueCount(t *testing.T, env *TestEnv) int {
	t.Helper()
	var count int
	sqliteQuery(t, env, &count, "SELECT count(*) FROM issues WHERE project_id = 123")
	return count
}

const (
	divideByZero = `{"message":"boom","exception":{"values":[{"type":"ZeroDivisionError","value":"divided by 0"}]},"timestamp":"2026-01-24T14:29:30Z"}`
	nilError     = `{"message":"nil","exception":{"values":[{"type":"NoMethodError","value":"undefined method for nil"}]},"timestamp":"2026-01-24T14:29:31Z"}`
)

func TestEventIsQueryableOnlyAfterProcessing(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	eventUUID := postEvent(t, env, divideByZero)

	if events := listEvents(t, env); len(events) != 0 {
		t.Fatalf("Expected no Events in queries before processing, got %d", len(events))
	}
	req, _ := http.NewRequest("GET", "/api/123/events/"+eventUUID, nil)
	if resp, err := env.QueryApp.Test(req, 2000); err != nil || resp.StatusCode != 200 {
		t.Fatalf("Expected the Event readable by UUID before processing, got %v %v", resp, err)
	}

	env.Process(t)

	if got := eventUUIDs(listEvents(t, env)); !slices.Equal(got, []string{eventUUID}) {
		t.Errorf("Expected [%s] after processing, got %v", eventUUID, got)
	}
}

func TestEventsStoredBeforeRestartAreIndexedAfterIt(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	first := postEvent(t, env, divideByZero)
	env.Process(t)
	second := postEvent(t, env, divideByZero)
	third := postEvent(t, env, nilError)

	env.Restart(t)
	env.Process(t)

	if got, want := eventUUIDs(listEvents(t, env)), []string{first, second, third}; !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

func TestEventsWithNoProcessingCursorAreCaughtUp(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Stored, then EventStore stops before processing any of them.
	first := postEvent(t, env, divideByZero)
	second := postEvent(t, env, nilError)

	env.Restart(t)
	env.Process(t)

	if got, want := eventUUIDs(listEvents(t, env)), []string{first, second}; !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got := issueCount(t, env); got != 2 {
		t.Errorf("Expected 2 Issues, got %d", got)
	}
}

func TestRowsDuckDBHoldsPastTheCursorAreNotAppendedAgain(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	first := postEvent(t, env, divideByZero)
	second := postEvent(t, env, divideByZero)
	third := postEvent(t, env, divideByZero)

	// A crash after a batch's DuckDB commit, before its cursor moved: DuckDB
	// holds the first two Events, and the cursor is still at the start.
	rows := []models.Event{
		{ProjectID: 123, EventUUID: first, Timestamp: time.Now().UTC(), Tags: map[string]string{}},
		{ProjectID: 123, EventUUID: second, Timestamp: time.Now().UTC(), Tags: map[string]string{}},
	}
	if err := env.DuckDBWriter.WriteBatch(rows); err != nil {
		t.Fatalf("Seed DuckDB: %v", err)
	}

	env.Restart(t)
	env.Process(t)

	if got, want := eventUUIDs(listEvents(t, env)), []string{first, second, third}; !slices.Equal(got, want) {
		t.Errorf("Expected each Event once, %v, got %v", want, got)
	}
	// The crash came before the Console was told, so the replay tells it.
	messages := consoleMessages(t, env)
	if len(messages) != 1 || messages[0].Type != "issue_created" {
		t.Errorf("Expected one issue_created, got %+v", messages)
	}
}

func TestReplayedEventOfAnIssueCreatedBeforeACrashIsNotNew(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// A crash after the Event's Issue was created, before its cursor moved.
	// md5("crash-gap"): the fingerprint of an Event with fingerprint ["crash-gap"].
	issueID, _, _, created, err := env.SQLiteWriter.FindOrCreateIssue(123, "71a1cb3dca40e1469777955e96799af3", "Crash gap", "", "default")
	if err != nil || !created {
		t.Fatalf("Seed Issue: created=%v err=%v", created, err)
	}
	postEvent(t, env, `{"message":"crash gap","fingerprint":["crash-gap"]}`)

	env.Restart(t)
	env.Process(t)

	// The accepted gap: the Console hears of an Event on an existing Issue,
	// never of the new Issue.
	messages := consoleMessages(t, env)
	want := []consoleMessage{{Type: "issue_received_event", IssueIDs: []int64{issueID}}}
	if len(messages) != 1 || messages[0].Type != want[0].Type || !slices.Equal(messages[0].IssueIDs, want[0].IssueIDs) {
		t.Errorf("Expected %+v, got %+v", want, messages)
	}
	if got := issueCount(t, env); got != 1 {
		t.Errorf("Expected 1 Issue, got %d", got)
	}
}

func TestTimestampFallsBackToReceiveTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	before := time.Now().Truncate(time.Millisecond)
	postEvent(t, env, `{"message":"no timestamp"}`)
	after := time.Now()
	env.Process(t)

	events := listEvents(t, env)
	if len(events) != 1 {
		t.Fatalf("Expected 1 Event, got %d", len(events))
	}
	if ts := events[0].Timestamp; ts.Before(before) || ts.After(after) {
		t.Errorf("Expected a timestamp between %v and %v, got %v", before, after, ts)
	}
}

func TestResolvedIssueReopensOnANewEvent(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postEvent(t, env, divideByZero)
	env.Process(t)

	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Open SQLite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE issues SET status = 1 WHERE project_id = 123"); err != nil {
		t.Fatalf("Resolve Issue: %v", err)
	}

	postEvent(t, env, divideByZero)
	env.Process(t)

	var status int
	sqliteQuery(t, env, &status, "SELECT status FROM issues WHERE project_id = 123")
	if status != 0 {
		t.Errorf("Expected the Issue reopened (status 0), got %d", status)
	}
}

func TestConcurrentFirstEventsForOneFingerprintCreateOneIssue(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if status, err := storeStatus(env, divideByZero); err != nil || status != 200 {
				t.Errorf("Expected 200, got %d %v", status, err)
			}
		})
	}
	wg.Wait()
	env.Process(t)

	if got := issueCount(t, env); got != 1 {
		t.Errorf("Expected 1 Issue, got %d", got)
	}
	if got := len(listEvents(t, env)); got != 5 {
		t.Errorf("Expected 5 Events, got %d", got)
	}
	messages := consoleMessages(t, env)
	if len(messages) != 1 || messages[0].Type != "issue_created" || len(messages[0].IssueIDs) != 1 {
		t.Errorf("Expected one issue_created for one Issue, got %+v", messages)
	}
}

func TestConsoleIsToldOfNewIssuesAndOfEventsOnExistingOnes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postEvent(t, env, divideByZero)
	env.Process(t)
	messages := consoleMessages(t, env)
	if len(messages) != 1 || messages[0].Type != "issue_created" || len(messages[0].IssueIDs) != 1 {
		t.Fatalf("Expected one issue_created, got %+v", messages)
	}
	divideByZeroIssue := messages[0].IssueIDs[0]

	postEvent(t, env, divideByZero)
	postEvent(t, env, nilError)
	postEvent(t, env, nilError)
	env.Process(t)

	messages = consoleMessages(t, env)[1:]
	if len(messages) != 2 {
		t.Fatalf("Expected 2 more messages, got %+v", messages)
	}
	created, received := messages[0], messages[1]
	if created.Type != "issue_created" || len(created.IssueIDs) != 1 || created.IssueIDs[0] == divideByZeroIssue {
		t.Errorf("Expected issue_created for the new Issue only, got %+v", created)
	}
	if received.Type != "issue_received_event" || !slices.Equal(received.IssueIDs, []int64{divideByZeroIssue}) {
		t.Errorf("Expected issue_received_event for Issue %d only, got %+v", divideByZeroIssue, received)
	}
}

func TestPoisonEventIsSkippedAndLaterEventsAreProcessed(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Valid JSON, so ingest stores it, but not an Event object.
	poison := postEvent(t, env, `[]`)
	later := postEvent(t, env, divideByZero)
	env.Process(t)

	if got := eventUUIDs(listEvents(t, env)); !slices.Equal(got, []string{later}) {
		t.Errorf("Expected only [%s] indexed, got %v", later, got)
	}
	// The skipped Event's raw payload stays readable.
	raw, err := env.PebbleWriter.GetEvent(storage.KeyForEvent(123, poison))
	if err != nil || string(raw) != `[]` {
		t.Errorf("Expected the poison Event's payload kept, got %q %v", raw, err)
	}

	// Skipped for good: a restart doesn't pick it up again.
	env.Restart(t)
	env.Process(t)
	if got := len(listEvents(t, env)); got != 1 {
		t.Errorf("Expected 1 Event after restart, got %d", got)
	}
}

func TestTransientIssueRepositoryErrorDelaysButSkipsNothing(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	env.Issues.failFinds.Store(2)
	first := postEvent(t, env, divideByZero)
	second := postEvent(t, env, nilError)
	env.Process(t)

	if got, want := eventUUIDs(listEvents(t, env)), []string{first, second}; !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got := env.Issues.finds.Load(); got < 4 {
		t.Errorf("Expected the failed lookups retried, got %d lookups", got)
	}
}

func TestEventWhoseRowDuckDBRejectsIsSkippedAndTheRestIndexed(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	before := postEvent(t, env, divideByZero)
	// A timestamp far past DuckDB's TIMESTAMP range: the row can't be appended.
	postEvent(t, env, `{"message":"far future","timestamp":9000000000000000}`)
	after := postEvent(t, env, nilError)
	env.Process(t)

	if got, want := eventUUIDs(listEvents(t, env)), []string{before, after}; !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}
