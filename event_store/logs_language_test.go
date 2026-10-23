package main_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// languageDay is the day the language tests' Logs are on.
func languageAt(hour, minute int) time.Time {
	return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC)
}

// seedLanguageLogs stores five Logs, newest first ed, cy, bob, ada and di
// (the day before), each hour's soon after it. It leaves the clock at 11:30.
func seedLanguageLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(10, 29))
	postAndProcess(t, env,
		logJSON(t, "ada", languageAt(10, 10), map[string]any{
			"status": 200, "user": "ada", "duration": 120, "path": "/api/users", "service": "api"}),
		logJSON(t, "bob", languageAt(10, 20), map[string]any{
			"status": "200", "user": "Bob", "duration": 80.5, "path": "/health", "service": "web"}),
		logJSON(t, "cy", languageAt(10, 30), map[string]any{
			"user": "cy", "level": "error", "duration": 300, "path": "/api/orders", "note": "", "service": "api"}),
		logJSON(t, "di", languageAt(10, 40).AddDate(0, 0, -1), map[string]any{
			"status": 500, "user": "di", "duration": 50, "service": "worker"}),
	)
	env.Clock.Set(languageAt(11, 29))
	postAndProcess(t, env,
		logJSON(t, "ed", languageAt(11, 10), map[string]any{
			"status": 404, "user": "ed", "level": "error", "duration": 900, "note": "Disk FULL", "path": "/API/users", "service": "api"}),
	)
	env.Clock.Set(languageAt(11, 30))
}

// postLateLanguageLog adds fy, a Log of 10:45 sent at 12:05.
func postLateLanguageLog(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(12, 5))
	postAndProcess(t, env, logJSON(t, "fy", languageAt(10, 45), map[string]any{
		"status": 200, "user": "fy", "duration": 10, "path": "/api/users/7", "service": "api"}))
}

// Until SQL mode moves to ClickHouse, in ticket 07, the root tests skip
// their checks of a Query's Readable SQL run in SQL mode. This turns them
// back on.
const sqlOnClickHouse = false // 07

func TestWhereComparesMatchesAndTestsFields(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	cases := map[string][]string{
		`where duration > 100`:                         {"ed", "cy", "ada"},
		`where duration >= 300`:                        {"ed", "cy"},
		`where duration < 100`:                         {"bob", "di"},
		`where duration <= 80.5`:                       {"bob", "di"},
		`where status < 300`:                           {"ada"},
		`where status >= 404`:                          {"ed", "di"},
		`where not(duration > 100)`:                    {"bob", "di"},
		`where path contains "USERS"`:                  {"ed", "ada"},
		`where path contains_cs "API"`:                 {"ed"},
		`where path !contains "users"`:                 {"cy", "bob", "di"},
		`where path !contains_cs "API"`:                {"cy", "bob", "ada", "di"},
		`where message contains "D"`:                   {"ed", "ada", "di"},
		`where path startswith "/api"`:                 {"ed", "cy", "ada"},
		`where path !startswith "/api"`:                {"bob", "di"},
		`where user matches regex "^[a-c]"`:            {"cy", "ada"},
		`where user in ("ada", "ed")`:                  {"ed", "ada"},
		`where status in (200, 404)`:                   {"ed", "ada"},
		`where status !in (200, 404)`:                  {"cy", "bob", "di"},
		`where level in ("error")`:                     {"ed", "cy"},
		`where isempty(note)`:                          {"cy", "bob", "ada", "di"},
		`where isnotempty(note)`:                       {"ed"},
		`where isnotempty(status)`:                     {"ed", "bob", "ada", "di"},
		`where duration > 100 and path contains "api"`: {"ed", "cy", "ada"},
		`where user == "bob"`:                          {},
		`where user =~ "bob"`:                          {"bob"},
	}
	cases[`where path startswith_cs "/API"`] = []string{"ed"}
	cases[`where path !startswith_cs "/api"`] = []string{"ed", "bob", "di"}
	cases[`where user !~ "BOB"`] = []string{"ed", "cy", "ada", "di"}
	cases[`where -duration < -500 or duration % 100 == 20`] = []string{"ed", "ada"}
	assertQueries(t, env, cases, "once stored")

	postLateLanguageLog(t, env)
	cases[`where user !~ "BOB"`] = []string{"ed", "fy", "cy", "ada", "di"}
	cases[`where path contains "USERS"`] = []string{"ed", "fy", "ada"}
	cases[`where path startswith "/api"`] = []string{"ed", "fy", "cy", "ada"}
	cases[`where user matches regex "^f"`] = []string{"fy"}
	cases[`where duration < 100`] = []string{"fy", "bob", "di"}
	cases[`where duration <= 80.5`] = []string{"fy", "bob", "di"}
	cases[`where status < 300`] = []string{"fy", "ada"}
	cases[`where not(duration > 100)`] = []string{"fy", "bob", "di"}
	cases[`where path !contains_cs "API"`] = []string{"fy", "cy", "bob", "ada", "di"}
	cases[`where status in (200, 404)`] = []string{"ed", "fy", "ada"}
	cases[`where isempty(note)`] = []string{"fy", "cy", "bob", "ada", "di"}
	cases[`where isnotempty(status)`] = []string{"ed", "fy", "bob", "ada", "di"}
	cases[`where duration > 100 and path contains "api"`] = []string{"ed", "cy", "ada"}
	assertQueries(t, env, cases, "with a late Log")
}

