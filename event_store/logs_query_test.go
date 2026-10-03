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

	"github.com/solidtrace/event_store/storage"
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

	// zone joins the day's set partway through the day.
	postAndProcess(t, env, logJSON(t, "gus", at(12, 1), map[string]any{"zone": "z", "user": "gus"}))
	if key, ok := env.LogStore.PromotedSet(123, at(12, 1)).Lookup(storage.StringAttribute, "zone"); !ok || !key.Partway {
		t.Fatalf("Expected zone promoted partway through the day, got %+v %v", key, ok)
	}
	assertQueries(t, env, map[string][]string{
		`where zone == "z"`:   {"gus"},
		`where zone != "z"`:   {"ed", "fy", "cy", "bob", "ada", "di"},
		`search "z"`:          {"gus"},
		`where user == "gus"`: {"gus"},
	}, "with a key promoted partway through the day")
}

// pageThrough runs a Query page by page until no next cursor is left, and
// returns each page's messages.
func pageThrough(t *testing.T, env *TestEnv, req queryRequest) [][]string {
	t.Helper()
	var pages [][]string
	for range 20 {
		page := runQuery(t, env, 123, req)
		pages = append(pages, messages(page.Logs))
		if page.NextCursor == nil {
			return pages
		}
		req.Cursor = *page.NextCursor
	}
	t.Fatalf("Expected paging to end, got %v", pages)
	return nil
}

func TestPagesContinueFromTheCursorAcrossHoursAndSources(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(11, 30))
	// Three Logs share one time, so pages split them by id.
	postAndProcess(t, env,
		logJSON(t, "a", at(10, 5), nil),
		logJSON(t, "b", at(10, 15), nil),
		logJSON(t, "c", at(10, 15), nil),
		logJSON(t, "d", at(10, 15), nil),
		logJSON(t, "e", at(11, 0), nil),
		logJSON(t, "f", at(11, 20), map[string]any{"skip": true}),
		logJSON(t, "g", at(11, 25), nil),
	)
	env.Clock.Set(at(12, 5))
	env.ExportLogs(t) // hour 10 to a file
	postAndProcess(t, env, logJSON(t, "late", at(10, 10), nil))

	all := queryMessages(t, env, "")
	if len(all) != 8 {
		t.Fatalf("Expected 8 Logs, got %v", all)
	}
	pages := pageThrough(t, env, queryRequest{From: allTime.from, To: allTime.to, Limit: 3})
	if got := slices.Concat(pages...); !slices.Equal(got, all) {
		t.Errorf("Expected pages of 3 to list %v, got %v", all, pages)
	}
	if len(pages) != 3 || len(pages[0]) != 3 || len(pages[2]) != 2 {
		t.Errorf("Expected pages of 3, 3 and 2, got %v", pages)
	}

	pages = pageThrough(t, env, queryRequest{Query: `where skip != true`, From: allTime.from, To: allTime.to, Limit: 2})
	if got, want := slices.Concat(pages...), slices.DeleteFunc(slices.Clone(all), func(m string) bool { return m == "f" }); !slices.Equal(got, want) {
		t.Errorf("Expected filtered pages to list %v, got %v", want, pages)
	}

	// The time range is [from, to).
	page := runQuery(t, env, 123, queryRequest{From: at(10, 10), To: at(11, 20), Limit: 100})
	if got, want := messages(page.Logs), []string{"e", "d", "c", "b", "late"}; !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) || got[0] != "e" || got[4] != "late" {
		t.Errorf("Expected %v in [10:10, 11:20), got %v", want, got)
	}
	if page.NextCursor != nil {
		t.Errorf("Expected no next cursor on a short page, got %q", *page.NextCursor)
	}
}

