package main_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// seedTwoProjects gives Project 123 the language Logs, some in files, and
// an Event, and Project 456 a Log and an Event of its own.
func seedTwoProjects(t *testing.T, env *TestEnv) {
	t.Helper()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	seedLanguageLogs(t, env)
	exportLanguageLogs(t, env)
	postAndProcessTo(t, env, 456, "other_public_key", logJSON(t, "other", languageAt(10, 35), map[string]any{"secret": "456's"}))
	postEventTo(t, env, 123, "test_public_key", fmt.Sprintf(`{"message":"mine","timestamp":%q}`, languageAt(10, 15).Format(time.RFC3339)))
	postEventTo(t, env, 456, "other_public_key", fmt.Sprintf(`{"message":"theirs","timestamp":%q}`, languageAt(10, 25).Format(time.RFC3339)))
	env.Process(t)
}

func TestSQLThatReadsBeyondItsProjectsViewsIsRefused(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedTwoProjects(t, env)

	files, err := env.LogStore.HourFiles(123, languageAt(10, 0))
	if err != nil || len(files) == 0 {
		t.Fatalf("Expected exported Log files, got %v (%v)", files, err)
	}
	file := files[0]
	wildcard := filepath.Join(env.dir, "logs_parquet", "*", "*", "*", "*.parquet")

	cases := []struct {
		name, sql, what string
	}{
		{"a raw hot table", "SELECT * FROM logs_hot_456", "logs_hot_456"},
		{"a qualified hot table", "SELECT * FROM logs.logs_hot_456", "logs.logs_hot_456"},
		{"a qualified hot table in full", "SELECT * FROM logs.main.logs_hot_456", "logs.main.logs_hot_456"},
		{"main.logs", "SELECT * FROM main.logs", "main.logs"},
		{"temp.logs", "SELECT * FROM temp.logs", "temp.logs"},
		{"the global events view", "SELECT * FROM main.events", "main.events"},
		{"the Event hot table", "SELECT * FROM events_hot", "events_hot"},
		{"a file path", "SELECT * FROM '" + file + "'", file},
		{"a wildcard", "SELECT * FROM '" + wildcard + "'", wildcard},
		{"an s3 URL", "SELECT * FROM 's3://acme/solidtrace/logs/x.parquet'", "s3://acme/solidtrace/logs/x.parquet"},
		{"read_parquet()", "SELECT * FROM read_parquet('" + wildcard + "')", "read_parquet()"},
		{"read_csv()", "SELECT * FROM read_csv('/etc/passwd')", "read_csv()"},
		{"read_json()", "SELECT * FROM read_json('/etc/hosts')", "read_json()"},
		{"read_text()", "SELECT * FROM read_text('/etc/passwd')", "read_text()"},
		{"read_blob()", "SELECT * FROM read_blob('/etc/passwd')", "read_blob()"},
		{"glob()", "SELECT * FROM glob('/*')", "glob()"},
		{"parquet_metadata()", "SELECT * FROM parquet_metadata('" + file + "')", "parquet_metadata()"},
		{"duckdb_tables()", "SELECT * FROM duckdb_tables()", "duckdb_tables()"},
		{"pragma_table_info()", "SELECT * FROM pragma_table_info('logs.logs_hot_456')", "pragma_table_info()"},
		{"a table macro", "SELECT * FROM histogram(logs.logs_hot_456, message)", "histogram()"},
		{"information_schema", "SELECT * FROM information_schema.tables", "information_schema.tables"},
		{"a pg_catalog view", "SELECT * FROM pg_class", "pg_class"},
		{"the catalogue's view", "SELECT * FROM duckdb_tables", "duckdb_tables"},
		{"SHOW TABLES", "SHOW TABLES", "SHOW TABLES"},
		{"SHOW ALL TABLES", "SHOW ALL TABLES", "SHOW TABLES"},
		{"SHOW of another table", "SHOW logs.logs_hot_456", "logs.logs_hot_456"},
		{"DESCRIBE of another table", "DESCRIBE logs.logs_hot_456", "logs.logs_hot_456"},
		{"SUMMARIZE of another table", "SUMMARIZE logs.logs_hot_456", "logs.logs_hot_456"},
		{"current_setting()", "SELECT current_setting('s3_endpoint')", "current_setting()"},
		{"getvariable()", "SELECT getvariable('x')", "getvariable()"},
		{"pg_get_viewdef()", "SELECT pg_get_viewdef(0)", "pg_get_viewdef()"},
		{"a WITH name that is a table", "WITH events_hot AS (SELECT 1) SELECT * FROM events_hot", "events_hot"},
		{"a WITH name used outside its scope",
			"SELECT * FROM (WITH mine AS (SELECT * FROM logs) SELECT * FROM mine), mine", "mine"},
		{"a WITH name used before its WITH", "SELECT (SELECT count(*) FROM later), * FROM (WITH later AS (SELECT 1) SELECT * FROM later)", "later"},
		{"a WITH part that names itself", "WITH events AS (SELECT * FROM events) SELECT * FROM events", "events"},
		{"a WITH part that names a later one",
			"WITH a AS (SELECT * FROM later), later AS (SELECT 1) SELECT * FROM a", "later"},
		{"a WITH part that names a later one called events",
			"WITH a AS (SELECT * FROM events), events AS (SELECT 1 AS p) SELECT * FROM a", "events"},
		{"a WITH name that is a file", "WITH \"" + file + "\" AS (SELECT 1) SELECT 1", file},
		{"a WITH name that is a wildcard",
			"WITH a AS (SELECT * FROM \"" + wildcard + "\"), \"" + wildcard + "\" AS (SELECT 0) SELECT * FROM a", wildcard},
		{"in a subquery", "SELECT * FROM logs WHERE id IN (SELECT id FROM logs.logs_hot_456)", "logs.logs_hot_456"},
		{"in a scalar subquery", "SELECT (SELECT count(*) FROM read_text('/etc/passwd'))", "read_text()"},
		{"in a join", "SELECT * FROM logs JOIN logs.logs_hot_456 USING (id)", "logs.logs_hot_456"},
		{"in a PIVOT", "SELECT * FROM logs.logs_hot_456 PIVOT (count(*) FOR level IN ('error'))", "logs.logs_hot_456"},
		{"in UNPIVOT", "SELECT * FROM (SELECT * FROM 'x.parquet') UNPIVOT (v FOR k IN (a, b))", "x.parquet"},
		{"in a WITH part", "WITH stolen AS (SELECT * FROM logs.logs_hot_456) SELECT * FROM stolen", "logs.logs_hot_456"},
		{"in a table function's argument", "SELECT * FROM unnest((SELECT list(message) FROM logs.logs_hot_456))", "logs.logs_hot_456"},
		{"in a lambda", "SELECT list_transform([1], lambda x: current_setting('threads'))", "current_setting()"},
		{"in capitals", "SELECT * FROM LOGS.LOGS_HOT_456", "LOGS.LOGS_HOT_456"},
		{"after a UNION", "SELECT message FROM logs UNION ALL SELECT message FROM logs.logs_hot_456", "logs.logs_hot_456"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := "SQL queries can read only the logs and events views: " + c.what + " isn't allowed"
			if got := sqlRefusal(t, env, c.sql); got != want {
				t.Errorf("SQL %q\n want %q\n got  %q", c.sql, want, got)
			}
		})
	}
}

