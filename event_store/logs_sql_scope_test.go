package main_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// seedTwoProjects gives Project 123 the language Logs, fy late among them,
// and an Event, and Project 456 a Log and an Event of its own.
func seedTwoProjects(t *testing.T, env *TestEnv) {
	t.Helper()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	seedLanguageLogs(t, env)
	postLateLanguageLog(t, env)
	postAndProcessTo(t, env, 456, "other_public_key", logJSON(t, "other", languageAt(10, 35), map[string]any{"secret": "456's"}))
	postEventTo(t, env, 123, "test_public_key", fmt.Sprintf(`{"message":"mine","timestamp":%q,"tags":{"host":"a"}}`, languageAt(10, 15).Format(time.RFC3339)))
	postEventTo(t, env, 456, "other_public_key", fmt.Sprintf(`{"message":"theirs","timestamp":%q}`, languageAt(10, 25).Format(time.RFC3339)))
	env.Process(t)
}

// wideOpen widens SQL's range as far as a query can: it may, within its own
// Project.
const wideOpen = " SETTINGS SQL_from = '1970-01-01 00:00:00', SQL_to = '2200-01-01 00:00:00'"

func TestSQLSeesOnlyItsOwnProjectsLogsAndEventsWhateverItAsks(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedTwoProjects(t, env)
	db := env.ClickHouseConfig.Database
	in := func(query string, from, to time.Time) []string {
		t.Helper()
		_, rows := sqlTable(t, env, sqlRequest{SQL: query, From: from, To: to})
		return rows
	}
	all := func(query string) []string {
		t.Helper()
		return in(query, allTime.from, allTime.to)
	}

	mine := []string{`["ada"]`, `["bob"]`, `["cy"]`, `["di"]`, `["ed"]`, `["fy"]`}
	cases := map[string][]string{
		"SELECT message FROM logs ORDER BY message":                                                          mine,
		"SELECT message FROM " + db + ".logs ORDER BY message":                                               mine,
		"SELECT message FROM logs ORDER BY message" + wideOpen:                                               mine,
		"SELECT count() FROM logs WHERE project_id = 456" + wideOpen:                                         {`[0]`},
		"SELECT count() FROM logs WHERE attributes.secret.:String IS NOT NULL" + wideOpen:                    {`[0]`},
		"SELECT count() FROM merge(currentDatabase(), '^logs$') WHERE project_id = 456" + wideOpen:           {`[0]`},
		"SELECT DISTINCT project_id FROM logs" + wideOpen:                                                    {`[123]`},
		"SELECT project_id, tags FROM events" + wideOpen:                                                     {`[123,{"host":"a"}]`},
		"SELECT count() FROM " + db + ".events WHERE project_id = 456" + wideOpen:                            {`[0]`},
		"SELECT count() FROM logs l JOIN events e ON l.project_id = e.project_id":                            {`[6]`},
		"WITH named AS (SELECT message FROM logs), again AS (SELECT * FROM named) SELECT count() FROM again": {`[6]`},
		"SELECT ';DROP TABLE logs' AS text -- ; DROP":                                                        {`[";DROP TABLE logs"]`},
		// The catalogue shows only the two tables.
		"SELECT name FROM system.tables ORDER BY name": {`["events"]`, `["logs"]`},
		"SHOW TABLES": {`["events"]`, `["logs"]`},
	}
	for query, want := range cases {
		if got := all(query); !slices.Equal(got, want) {
			t.Errorf("SQL %q\n want %v\n got  %v", query, want, got)
		}
	}

	// logs and events show the explorer's range, which a query may widen.
	from, to := languageAt(10, 20), languageAt(11, 0)
	if got, want := in("SELECT message FROM logs ORDER BY time", from, to), []string{`["bob"]`, `["cy"]`, `["fy"]`}; !slices.Equal(got, want) {
		t.Errorf("Expected the Logs of [10:20, 11:00), got %v", got)
	}
	if got := in("SELECT count() FROM events", from, to); !slices.Equal(got, []string{`[0]`}) {
		t.Errorf("Expected none of the Events before 10:20, got %v", got)
	}
	if got := in("SELECT count() FROM events"+wideOpen, from, to); !slices.Equal(got, []string{`[1]`}) {
		t.Errorf("Expected the Project's one Event with the range widened, got %v", got)
	}
}

// sqlRefusal runs SQL for Project 123 that must be refused, and returns
// the error the answer carries.
func sqlRefusal(t *testing.T, env *TestEnv, query string) string {
	t.Helper()
	status, raw := runSQL(t, env, 123, sqlRequest{SQL: query, From: allTime.from, To: allTime.to})
	var answer struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || status != 400 {
		t.Fatalf("SQL %q expected 400 with an error, got %d: %s", query, status, raw)
	}
	return answer.Error
}

func TestSQLCantReadAnotherTableAFileAURLTheBucketOrASystemTable(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedTwoProjects(t, env)
	db := env.ClickHouseConfig.Database

	for _, sql := range []string{
		// EventStore's other tables.
		"SELECT * FROM log_key_counts",
		"SELECT * FROM " + db + ".log_minute_counts",
		"SELECT * FROM log_ingest_counts",
		"SELECT * FROM log_source_counts",
		"SELECT * FROM schema_migrations",
		"SELECT * FROM logs WHERE project_id IN (SELECT project_id FROM log_key_counts)",
		// The server's.
		"SELECT * FROM system.users",
		"SELECT * FROM system.query_log",
		"SELECT * FROM system.processes",
		"SELECT * FROM system.parts",
		"SELECT * FROM system.row_policies",
		"SELECT * FROM information_schema.tables",
		"SHOW USERS",
		// Files: the old Log files are where file() reads.
		"SELECT * FROM file('project=456/day=2026-10-02/hour=10/data.parquet')",
		"SELECT file('project=456/day=2026-10-02/hour=10/data.parquet')",
		// URLs, the bucket and other servers.
		"SELECT * FROM url('http://127.0.0.1:4201/?query=SELECT%201', 'LineAsString')",
		"SELECT * FROM s3('http://127.0.0.1:9000/solidtrace/clickhouse/x.bin')",
		"SELECT * FROM s3('http://127.0.0.1:9000/solidtrace/logs/x.parquet', 'key', 'secret', 'Parquet')",
		"SELECT * FROM remote('127.0.0.1:4200', system.users, 'default')",
		"SELECT * FROM remote('127.0.0.1:4200', " + db + ".logs, 'solidtrace')",
		// Other users.
		"EXECUTE AS solidtrace SELECT * FROM log_key_counts",
	} {
		if got := sqlRefusal(t, env, sql); !strings.Contains(got, "Not enough privileges") {
			t.Errorf("SQL %q: expected it refused for want of a grant, got %q", sql, got)
		}
	}
}