func TestContainsFindsTextInArrays(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	env.Clock.Set(languageAt(10, 29))
	postAndProcess(t, env,
		logJSON(t, "tagged", languageAt(10, 10), map[string]any{"tags": []any{"Checkout", "db"}}),
		logJSON(t, "named", languageAt(10, 20), map[string]any{"tags": "checkout"}),
		logJSON(t, "untagged", languageAt(10, 30), map[string]any{"user": "ada"}),
	)
	cases := map[string][]string{
		`where tags contains "checkout"`:     {"named", "tagged"},
		`where tags contains_cs "Checkout"`:  {"tagged"},
		`where tags !contains "db"`:          {"untagged", "named"},
		`where tags !contains_cs "Checkout"`: {"untagged", "named"},
	}
	assertQueries(t, env, cases, "once stored")
	for query, want := range cases {
		if !sqlOnClickHouse {
			break
		}
		_, rows := sqlTable(t, env, sqlRequest{SQL: readableSQL(t, env, query), From: allTime.from, To: allTime.to})
		if len(rows) != len(want) {
			t.Errorf("Readable SQL of %s: expected %d rows, got %v", query, len(want), rows)
		}
	}

	env.Reopen(t)
	assertQueries(t, env, cases, "after a restart")
}

func TestWhereOnTimeNarrowsTheRangeButNeverWidensIt(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env) // the clock reads 11:30

	inRange := func(query string, from, to time.Time) []string {
		t.Helper()
		return messages(runQuery(t, env, 123, queryRequest{Query: query, From: from, To: to, Limit: 100}).Logs)
	}
	cases := []struct {
		query    string
		from, to time.Time
		want     []string
	}{
		{`where time >= datetime(2026-10-02T10:15:00Z)`, allTime.from, allTime.to, []string{"ed", "cy", "bob"}},
		{`where time < datetime(2026-10-02 10:25)`, languageAt(10, 15), languageAt(12, 0), []string{"bob"}},
		{`where time == datetime("2026-10-02T10:20:00Z")`, allTime.from, allTime.to, []string{"bob"}},
		{`where time > ago(1h)`, allTime.from, allTime.to, []string{"ed"}},
		{`where _time > ago(1h)`, allTime.from, allTime.to, []string{"ed"}},
		{`where time > now() - 1h`, allTime.from, allTime.to, []string{"ed"}},
		{`where time >= ago(2h) and time < ago(1h)`, allTime.from, allTime.to, []string{"bob", "ada"}},
		{`where datetime(2026-10-02) > time`, allTime.from, allTime.to, []string{"di"}},
		// Never wider than the request's range.
		{`where time > datetime(2026-10-01)`, languageAt(10, 0), languageAt(11, 0), []string{"cy", "bob", "ada"}},
		{`where time < datetime(2100-01-01)`, languageAt(10, 0), languageAt(10, 25), []string{"bob", "ada"}},
		{`where time > datetime(2000-01-01) or user == "ed"`, languageAt(10, 0), languageAt(10, 25), []string{"bob", "ada"}},
		{`where time > datetime(2026-10-02T11:00) | where time < datetime(2026-10-02T10:00)`, allTime.from, allTime.to, []string{}},
	}
	for _, c := range cases {
		if got := inRange(c.query, c.from, c.to); !slices.Equal(got, c.want) {
			t.Errorf("%s in [%s, %s): want %v, got %v", c.query, c.from.Format(time.TimeOnly), c.to.Format(time.TimeOnly), c.want, got)
		}
	}
}