func TestSQLReadsItsOwnProjectsLogsAndEvents(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedTwoProjects(t, env)
	all := func(query string) []string {
		t.Helper()
		_, rows := sqlTable(t, env, sqlRequest{SQL: query, From: allTime.from, To: allTime.to})
		return rows
	}

	// The events view holds only the Project's Events, and hides the
	// global one.
	if got := all("SELECT DISTINCT project_id FROM events"); !slices.Equal(got, []string{"[123]"}) {
		t.Errorf("Expected the events view to hold Project 123's Events only, got %v", got)
	}
	_, inRange := sqlTable(t, env, sqlRequest{SQL: "SELECT count(*) FROM events", From: languageAt(10, 20), To: languageAt(11, 0)})
	if !slices.Equal(inRange, []string{"[0]"}) {
		t.Errorf("Expected the events view to hold only the time range's Events, got %v", inRange)
	}

	total := all("SELECT count(*) FROM logs")
	if len(total) != 1 || total[0] == "[0]" {
		t.Fatalf("Expected a count of the seeded Logs, got %v", total)
	}
	cases := map[string][]string{
		// The explorer's and the docs' examples.
		"SELECT level, count(*) FROM logs GROUP BY level ORDER BY level":                                       all("SELECT level, count(*) FROM logs GROUP BY ALL ORDER BY ALL"),
		"SELECT service, count(*) AS logs FROM logs WHERE level = 'error' GROUP BY service ORDER BY logs DESC": {`["api",2]`},
		"SELECT count(*) FROM logs WHERE attrs_string['path'] = '/api/users'":                                  {`[1]`},
		"WITH named AS (SELECT message FROM logs), again AS (SELECT * FROM named) SELECT count(*) FROM again":  total,
		"WITH logs AS (SELECT 1 AS n) SELECT count(*) FROM logs":                                               {`[1]`},
		"WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM t WHERE n < 3) SELECT max(n) FROM t":     {`[3]`},
		"SELECT count(*) FROM logs l JOIN events e ON l.project_id = e.project_id":                             all("SELECT count(*) FROM logs"),
		"SELECT count(*) FROM range(3)":                                                                                 {`[3]`},
		"SELECT count(*) FROM generate_series(1, 4)":                                                                    {`[4]`},
		"SELECT count(*) FROM unnest([1, 2])":                                                                           {`[2]`},
		"SELECT count(*) FROM (VALUES (1), (2)) v(x)":                                                                   {`[2]`},
		"SELECT error FROM (SELECT level FROM logs) PIVOT (count(*) FOR level IN ('error'))":                            {`[2]`},
		"SELECT count(*) FROM (SELECT 1 AS a, 2 AS b) UNPIVOT (v FOR k IN (a, b))":                                      {`[2]`},
		"SELECT count(*) FROM (DESCRIBE logs) WHERE column_name = 'message'":                                            {`[1]`},
		"SELECT count(*) FROM (DESCRIBE events) WHERE column_name = 'tags'":                                             {`[1]`},
		"SELECT count(*) FROM (SUMMARIZE SELECT duration FROM (SELECT attrs_number['duration'] AS duration FROM logs))": {`[1]`},
		"SELECT current_date = current_date":                                                                            {`[true]`},
	}
	for query, want := range cases {
		if got := all(query); !slices.Equal(got, want) {
			t.Errorf("SQL %q\n want %v\n got  %v", query, want, got)
		}
	}
	if columns, _ := sqlTable(t, env, sqlRequest{SQL: "DESCRIBE logs", From: allTime.from, To: allTime.to}); !slices.Contains(columns, "column_name") {
		t.Errorf("Expected DESCRIBE logs to describe the view, got columns %v", columns)
	}
	if got := strings.Join(all("SELECT message FROM logs ORDER BY ALL"), " "); strings.Contains(got, "other") {
		t.Errorf("Expected none of Project 456's Logs, got %s", got)
	}
}
