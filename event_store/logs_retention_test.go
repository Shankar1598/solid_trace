package main_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/storage"
)

// seedProject adds a Project's row to the Console database, with its own
// Retention in days, or the instance default if retentionDays is 0.
func seedProject(t *testing.T, sqlitePath string, projectID uint32, retentionDays int) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqlitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite for seeding: %v", err)
	}
	defer db.Close()
	retention := sql.NullInt64{Int64: int64(retentionDays), Valid: retentionDays != 0}
	if _, err := db.Exec(
		"INSERT INTO projects (id, name, organization_id, log_retention_days, created_at, updated_at) VALUES (?, ?, 1, ?, ?, ?)",
		projectID, fmt.Sprintf("Project %d", projectID), retention, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to seed project %d: %v", projectID, err)
	}
}

// postAndProcessTo sends one request of Logs to a Project with its key and
// processes it.
func postAndProcessTo(t *testing.T, env *TestEnv, projectID uint32, key string, logs ...string) {
	t.Helper()
	resp := sendLogs(t, env, fmt.Sprintf("/api/%d/logs", projectID), "application/json",
		"["+strings.Join(logs, ",")+"]", map[string]string{"Authorization": "Bearer " + key})
	if counts := decodeLogCounts(t, resp); counts.Accepted != len(logs) {
		t.Fatalf("Expected %d Logs accepted by Project %d, got %+v", len(logs), projectID, counts)
	}
	env.ProcessLogs(t)
}

func TestALogOlderThanItsProjectsRetentionGetsItsReceiptTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 7)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	eightDaysAgo := testNow.AddDate(0, 0, -8)
	postAndProcess(t, env, logJSON(t, "eight days old", eightDaysAgo, nil))
	postAndProcessTo(t, env, 456, "other_public_key", logJSON(t, "eight days old", eightDaysAgo, nil))

	if got := latestLogs(t, env, 123, 10); len(got) != 1 || !got[0].Time.Equal(eightDaysAgo) {
		t.Errorf("Expected the default Retention of 30 days to keep the time %v, got %+v", eightDaysAgo, got)
	}
	got := latestLogs(t, env, 456, 10)
	if len(got) != 1 || !got[0].Time.Equal(testNow) || got[0].Attributes["original.time"] != eightDaysAgo.Format(time.RFC3339Nano) {
		t.Errorf("Expected a Retention of 7 days to give the receipt time %v and keep original.time, got %+v", testNow, got)
	}
}

// postAt sends one Log to a Project at its own time, with the clock at that
// time, with its message as its user Attribute.
func postAt(t *testing.T, env *TestEnv, projectID uint32, key, message string, at time.Time) {
	t.Helper()
	env.Clock.Set(at)
	postAndProcessTo(t, env, projectID, key, logJSON(t, message, at, map[string]any{"user": message}))
}

// countedRange is a range that holds every Log the Retention and disk limit
// tests send, in whole days.
var countedRange = struct{ from, to time.Time }{
	from: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	to:   time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
}

// assertCounted checks that a Project's histogram over countedRange counts
// want Logs, and that its Fields count want Logs with the user Attribute.
func assertCounted(t *testing.T, env *TestEnv, projectID uint32, want int, when string) {
	t.Helper()
	assertCountedIn(t, env, projectID, countedRange.from, countedRange.to, want, when)
}

// assertCountedIn is assertCounted over [from, to), whole days.
func assertCountedIn(t *testing.T, env *TestEnv, projectID uint32, from, to time.Time, want int, when string) {
	t.Helper()
	path := fmt.Sprintf("/api/%d/logs/histogram", projectID)
	status, raw := postQueryApp(t, env, path, histogramRequest{From: from, To: to, BucketSeconds: 86400})
	var answer histogramAnswer
	if err := json.Unmarshal(raw, &answer); status != 200 || err != nil {
		t.Fatalf("Histogram of Project %d expected 200, got %d: %s", projectID, status, raw)
	}
	if answer.Count != want {
		t.Errorf("Expected Project %d's histogram to count %d Logs %s, got %d", projectID, want, when, answer.Count)
	}
	var users int64
	for _, field := range getFields(t, env, projectID, from, to, "").Attributes {
		if field.Name == "user" {
			users = field.Logs
		}
	}
	if users != int64(want) {
		t.Errorf("Expected Project %d's Fields to count %d Logs with user %s, got %d", projectID, want, when, users)
	}
}

