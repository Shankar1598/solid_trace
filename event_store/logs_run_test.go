package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// wholeNumber is how a whole number of milliseconds is written in JSON.
var wholeNumber = regexp.MustCompile(`^[0-9]+$`)

// answerOf posts a run to the query app, expects status, and returns the
// answer's top-level fields.
func answerOf(t *testing.T, env *TestEnv, path string, body map[string]any, status int) map[string]json.RawMessage {
	t.Helper()
	got, raw := postQueryApp(t, env, path, body)
	if got != status {
		t.Fatalf("POST %s %v expected %d, got %d: %s", path, body, status, got, raw)
	}
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode answer %q: %v", raw, err)
	}
	return answer
}

// onlyErrorMessage is the message of a run's 504 or 400, which must be the
// only error.
func onlyErrorMessage(t *testing.T, answer map[string]json.RawMessage) string {
	t.Helper()
	var errors []struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(answer["errors"], &errors); err != nil || len(errors) != 1 {
		t.Fatalf("Expected one error, got %s", answer["errors"])
	}
	return errors[0].Message
}

func TestEveryRunSaysHowLongItTook(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)
	setSQLQueries(t, env, true)

	runs := []struct {
		path string
		body map[string]any
		// has is a field the answer must have besides elapsed_ms.
		has string
	}{
		{"/api/123/logs/query", map[string]any{"query": `where duration > 100`}, "logs"},
		{"/api/123/logs/query", map[string]any{"query": `summarize count() by service`}, "table"},
		{"/api/123/logs/histogram", map[string]any{"query": ``}, "buckets"},
		{"/api/123/logs/sql", map[string]any{"sql": `SELECT count(*) FROM logs`}, "table"},
	}
	for _, run := range runs {
		run.body["from"], run.body["to"] = allTime.from, allTime.to
		answer := answerOf(t, env, run.path, run.body, 200)
		if _, ok := answer[run.has]; !ok {
			t.Errorf("%s %v: expected %s in the answer, got %v", run.path, run.body, run.has, answer)
		}
		if elapsed := string(answer["elapsed_ms"]); !wholeNumber.MatchString(elapsed) {
			t.Errorf("%s %v: expected elapsed_ms, a whole number of milliseconds, got %q", run.path, run.body, elapsed)
		}
	}
}

func TestARunWithinItsTimeoutAnswersAsUsual(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)
	setSQLQueries(t, env, true)

	rows := answerOf(t, env, "/api/123/logs/query", map[string]any{
		"query": `where duration > 100`, "from": allTime.from, "to": allTime.to, "timeout_seconds": 60}, 200)
	var page queryPage
	if err := json.Unmarshal(mustMarshal(t, rows), &page); err != nil || len(page.Logs) != 3 {
		t.Errorf("Expected 3 Logs within the timeout, got %v (%v)", rows["logs"], err)
	}
	table := answerOf(t, env, "/api/123/logs/query", map[string]any{
		"query": `summarize count() by service`, "from": allTime.from, "to": allTime.to, "timeout_seconds": 30}, 200)
	if !strings.Contains(string(table["table"]), `"rows":[`) {
		t.Errorf("Expected a table within the timeout, got %s", table["table"])
	}
	answerOf(t, env, "/api/123/logs/histogram", map[string]any{
		"query": ``, "from": allTime.from, "to": allTime.to, "timeout_seconds": 60}, 200)
	answerOf(t, env, "/api/123/logs/sql", map[string]any{
		"sql": `SELECT count(*) FROM logs`, "from": allTime.from, "to": allTime.to, "timeout_seconds": 60}, 200)
}