func TestAWhereOnTimeOutsideTheRangeGivesAnEmptyTableAndHistogram(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env) // the clock reads 11:30

	// The where narrows the range to nothing.
	const outside = `where time > datetime(2030-01-01)`
	from, to := languageAt(0, 0), languageAt(13, 0)
	check := func(when string) {
		t.Helper()
		for query, want := range map[string][]string{
			outside + " | summarize count() by user":          nil,
			outside + " | summarize count() by bin(time, 1h)": nil,
			outside + " | summarize n = count()":              {"[0]"},
		} {
			status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: query, From: from, To: to})
			var answer tableAnswer
			if status != 200 || json.Unmarshal(raw, &answer) != nil || answer.Table == nil {
				t.Errorf("%s: %s: expected 200 with a table, got %d: %s", when, query, status, raw)
				continue
			}
			var rows []string
			for _, row := range answer.Table.Rows {
				rows = append(rows, string(row))
			}
			if !slices.Equal(rows, want) {
				t.Errorf("%s: %s: want rows %v, got %v", when, query, want, rows)
			}
		}
		if answer := histogram(t, env, histogramRequest{Query: outside, From: from, To: to, BucketSeconds: 3600}); answer.Count != 0 {
			t.Errorf("%s: expected the histogram to count no Logs, got %d", when, answer.Count)
		}
	}
	check("once stored")
	postLateLanguageLog(t, env)
	check("with a late Log")
}

// tableAnswer is a run's answer for a Query that returns a table.
type tableAnswer struct {
	Table *struct {
		Columns []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
		Rows      []json.RawMessage `json:"rows"`
		Truncated bool              `json:"truncated"`
	} `json:"table"`
	Logs []listedLog `json:"logs"`
}

// runTable runs a Query over all time for Project 123 and returns its
// table's column names and its rows as JSON, one row per element.
func runTable(t *testing.T, env *TestEnv, query string) ([]string, []string) {
	t.Helper()
	return runTableIn(t, env, query, allTime.from, allTime.to)
}

// runTableIn is runTable over [from, to).
func runTableIn(t *testing.T, env *TestEnv, query string, from, to time.Time) ([]string, []string) {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: query, From: from, To: to})
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
	}
	var answer tableAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode table %q: %v", raw, err)
	}
	if answer.Table == nil || answer.Logs != nil {
		t.Fatalf("Query %q expected a table and no Logs, got %s", query, raw)
	}
	var columns, rows []string
	for _, col := range answer.Table.Columns {
		columns = append(columns, col.Name)
	}
	for _, row := range answer.Table.Rows {
		rows = append(rows, string(row))
	}
	return columns, rows
}

// tableCase is a Query and the table it returns: its columns, and its rows
// as JSON. unordered compares the rows in any order.
type tableCase struct {
	columns   string
	rows      []string
	unordered bool
}

// assertTables runs each Query over all time and checks its table.
func assertTables(t *testing.T, env *TestEnv, cases map[string]tableCase, when string) {
	t.Helper()
	assertTablesIn(t, env, cases, allTime.from, allTime.to, when)
}

// assertTablesIn is assertTables over [from, to).
func assertTablesIn(t *testing.T, env *TestEnv, cases map[string]tableCase, from, to time.Time, when string) {
	t.Helper()
	for query, want := range cases {
		columns, rows := runTableIn(t, env, query, from, to)
		if want.unordered {
			slices.Sort(rows)
			want.rows = slices.Sorted(slices.Values(want.rows))
		}
		if got := strings.Join(columns, ","); got != want.columns || !slices.Equal(rows, want.rows) {
			t.Errorf("%s: %s\n want %s %v\n got  %s %v", when, query, want.columns, want.rows, got, rows)
		}
	}
}

