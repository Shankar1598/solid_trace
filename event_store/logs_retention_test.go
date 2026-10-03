package main_test

import (
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

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
// time, so that it is a hot Log and promotes its keys for its day.
func postAt(t *testing.T, env *TestEnv, projectID uint32, key, message string, at time.Time) {
	t.Helper()
	env.Clock.Set(at)
	postAndProcessTo(t, env, projectID, key, logJSON(t, message, at, map[string]any{"user": message}))
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
	env.ExportLogs(t)
	env.DeleteLogsPastRetention(t)

	assertMessages := func(projectID uint32, want ...string) {
		t.Helper()
		if got := messages(latestLogs(t, env, projectID, 10)); !slices.Equal(got, want) {
			t.Errorf("Expected Project %d to keep %v, got %v", projectID, want, got)
		}
	}
	assertSets := func(when string) {
		t.Helper()
		for _, c := range []struct {
			projectID uint32
			day       time.Time
			want      []string
		}{
			{123, day(9, 1, 0), nil},
			{123, day(9, 2, 0), []string{"string user"}},
			{456, day(9, 24, 0), nil},
			{456, day(9, 25, 0), []string{"string user"}},
		} {
			if got := promotedNames(env.LogStore.PromotedSet(c.projectID, c.day)); !slices.Equal(got, c.want) {
				t.Errorf("Expected Project %d's set of %s to be %v %s, got %v", c.projectID, c.day.Format(time.DateOnly), c.want, when, got)
			}
		}
	}
	assertMessages(123, "yesterday", "first day within 30")
	assertMessages(456, "first day within 7")
	assertSets("after Retention")

	env.Reopen(t)
	assertMessages(123, "yesterday", "first day within 30")
	assertSets("after a restart")
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

	// Each Project gets a Log in a file, one in its hot table and a late
	// one.
	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	for _, p := range []struct {
		id  uint32
		key string
	}{{123, "test_public_key"}, {456, "other_public_key"}} {
		env.Clock.Set(at(10, 30))
		postAndProcessTo(t, env, p.id, p.key, logJSON(t, "exported", at(10, 10), map[string]any{"user": "ada"}))
		env.Clock.Set(at(12, 30))
		postAndProcessTo(t, env, p.id, p.key,
			logJSON(t, "hot", at(12, 10), map[string]any{"user": "bob"}),
			logJSON(t, "late", at(9, 0), map[string]any{"user": "cy"}))
	}
	env.ExportLogs(t)
	want := latestLogs(t, env, 123, 10)
	if got := messages(want); !slices.Equal(got, []string{"hot", "exported", "late"}) {
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
		if got := env.LogStore.PromotedSet(456, at(12, 0)); len(got.Keys) != 0 {
			t.Errorf("Expected the deleted Project to have no promoted set %s, got %v", when, promotedNames(got))
		}
		assertListsUnchanged(t, env, 123, want, when)
		if got := promotedNames(env.LogStore.PromotedSet(123, at(12, 0))); !slices.Equal(got, []string{"string user"}) {
			t.Errorf("Expected Project 123 to keep its promoted set %s, got %v", when, got)
		}
	}
	assertDeleted("after the pass")
	env.Reopen(t)
	assertDeleted("after a restart")
	env.DeleteLogsPastRetention(t)
	assertDeleted("after another pass")
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

// hourBytes is the size of a Project's files of an hour.
func hourBytes(t *testing.T, env *TestEnv, projectID uint32, hour time.Time) int64 {
	t.Helper()
	files, err := env.LogStore.Files().Hour(projectID, hour)
	if err != nil || len(files) == 0 {
		t.Fatalf("Expected files for Project %d's hour %v, got %v, %v", projectID, hour, files, err)
	}
	var size int64
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("Stat %s: %v", file, err)
		}
		size += info.Size()
	}
	return size
}

// enforceLogDiskLimit deletes the oldest Log files until they take at most
// bytes.
func enforceLogDiskLimit(t *testing.T, env *TestEnv, bytes int64) {
	t.Helper()
	if err := env.LogStore.EnforceDiskLimit(storage.LogDiskLimit{Bytes: bytes}); err != nil {
		t.Fatalf("Enforcing the Log disk limit failed: %v", err)
	}
}

func TestTheDiskLimitDeletesTheOldestHoursOfAnyProjectFirst(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 10, 0, 0, time.UTC) }
	postAt(t, env, 456, "other_public_key", "456 yesterday 23:10", at(1, 23))
	postAt(t, env, 123, "test_public_key", "123 08:10", at(2, 8))
	postAt(t, env, 456, "other_public_key", "456 09:10", at(2, 9))
	postAt(t, env, 123, "test_public_key", "123 10:10", at(2, 10))
	postAt(t, env, 123, "test_public_key", "123 12:10", at(2, 12)) // stays in the hot table
	env.Clock.Set(at(2, 12))
	env.ExportLogs(t)

	assertMessages := func(projectID uint32, when string, want ...string) {
		t.Helper()
		if got := messages(latestLogs(t, env, projectID, 10)); !slices.Equal(got, want) {
			t.Errorf("Expected Project %d to keep %v %s, got %v", projectID, want, when, got)
		}
	}
	oldest := hourBytes(t, env, 456, at(1, 23))
	total := oldest + hourBytes(t, env, 123, at(2, 8)) + hourBytes(t, env, 456, at(2, 9)) + hourBytes(t, env, 123, at(2, 10))

	enforceLogDiskLimit(t, env, total)
	assertMessages(456, "at the limit", "456 09:10", "456 yesterday 23:10")

	enforceLogDiskLimit(t, env, total-1)
	assertMessages(456, "past the limit by a byte", "456 09:10")
	assertMessages(123, "past the limit by a byte", "123 12:10", "123 10:10", "123 08:10")

	enforceLogDiskLimit(t, env, hourBytes(t, env, 123, at(2, 10)))
	assertMessages(456, "with room for one hour")
	assertMessages(123, "with room for one hour", "123 12:10", "123 10:10")
}