func TestARunPastItsTimeoutIsStoppedOnTheServer(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)
	setSQLQueries(t, env, true)

	// SQL that would run for minutes. The handler answers only once DuckDB
	// has returned, so a quick 504 means the query itself was stopped.
	slow := `SELECT sum(hash(i)) FROM range(1000000000000) t(i)`
	began := time.Now()
	answer := answerOf(t, env, "/api/123/logs/sql", map[string]any{
		"sql": slow, "from": allTime.from, "to": allTime.to, "timeout_seconds": 0.25}, 504)
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("Expected the SQL stopped soon after 0.25 s, took %v", took)
	}
	if got := onlyErrorMessage(t, answer); !strings.Contains(got, "stopped after 0.25 s") || !strings.Contains(got, "a simpler query") {
		t.Errorf("Expected a message saying the run was stopped after 0.25 s, and to try a simpler query, got %q", got)
	}

	// Runs of a Query and histograms hand their timeout to their scan: one
	// of a microsecond has ended before the scan takes a connection. That a
	// scan stops while DuckDB runs it is storage's test
	// (TestAScanPastItsContextIsStoppedPartWayAndItsConnectionStillReads),
	// and that a run of Log rows stops part-way, the test below.
	// A run after a batch reads no time range, so it isn't told to shorten
	// one.
	shorter, ingested := "try a shorter time range", "too many Logs were ingested since the last run"
	runs := []struct {
		path   string
		body   map[string]any
		advice string
	}{
		{"/api/123/logs/query", map[string]any{"query": `where duration > 100`}, shorter},
		{"/api/123/logs/query", map[string]any{"query": `summarize count() by bin(time, 5m)`}, shorter},
		{"/api/123/logs/histogram", map[string]any{"query": `where level == "error"`}, shorter},
		{"/api/123/logs/query", map[string]any{"query": `where duration > 100`, "after_batch": "00000000-0000-0000-0000-000000000000"}, ingested},
	}
	for _, run := range runs {
		run.body["from"], run.body["to"], run.body["timeout_seconds"] = allTime.from, allTime.to, 0.000001
		answer := answerOf(t, env, run.path, run.body, 504)
		if got := onlyErrorMessage(t, answer); !strings.Contains(got, "stopped after 0.000001 s") || !strings.Contains(got, run.advice) {
			t.Errorf("%s %v: expected a message saying the run was stopped, and %q, got %q", run.path, run.body, run.advice, got)
		}
	}

	// The stopped runs leave nothing behind that stops the next.
	if _, rows := runTable(t, env, `summarize count()`); len(rows) != 1 || rows[0] != "[5]" {
		t.Errorf("Expected a run after the timeouts to count 5 Logs, got %v", rows)
	}
	answerOf(t, env, "/api/123/logs/sql", map[string]any{
		"sql": `SELECT count(*) FROM logs`, "from": allTime.from, "to": allTime.to}, 200)
}

func TestARunOfLogRowsPastItsTimeoutIsStoppedPartWay(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// A year between two Logs is some 8,760 hours to read, one at a time,
	// for a Query no Log passes.
	seedProject(t, env.SQLitePath, 123, 400)
	postAndProcess(t, env, logJSON(t, "new", testNow, nil), logJSON(t, "old", testNow.AddDate(-1, 0, 0), nil))
	none := map[string]any{"query": `where message == "none"`, "from": allTime.from, "to": allTime.to}

	// Half a second reads only some of them.
	env.LogsHandler.ScanBudget = 500 * time.Millisecond
	if page := answerOf(t, env, "/api/123/logs/query", none, 200); string(page["next_cursor"]) == "null" {
		t.Fatalf("Expected a run to read for its budget and leave hours to go on from, got %s", mustMarshal(t, page))
	}

	// So a timeout of 0.25 s stops the run while it reads them.
	none["timeout_seconds"] = 0.25
	began := time.Now()
	answer := answerOf(t, env, "/api/123/logs/query", none, 504)
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("Expected the run stopped soon after 0.25 s, took %v", took)
	}
	if got := onlyErrorMessage(t, answer); !strings.Contains(got, "stopped after 0.25 s") {
		t.Errorf("Expected a message saying the run was stopped after 0.25 s, got %q", got)
	}
}

