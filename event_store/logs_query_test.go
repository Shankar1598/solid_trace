package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/logquery"
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

func TestASearchAnswersWhatItFoundOnceItsBudgetIsSpentAndPagesOn(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// A Log an hour for five hours; the newest and the oldest match.
	var logs []string
	for h := 1; h <= 5; h++ {
		logs = append(logs, logJSON(t, fmt.Sprintf("hour %d", h), testNow.Add(-time.Duration(h)*time.Hour+time.Minute),
			map[string]any{"rare": h == 1 || h == 5}))
	}
	postAndProcess(t, env, logs...)
	env.ExportLogs(t)

	// Every scan spends its budget on its first hour.
	env.LogsHandler.ScanBudget = time.Nanosecond
	req := queryRequest{Query: `where rare == true`, From: testNow.Add(-6 * time.Hour), To: testNow, Limit: 200}
	pages := pageThrough(t, env, req)
	if got, want := slices.Concat(pages...), []string{"hour 1", "hour 5"}; !slices.Equal(got, want) {
		t.Errorf("Expected the pages to find %v, got %v", want, pages)
	}
	if len(pages) != 5 || len(pages[0]) != 1 || len(pages[1]) != 0 {
		t.Errorf("Expected a page an hour, the first with its match and the next empty, got %v", pages)
	}

	// A take still gives no more than it asks for.
	req.Query = `where rare == true | take 1`
	if got := slices.Concat(pageThrough(t, env, req)...); !slices.Equal(got, []string{"hour 1"}) {
		t.Errorf("Expected take 1 to give one Log, got %v", got)
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

// validation is validate's answer.
type validation struct {
	Errors []queryProblem `json:"errors"`
	// Chips is the Builder's chips as JSON, or null.
	Chips  json.RawMessage  `json:"chips"`
	Stages []validatedStage `json:"stages"`
}

// validatedStage is where validate places one stage, and what it says of it.
type validatedStage struct {
	Line      int  `json:"line"`
	Column    int  `json:"column"`
	EndLine   int  `json:"end_line"`
	EndColumn int  `json:"end_column"`
	Filter    bool `json:"filter"`
	Builder   bool `json:"builder"`
}

// builderChips is validate's chips as compact JSON, leaving out the
// summarize, sort and take of a Query without them, or "null".
func builderChips(v validation) string {
	var chips *logquery.Chips
	if err := json.Unmarshal(v.Chips, &chips); err != nil || chips == nil {
		return "null"
	}
	shown := struct {
		Filters   []logquery.Chip     `json:"filters"`
		Search    *string             `json:"search"`
		Summarize *logquery.Summarize `json:"summarize,omitempty"`
		Sort      []logquery.SortChip `json:"sort,omitempty"`
		Take      *int                `json:"take,omitempty"`
	}{chips.Filters, chips.Search, chips.Summarize, chips.Sort, chips.Take}
	var out strings.Builder
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.Encode(shown)
	return strings.TrimSpace(out.String())
}

func validateQuery(t *testing.T, env *TestEnv, query string) validation {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/validate", map[string]string{"query": query})
	if status != 200 {
		t.Fatalf("Validate %q expected 200, got %d: %s", query, status, raw)
	}
	var v validation
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("Decode validation %q: %v", raw, err)
	}
	return v
}

func TestValidateGivesChipsForBuilderQueriesAndPlacesMistakes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	chips := map[string]string{
		``:     `{"filters":[],"search":null}`,
		`logs`: `{"filters":[],"search":null}`,
		`where status == 200 | search "time out"`: `{"filters":[{"field":"status","op":"==","value":200}],"search":"time out"}`,
		"logs\n| where level != \"error\"\n| where cached == true | where ['http.status'] == -1.5": `{"filters":[` +
			`{"field":"level","op":"!=","value":"error"},` +
			`{"field":"cached","op":"==","value":true},` +
			`{"field":"http.status","op":"==","value":-1.5}],"search":null}`,
		`search "" | where http.method == 'GET'`: `{"filters":[{"field":"http.method","op":"==","value":"GET"}],"search":""}`,
		// Every operator the Builder offers.
		`where duration >= 500 | where path startswith "/api" | where tags !contains "beta" | where status in (500, "502") | where user !in ("a") | where isnotnull(trace_id) | where isnull(['user.id'])`: `{"filters":[` +
			`{"field":"duration","op":">=","value":500},` +
			`{"field":"path","op":"startswith","value":"/api"},` +
			`{"field":"tags","op":"!contains","value":"beta"},` +
			`{"field":"status","op":"in","value":[500,"502"]},` +
			`{"field":"user","op":"!in","value":["a"]},` +
			`{"field":"trace_id","op":"exists","value":null},` +
			`{"field":"user.id","op":"missing","value":null}],"search":null}`,
		// Summarize, sort and take after the filters.
		`where level == "error" | summarize count(), avg(duration_ms), percentile(duration_ms, 95), dcount(user.id) by service, bin(_time, 5m) | sort by count_ desc, service asc | take 20`: `{"filters":[{"field":"level","op":"==","value":"error"}],"search":null,` +
			`"summarize":{"aggregates":[{"fn":"count"},{"fn":"avg","field":"duration_ms"},{"fn":"percentile","field":"duration_ms","percent":95},{"fn":"dcount","field":"user.id"}],` +
			`"by":[{"field":"service"},{"field":"time","bin":"5m"}]},` +
			`"sort":[{"column":"count_","asc":false},{"column":"service","asc":true}],"take":20}`,
		`summarize count() by bin_auto(time) | limit 5`: `{"filters":[],"search":null,"summarize":{"aggregates":[{"fn":"count"}],"by":[{"field":"time","bin":"auto"}]},"take":5}`,
		`summarize by service`:                          `{"filters":[],"search":null,"summarize":{"aggregates":[],"by":[{"field":"service"}]}}`,
		`sort by duration asc`:                          `{"filters":[],"search":null,"sort":[{"column":"duration","asc":true}]}`,
	}
	for query, want := range chips {
		v := validateQuery(t, env, query)
		got := builderChips(v)
		if len(v.Errors) != 0 || got != want {
			t.Errorf("%q: expected chips %s, got %s and errors %+v", query, want, got, v.Errors)
		}
	}

	// Valid Queries the Builder can't show.
	for _, query := range []string{
		`where a == 1 or b == 2`,
		`where a == 1 and b == 2`,
		`where not(a == 1)`,
		`search "a" | search "b"`,
		`where time > ago(1h)`,
		`where a == b + 1`,
		`where a matches regex "x"`,
		`where a contains_cs "x"`,
		`where isempty(a)`,
		`take 5 | where a == 1`,
		`summarize count() | where count_ > 1`,
		`take 5 | sort by a`,
		`summarize n = count()`,
		`summarize countif(a == 1)`,
		`summarize count() by bin(duration, 10)`,
		`summarize count() by s = service`,
		`summarize count() | summarize count()`,
		`sort by a nulls first`,
		`sort by tolower(a)`,
		`project a`,
		`top 5 by a`,
		`count`,
	} {
		if v := validateQuery(t, env, query); len(v.Errors) != 0 || builderChips(v) != "null" {
			t.Errorf("%q: expected no chips and no errors, got %+v", query, v)
		}
	}

	mistakes := map[string]queryProblem{
		"where status == 200\n| mv-expand tags": {Message: `unknown stage "mv"`, Line: 2, Column: 3, EndLine: 2, EndColumn: 5},
		`where startswith(message, "x")`:        {Message: `unknown function "startswith"`, Line: 1, Column: 7, EndLine: 1, EndColumn: 17},
		`where status = 200`:                    {Message: `expected | or the end of the Query, got "="`, Line: 1, Column: 14, EndLine: 1, EndColumn: 15},
		`where message == "unterminated`:        {Message: "unterminated string", Line: 1, Column: 18, EndLine: 1, EndColumn: 31},
		`where user == "é" | bogus`:             {Message: `unknown stage "bogus"`, Line: 1, Column: 21, EndLine: 1, EndColumn: 26},
		`where level == "x" |`:                  {Message: "expected a stage, such as where, got the end of the Query", Line: 1, Column: 21, EndLine: 1, EndColumn: 21},
		`where status == other`:                 {Message: "expected a value: a string, a number, true or false", Line: 1, Column: 17, EndLine: 1, EndColumn: 22},
		`where status`:                          {Message: `expected a condition, such as level == "error"`, Line: 1, Column: 7, EndLine: 1, EndColumn: 13},
		`search timeout`:                        {Message: `search needs a quoted string, such as search "timeout"`, Line: 1, Column: 8, EndLine: 1, EndColumn: 15},
	}
	for query, want := range mistakes {
		v := validateQuery(t, env, query)
		if len(v.Errors) != 1 || v.Errors[0] != want || builderChips(v) != "null" {
			t.Errorf("%q: expected %+v and no chips, got %+v", query, want, v)
		}
	}

	// A mistake leaves no stages to place.
	if v := validateQuery(t, env, `where status = 200`); v.Stages == nil || len(v.Stages) != 0 {
		t.Errorf("Expected no stages for a Query with mistakes, got %+v", v.Stages)
	}

	// A run refuses a Query with mistakes the same way.
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: `where startswith(message, "x")`, From: allTime.from, To: allTime.to})
	var refused struct {
		Errors []queryProblem `json:"errors"`
	}
	json.Unmarshal(raw, &refused)
	if status != 400 || len(refused.Errors) != 1 || refused.Errors[0] != mistakes[`where startswith(message, "x")`] {
		t.Errorf("Expected 400 with the mistake, got %d: %s", status, raw)
	}

	// A run needs its time range, and a cursor it gave.
	for name, req := range map[string]any{
		"no time range":    map[string]string{"query": ""},
		"from after to":    queryRequest{From: allTime.to, To: allTime.from},
		"a forged cursor":  queryRequest{From: allTime.from, To: allTime.to, Cursor: "bm90IGEgY3Vyc29y"},
		"a negative limit": queryRequest{From: allTime.from, To: allTime.to, Limit: -1},
	} {
		if status, raw := postQueryApp(t, env, "/api/123/logs/query", req); status != 400 {
			t.Errorf("Expected a run with %s to be 400, got %d: %s", name, status, raw)
		}
	}
}

