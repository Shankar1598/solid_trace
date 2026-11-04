package main_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The Log read threads an Instance admin sets on Settings → Instance are
// ClickHouse's max_threads for every tagged Log read, from the next request.

// setLogReadThreads sets the Log read threads, as an Instance admin does on
// Settings → Instance.
func setLogReadThreads(t *testing.T, env *TestEnv, threads int) {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		INSERT INTO instance_settings (id, logs_read_threads, created_at, updated_at) VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET logs_read_threads = excluded.logs_read_threads`,
		threads, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to set the Log read threads: %v", err)
	}
}

// runTaggedReads runs a Project's newest page, a table, a forced table, a
// histogram, its Fields, a Log by id and its Transactions.
func runTaggedReads(t *testing.T, env *TestEnv, projectID uint32) {
	t.Helper()
	page := runQuery(t, env, projectID, queryRequest{Query: "", From: allTime.from, To: allTime.to})
	runQuery(t, env, projectID, queryRequest{Query: "summarize count()", From: allTime.from, To: allTime.to})
	if status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/logs/query", projectID), map[string]any{
		"query": `search "hello" | summarize count()`, "from": allTime.from, "to": allTime.to, "force": true,
	}); status != 200 {
		t.Fatalf("Forced Query: %d %s", status, raw)
	}
	if status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/logs/histogram", projectID), histogramRequest{From: allTime.from, To: allTime.to}); status != 200 {
		t.Fatalf("Histogram: %d %s", status, raw)
	}
	getQueryApp(t, env, fieldsPath(projectID, allTime.from, allTime.to, ""))
	getQueryApp(t, env, fmt.Sprintf("/api/%d/logs/%s", projectID, page.Logs[0].ID))
	readTransactions(t, env, projectID, testNow.AddDate(0, 0, -7), testNow.Add(time.Minute), "")
}

// readThreadsOf is the max_threads each of a Project's tagged statements ran
// with, as query_log keeps their changed settings, by kind of read: "" for
// none, so ClickHouse's default. A forced read's kind ends in " (forced)".
func readThreadsOf(t *testing.T, env *TestEnv, projectID uint32) map[string][]string {
	t.Helper()
	flushQueryLog(t, env)
	rows, err := env.ClickHouse.Conn().Query(context.Background(), `
		SELECT JSONExtractString(log_comment, 'read') AS read, JSONExtractBool(log_comment, 'forced') AS forced,
			Settings['max_threads'] AS threads
		FROM system.query_log
		WHERE event_date >= yesterday() AND current_database = currentDatabase() AND is_initial_query
			AND type = 'QueryFinish' AND JSONExtractUInt(log_comment, 'project_id') = ?
		ORDER BY event_time_microseconds`, projectID)
	if err != nil {
		t.Fatalf("Read query_log: %v", err)
	}
	defer rows.Close()
	threads := map[string][]string{}
	for rows.Next() {
		var (
			read, n string
			forced  bool
		)
		if err := rows.Scan(&read, &forced, &n); err != nil {
			t.Fatalf("Scan query_log: %v", err)
		}
		if forced {
			read += " (forced)"
		}
		threads[read] = append(threads[read], n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Read query_log: %v", err)
	}
	return threads
}

// assertReadThreads fails the test unless each kind of tagged read ran, and
// every statement of each ran with max_threads want, "" for none.
func assertReadThreads(t *testing.T, what string, threads map[string][]string, want string) {
	t.Helper()
	for _, kind := range []string{"newest page", "table", "table (forced)", "histogram", "Fields", "Log by id", "transactions"} {
		ran := threads[kind]
		if len(ran) == 0 {
			t.Errorf("%s: expected a %s read in query_log, got %v", what, kind, threads)
		}
		for _, n := range ran {
			if n != want {
				t.Errorf("%s: expected each %s statement with max_threads %q, got %q", what, kind, want, n)
			}
		}
	}
}

func TestTheLogReadThreadsSetInConsoleAreEachTaggedReadsMaxThreadsFromTheNextRead(t *testing.T) {
	// With no bin cache, each histogram is read from ClickHouse.
	env := newTestEnv(t, func(env *TestEnv) { env.BinCacheBytes = 0 })
	defer env.Cleanup()
	ids := freshProjectIDs(4)
	for i, id := range ids {
		seedProject(t, env.SQLitePath, id, 0)
		seedProjectKey(t, env.SQLitePath, id, fmt.Sprintf("key_%d", i))
		postAndProcessTo(t, env, id, fmt.Sprintf("key_%d", i), logJSON(t, "hello", testNow, nil))
	}

	setLogReadThreads(t, env, 8)
	runTaggedReads(t, env, ids[0])

	// 0 sets none, so ClickHouse's own default applies.
	setLogReadThreads(t, env, 0)
	runTaggedReads(t, env, ids[1])

	// A change applies from the next read, without a restart.
	setLogReadThreads(t, env, 16)
	runTaggedReads(t, env, ids[2])

	// A value out of range, which only Console's validations prevent, is 0,
	// and is logged.
	logs := captureLogs(t)
	setLogReadThreads(t, env, 65)
	runTaggedReads(t, env, ids[3])

	assertReadThreads(t, "at 8", readThreadsOf(t, env, ids[0]), "8")
	assertReadThreads(t, "at 0", readThreadsOf(t, env, ids[1]), "")
	assertReadThreads(t, "at 16", readThreadsOf(t, env, ids[2]), "16")
	assertReadThreads(t, "at 65", readThreadsOf(t, env, ids[3]), "")
	if !strings.Contains(logs.String(), `message="Reading the Log read threads failed, so ClickHouse's default applies"`) {
		t.Errorf("Expected the out-of-range threads logged, got %s", logs)
	}
}

// SQL queries run as their Project's SQL user, with its own limits: the Log
// read threads don't apply to them, nor could they: the SQL users' profile
// fixes max_threads.
func TestTheLogReadThreadsDontApplyToSQLQueries(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) { env.SQLLimits.Threads = 1 })
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	ids := freshProjectIDs(1)
	seedProject(t, env.SQLitePath, ids[0], 0)
	seedProjectKey(t, env.SQLitePath, ids[0], "key_sql")
	postAndProcessTo(t, env, ids[0], "key_sql", logJSON(t, "hello", testNow, nil))
	setLogReadThreads(t, env, 8)

	if status, raw := runSQL(t, env, ids[0], sqlRequest{SQL: "SELECT count() FROM logs", From: allTime.from, To: allTime.to}); status != 200 {
		t.Fatalf("SQL: %d %s", status, raw)
	}
	flushQueryLog(t, env)
	var threads []string
	if err := env.ClickHouse.Conn().QueryRow(context.Background(), `
		SELECT groupArray(Settings['max_threads']) FROM system.query_log
		WHERE event_date >= yesterday() AND is_initial_query AND type = 'QueryFinish'
			AND user = concat(currentDatabase(), '_sql_', toString(?))`, ids[0]).Scan(&threads); err != nil {
		t.Fatalf("Read query_log: %v", err)
	}
	if len(threads) == 0 {
		t.Fatalf("Expected the SQL query in query_log")
	}
	for _, n := range threads {
		if n == "8" {
			t.Errorf("Expected the SQL query to run without the Log read threads, got max_threads %q", n)
		}
	}
}
