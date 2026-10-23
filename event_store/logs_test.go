package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/logprocessing"
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
	return runQueryPages(t, env, projectID, queryRequest{From: allTime.from, To: allTime.to, Limit: limit})
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

// onlyLog processes a Project's Logs and returns the one Log listed.
func onlyLog(t *testing.T, env *TestEnv) listedLog {
	t.Helper()
	env.ProcessLogs(t)
	logs := latestLogs(t, env, 123, 10)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 Log, got %d", len(logs))
	}
	return logs[0]
}

func TestADuplicatedNameInALogKeepsItsLastValue(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// An array, so that the body is first checked as one JSON value.
	counts := postLogs(t, env, "application/json", `[{
		"message": "first", "message": "last",
		"time": "2026-10-02T11:00:01Z",
		"user": "ada", "user": "grace",
		"http": {"status": 200, "status": 404}
	}]`)
	if want := (logCounts{Accepted: 1}); counts != want {
		t.Fatalf("Expected %+v, got %+v", want, counts)
	}

	got := onlyLog(t, env)
	if got.Message != "last" {
		t.Errorf("Expected the message last, got %q", got.Message)
	}
	if want := map[string]any{"user": "grace", "http.status": 404.0}; !reflect.DeepEqual(got.Attributes, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got.Attributes)
	}
}

func TestInvalidUTF8InALogIsStoredAsTheReplacementCharacter(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	counts := postLogs(t, env, "application/json", "[{"+
		"\"message\": \"bad \xff byte\", \"time\": \"2026-10-02T11:00:01Z\","+
		"\"user\": \"\xfe\", \"n\xffame\": 1"+
		"}]")
	if want := (logCounts{Accepted: 1}); counts != want {
		t.Fatalf("Expected %+v, got %+v", want, counts)
	}

	got := onlyLog(t, env)
	if want := "bad \uFFFD byte"; got.Message != want {
		t.Errorf("Expected the message %q, got %q", want, got.Message)
	}
	if want := map[string]any{"user": "\uFFFD", "n\uFFFDame": 1.0}; !reflect.DeepEqual(got.Attributes, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got.Attributes)
	}
}

func TestNumbersADoubleCannotHoldExactlyAreStoredAsText(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	counts := postLogs(t, env, "application/json", `{
		"message": "m", "time": "2026-10-02T11:00:01Z",
		"past_range": 1e400,
		"long_whole": 123456789012345678901234567890,
		"exact_whole": 9007199254740992000
	}`)
	if want := (logCounts{Accepted: 1}); counts != want {
		t.Fatalf("Expected %+v, got %+v", want, counts)
	}

	want := map[string]any{
		"past_range":  "1e400",
		"long_whole":  "123456789012345678901234567890",
		"exact_whole": 9007199254740992000.0,
	}
	if got := onlyLog(t, env); !reflect.DeepEqual(got.Attributes, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got.Attributes)
	}
}