func TestATimeoutMustBeAPositiveNumberOfSeconds(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	for _, path := range []string{"/api/123/logs/query", "/api/123/logs/histogram", "/api/123/logs/sql"} {
		for _, timeout := range []any{0, -1, 24*60*60 + 1, "5"} {
			body := map[string]any{"query": ``, "sql": `SELECT 1`, "from": allTime.from, "to": allTime.to, "timeout_seconds": timeout}
			if status, raw := postQueryApp(t, env, path, body); status != 400 {
				t.Errorf("%s with timeout_seconds %v: expected 400, got %d: %s", path, timeout, status, raw)
			}
		}
	}
}

// mustMarshal writes a decoded answer back as JSON.
func mustMarshal(t *testing.T, answer map[string]json.RawMessage) []byte {
	t.Helper()
	raw, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("Encode %v: %v", answer, err)
	}
	return raw
}

// Each run Console asks with X-Log-SQL logs every SQL statement it ran
// for it, with the request id Console sent, so a Console call timing leads
// to the SQL its Query became. A run not asked logs none.
func TestEveryRunLogsTheSQLItRanWithItsRequestID(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)
	setSQLQueries(t, env, true)

	type run struct {
		path, read string
		body       map[string]any
		// ran is SQL one of the run's statements must hold.
		ran []string
	}
	runs := []run{
		{"/api/123/logs/query", "query", map[string]any{"query": `where duration > 100`}, []string{"duration"}},
		{"/api/123/logs/query", "match", map[string]any{"query": ``, "after_batch": "00000000-0000-0000-0000-000000000000"}, []string{"toUInt128(batch)"}},
		{"/api/123/logs/query", "query", map[string]any{"query": `summarize count() by service`}, []string{"count()", "GROUP BY"}},
		{"/api/123/logs/histogram", "histogram", map[string]any{"query": `where level == "error"`}, []string{"toStartOfInterval"}},
		// With no filter, the histogram reads the counts kept by minute.
		{"/api/123/logs/histogram", "histogram", map[string]any{"query": ``}, []string{"log_minute_counts"}},
	}
	if sqlOnClickHouse {
		runs = append(runs, run{"/api/123/logs/sql", "sql", map[string]any{"sql": `SELECT count(*) FROM logs`}, []string{"CREATE TEMPORARY VIEW logs AS", "SELECT count(*) FROM logs"}})
	}
	for i, run := range runs {
		logs := captureLogs(t)
		run.body["from"], run.body["to"] = allTime.from, allTime.to
		requestID := fmt.Sprintf("req-%d", i)
		raw, _ := json.Marshal(run.body)
		req, _ := http.NewRequest("POST", run.path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-Id", requestID)
		req.Header.Set("X-Log-SQL", "1")
		resp, err := env.QueryApp.Test(req, 10000)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("POST %s %v: %v %v", run.path, run.body, resp, err)
		}

		var lines []string
		for _, line := range strings.Split(logs.String(), "\n") {
			if strings.Contains(line, `message="Log read SQL"`) {
				lines = append(lines, line)
			}
		}
		if len(lines) == 0 {
			t.Fatalf("%s %v: expected its SQL logged, got %q", run.path, run.body, logs.String())
		}
		for n, line := range lines {
			for _, field := range []string{
				"kind=log_read_sql", "read=" + run.read, "project_id=123", "request_id=" + requestID,
				fmt.Sprintf("statement=%d", n+1), fmt.Sprintf("statements=%d", len(lines)), "duration_ms=", "sql=",
			} {
				if !strings.Contains(line, field) {
					t.Errorf("%s %v: expected %s in %q", run.path, run.body, field, line)
				}
			}
		}
		all := strings.Join(lines, "\n")
		for _, sql := range run.ran {
			if !strings.Contains(all, sql) {
				t.Errorf("%s %v: expected a statement with %q, got %q", run.path, run.body, sql, all)
			}
		}
	}
}

func TestARunNotAskedToLogItsSQLLogsNone(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)
	logs := captureLogs(t)

	answerOf(t, env, "/api/123/logs/query", map[string]any{"query": `where duration > 100`, "from": allTime.from, "to": allTime.to}, 200)

	if strings.Contains(logs.String(), "Log read SQL") {
		t.Errorf("Expected no SQL logged, got %q", logs.String())
	}
}