func TestValidatePlacesEachStageAndSaysWhichTheBuilderCanShow(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	cases := map[string][]validatedStage{
		"": {},
		"logs\n| where service == \"api\"\n| where duration > 500\n| summarize count() by service": {
			{Line: 2, Column: 3, EndLine: 2, EndColumn: 25, Filter: true, Builder: true},
			{Line: 3, Column: 3, EndLine: 3, EndColumn: 23, Filter: true, Builder: true},
			{Line: 4, Column: 3, EndLine: 4, EndColumn: 31, Filter: false, Builder: true},
		},
		`search "é" | search "b" | where a == 1`: {
			{Line: 1, Column: 1, EndLine: 1, EndColumn: 11, Filter: true, Builder: true},
			{Line: 1, Column: 14, EndLine: 1, EndColumn: 24, Filter: true, Builder: false},
			{Line: 1, Column: 27, EndLine: 1, EndColumn: 39, Filter: true, Builder: true},
		},
		"take 5 | where a == 1": {
			{Line: 1, Column: 1, EndLine: 1, EndColumn: 7, Filter: false, Builder: true},
			{Line: 1, Column: 10, EndLine: 1, EndColumn: 22, Filter: false, Builder: false},
		},
	}
	for query, want := range cases {
		v := validateQuery(t, env, query)
		if len(v.Errors) != 0 || !slices.Equal(v.Stages, want) {
			t.Errorf("%q: expected stages %+v, got %+v and errors %+v", query, want, v.Stages, v.Errors)
		}
	}
}

