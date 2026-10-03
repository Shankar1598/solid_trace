package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// queryRequest is a run of a Query, as the console sends it.
type queryRequest struct {
	Query  string    `json:"query"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Limit  int       `json:"limit,omitempty"`
	Cursor string    `json:"cursor,omitempty"`
}

// queryPage is a run's answer.
type queryPage struct {
	Logs       []listedLog `json:"logs"`
	NextCursor *string     `json:"next_cursor"`
}

// queryProblem is a mistake in a Query, as run and validate report it.
type queryProblem struct {
	Message   string `json:"message"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
}

// allTime is a time range wider than any Log's time in these tests.
var allTime = struct{ from, to time.Time }{
	from: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
	to:   time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
}

// postQueryApp posts a JSON body to the query app and returns the status and
// the raw answer.
func postQueryApp(t *testing.T, env *TestEnv, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Encode %v: %v", body, err)
	}
	req, _ := http.NewRequest("POST", path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.QueryApp.Test(req, 10000)
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	answer, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, answer
}

// runQuery runs a Query for a Project and returns its page of Logs.
func runQuery(t *testing.T, env *TestEnv, projectID uint32, req queryRequest) queryPage {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/logs/query", projectID), req)
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", req.Query, status, raw)
	}
	var page queryPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("Decode query page %q: %v", raw, err)
	}
	return page
}

// queryMessages runs a Query over all time for Project 123 and returns the
// messages of the Logs it matches, newest first.
func queryMessages(t *testing.T, env *TestEnv, query string) []string {
	t.Helper()
	return messages(runQuery(t, env, 123, queryRequest{Query: query, From: allTime.from, To: allTime.to, Limit: 1000}).Logs)
}

// assertQueries checks each Query's messages, newest first.
func assertQueries(t *testing.T, env *TestEnv, cases map[string][]string, when string) {
	t.Helper()
	for query, want := range cases {
		if got := queryMessages(t, env, query); !slices.Equal(got, want) {
			t.Errorf("%s: %s\n want %v\n got  %v", when, query, want, got)
		}
	}
}

func TestEachOperatorFindsLogsInTheHotTableFilesAndLateLogs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	yesterday := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)

	// At 11:30 the hot window is 10:00 to 12:00.
	env.Clock.Set(at(11, 30))
	postAndProcess(t, env,
		logJSON(t, "ada", at(10, 10), map[string]any{"status": 200, "user": "ada", "cached": true}),
		logJSON(t, "bob", at(10, 20), map[string]any{"status": "200", "user": "bob"}),
		logJSON(t, "cy", at(10, 30), map[string]any{"user": "cy", "level": "error"}),
		logJSON(t, "di", yesterday, map[string]any{"status": 200, "user": "di"}),
		logJSON(t, "ed", at(11, 10), map[string]any{"status": 404, "user": "ed", "level": "error", "note": "Disk FULL"}),
	)
	cases := map[string][]string{
		``:                                             {"ed", "cy", "bob", "ada", "di"},
		`logs`:                                         {"ed", "cy", "bob", "ada", "di"},
		`where status == 200`:                          {"ada", "di"},
		`logs | where status == "200"`:                 {"bob"},
		`where status != 200`:                          {"ed", "cy", "bob"},
		`where not(status == 200)`:                     {"ed", "cy", "bob"},
		`where status != "200"`:                        {"ed", "cy", "ada", "di"},
		`where cached == true`:                         {"ada"},
		`where cached != true`:                         {"ed", "cy", "bob", "di"},
		`where level == "error"`:                       {"ed", "cy"},
		`where level != "error"`:                       {"bob", "ada", "di"},
		`where level == 5`:                             {},
		`where level != 5`:                             {"ed", "cy", "bob", "ada", "di"},
		`where user == "ada" or user == "ed"`:          {"ed", "ada"},
		`where level == "error" and status == 404`:     {"ed"},
		`where not(level == "error" or user == "bob")`: {"ada", "di"},
		`where (user == "ada" or user == "bob") and status == 200`: {"ada"},
		`where status == 200 | where user == "di"`:                 {"di"},
		`where ['user'] == "cy"`:                                   {"cy"},
		`where missing == "x"`:                                     {},
		`search "full"`:                                            {"ed"},
		`search "DI"`:                                              {"ed", "di"},
		`search "o"`:                                               {"bob"},
		`where status == 200 | search "ADA"`:                       {"ada"},
	}
	assertQueries(t, env, cases, "all in the hot table and logs_late")

	// At 12:05 the window is 11:00 to 13:00: the export moves hour 10 and
	// yesterday's Log to files, and a Log of 10:40 arriving now is late and
	// stays in logs_late beside hour 10's file.
	env.Clock.Set(at(12, 5))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, at(10, 0), 1)
	assertQueries(t, env, cases, "after the export")

	postAndProcess(t, env, logJSON(t, "fy", at(10, 40), map[string]any{"status": 200, "user": "fy", "note": "full again"}))
	cases[``] = []string{"ed", "fy", "cy", "bob", "ada", "di"}
	cases[`logs`] = cases[``]
	cases[`where status == 200`] = []string{"fy", "ada", "di"}
	cases[`where status != "200"`] = []string{"ed", "fy", "cy", "ada", "di"}
	cases[`where cached != true`] = []string{"ed", "fy", "cy", "bob", "di"}
	cases[`where level != "error"`] = []string{"fy", "bob", "ada", "di"}
	cases[`where level != 5`] = cases[``]
	cases[`where not(level == "error" or user == "bob")`] = []string{"fy", "ada", "di"}
	cases[`search "full"`] = []string{"ed", "fy"}
	assertQueries(t, env, cases, "with a late Log beside the files")
}