func TestARareSearchFindsItsLogsNewestFirstAcrossDays(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	start := time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC)
	var expected []string
	// A Log every hour for two days, processed as they arrive; three carry
	// the rare value, in an Attribute or the message.
	for h := range 48 {
		now := start.Add(time.Duration(h) * time.Hour)
		env.Clock.Set(now)
		attributes := map[string]any{"user": fmt.Sprintf("user-%d", h)}
		message := fmt.Sprintf("hour %d", h)
		switch h {
		case 3:
			attributes["reason"] = "a NEEDLE in hour 3"
			expected = append(expected, message)
		case 20:
			message = "needle in the message, hour 20"
			expected = append(expected, message)
		case 41:
			attributes["detail"] = "needle"
			expected = append(expected, message)
		}
		postAndProcess(t, env, logJSON(t, message, now, attributes))
		if now.Minute() == 30 && h%5 == 0 {
			env.ExportLogs(t)
		}
	}
	slices.Reverse(expected)

	if got := queryMessages(t, env, `search "Needle"`); !slices.Equal(got, expected) {
		t.Errorf("Expected %v, got %v", expected, got)
	}
	env.Clock.Set(start.Add(50 * time.Hour))
	env.ExportLogs(t)
	if got := queryMessages(t, env, `search "Needle"`); !slices.Equal(got, expected) {
		t.Errorf("After every hour is exported, expected %v, got %v", expected, got)
	}
	page := runQuery(t, env, 123, queryRequest{Query: `search "needle"`, From: allTime.from, To: allTime.to, Limit: 2})
	if got := messages(page.Logs); !slices.Equal(got, expected[:2]) || page.NextCursor == nil {
		t.Errorf("Expected the newest 2 %v and a cursor, got %v", expected[:2], got)
	}
}

func TestQueriesAgreeAcrossPromotionTheDailySetAndDemotion(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	day1 := testNow
	day2 := testNow.AddDate(0, 0, 1)

	// Day 1's set fills with k000…k099, so extra stays in the maps.
	env.Clock.Set(day1)
	postAndProcess(t, env, logJSON(t, "full", day1, numberedKeys("k", 100, "a")))
	postAndProcess(t, env, logJSON(t, "extra on day 1", day1.Add(time.Minute), map[string]any{"extra": "x", "k000": "a"}))
	cases := map[string][]string{
		`where extra == "x"`:   {"extra on day 1"},
		`where k000 == "a"`:    {"extra on day 1", "full"},
		`where k099 == "a"`:    {"full"},
		`where k099 != "a"`:    {"extra on day 1"},
		`search "x"`:           {"extra on day 1"},
		`where region == "eu"`: {},
	}
	assertQueries(t, env, cases, "on day 1")

	// Day 2's set is day 1's most common keys, extra among them, so a key of
	// day 1's set leaves it, and its column goes at the export after 01:00.
	env.Clock.Set(day2)
	postAndProcess(t, env, logJSON(t, "day 2", day2, map[string]any{"extra": "x", "k099": "a", "k000": "b"}))
	set := env.LogStore.PromotedSet(123, day2)
	if len(set.Keys) != 100 {
		t.Fatalf("Expected day 2's set to be full, got %d keys", len(set.Keys))
	}
	cases[`where extra == "x"`] = []string{"day 2", "extra on day 1"}
	cases[`where k099 == "a"`] = []string{"day 2", "full"}
	cases[`where k099 != "a"`] = []string{"extra on day 1"}
	cases[`where k000 == "b"`] = []string{"day 2"}
	cases[`search "x"`] = []string{"day 2", "extra on day 1"}
	assertQueries(t, env, cases, "on day 2")

	env.ExportLogs(t) // day 1's hours, dropping its demoted columns
	assertQueries(t, env, cases, "after day 1 is exported")

	// A late Log of day 1 holds every Attribute in the maps.
	postAndProcess(t, env, logJSON(t, "late on day 1", day1.Add(2*time.Minute), map[string]any{"region": "eu", "k099": "a"}))
	cases[`where region == "eu"`] = []string{"late on day 1"}
	cases[`where k099 == "a"`] = []string{"day 2", "late on day 1", "full"}
	assertQueries(t, env, cases, "with a late Log of day 1")

	env.Clock.Set(day2.Add(3 * time.Hour))
	env.ExportLogs(t)
	env.Reopen(t)
	assertQueries(t, env, cases, "after day 2's hour is exported and a restart")
}
