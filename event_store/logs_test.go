package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// logCounts is the JSON route's answer.
type logCounts struct {
	Accepted  int `json:"accepted"`
	Dropped   int `json:"dropped"`
	Truncated int `json:"truncated"`
}

// listedLog is one Log as the query app lists it.
type listedLog struct {
	ID          string         `json:"id"`
	Time        time.Time      `json:"time"`
	Level       string         `json:"level"`
	Message     string         `json:"message"`
	Service     string         `json:"service"`
	Environment string         `json:"environment"`
	TraceID     string         `json:"trace_id"`
	SpanID      string         `json:"span_id"`
	Attributes  map[string]any `json:"attributes"`
}

// sendLogs posts a body to a Project's JSON Log route.
func sendLogs(t *testing.T, env *TestEnv, path, contentType, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", contentType)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := env.App.Test(req, 2000)
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	return resp
}

// postLogs sends Logs to Project 123 with its key and returns the counts.
func postLogs(t *testing.T, env *TestEnv, contentType, body string) logCounts {
	t.Helper()
	resp := sendLogs(t, env, "/api/123/logs?sentry_key=test_public_key", contentType, body, nil)
	return decodeLogCounts(t, resp)
}

func decodeLogCounts(t *testing.T, resp *http.Response) logCounts {
	t.Helper()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, raw)
	}
	var counts logCounts
	if err := json.Unmarshal(raw, &counts); err != nil {
		t.Fatalf("Decode counts %q: %v", raw, err)
	}
	return counts
}

// latestLogs returns a Project's latest Logs from the query app, newest
// first: a run of the empty Query over every time a Log can have.
func latestLogs(t *testing.T, env *TestEnv, projectID uint32, limit int) []listedLog {
	t.Helper()
	return runQuery(t, env, projectID, queryRequest{From: allTime.from, To: allTime.to, Limit: limit}).Logs
}

func TestJSONLogIsListedWithEveryFieldAfterProcessing(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	counts := postLogs(t, env, "application/json", `{
		"message": "user signed in",
		"time": "2026-10-02T11:59:00.25Z",
		"level": "info",
		"service": "web",
		"environment": "production",
		"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
		"span_id": "00f067aa0ba902b7",
		"user": "ada",
		"status": 200,
		"duration_ms": 12.5,
		"cached": true,
		"roles": ["admin", "dev"],
		"http": {"method": "GET"}
	}`)
	if counts != (logCounts{Accepted: 1}) {
		t.Fatalf("Expected 1 accepted, got %+v", counts)
	}

	if logs := latestLogs(t, env, 123, 10); len(logs) != 0 {
		t.Fatalf("Expected no Logs listed before processing, got %d", len(logs))
	}
	env.ProcessLogs(t)

	logs := latestLogs(t, env, 123, 10)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 Log, got %d", len(logs))
	}
	got := logs[0]
	if got.ID == "" {
		t.Errorf("Expected the Log to have an id")
	}
	got.ID = ""
	want := listedLog{
		Time:        time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC),
		Level:       "info",
		Message:     "user signed in",
		Service:     "web",
		Environment: "production",
		TraceID:     "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:      "00f067aa0ba902b7",
		Attributes: map[string]any{
			"user":        "ada",
			"status":      200.0,
			"duration_ms": 12.5,
			"cached":      true,
			"roles":       []any{"admin", "dev"},
			"http.method": "GET",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
}

// messages lists the messages of Logs in the order given.
func messages(logs []listedLog) []string {
	out := make([]string, len(logs))
	for i, log := range logs {
		out[i] = log.Message
	}
	return out
}

func TestJSONArrayLogsAreListedNewestFirstAndNonObjectsDropped(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	counts := postLogs(t, env, "application/json", `[
		{"message": "second", "time": "2026-10-02T11:00:02Z"},
		42,
		{"message": "third", "time": "2026-10-02T11:00:03Z"},
		"not a log",
		null,
		{"message": "first", "time": "2026-10-02T11:00:01Z"}
	]`)
	if want := (logCounts{Accepted: 3, Dropped: 3}); counts != want {
		t.Fatalf("Expected %+v, got %+v", want, counts)
	}
	env.ProcessLogs(t)

	if got, want := messages(latestLogs(t, env, 123, 10)), []string{"third", "second", "first"}; !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got, want := messages(latestLogs(t, env, 123, 2)), []string{"third", "second"}; !slices.Equal(got, want) {
		t.Errorf("Expected the latest 2 to be %v, got %v", want, got)
	}
}

