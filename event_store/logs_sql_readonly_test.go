package main_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/solidtrace/event_store/storage"
)

func TestSQLThatWouldChangeAnythingIsRefusedAndChangesNothing(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedLanguageLogs(t, env)

	// SQL can't read the key counts table, so the test reads it as the
	// field list does.
	keyCounts := func() []storage.AttributeCount {
		t.Helper()
		counts, _, err := env.ClickHouseLogs.AttributeCounts(context.Background(), 123, allTime.from, allTime.to, 1000)
		if err != nil {
			t.Fatalf("AttributeCounts: %v", err)
		}
		return counts
	}
	counts := keyCounts()
	if len(counts) == 0 {
		t.Fatal("Expected the seeded Logs to have key counts")
	}
	_, logs := sqlTable(t, env, sqlRequest{SQL: "SELECT message FROM logs ORDER BY message", From: allTime.from, To: allTime.to})

	for _, sql := range []string{
		"DROP TABLE log_key_counts",
		"TRUNCATE TABLE logs",
		"INSERT INTO logs (project_id, message) VALUES (123, 'forged')",
		"INSERT INTO log_key_counts VALUES (123, '2026-01-01', 'string', 'stolen', 1)",
		"ALTER TABLE logs DELETE WHERE 1",
		"DELETE FROM logs WHERE 1",
		"UPDATE logs SET message = 'x' WHERE 1",
		"CREATE TABLE stolen ENGINE = Memory AS SELECT * FROM logs",
		"CREATE TEMPORARY TABLE stolen (x String)",
		"INSERT INTO FUNCTION file('stolen.csv') SELECT * FROM logs",
		"OPTIMIZE TABLE logs",
		"SYSTEM DROP DNS CACHE",
		"KILL QUERY WHERE 1",
		"CREATE USER stolen",
		"GRANT SELECT ON *.* TO CURRENT_USER",
	} {
		status, raw := runSQL(t, env, 123, sqlRequest{SQL: sql, From: allTime.from, To: allTime.to})
		if status != 400 {
			t.Errorf("SQL %q: expected 400, got %d: %s", sql, status, raw)
		}
	}

	// Nothing changed.
	if after := keyCounts(); !slices.Equal(after, counts) {
		t.Errorf("Expected the key counts unchanged\n want %v\n got  %v", counts, after)
	}
	if _, after := sqlTable(t, env, sqlRequest{SQL: "SELECT message FROM logs ORDER BY message", From: allTime.from, To: allTime.to}); !slices.Equal(after, logs) {
		t.Errorf("Expected the Logs unchanged\n want %v\n got  %v", logs, after)
	}
}

func TestSQLCantRaiseItsLimits(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	for _, sql := range []string{
		"SELECT 1 SETTINGS max_execution_time = 1000",
		"SELECT 1 SETTINGS max_memory_usage = 0",
		"SELECT 1 SETTINGS max_threads = 64",
		"SELECT 1 SETTINGS max_result_rows = 0",
		"SELECT 1 SETTINGS result_overflow_mode = 'throw'",
		"SELECT 1 SETTINGS max_temporary_data_on_disk_size_for_query = 0",
		"SELECT 1 SETTINGS readonly = 0",
		"SELECT 1 SETTINGS allow_introspection_functions = 1",
		"SET max_execution_time = 1000",
	} {
		if got := sqlRefusal(t, env, sql); !strings.Contains(got, "readonly mode") {
			t.Errorf("SQL %q: expected it refused as read-only, got %q", sql, got)
		}
	}
}

func TestSQLStopsAtItsTimeAndMemoryLimits(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		env.SQLLimits.Time = time.Second
		env.SQLLimits.MemoryBytes = 64 << 20
	})
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	// SQL that would run for hours, asking for no timeout.
	began := time.Now()
	answer := answerOf(t, env, "/api/123/logs/sql", map[string]any{
		"sql": `SELECT sum(cityHash64(number)) FROM numbers(1000000000000)`, "from": allTime.from, "to": allTime.to}, 504)
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("Expected the SQL stopped soon after its limit of 1 s, took %v", took)
	}
	if got := onlyErrorMessage(t, answer); !strings.Contains(got, "stopped after 1 s") {
		t.Errorf("Expected a message saying the run was stopped after 1 s, got %q", got)
	}

	if got := sqlRefusal(t, env, `SELECT length(groupArray(number)) FROM numbers(100000000)`); !strings.Contains(got, "memory limit") {
		t.Errorf("Expected SQL that needs more memory than its limit to be stopped, got %q", got)
	}

	// The next SQL runs as usual.
	if _, rows := sqlTable(t, env, sqlRequest{SQL: "SELECT 1", From: allTime.from, To: allTime.to}); !slices.Equal(rows, []string{"[1]"}) {
		t.Errorf("Expected SQL after the limits to run, got %v", rows)
	}
}

func TestSQLThatDoesNotParseSaysWhatClickHouseSaysWithItsPosition(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	for _, query := range []string{
		"SELECT message FROM logs WHER level = 'error'",
		"SELECT 'é', message\nFROM logs\nWHERE level = 'error' ORDR BY time",
	} {
		// ClickHouse's own message, as EventStore's user.
		err := env.ClickHouse.Conn().Exec(context.Background(), query)
		var exception *clickhouse.Exception
		if !errors.As(err, &exception) {
			t.Fatalf("Expected %q not to parse, got %v", query, err)
		}
		got := sqlRefusal(t, env, query)
		if got != exception.Message {
			t.Errorf("SQL %q\n want %q\n got  %q", query, exception.Message, got)
		}
		if !strings.Contains(got, "failed at position") {
			t.Errorf("SQL %q: expected the error's position, got %q", query, got)
		}
	}
}