func TestALogNestedPastTheJSONDepthLimitIsDropped(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	nested := func(depth int) string {
		return strings.Repeat("[", depth) + strings.Repeat("]", depth)
	}
	body := `{"message": "too deep", "time": "2026-10-02T11:00:01Z", "a": ` + nested(10_001) + "}\n" +
		`{"message": "deep enough", "time": "2026-10-02T11:00:02Z", "a": ` + nested(100) + "}\n"
	counts := postLogs(t, env, "application/x-ndjson", body)
	if want := (logCounts{Accepted: 1, Dropped: 1}); counts != want {
		t.Fatalf("Expected %+v, got %+v", want, counts)
	}

	if got := onlyLog(t, env); got.Message != "deep enough" {
		t.Errorf("Expected the Log deep enough, got %q", got.Message)
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

func TestKeyOnlyLogRouteStoresTheLogsInTheKeysProject(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	forms := map[string]struct {
		path    string
		headers map[string]string
	}{
		"sentry_key query":     {"/api/logs?sentry_key=other_public_key", nil},
		"X-Sentry-Auth":        {"/api/logs", map[string]string{"X-Sentry-Auth": "Sentry sentry_version=7, sentry_key=other_public_key"}},
		"Authorization Sentry": {"/api/logs", map[string]string{"Authorization": "Sentry sentry_key=other_public_key, sentry_version=7"}},
		"Authorization Bearer": {"/api/logs", map[string]string{"Authorization": "Bearer other_public_key"}},
	}
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			resp := sendLogs(t, env, form.path, "application/json", fmt.Sprintf(`{"message": %q}`, name), form.headers)
			if counts := decodeLogCounts(t, resp); counts != (logCounts{Accepted: 1}) {
				t.Errorf("Expected 1 accepted, got %+v", counts)
			}
		})
	}
	resp := sendLogs(t, env, "/api/logs", "application/x-ndjson", "{\"message\": \"line 1\"}\n{\"message\": \"line 2\"}\n", map[string]string{"Authorization": "Bearer other_public_key"})
	if counts := decodeLogCounts(t, resp); counts != (logCounts{Accepted: 2}) {
		t.Errorf("Expected 2 NDJSON lines accepted, got %+v", counts)
	}

	env.ProcessLogs(t)
	if got := len(latestLogs(t, env, 456, 10)); got != len(forms)+2 {
		t.Errorf("Expected %d Logs stored in Project 456, got %d", len(forms)+2, got)
	}
	if got := latestLogs(t, env, 123, 10); len(got) != 0 {
		t.Errorf("Expected no Logs stored for Project 123, got %v", messages(got))
	}
}

func TestKeyOnlyLogRouteRejectsAMissingOrUnknownKey(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	cases := map[string]map[string]string{
		"missing key": nil,
		"unknown key": {"Authorization": "Bearer no_such_key"},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			resp := sendLogs(t, env, "/api/logs", "application/json", `{"message": "rejected"}`, headers)
			if resp.StatusCode != 401 {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("Expected 401, got %d: %s", resp.StatusCode, body)
			}
		})
	}

	env.ProcessLogs(t)
	if got := latestLogs(t, env, 123, 10); len(got) != 0 {
		t.Errorf("Expected no Logs stored, got %v", messages(got))
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

// assertStoredOnce checks that Project 123 lists each of want once, newest
// first, and that its histogram and Fields count each Log once.
func assertStoredOnce(t *testing.T, env *TestEnv, want []string, when string) {
	t.Helper()
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, want) {
		t.Errorf("%s: expected each Log once, %v, got %v", when, want, got)
	}
	if got := histogram(t, env, histogramRequest{From: testNow.Add(-24 * time.Hour), To: testNow, BucketSeconds: 3600}); got.Count != len(want) {
		t.Errorf("%s: expected the histogram to count each Log once, %d, got %d", when, len(want), got.Count)
	}
	fields := getFields(t, env, 123, testNow.Add(-24*time.Hour), testNow, "")
	assertAttributes(t, fields, []attributeCount{{"user", "string", int64(len(want))}}, when)
}

func TestACrashBetweenTheInsertAndTheCursorRepeatsNothingAndLosesNothing(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postLogs(t, env, "application/json", `{"message": "one", "time": "2026-10-02T11:00:01Z", "user": "ada"}`)
	postLogs(t, env, "application/json", `[{"message": "two", "time": "2026-10-02T11:00:02Z", "user": "bob"}, {"message": "three", "time": "2026-10-02T11:00:03Z", "user": "cy"}]`)

	// Log processing inserts both batches, and EventStore crashes before it
	// hears back: its cursor stays where it was, and the batches in Pebble.
	crashed := make(chan struct{})
	env.FlakyLogs.failAfterInsert(connectionLost, func() {
		env.FlakyLogs.goDown()
		close(crashed)
	})
	done := startLogProcessing(env)
	select {
	case <-crashed:
	case <-time.After(10 * time.Second):
		t.Fatalf("Expected Log processing to insert the batches")
	}
	// A Log the restart waits on, which comes after the range.
	postLogs(t, env, "application/json", `{"message": "four", "time": "2026-10-02T11:00:04Z", "user": "di"}`)
	env.Reopen(t)
	if err := <-done; !errors.Is(err, logprocessing.ErrStopped) {
		t.Fatalf("Expected the crashed Log processing to stop, got %v", err)
	}
	env.ProcessLogs(t)
	assertStoredOnce(t, env, []string{"four", "three", "two", "one"}, "after the restart")

	// Nothing is inserted again later, either.
	postAndProcess(t, env, `{"message": "five", "time": "2026-10-02T11:00:05Z", "user": "ed"}`)
	assertStoredOnce(t, env, []string{"five", "four", "three", "two", "one"}, "after the next insert")
}