func TestTabularStagesReturnTables(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	cases := map[string]tableCase{
		`summarize count() by level`: {"level,count_", []string{`["error",2]`, `[null,3]`}, false},
		`summarize count()`:          {"count_", []string{`[5]`}, false},
		`summarize n = count(), avg(duration), max(duration), min(duration), sum(duration) by service`: {
			"service,n,avg_duration,max_duration,min_duration,sum_duration",
			[]string{`["api",3,440,900,120,1320]`, `["web",1,80.5,80.5,80.5,80.5]`, `["worker",1,50,50,50,50]`}, false},
		`summarize dcount(service), dcount(user)`:                    {"dcount_service,dcount_user", []string{`[3,5]`}, false},
		`summarize percentile(duration, 100)`:                        {"percentile_duration_100", []string{`[900]`}, false},
		`summarize count() by bin(time, 1h)`:                         {"time,count_", []string{`["2026-10-01T10:00:00Z",1]`, `["2026-10-02T10:00:00Z",3]`, `["2026-10-02T11:00:00Z",1]`}, false},
		`summarize count() by status`:                                {"status,count_", []string{`[200,1]`, `["200",1]`, `[null,1]`, `[500,1]`, `[404,1]`}, true},
		`summarize count() by tostring(status)`:                      {"status,count_", []string{`["200",2]`, `["404",1]`, `["500",1]`, `[null,1]`}, false},
		`summarize avg(duration) by level | where isnotempty(level)`: {"level,avg_duration", []string{`["error",600]`}, false},
		`project message, status, duration`: {"message,status,duration",
			[]string{`["ada",200,120]`, `["bob","200",80.5]`, `["cy",null,300]`, `["di",500,50]`, `["ed",404,900]`}, false},
		`project message, ms = duration * 1000 | where ms > 200000 | sort by ms asc`: {"message,ms", []string{`["cy",300000]`, `["ed",900000]`}, false},
		`extend slow = duration > 100 | where slow == true | project message`:        {"message", []string{`["ada"]`, `["cy"]`, `["ed"]`}, false},
		`sort by duration desc | take 2 | project message, duration`:                 {"message,duration", []string{`["ed",900]`, `["cy",300]`}, false},
		`top 2 by duration asc | project message`:                                    {"message", []string{`["di"]`, `["bob"]`}, false},
		`top 1 by duration`: {"time,level,message,service,environment,trace_id,span_id,duration",
			[]string{`["2026-10-02T11:10:00Z","error","ed","api",null,null,null,900]`}, false},
		`count`:                          {"Count", []string{`[5]`}, false},
		`where level == "error" | count`: {"Count", []string{`[2]`}, false},
		`distinct service`:               {"service", []string{`["api"]`, `["web"]`, `["worker"]`}, false},
		`distinct service, level`:        {"service,level", []string{`["api","error"]`, `["api",null]`, `["web",null]`, `["worker",null]`}, false},
		`summarize count() by service | sort by count_ desc | take 1`: {"service,count_", []string{`["api",3]`}, false},
	}
	assertTables(t, env, cases, "once stored")

	env.Reopen(t)
	assertTables(t, env, cases, "after a restart")

	// A late Log of hour 10.
	env.Clock.Set(languageAt(12, 5))
	postAndProcess(t, env, logJSON(t, "fy", languageAt(10, 45), map[string]any{"duration": 10, "service": "api"}))
	assertTables(t, env, map[string]tableCase{
		`summarize count() by bin(time, 1h)`:      {"time,count_", []string{`["2026-10-01T10:00:00Z",1]`, `["2026-10-02T10:00:00Z",4]`, `["2026-10-02T11:00:00Z",1]`}, false},
		`top 1 by duration asc | project message`: {"message", []string{`["fy"]`}, false},
	}, "with a late Log")
}

func TestTakeAndANewestFirstSortKeepLogRows(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	assertQueries(t, env, map[string][]string{
		`where level == "error" | take 1`:  {"ed"},
		`sort by time desc | take 2`:       {"ed", "cy"},
		`order by _time desc`:              {"ed", "cy", "bob", "ada", "di"},
		`take 4 | take 2`:                  {"ed", "cy"},
		`take 0`:                           {},
		`limit 3 | sort by time desc`:      {"ed", "cy", "bob"},
		`where duration > 100 | limit 100`: {"ed", "cy", "ada"},
	}, "once stored")

	// Pages stop at take's number of Logs.
	pages := pageThrough(t, env, queryRequest{Query: `take 3`, From: allTime.from, To: allTime.to, Limit: 2})
	if want := [][]string{{"ed", "cy"}, {"bob"}}; !reflect.DeepEqual(pages, want) {
		t.Errorf("Expected take 3 in pages of 2 to give %v, got %v", want, pages)
	}
	pages = pageThrough(t, env, queryRequest{Query: `take 4`, From: allTime.from, To: allTime.to, Limit: 2})
	if want := [][]string{{"ed", "cy"}, {"bob", "ada"}}; !reflect.DeepEqual(pages, want) {
		t.Errorf("Expected take 4 in pages of 2 to give %v, got %v", want, pages)
	}
}

