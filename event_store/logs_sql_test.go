package main_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// setSQLQueries turns the instance's SQL queries setting on or off, as the
// operator does in Console.
func setSQLQueries(t *testing.T, env *TestEnv, on bool) {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		INSERT INTO instance_settings (id, sql_queries_enabled, default_log_retention_days, created_at, updated_at)
		VALUES (1, ?, 30, ?, ?)
		ON CONFLICT (id) DO UPDATE SET sql_queries_enabled = excluded.sql_queries_enabled`,
		on, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to set SQL queries: %v", err)
	}
}

// sqlRequest is a run of SQL, as the console sends it.
type sqlRequest struct {
	SQL  string    `json:"sql"`
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// runSQL runs SQL for a Project and returns the status and the raw answer.
func runSQL(t *testing.T, env *TestEnv, projectID uint32, req sqlRequest) (int, []byte) {
	t.Helper()
	return postQueryApp(t, env, fmt.Sprintf("/api/%d/logs/sql", projectID), req)
}

// sqlTable runs SQL for Project 123 that must succeed, and returns its
// table's column names and its rows as JSON, one row per element.
func sqlTable(t *testing.T, env *TestEnv, req sqlRequest) ([]string, []string) {
	t.Helper()
	status, raw := runSQL(t, env, 123, req)
	if status != 200 {
		t.Fatalf("SQL %q expected 200, got %d: %s", req.SQL, status, raw)
	}
	var answer tableAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode table %q: %v", raw, err)
	}
	if answer.Table == nil {
		t.Fatalf("SQL %q expected a table, got %s", req.SQL, raw)
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

func TestRunSQLRunsByDefaultAndIsRefusedWhileTheSettingIsOff(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	req := sqlRequest{SQL: "SELECT count(*) FROM logs", From: allTime.from, To: allTime.to}
	if status, raw := runSQL(t, env, 123, req); status != 200 {
		t.Errorf("Expected 200 before the operator sets anything, got %d: %s", status, raw)
	}
	setSQLQueries(t, env, false)
	if status, raw := runSQL(t, env, 123, req); status != 403 {
		t.Errorf("Expected 403 with the setting off, got %d: %s", status, raw)
	}
	setSQLQueries(t, env, true)
	if status, raw := runSQL(t, env, 123, req); status != 200 {
		t.Errorf("Expected 200 with the setting on, got %d: %s", status, raw)
	}
	req.SQL = "SELECT no_such_column FROM logs"
	if status, raw := runSQL(t, env, 123, req); status != 400 || !strings.Contains(string(raw), "no_such_column") {
		t.Errorf("Expected 400 naming the mistake, got %d: %s", status, raw)
	}
}

func TestSQLsLogsHoldTheProjectsLogsOfTheExplorersRange(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	seedLanguageLogs(t, env)
	env.Clock.Set(languageAt(10, 29))
	postAndProcessTo(t, env, 456, "other_public_key", logJSON(t, "other", languageAt(10, 35), map[string]any{"duration": 1}))
	postLateLanguageLog(t, env) // fy, late

	query := `SELECT project_id, message, attributes.duration.:Float64 AS duration, attributes.user.:String AS user
		FROM logs ORDER BY time`
	cases := []struct {
		from, to time.Time
		want     []string
	}{
		{allTime.from, allTime.to, []string{
			`[123,"di",50,"di"]`,
			`[123,"ada",120,"ada"]`,
			`[123,"bob",80.5,"Bob"]`,
			`[123,"cy",300,"cy"]`,
			`[123,"fy",10,"fy"]`,
			`[123,"ed",900,"ed"]`,
		}},
		{languageAt(10, 30), languageAt(11, 30), []string{
			`[123,"cy",300,"cy"]`,
			`[123,"fy",10,"fy"]`,
			`[123,"ed",900,"ed"]`,
		}},
		{languageAt(10, 30), languageAt(11, 10), []string{
			`[123,"cy",300,"cy"]`,
			`[123,"fy",10,"fy"]`,
		}},
	}
	for _, c := range cases {
		columns, rows := sqlTable(t, env, sqlRequest{SQL: query, From: c.from, To: c.to})
		if got := strings.Join(columns, ","); got != "project_id,message,duration,user" {
			t.Errorf("Expected the selected columns, got %s", got)
		}
		if !slices.Equal(rows, c.want) {
			t.Errorf("In [%s, %s)\n want %v\n got  %v", c.from, c.to, c.want, rows)
		}
	}
}

// readableSQL asks for a Query's readable SQL, over all time.
func readableSQL(t *testing.T, env *TestEnv, query string) string {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/readable-sql", queryRequest{Query: query, From: allTime.from, To: allTime.to})
	if status != 200 {
		t.Fatalf("Readable SQL of %q expected 200, got %d: %s", query, status, raw)
	}
	var answer struct {
		SQL string `json:"sql"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || answer.SQL == "" {
		t.Fatalf("Decode readable SQL %q: %v", raw, err)
	}
	return answer.SQL
}

