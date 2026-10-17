package main_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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

func TestSQLThatWouldChangeAnythingIsRefusedBeforeItRuns(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedLanguageLogs(t, env)

	countsSQL := sqlRequest{SQL: "SELECT * FROM logs.log_key_counts ORDER BY ALL", From: allTime.from, To: allTime.to}
	_, counts := sqlTable(t, env, countsSQL)
	if len(counts) == 0 {
		t.Fatal("Expected the seeded Logs to have key counts")
	}
	copied := filepath.Join(t.TempDir(), "copied.csv")
	attached := filepath.Join(t.TempDir(), "attached.duckdb")

	cases := []struct {
		name, sql, what string
	}{
		{"DROP", "DROP TABLE logs.log_key_counts", "DROP"},
		{"INSERT", "INSERT INTO logs.log_key_counts VALUES (123, DATE '2026-01-01', 'string', 'stolen', 1)", "INSERT"},
		{"UPDATE", "UPDATE logs.log_key_counts SET logs = 0", "UPDATE"},
		{"DELETE", "DELETE FROM logs.log_key_counts", "DELETE"},
		{"CREATE", "CREATE TABLE logs.stolen AS SELECT * FROM logs", "CREATE"},
		{"COPY TO", "COPY (SELECT * FROM logs) TO '" + copied + "'", "COPY"},
		{"ATTACH", "ATTACH '" + attached + "' AS attached", "ATTACH"},
		{"SET", "SET threads = 1", "SET"},
		{"PRAGMA", "PRAGMA enable_profiling", "PRAGMA"},
		{"CALL checkpoint()", "CALL checkpoint()", "CALL"},
		{"after comments", "-- tidy up\n/* first */ drop table logs.log_key_counts", "DROP"},
		{"a CTE before DELETE", "WITH old AS (SELECT 1) DELETE FROM logs.log_key_counts", "a statement other than SELECT"},
		{"two SELECTs", "SELECT 1; SELECT 2", "more than one statement"},
		{"a SELECT then DROP", "SELECT 1; DROP TABLE logs.log_key_counts", "a statement other than SELECT"},
		{"DROP then a SELECT", "DROP TABLE logs.log_key_counts; SELECT 1", "DROP"},
		{"query()", "SELECT * FROM query('DROP TABLE logs.log_key_counts')", "query()"},
		{"query_table()", "SELECT * FROM query_table('logs.log_key_counts')", "query_table()"},
		{"enable_logging()", "SELECT * FROM enable_logging()", "enable_logging()"},
		{"disable_logging()", "SELECT * FROM disable_logging()", "disable_logging()"},
		{"checkpoint()", "SELECT * FROM checkpoint()", "checkpoint()"},
		{"force_checkpoint() in a subquery", "SELECT count(*) FROM logs WHERE EXISTS (SELECT * FROM force_checkpoint())", "force_checkpoint()"},
		{"nextval()", "SELECT nextval('no_such_sequence')", "nextval()"},
		{"setseed() named in full", "SELECT system.main.setseed(0.5)", "setseed()"},
		{"in capitals", "SELECT * FROM QUERY_TABLE('logs.log_key_counts')", "query_table()"},
		{"in a lambda", "SELECT list_transform([1], lambda x: nextval('no_such_sequence'))", "nextval()"},
		{"serialized SQL run", "SELECT * FROM json_execute_serialized_sql(json_serialize_sql('SELECT 1'))", "json_execute_serialized_sql()"},
		{"enable_profiling()", "SELECT * FROM enable_profiling()", "enable_profiling()"},
		{"truncate_duckdb_logs()", "SELECT * FROM truncate_duckdb_logs()", "truncate_duckdb_logs()"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := "SQL queries are read-only: " + c.what + " isn't allowed"
			if got := sqlRefusal(t, env, c.sql); got != want {
				t.Errorf("SQL %q\n want %q\n got  %q", c.sql, want, got)
			}
		})
	}

	// Nothing ran: the counts, the files and the databases are as they were.
	if _, after := sqlTable(t, env, countsSQL); !slices.Equal(after, counts) {
		t.Errorf("Expected the key counts unchanged\n want %v\n got  %v", counts, after)
	}
	for _, path := range []string{copied, attached} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("Expected no %s, got %v", path, err)
		}
	}
	_, databases := sqlTable(t, env, sqlRequest{SQL: "SELECT database_name FROM duckdb_databases() WHERE database_name IN ('attached', 'stolen')", From: allTime.from, To: allTime.to})
	if len(databases) != 0 {
		t.Errorf("Expected nothing attached, got %v", databases)
	}
}

func TestReadOnlySQLStillRuns(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedLanguageLogs(t, env)

	_, total := sqlTable(t, env, sqlRequest{SQL: "SELECT count(*) FROM logs", From: allTime.from, To: allTime.to})
	cases := map[string][]string{
		"SELECT count(*) FROM logs;":                                                             total,
		"WITH named AS (SELECT message FROM logs) SELECT count(*) FROM named":                    total,
		"FROM logs SELECT count(*)":                                                              total,
		"SELECT count(*) FROM (SELECT row_number() OVER (ORDER BY time) AS n FROM logs)":         total,
		"SELECT max(n) FROM (SELECT count(*) OVER w AS n FROM logs WINDOW w AS (ORDER BY time))": total,
		"SELECT ';DROP TABLE logs.log_key_counts' AS text -- ; DROP":                             {`[";DROP TABLE logs.log_key_counts"]`},
		// EventStore loads what it needs itself, so a query can't load more.
		"SELECT current_setting('autoload_known_extensions'), current_setting('autoinstall_known_extensions')": {`[false,false]`},
	}
	for query, want := range cases {
		if _, rows := sqlTable(t, env, sqlRequest{SQL: query, From: allTime.from, To: allTime.to}); !slices.Equal(rows, want) {
			t.Errorf("SQL %q\n want %v\n got  %v", query, want, rows)
		}
	}
	if len(total) != 1 || total[0] == "[0]" {
		t.Errorf("Expected a count of the seeded Logs, got %v", total)
	}
}

func TestSQLThatDoesNotParseSaysWhatDuckDBWouldSay(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	// DuckDB's own message for SQL that doesn't parse, from a database of
	// its own.
	duckdb, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer duckdb.Close()
	for _, query := range []string{
		"SELECT * FORM logs",
		"SELECT message\nFROM logs\nWHERE level = 'error' ORDR BY time",
		"SELECT 'é', message FROM logs WHERE",
	} {
		_, err := duckdb.Exec(query)
		if err == nil {
			t.Fatalf("Expected %q not to parse", query)
		}
		if got := sqlRefusal(t, env, query); got != err.Error() {
			t.Errorf("SQL %q\n want %q\n got  %q", query, err.Error(), got)
		}
	}
	if got := sqlRefusal(t, env, "  -- nothing\n"); !strings.Contains(got, "no statement") {
		t.Errorf("Expected SQL with no statement to be refused, got %q", got)
	}
}