func TestPastedAxiomStyleQueriesWork(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env) // the clock reads 11:30

	run := func(query string, from, to time.Time) string {
		t.Helper()
		status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: query, From: from, To: to})
		if status != 200 {
			t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
		}
		var answer struct {
			Table struct {
				Columns []struct {
					Name string `json:"name"`
				} `json:"columns"`
				Rows json.RawMessage `json:"rows"`
			} `json:"table"`
		}
		json.Unmarshal(raw, &answer)
		var names []string
		for _, col := range answer.Table.Columns {
			names = append(names, col.Name)
		}
		return strings.Join(names, ",") + " " + string(answer.Table.Rows)
	}
	day := []time.Time{languageAt(0, 0), languageAt(0, 0).AddDate(0, 0, 1)}
	cases := []struct {
		query    string
		from, to time.Time
		want     string
	}{
		{"['my-dataset']\n| where _time > ago(1h)\n| summarize count() by bin_auto(_time)", languageAt(10, 0), languageAt(12, 0),
			`time,count_ [["2026-10-02T11:10:00Z",1]]`},
		{"['my-dataset'] | summarize count() by bin_auto(_time)", languageAt(10, 0), languageAt(12, 0),
			`time,count_ [["2026-10-02T10:10:00Z",1],["2026-10-02T10:20:00Z",1],["2026-10-02T10:30:00Z",1],["2026-10-02T11:10:00Z",1]]`},
		{`['logs'] | where level == "error" | summarize count() by service`, day[0], day[1], `service,count_ [["api",2]]`},
		{`['logs'] | where ['duration'] > 100 | project _time, message, duration | sort by _time desc | take 2`, day[0], day[1],
			`time,message,duration [["2026-10-02T11:10:00Z","ed",900],["2026-10-02T10:30:00Z","cy",300]]`},
		{`where isnotnull(status) | summarize count() by toint(status) | sort by count_ desc, status asc`, allTime.from, allTime.to,
			`status,count_ [[200,2],[404,1],[500,1]]`},
		{`where message contains "e" or duration >= 100 | summarize users = dcount(user), slowest = max(duration) by service`, allTime.from, allTime.to,
			`service,users,slowest [["api",3,900]]`},
		{`summarize errors = countif(level == "error"), total = count() by service | extend rate = errors * 100 / total | where total > 1`, allTime.from, allTime.to,
			`service,errors,total,rate [["api",2,3,66.66666666666667]]`},
		{`project service, level | summarize by service`, allTime.from, allTime.to, `service [["api"],["web"],["worker"]]`},
	}
	for _, c := range cases {
		if got := run(c.query, c.from, c.to); got != c.want {
			t.Errorf("%s\n want %s\n got  %s", c.query, c.want, got)
		}
	}
}