func TestRetentionDeletesEachProjectsDaysPastItsOwnRetention(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Project 123 keeps the default 30 days, Project 456 its own 7.
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 7)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	day := func(month, day, hour int) time.Time {
		return time.Date(2026, time.Month(month), day, hour, 30, 0, 0, time.UTC)
	}
	postAt(t, env, 123, "test_public_key", "last day past 30", day(9, 1, 23))
	postAt(t, env, 123, "test_public_key", "first day within 30", day(9, 2, 0))
	postAt(t, env, 123, "test_public_key", "yesterday", day(10, 1, 12))
	postAt(t, env, 456, "other_public_key", "last day past 7", day(9, 24, 23))
	postAt(t, env, 456, "other_public_key", "first day within 7", day(9, 25, 0))

	// At noon on 2 October, 30 days back is noon on 2 September: that day
	// still holds Logs within Retention, so only the days before it go.
	env.Clock.Set(testNow)
	env.DeleteLogsPastRetention(t)

	assertMessages := func(projectID uint32, when string, want ...string) {
		t.Helper()
		if got := messages(latestLogs(t, env, projectID, 10)); !slices.Equal(got, want) {
			t.Errorf("Expected Project %d to keep %v %s, got %v", projectID, want, when, got)
		}
	}
	// The days kept keep their counts: the histogram and the Fields count
	// their Logs alone.
	assertKept := func(when string) {
		t.Helper()
		assertMessages(123, when, "yesterday", "first day within 30")
		assertMessages(456, when, "first day within 7")
		assertProjectQueries(t, env, 123, map[string][]string{
			`where user == "last day past 30"`:    {},
			`where user == "first day within 30"`: {"first day within 30"},
			`where user == "yesterday"`:           {"yesterday"},
		}, when)
		assertProjectQueries(t, env, 456, map[string][]string{
			`where user == "last day past 7"`:    {},
			`where user == "first day within 7"`: {"first day within 7"},
		}, when)
		assertCounted(t, env, 123, 2, when)
		assertCounted(t, env, 456, 1, when)
	}
	assertKept("after Retention")

	env.Reopen(t)
	assertKept("after a restart")
	env.DeleteLogsPastRetention(t)
	assertKept("after another pass")
}

// consoleExec changes the Console database as Console would.
func consoleExec(t *testing.T, env *TestEnv, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("Console write failed: %v", err)
	}
}

func TestADeletedProjectsLogsAreDeletedAndAProjectWithoutKeysKeepsItsOwn(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	// Each Project gets Logs of two days, one of them late.
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 10, 0, 0, time.UTC) }
	for _, p := range []struct {
		id  uint32
		key string
	}{{123, "test_public_key"}, {456, "other_public_key"}} {
		env.Clock.Set(at(1, 10))
		postAndProcessTo(t, env, p.id, p.key, logJSON(t, "yesterday", at(1, 10), map[string]any{"user": "ada"}))
		env.Clock.Set(at(2, 12))
		postAndProcessTo(t, env, p.id, p.key,
			logJSON(t, "today", at(2, 12), map[string]any{"user": "bob"}),
			logJSON(t, "late", at(1, 9), map[string]any{"user": "cy"}))
	}
	want := latestLogs(t, env, 123, 10)
	if got := messages(want); !slices.Equal(got, []string{"today", "yesterday", "late"}) {
		t.Fatalf("Unexpected Logs of Project 123: %v", got)
	}

	// Project 123 loses its last key, and Project 456 is deleted.
	consoleExec(t, env, "DELETE FROM project_keys WHERE project_id = 123")
	consoleExec(t, env, "DELETE FROM projects WHERE id = 456")
	env.DeleteLogsPastRetention(t)

	assertDeleted := func(when string) {
		t.Helper()
		if got := latestLogs(t, env, 456, 10); len(got) != 0 {
			t.Errorf("Expected the deleted Project to have no Logs %s, got %v", when, messages(got))
		}
		if got := getFields(t, env, 456, countedRange.from, countedRange.to, ""); len(got.Attributes) != 0 {
			t.Errorf("Expected the deleted Project to have no Fields %s, got %v", when, got.Attributes)
		}
		status, raw := postQueryApp(t, env, "/api/456/logs/histogram",
			histogramRequest{From: countedRange.from, To: countedRange.to, BucketSeconds: 86400})
		var answer histogramAnswer
		if err := json.Unmarshal(raw, &answer); status != 200 || err != nil {
			t.Fatalf("Histogram of Project 456 expected 200, got %d: %s", status, raw)
		}
		if answer.Count != 0 {
			t.Errorf("Expected the deleted Project's histogram to count nothing %s, got %d", when, answer.Count)
		}
		assertListsUnchanged(t, env, 123, want, when)
		assertProjectQueries(t, env, 123, map[string][]string{
			`where user == "ada"`: {"yesterday"},
			`where user == "bob"`: {"today"},
			`where user == "cy"`:  {"late"},
		}, when)
		assertCounted(t, env, 123, 3, when)
	}
	assertDeleted("after the pass")
	env.Reopen(t)
	assertDeleted("after a restart")
	env.DeleteLogsPastRetention(t)
	assertDeleted("after another pass")
}

