package main_test

import (
	"encoding/json"
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
// (the day before), with the clock at 11:30, so all but di are in the hot
// table.
func seedLanguageLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(11, 30))
	postAndProcess(t, env,
		logJSON(t, "ada", languageAt(10, 10), map[string]any{
			"status": 200, "user": "ada", "duration": 120, "path": "/api/users", "service": "api"}),
		logJSON(t, "bob", languageAt(10, 20), map[string]any{
			"status": "200", "user": "Bob", "duration": 80.5, "path": "/health", "service": "web"}),
		logJSON(t, "cy", languageAt(10, 30), map[string]any{
			"user": "cy", "level": "error", "duration": 300, "path": "/api/orders", "note": "", "service": "api"}),
		logJSON(t, "di", languageAt(10, 40).AddDate(0, 0, -1), map[string]any{
			"status": 500, "user": "di", "duration": 50, "service": "worker"}),
		logJSON(t, "ed", languageAt(11, 10), map[string]any{
			"status": 404, "user": "ed", "level": "error", "duration": 900, "note": "Disk FULL", "path": "/API/users", "service": "api"}),
	)
}

// exportLanguageLogs moves hour 10 and the day before to files, and adds fy,
// a late Log of 10:45 beside hour 10's file.
func exportLanguageLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(12, 5))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, languageAt(10, 0), 1)
	postAndProcess(t, env, logJSON(t, "fy", languageAt(10, 45), map[string]any{
		"status": 200, "user": "fy", "duration": 10, "path": "/api/users/7", "service": "api"}))
}

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
	assertQueries(t, env, cases, "in the hot table")

	exportLanguageLogs(t, env)
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
	assertQueries(t, env, cases, "across files and a late Log")
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
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: query, From: allTime.from, To: allTime.to})
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

func assertTables(t *testing.T, env *TestEnv, cases map[string]tableCase, when string) {
	t.Helper()
	for query, want := range cases {
		columns, rows := runTable(t, env, query)
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
	assertTables(t, env, cases, "in the hot table")

	// Hour 10 and the day before go to files.
	env.Clock.Set(languageAt(12, 5))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, languageAt(10, 0), 1)
	assertTables(t, env, cases, "after the export")

	// A late Log of hour 10, beside its file.
	postAndProcess(t, env, logJSON(t, "fy", languageAt(10, 45), map[string]any{"duration": 10, "service": "api"}))
	assertTables(t, env, map[string]tableCase{
		`summarize count() by bin(time, 1h)`:      {"time,count_", []string{`["2026-10-01T10:00:00Z",1]`, `["2026-10-02T10:00:00Z",4]`, `["2026-10-02T11:00:00Z",1]`}, false},
		`top 1 by duration asc | project message`: {"message", []string{`["fy"]`}, false},
	}, "with a late Log")
}