func TestNDJSONLinesAreLogsAndBadLinesDropped(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	body := "{\"message\":\"one\",\"time\":\"2026-10-02T11:00:01Z\"}\r\n" +
		"{\"message\":\"broken\"\n" +
		"\n" +
		"[\"an array\"]\n" +
		"{\"message\":\"two\",\"time\":\"2026-10-02T11:00:02Z\"}"
	counts := postLogs(t, env, "application/x-ndjson; charset=utf-8", body)
	if want := (logCounts{Accepted: 2, Dropped: 2}); counts != want {
		t.Fatalf("Expected %+v, got %+v", want, counts)
	}

	// NDJSON sent as JSON is read as NDJSON too.
	counts = postLogs(t, env, "application/json", "{\"message\":\"three\",\"time\":\"2026-10-02T11:00:03Z\"}\n{\"message\":\"four\",\"time\":\"2026-10-02T11:00:04Z\"}\n")
	if want := (logCounts{Accepted: 2}); counts != want {
		t.Fatalf("Expected %+v for NDJSON labelled JSON, got %+v", want, counts)
	}
	env.ProcessLogs(t)

	if got, want := messages(latestLogs(t, env, 123, 10)), []string{"four", "three", "two", "one"}; !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

func TestLogWithoutAUsableTimeGetsItsReceiptTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	receivedAt := time.Date(2026, 10, 2, 9, 30, 15, 0, time.UTC)
	env.Clock.Set(receivedAt)
	postLogs(t, env, "application/json", `[{"message": "no time"}, {"message": "bad time", "time": "yesterday"}]`)
	env.ProcessLogs(t)

	logs := latestLogs(t, env, 123, 10)
	if len(logs) != 2 {
		t.Fatalf("Expected 2 Logs, got %d", len(logs))
	}
	for _, log := range logs {
		if !log.Time.Equal(receivedAt) {
			t.Errorf("Expected %q at the receipt time %v, got %v", log.Message, receivedAt, log.Time)
		}
	}
	// The time it couldn't use is kept.
	for _, log := range logs {
		if log.Message == "bad time" && log.Attributes["original.time"] != "yesterday" {
			t.Errorf("Expected original.time kept, got %v", log.Attributes)
		}
	}
}

func TestLogRouteAcceptsTheProjectKeyInEverySentryFormAndAsBearer(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	forms := map[string]struct {
		path    string
		headers map[string]string
	}{
		"sentry_key query":     {"/api/123/logs?sentry_key=test_public_key", nil},
		"X-Sentry-Auth":        {"/api/123/logs", map[string]string{"X-Sentry-Auth": "Sentry sentry_version=7, sentry_key=test_public_key"}},
		"Authorization Sentry": {"/api/123/logs", map[string]string{"Authorization": "Sentry sentry_key=test_public_key, sentry_version=7"}},
		"Authorization Bearer": {"/api/123/logs", map[string]string{"Authorization": "Bearer test_public_key"}},
	}
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			resp := sendLogs(t, env, form.path, "application/json", fmt.Sprintf(`{"message": %q}`, name), form.headers)
			if counts := decodeLogCounts(t, resp); counts != (logCounts{Accepted: 1}) {
				t.Errorf("Expected 1 accepted, got %+v", counts)
			}
		})
	}

	env.ProcessLogs(t)
	if got := len(latestLogs(t, env, 123, 10)); got != len(forms) {
		t.Errorf("Expected %d Logs stored, got %d", len(forms), got)
	}
}

func TestLogRouteRejectsAMissingUnknownOrOtherProjectsKey(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	cases := map[string]struct {
		path    string
		headers map[string]string
	}{
		"missing key":            {"/api/123/logs", nil},
		"unknown key":            {"/api/123/logs", map[string]string{"Authorization": "Bearer no_such_key"}},
		"other Project's Bearer": {"/api/123/logs", map[string]string{"Authorization": "Bearer other_public_key"}},
		"other Project's sentry": {"/api/123/logs?sentry_key=other_public_key", nil},
		"non-numeric Project":    {"/api/abc/logs?sentry_key=test_public_key", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			resp := sendLogs(t, env, c.path, "application/json", `{"message": "rejected"}`, c.headers)
			if resp.StatusCode != 401 {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("Expected 401, got %d: %s", resp.StatusCode, body)
			}
		})
	}

	env.ProcessLogs(t)
	for _, projectID := range []uint32{123, 456} {
		if got := latestLogs(t, env, projectID, 10); len(got) != 0 {
			t.Errorf("Expected no Logs stored for Project %d, got %v", projectID, messages(got))
		}
	}
}

func TestEachProjectListsOnlyItsOwnLogs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	postLogs(t, env, "application/json", `{"message": "for 123"}`)
	resp := sendLogs(t, env, "/api/456/logs", "application/json", `{"message": "for 456"}`, map[string]string{"Authorization": "Bearer other_public_key"})
	decodeLogCounts(t, resp)
	env.ProcessLogs(t)

	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"for 123"}) {
		t.Errorf("Expected Project 123's Log only, got %v", got)
	}
	if got := messages(latestLogs(t, env, 456, 10)); !slices.Equal(got, []string{"for 456"}) {
		t.Errorf("Expected Project 456's Log only, got %v", got)
	}
}