// fetchLog fetches one Log by id, with its time as a hint if at is set.
func fetchLog(t *testing.T, env *TestEnv, projectID uint32, id string, at *time.Time) (int, listedLog) {
	t.Helper()
	path := fmt.Sprintf("/api/%d/logs/%s", projectID, id)
	if at != nil {
		path += "?time=" + at.Format(time.RFC3339Nano)
	}
	req, _ := http.NewRequest("GET", path, nil)
	resp, err := env.QueryApp.Test(req, 10000)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	var log listedLog
	if resp.StatusCode == 200 {
		if err := json.Unmarshal(raw, &log); err != nil {
			t.Fatalf("Decode Log %q: %v", raw, err)
		}
	}
	return resp.StatusCode, log
}

func TestALogIsFetchedByIDWhereverItIsStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(11, 30))
	postAndProcess(t, env,
		logJSON(t, "exported", at(10, 10), map[string]any{"user": "ada", "status": 200}),
		logJSON(t, "hot", at(11, 10), map[string]any{"user": "bob"}),
	)
	env.Clock.Set(at(12, 5))
	env.ExportLogs(t)
	postAndProcess(t, env, logJSON(t, "late", at(10, 20), map[string]any{"user": "cy", "cached": true}))

	logs := latestLogs(t, env, 123, 10)
	if len(logs) != 3 {
		t.Fatalf("Expected 3 Logs, got %v", messages(logs))
	}
	for _, want := range logs {
		for _, hint := range []*time.Time{nil, &want.Time} {
			status, got := fetchLog(t, env, 123, want.ID, hint)
			if status != 200 || !reflect.DeepEqual(got, want) {
				t.Errorf("Fetch %q (time hint %v): expected %+v, got %d %+v", want.Message, hint != nil, want, status, got)
			}
		}
	}

	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	if status, _ := fetchLog(t, env, 456, logs[0].ID, nil); status != 404 {
		t.Errorf("Expected another Project's Log to be 404, got %d", status)
	}
	if status, _ := fetchLog(t, env, 123, "0192d6a0-0000-7000-8000-000000000000", nil); status != 404 {
		t.Errorf("Expected an unknown id to be 404, got %d", status)
	}
	if status, _ := fetchLog(t, env, 123, "not-a-uuid", nil); status != 400 {
		t.Errorf("Expected a malformed id to be 400, got %d", status)
	}
}