func TestARangeOfLogBatchesInsertedAgainIsStoredOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postLogs(t, env, "application/json", `{"message": "one", "time": "2026-10-02T11:00:01Z", "user": "ada"}`)
	postLogs(t, env, "application/json", `[{"message": "two", "time": "2026-10-02T11:00:02Z", "user": "bob"}, {"message": "three", "time": "2026-10-02T11:00:03Z", "user": "cy"}]`)

	// ClickHouse takes the insert, but its answer is lost; a Log arrives
	// before Log processing tries again, and the range it inserts again
	// doesn't take it in.
	env.FlakyLogs.failAfterInsert(connectionLost, func() {
		postLogs(t, env, "application/json", `{"message": "four", "time": "2026-10-02T11:00:04Z", "user": "di"}`)
	})
	tries := env.FlakyLogs.attempts.Load()
	processLogsWithin(t, env, 10*time.Second)
	if got := env.FlakyLogs.attempts.Load() - tries; got != 3 {
		t.Errorf("Expected the range inserted twice, then the new Log, got %d inserts", got)
	}
	assertStoredOnce(t, env, []string{"four", "three", "two", "one"}, "after the range was inserted again")
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

// attributesByMessage maps each listed Log's message to its Attributes.
func attributesByMessage(logs []listedLog) map[string]map[string]any {
	out := make(map[string]map[string]any, len(logs))
	for _, log := range logs {
		out[log.Message] = log.Attributes
	}
	return out
}

// assertProjectQueries checks each Query's messages for a Project over all
// time, newest first.
func assertProjectQueries(t *testing.T, env *TestEnv, projectID uint32, cases map[string][]string, when string) {
	t.Helper()
	for query, want := range cases {
		logs := runQueryPages(t, env, projectID, queryRequest{Query: query, From: allTime.from, To: allTime.to, Limit: 1000})
		if got := messages(logs); !slices.Equal(got, want) {
			t.Errorf("%s: Project %d: %s\n want %v\n got  %v", when, projectID, query, want, got)
		}
	}
}

func TestALogIsFoundByEachOfItsKeysAndReadBackUnchanged(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postLogs(t, env, "application/json", `{
		"message": "checkout",
		"time": "2026-10-02T11:59:00Z",
		"user": "ada",
		"status": 200,
		"cached": true,
		"roles": ["admin", "dev"]
	}`)
	env.ProcessLogs(t)

	// Built-in fields aren't Attributes, and an array is JSON text.
	want := map[string]any{"user": "ada", "status": 200.0, "cached": true, "roles": []any{"admin", "dev"}}
	queries := map[string][]string{
		`where user == "ada"`:          {"checkout"},
		`where status == 200`:          {"checkout"},
		`where cached == true`:         {"checkout"},
		`where roles contains "admin"`: {"checkout"},
		`where user != "ada"`:          {},
		`where status == "200"`:        {},
	}
	check := func(when string) {
		t.Helper()
		if got := attributesByMessage(latestLogs(t, env, 123, 10))["checkout"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected Attributes %v, got %v", when, want, got)
		}
		assertQueries(t, env, queries, when)
	}
	check("once stored")
	env.Reopen(t)
	check("after a restart")
}

// logJSON is one Log as a sender would send it: a message, a time and
// Attributes.
func logJSON(t *testing.T, message string, at time.Time, attributes map[string]any) string {
	t.Helper()
	object := map[string]any{"message": message, "time": at.Format(time.RFC3339Nano)}
	for name, value := range attributes {
		object[name] = value
	}
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("Encode Log: %v", err)
	}
	return string(raw)
}

// numberedKeys returns Attributes named <prefix>000, <prefix>001 … each with
// value.
func numberedKeys(prefix string, n int, value any) map[string]any {
	attributes := make(map[string]any, n)
	for i := range n {
		attributes[fmt.Sprintf("%s%03d", prefix, i)] = value
	}
	return attributes
}