func TestLogsAcknowledgedBeforeARestartAreListedAfterIt(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postLogs(t, env, "application/json", `{"message": "before", "time": "2026-10-02T11:00:01Z"}`)
	env.ProcessLogs(t)
	postLogs(t, env, "application/json", `{"message": "unprocessed", "time": "2026-10-02T11:00:02Z"}`)

	env.Reopen(t)
	env.ProcessLogs(t)

	if got, want := messages(latestLogs(t, env, 123, 10)), []string{"unprocessed", "before"}; !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

func TestCrashAfterLogsCommitBeforeTheirBatchesAreDeletedStoresThemOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postLogs(t, env, "application/json", `{"message": "one", "time": "2026-10-02T11:00:01Z"}`)
	postLogs(t, env, "application/json", `[{"message": "two", "time": "2026-10-02T11:00:02Z"}, {"message": "three", "time": "2026-10-02T11:00:03Z"}]`)

	// Log processing commits the batches to DuckDB, then crashes before
	// deleting them from Pebble.
	batches, err := env.PebbleWriter.LogBatchesAfter(123, uuid.Nil, 100)
	if err != nil || len(batches) != 2 {
		t.Fatalf("Expected 2 Log batches, got %d %v", len(batches), err)
	}
	if err := env.LogStore.AppendLogBatches(123, batches, env.Clock.Now()); err != nil {
		t.Fatalf("Commit Log batches: %v", err)
	}
	postLogs(t, env, "application/json", `{"message": "four", "time": "2026-10-02T11:00:04Z"}`)

	env.Reopen(t)
	env.ProcessLogs(t)

	if got, want := messages(latestLogs(t, env, 123, 10)), []string{"four", "three", "two", "one"}; !slices.Equal(got, want) {
		t.Errorf("Expected each Log once, %v, got %v", want, got)
	}
}

func TestLogsSentAfterTheClockStepsBackAreStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Each Log's own time is before every reading of the clock.
	postLogs(t, env, "application/json", `{"message": "before", "time": "2026-10-02T09:00:01Z"}`)
	env.ProcessLogs(t)
	postLogs(t, env, "application/json", `{"message": "unprocessed", "time": "2026-10-02T09:00:02Z"}`)

	// The clock steps back an hour while EventStore runs: one Log comes
	// before the next processing, one after.
	env.Clock.Set(testNow.Add(-time.Hour))
	postLogs(t, env, "application/json", `{"message": "stepped back", "time": "2026-10-02T09:00:03Z"}`)
	env.ProcessLogs(t)
	postLogs(t, env, "application/json", `{"message": "processed apart", "time": "2026-10-02T09:00:04Z"}`)
	env.ProcessLogs(t)

	// It steps back again across a restart.
	env.Clock.Set(testNow.Add(-2 * time.Hour))
	env.Reopen(t)
	postLogs(t, env, "application/json", `{"message": "after a restart", "time": "2026-10-02T09:00:05Z"}`)
	env.ProcessLogs(t)
	postLogs(t, env, "application/json", `{"message": "last", "time": "2026-10-02T09:00:06Z"}`)
	env.ProcessLogs(t)

	want := []string{"last", "after a restart", "processed apart", "stepped back", "unprocessed", "before"}
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, want) {
		t.Errorf("Expected every acknowledged Log, %v, got %v", want, got)
	}
}

func TestLogsAndEventsOfOneProjectDoNotMeet(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postLogs(t, env, "application/json", `{"message": "a Log"}`)
	eventUUID := postEvent(t, env, divideByZero)

	// Event processing and Log processing each find their own work in
	// Pebble at startup.
	env.Restart(t)
	env.Process(t)
	env.ProcessLogs(t)

	if got := eventUUIDs(listEvents(t, env)); !slices.Equal(got, []string{eventUUID}) {
		t.Errorf("Expected only the Event [%s], got %v", eventUUID, got)
	}
	if got := issueCount(t, env); got != 1 {
		t.Errorf("Expected 1 Issue, got %d", got)
	}
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"a Log"}) {
		t.Errorf("Expected only the Log, got %v", got)
	}

	// With both processing cursors in place, more of each still go through.
	secondEvent := postEvent(t, env, nilError)
	postLogs(t, env, "application/json", `{"message": "another Log"}`)
	env.Restart(t)
	env.Process(t)
	env.ProcessLogs(t)

	if got, want := eventUUIDs(listEvents(t, env)), []string{eventUUID, secondEvent}; !slices.Equal(got, want) {
		t.Errorf("Expected Events %v, got %v", want, got)
	}
	if got := len(latestLogs(t, env, 123, 10)); got != 2 {
		t.Errorf("Expected 2 Logs, got %d", got)
	}
}

func TestLogIngestAfterTheWriterStopsIsServiceUnavailable(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	env.PebbleWriter.StopWrites()

	resp := sendLogs(t, env, "/api/123/logs?sentry_key=test_public_key", "application/json", `{"message": "too late"}`, nil)
	if resp.StatusCode != 503 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 503, got %d: %s", resp.StatusCode, body)
	}
	assertRetryAfterAFewSeconds(t, resp)

	// Logs in an envelope too.
	resp = sendEncoded(t, env, "/api/123/envelope", "application/x-sentry-envelope", "", logEnvelope("too late"))
	if resp.StatusCode != 503 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 503 for an envelope, got %d: %s", resp.StatusCode, body)
	}
	assertRetryAfterAFewSeconds(t, resp)
}