func TestValidatePlacesTheRestOfTheLanguagesMistakes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	mistakes := map[string]queryProblem{
		`summarize count() by hash(user)`:          {Message: `unknown function "hash"`, Line: 1, Column: 22, EndLine: 1, EndColumn: 26},
		`where count() > 1`:                        {Message: `count() only works in summarize`, Line: 1, Column: 7, EndLine: 1, EndColumn: 12},
		`summarize count(), count()`:               {Message: `two columns are named "count_"; name one, as in count_2 = …`, Line: 1, Column: 20, EndLine: 1, EndColumn: 27},
		`project message | where user == "ada"`:    {Message: `unknown column "user"; the columns here are message`, Line: 1, Column: 25, EndLine: 1, EndColumn: 29},
		`where level > 5`:                          {Message: `can't compare a string with a number`, Line: 1, Column: 7, EndLine: 1, EndColumn: 16},
		`where user matches regex "("`:             {Message: "invalid regular expression: missing closing ): `(`", Line: 1, Column: 26, EndLine: 1, EndColumn: 29},
		`take ten`:                                 {Message: `take needs a number of rows, as in take 10`, Line: 1, Column: 1, EndLine: 1, EndColumn: 5},
		`summarize count() | search "x"`:           {Message: `search only comes before other stages; after them, use where … contains`, Line: 1, Column: 19, EndLine: 1, EndColumn: 31},
		`where time > ago(5)`:                      {Message: `expected a timespan, such as 1h or 30m`, Line: 1, Column: 18, EndLine: 1, EndColumn: 19},
		`where time > 5x`:                          {Message: `"5x" isn't a number`, Line: 1, Column: 14, EndLine: 1, EndColumn: 16},
		`summarize avg(level)`:                     {Message: `avg takes a number, not a string`, Line: 1, Column: 15, EndLine: 1, EndColumn: 20},
		`summarize percentile(duration, 150)`:      {Message: `expected a percentage from 0 to 100`, Line: 1, Column: 32, EndLine: 1, EndColumn: 35},
		`where user in ("a", other)`:               {Message: `expected a value: a string, a number, true or false`, Line: 1, Column: 21, EndLine: 1, EndColumn: 26},
		"where status == 200\n| join kind=inner x": {Message: `unknown stage "join"`, Line: 2, Column: 3, EndLine: 2, EndColumn: 7},
	}
	for query, want := range mistakes {
		v := validateQuery(t, env, query)
		if len(v.Errors) != 1 || v.Errors[0] != want || builderChips(v) != "null" {
			t.Errorf("%q: expected %+v and no chips, got %+v", query, want, v)
		}
	}

	// Valid Queries the Builder can't show.
	for _, query := range []string{
		`where a == 1 | summarize n = count() by b`,
		`where a == 1 | take 10 | where b == 2`,
		`where a > 1 or a < 0`,
		`where a contains_cs "x"`,
		`where time > ago(1h)`,
	} {
		if v := validateQuery(t, env, query); len(v.Errors) != 0 || builderChips(v) != "null" {
			t.Errorf("%q: expected no chips and no errors, got %+v", query, v)
		}
	}
}

func TestScalarFunctionsAndSortsInTables(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	assertTables(t, env, map[string]tableCase{
		`where message in ("ada", "bob", "cy") | project message, u = toupper(user), l = tolower(path), n = strlen(message), ` +
			`d = todouble(status), i = toint(duration), r = duration % 100, neg = -duration, b = bin(duration, 100), s = tostring(duration)`: {
			"message,u,l,n,d,i,r,neg,b,s", []string{
				`["ada","ADA","/api/users",3,200,120,20,-120,100,"120"]`,
				`["bob","BOB","/health",3,200,80,80.5,-80.5,0,"80.5"]`,
				`["cy","CY","/api/orders",2,null,300,0,-300,300,"300"]`,
			}, false},
		// Numbers sort before strings, and missing values go last.
		`project message, status | sort by status asc nulls last`: {"message,status",
			[]string{`["ada",200]`, `["ed",404]`, `["di",500]`, `["bob","200"]`, `["cy",null]`}, false},
		`project message, status | sort by status`: {"message,status",
			[]string{`["di",500]`, `["ed",404]`, `["ada",200]`, `["bob","200"]`, `["cy",null]`}, false},
		`sort by message asc | project message, isslow = duration > 200, hasnote = isnotempty(note)`: {"message,isslow,hasnote",
			[]string{`["ada",false,false]`, `["bob",false,false]`, `["cy",true,false]`, `["di",false,false]`, `["ed",true,true]`}, false},
		`extend message = toupper(message) | where duration > 200 | project message`: {"message", []string{`["CY"]`, `["ED"]`}, false},
		// Each column is worked out from the stage before, never from a
		// column of the same name the stage makes.
		`where message == "bob" | project a = user, user = message, message = a`: {"a,user,message", []string{`["Bob","bob",null]`}, false},
		`summarize first = min(time), last = max(time), quietest = min(message)`: {"first,last,quietest",
			[]string{`["2026-10-01T10:40:00Z","2026-10-02T11:10:00Z","ada"]`}, false},
	}, "once stored")
}