func TestReadableSQLRunThroughRunSQLReturnsTheQuerysRows(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedLanguageLogs(t, env)
	postLateLanguageLog(t, env) // ada, bob, cy and di in files, ed hot, fy late

	logRows := []string{
		``,
		`where status == 200`,
		`where status != 200 and duration > 100`,
		`where path contains "users" or isempty(note)`,
		`where user in ("ada", "Bob", "fy") | where not(path startswith "/health")`,
		`search "disk"`,
		`search "api" | where level == "error"`,
		`where duration > 10 | take 3`,
		`sort by time desc | take 2`,
	}
	for _, query := range logRows {
		want := queryMessages(t, env, query)
		sql := readableSQL(t, env, query)
		columns, rows := sqlTable(t, env, sqlRequest{SQL: sql, From: allTime.from, To: allTime.to})
		message := slices.Index(columns, "message")
		if message < 0 {
			t.Fatalf("Readable SQL of %q has no message column: %v\n%s", query, columns, sql)
		}
		got := []string{}
		for _, row := range rows {
			var values []any
			if err := json.Unmarshal([]byte(row), &values); err != nil {
				t.Fatalf("Decode row %s: %v", row, err)
			}
			got = append(got, values[message].(string))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s\n want %v\n got  %v\n%s", query, want, got, sql)
		}
	}

	tables := map[string]bool{ // each Query, and whether its rows have an order
		`summarize count() by level`:                                          false,
		`summarize n = count(), avg(duration), max(duration) by service`:      false,
		`summarize count() by status`:                                         false,
		`summarize count() by bin(time, 1h)`:                                  false,
		`summarize dcount(user), percentile(duration, 50)`:                    false,
		`where duration > 50 | summarize count() by tostring(status)`:         false,
		`search "api" | summarize sum(duration) by service`:                   false,
		`top 3 by duration`:                                                   true,
		`top 2 by duration asc | project message, status`:                     true,
		`distinct service`:                                                    false,
		`distinct service, status`:                                            false,
		`project message, status, ms = duration * 1000 | where ms > 100000`:   false,
		`extend slow = duration > 100 | where slow == true | project message`: false,
		`sort by user asc | take 3 | project user, path`:                      true,
		`summarize c = count() by service | sort by c desc, service asc`:      true,
		`count`:                                false,
		`where level == "error" | count`:       false,
		`where time > ago(2h) | distinct user`: false,
	}
	for query, ordered := range tables {
		wantColumns, wantRows := runTable(t, env, query)
		sql := readableSQL(t, env, query)
		columns, rows := sqlTable(t, env, sqlRequest{SQL: sql, From: allTime.from, To: allTime.to})
		if !ordered {
			slices.Sort(wantRows)
			slices.Sort(rows)
		}
		if !slices.Equal(columns, wantColumns) || !slices.Equal(rows, wantRows) {
			t.Errorf("%s\n want %v %v\n got  %v %v\n%s", query, wantColumns, wantRows, columns, rows, sql)
		}
	}
}