// sqlUsersAndPolicies lists the SQL users of the environment's database and
// their row policies, as an operator sees them with SHOW USERS and SHOW ROW
// POLICIES.
func sqlUsersAndPolicies(t *testing.T, env *TestEnv) (users, policies []string) {
	t.Helper()
	ctx := context.Background()
	prefix := env.ClickHouse.Database() + "_sql_"
	read := func(query string) []string {
		rows, err := env.ClickHouse.Conn().Query(ctx, query, prefix)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		return names
	}
	users = read("SELECT name FROM system.users WHERE startsWith(name, ?) ORDER BY name")
	policies = read("SELECT name FROM system.row_policies WHERE startsWith(short_name, ?) ORDER BY name")
	return users, policies
}

// A deleted Project's SQL user, with its row policies, goes with its Logs,
// and other Projects keep theirs.
func TestADeletedProjectsSQLUserGoesWithItsLogs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	env.Clock.Set(testNow)
	countLogs := func(projectID uint32) string {
		t.Helper()
		status, raw := runSQL(t, env, projectID, sqlRequest{SQL: "SELECT count() FROM logs", From: allTime.from, To: allTime.to})
		if status != 200 {
			t.Fatalf("SQL of Project %d expected 200, got %d: %s", projectID, status, raw)
		}
		var answer tableAnswer
		if err := json.Unmarshal(raw, &answer); err != nil || answer.Table == nil || len(answer.Table.Rows) != 1 {
			t.Fatalf("Decode SQL answer %s: %v", raw, err)
		}
		return string(answer.Table.Rows[0])
	}
	for _, p := range []struct {
		id  uint32
		key string
	}{{123, "test_public_key"}, {456, "other_public_key"}} {
		postAndProcessTo(t, env, p.id, p.key, logJSON(t, "m", testNow.Add(-time.Minute), nil))
		if got := countLogs(p.id); got != "[1]" {
			t.Fatalf("Expected Project %d's SQL to count 1 Log, got %s", p.id, got)
		}
	}
	db := env.ClickHouse.Database()
	user := func(projectID uint32) string { return fmt.Sprintf("%s_sql_%d", db, projectID) }
	if users, policies := sqlUsersAndPolicies(t, env); len(users) != 2 || len(policies) != 4 {
		t.Fatalf("Expected two SQL users with two row policies each, got %v and %v", users, policies)
	}

	consoleExec(t, env, "DELETE FROM projects WHERE id = 456")
	env.DeleteLogsPastRetention(t)

	users, policies := sqlUsersAndPolicies(t, env)
	if want := []string{user(123)}; !slices.Equal(users, want) {
		t.Errorf("Expected the SQL users %v, got %v", want, users)
	}
	if want := []string{user(123) + " ON " + db + ".events", user(123) + " ON " + db + ".logs"}; !slices.Equal(policies, want) {
		t.Errorf("Expected the row policies %v, got %v", want, policies)
	}
	if got := countLogs(123); got != "[1]" {
		t.Errorf("Expected Project 123's SQL to count 1 Log still, got %s", got)
	}
}

func TestAnEmptyProjectsTableDeletesNoProjectsLogs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 0)
	postAndProcess(t, env, logJSON(t, "kept", testNow, nil))

	// Console's database comes back with no Projects, as after a reset.
	consoleExec(t, env, "DELETE FROM projects")
	env.DeleteLogsPastRetention(t)
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"kept"}) {
		t.Errorf("Expected the Logs kept while no Project exists, got %v", got)
	}

	// Once Projects exist again, a missing one's Logs go.
	seedProject(t, env.SQLitePath, 456, 0)
	env.DeleteLogsPastRetention(t)
	if got := messages(latestLogs(t, env, 123, 10)); len(got) != 0 {
		t.Errorf("Expected the deleted Project's Logs gone, got %v", got)
	}
}