func TestALogWithManyAttributesIsFoundByEachAndReadBackWhole(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Every built-in field, and 150 keys.
	wide := numberedKeys("k", 150, "v")
	wide["level"] = "info"
	wide["service"] = "web"
	wide["environment"] = "production"
	wide["trace_id"] = "4bf92f3577b34da6a3ce929d0e0e4736"
	wide["span_id"] = "00f067aa0ba902b7"
	postLogs(t, env, "application/json", logJSON(t, "wide", testNow.Add(-time.Minute), wide))
	env.ProcessLogs(t)

	// Then a Log with a new key and one of the same keys.
	env.Clock.Advance(time.Minute)
	postLogs(t, env, "application/json", logJSON(t, "after", testNow, map[string]any{"late": 1, "k000": "w"}))
	env.ProcessLogs(t)

	queries := map[string][]string{
		`where k000 == "v"`: {"wide"},
		`where k000 == "w"`: {"after"},
		`where k099 == "v"`: {"wide"},
		`where k100 == "v"`: {"wide"},
		`where k149 == "v"`: {"wide"},
		`where k120 != "v"`: {"after"},
		`where late == 1`:   {"after"},
	}
	attributes := attributesByMessage(latestLogs(t, env, 123, 10))
	if got, want := attributes["wide"], numberedKeys("k", 150, "v"); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected all 150 Attributes back, got %d: %v", len(got), got)
	}
	if got, want := attributes["after"], map[string]any{"late": 1.0, "k000": "w"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	assertQueries(t, env, queries, "once stored")
}

// postAndProcess sends one request of Logs and processes it before the next,
// so each is its own Log processing insert.
func postAndProcess(t *testing.T, env *TestEnv, logs ...string) {
	t.Helper()
	body := "[" + strings.Join(logs, ",") + "]"
	if counts := postLogs(t, env, "application/json", body); counts.Accepted != len(logs) {
		t.Fatalf("Expected %d Logs accepted, got %+v", len(logs), counts)
	}
	env.ProcessLogs(t)
}

// postWhileHot sends Logs with the clock at 29 minutes past each one's hour,
// as DuckDB's hot window once needed. A run of Logs of one hour is one
// request. It leaves the clock at the last Log's hour.
func postWhileHot(t *testing.T, env *TestEnv, logs ...string) {
	t.Helper()
	hourOf := func(log string) time.Time {
		var object struct {
			Time time.Time `json:"time"`
		}
		if err := json.Unmarshal([]byte(log), &object); err != nil {
			t.Fatalf("Decode Log %q: %v", log, err)
		}
		return object.Time.UTC().Truncate(time.Hour)
	}
	for len(logs) > 0 {
		hour := hourOf(logs[0])
		n := 1
		for n < len(logs) && hourOf(logs[n]).Equal(hour) {
			n++
		}
		env.Clock.Set(hour.Add(29 * time.Minute))
		postAndProcess(t, env, logs[:n]...)
		logs = logs[n:]
	}
}