// localDiskUse is how much local disk each Project's day of Logs takes, by
// "<Project> <day>", and all of them.
func localDiskUse(t *testing.T, env *TestEnv) (map[string]int64, int64) {
	t.Helper()
	days, err := env.ClickHouseLogs.LogDiskUse()
	if err != nil {
		t.Fatalf("Reading the Logs' disk use failed: %v", err)
	}
	use := map[string]int64{}
	var total int64
	for _, day := range days {
		use[fmt.Sprintf("%d %s", day.ProjectID, day.Day.Format(time.DateOnly))] = day.Bytes
		total += day.Bytes
	}
	return use, total
}

// enforceLogDiskLimit keeps the Logs on local disk within bytes.
func enforceLogDiskLimit(t *testing.T, env *TestEnv, bytes int64) {
	t.Helper()
	if err := env.ClickHouseLogs.EnforceDiskLimit(storage.LogDiskLimit{Bytes: bytes}); err != nil {
		t.Fatalf("Enforcing the Log disk limit failed: %v", err)
	}
}

func TestTheDefaultDiskLimitIsHalfOfClickHousesLocalDisk(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	var fs syscall.Statfs_t
	if err := syscall.Statfs(clickhousetest.DataDir(t), &fs); err != nil {
		t.Fatalf("statfs: %v", err)
	}
	disk := int64(fs.Blocks) * int64(fs.Bsize)
	if limit, err := env.ClickHouseLogs.DiskLimit(0); err != nil || limit.Bytes != disk/2 {
		t.Errorf("Expected the default limit to be half of ClickHouse's %d-byte disk, got %d, %v", disk, limit.Bytes, err)
	}
	if limit, err := env.ClickHouseLogs.DiskLimit(800_000_000_000); err != nil || limit.Bytes != 800_000_000_000 {
		t.Errorf("Expected a configured limit to win, got %d, %v", limit.Bytes, err)
	}
}

func TestTheDiskLimitDropsTheOldestDaysOfAnyProjectFirst(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 10, 0, 0, time.UTC) }
	postAt(t, env, 456, "other_public_key", "456 September 30th", time.Date(2026, 9, 30, 23, 10, 0, 0, time.UTC))
	postAt(t, env, 123, "test_public_key", "123 October 1st", at(1, 8))
	postAt(t, env, 456, "other_public_key", "456 October 1st", at(1, 9))
	// One insert a day, so that no merge changes a day's size meanwhile.
	env.Clock.Set(at(2, 11))
	postAndProcessTo(t, env, 123, "test_public_key",
		logJSON(t, "123 October 2nd", at(2, 10), map[string]any{"user": "ada"}),
		logJSON(t, "123 October 2nd again", at(2, 11), map[string]any{"user": "bob"}))

	use, total := localDiskUse(t, env)
	if len(use) != 4 {
		t.Fatalf("Expected four Project days on local disk, got %v", use)
	}
	assertMessages := func(projectID uint32, when string, want ...string) {
		t.Helper()
		if got := messages(latestLogs(t, env, projectID, 10)); !slices.Equal(got, want) {
			t.Errorf("Expected Project %d to keep %v %s, got %v", projectID, want, when, got)
		}
	}

	enforceLogDiskLimit(t, env, total)
	assertMessages(456, "at the limit", "456 October 1st", "456 September 30th")
	assertMessages(123, "at the limit", "123 October 2nd again", "123 October 2nd", "123 October 1st")

	// The oldest day goes, whichever Project's it is.
	enforceLogDiskLimit(t, env, total-1)
	assertMessages(456, "past the limit by a byte", "456 October 1st")
	assertMessages(123, "past the limit by a byte", "123 October 2nd again", "123 October 2nd", "123 October 1st")
	assertCounted(t, env, 456, 1, "past the limit by a byte")

	// Of a day's Projects, the first goes first.
	enforceLogDiskLimit(t, env, use["123 2026-10-02"])
	assertMessages(456, "with room for one day")
	assertMessages(123, "with room for one day", "123 October 2nd again", "123 October 2nd")
	assertCounted(t, env, 456, 0, "with room for one day")
	assertCounted(t, env, 123, 2, "with room for one day")
	if after, _ := localDiskUse(t, env); !maps.Equal(after, map[string]int64{"123 2026-10-02": use["123 2026-10-02"]}) {
		t.Errorf("Expected only October 2nd of Project 123 on local disk, got %v", after)
	}
}