func TestAttributesWithAwkwardNamesAreFoundAndReadBack(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Names that differ only in case, a name with a quote, a NUL or a dot, a
	// name that is a prefix of another, and one name carrying two types.
	postAndProcess(t, env,
		`{"message": "one", "time": "2026-10-02T11:59:00Z", "User": "a", "user": "b", "code": 5, "say \"hi\"": "q", "nul\u0000key": "n", "tags": "x", "tags.host": "h", "back`+"`"+`tick": "b", "C:\\dir": "d", "": "empty", "a..b": "dots", ".lead": "l"}`,
		`{"message": "two", "time": "2026-10-02T11:59:01Z", "code": "E5", "tags": {"host": "h2"}}`)

	queries := map[string][]string{
		`where User == "a"`:            {"one"},
		`where user == "b"`:            {"one"},
		`where user == "a"`:            {},
		`where code == 5`:              {"one"},
		`where code == "E5"`:           {"two"},
		`where ['say "hi"'] == "q"`:    {"one"},
		`where tags == "x"`:            {"one"},
		`where ['tags.host'] == "h"`:   {"one"},
		`where ['tags.host'] == "h2"`:  {"two"},
		"where ['back`tick'] == \"b\"": {"one"},
		`where ['C:\\dir'] == "d"`:     {"one"},
		`where [''] == "empty"`:        {"one"},
		`where ['a..b'] == "dots"`:     {"one"},
		`where ['.lead'] == "l"`:       {"one"},
	}
	check := func(when string) {
		t.Helper()
		attributes := attributesByMessage(latestLogs(t, env, 123, 10))
		want := map[string]any{"User": "a", "user": "b", "code": 5.0, "say \"hi\"": "q", "nul\u0000key": "n", "tags": "x", "tags.host": "h", "back`tick": "b", `C:\dir`: "d", "": "empty", "a..b": "dots", ".lead": "l"}
		if got := attributes["one"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		if got, want := attributes["two"], map[string]any{"code": "E5", "tags.host": "h2"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		assertQueries(t, env, queries, when)
	}
	check("once stored")
}

func TestLateAndFutureLogsAreFoundAtOnceByTheirAttributes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	now := testNow.Add(30 * time.Minute)
	env.Clock.Set(now)
	yesterday := testNow.AddDate(0, 0, -1)
	postAndProcess(t, env,
		logJSON(t, "yesterday", yesterday, map[string]any{"user": "ada", "status": 200}),
		logJSON(t, "five hours ago", now.Add(-5*time.Hour), map[string]any{"user": "eve", "status": 503}),
		logJSON(t, "a quarter-hour ago", time.Date(2026, 10, 2, 12, 14, 59, 0, time.UTC), map[string]any{"user": "bob"}),
		logJSON(t, "just now", time.Date(2026, 10, 2, 12, 15, 0, 0, time.UTC), map[string]any{"user": "cy"}),
		logJSON(t, "next hour", time.Date(2026, 10, 2, 13, 10, 0, 0, time.UTC), map[string]any{"user": "di"}),
	)

	logs := latestLogs(t, env, 123, 10)
	if got, want := messages(logs), []string{"next hour", "just now", "a quarter-hour ago", "five hours ago", "yesterday"}; !slices.Equal(got, want) {
		t.Fatalf("Expected %v, got %v", want, got)
	}
	if got, want := attributesByMessage(logs)["yesterday"], map[string]any{"user": "ada", "status": 200.0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected a late Log's Attributes %v, got %v", want, got)
	}

	// Each Log is found by its Attributes as soon as it is stored, whatever
	// its time.
	queries := map[string][]string{
		`where user == "ada"`: {"yesterday"},
		`where user == "eve"`: {"five hours ago"},
		`where user == "bob"`: {"a quarter-hour ago"},
		`where user == "cy"`:  {"just now"},
		`where user == "di"`:  {"next hour"},
		`where status == 200`: {"yesterday"},
		`where status >= 500`: {"five hours ago"},
		`where user != "cy"`:  {"next hour", "a quarter-hour ago", "five hours ago", "yesterday"},
	}
	assertQueries(t, env, queries, "with late and future Logs")

	// A Project whose only Log is late lists it too.
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	resp := sendLogs(t, env, "/api/456/logs", "application/json",
		logJSON(t, "only late", yesterday, map[string]any{"k": "v"}),
		map[string]string{"Authorization": "Bearer other_public_key"})
	decodeLogCounts(t, resp)
	env.ProcessLogs(t)
	if got, want := messages(latestLogs(t, env, 456, 10)), []string{"only late"}; !slices.Equal(got, want) {
		t.Errorf("Expected Project 456 to list %v, got %v", want, got)
	}
	assertProjectQueries(t, env, 456, map[string][]string{`where k == "v"`: {"only late"}}, "Project 456")
}

// assertListsUnchanged checks that a Project's latest Logs are still want,
// every field and Attribute alike.
func assertListsUnchanged(t *testing.T, env *TestEnv, projectID uint32, want []listedLog, when string) {
	t.Helper()
	if got := latestLogs(t, env, projectID, 100); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected the same Logs %s:\n want %+v\n got  %+v", when, want, got)
	}
}
