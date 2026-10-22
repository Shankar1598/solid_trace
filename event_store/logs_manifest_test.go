package main_test

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// logAnswers is what Queries of Project 123 answer: its Logs, newest first,
// and a table of them counted by hour.
type logAnswers struct {
	logs   []listedLog
	byHour []string
}

func answersOf(t *testing.T, env *TestEnv) logAnswers {
	t.Helper()
	_, byHour := runTable(t, env, "summarize count() by bin(time, 1h)")
	slices.Sort(byHour)
	return logAnswers{logs: latestLogs(t, env, 123, 100), byHour: byHour}
}

// assertAnswersUnchanged checks that Project 123's Queries still answer
// want, and that each Log is still fetched by its id, with and without its
// time.
func assertAnswersUnchanged(t *testing.T, env *TestEnv, want logAnswers, when string) {
	t.Helper()
	got := answersOf(t, env)
	if !reflect.DeepEqual(got.logs, want.logs) {
		t.Errorf("Expected the same Logs %s:\n want %+v\n got  %+v", when, want.logs, got.logs)
	}
	if !slices.Equal(got.byHour, want.byHour) {
		t.Errorf("Expected the same counts by hour %s:\n want %v\n got  %v", when, want.byHour, got.byHour)
	}
	for _, log := range want.logs {
		for _, hint := range []*time.Time{nil, &log.Time} {
			if status, fetched := fetchLog(t, env, 123, log.ID, hint); status != 200 || !reflect.DeepEqual(fetched, log) {
				t.Errorf("Expected %q fetched by its id %s (time hint %v), got %d %+v", log.Message, when, hint != nil, status, fetched)
			}
		}
	}
}

// forgetLogFileManifest drops the Log file manifest from the closed
// environment's logs.duckdb, as an install from before the manifest has
// none.
func forgetLogFileManifest(t *testing.T, env *TestEnv) {
	t.Helper()
	db, err := sql.Open("duckdb", filepath.Join(env.dir, "logs.duckdb"))
	if err != nil {
		t.Fatalf("Open logs.duckdb: %v", err)
	}
	defer db.Close()
	for _, table := range []string{"log_files", "log_file_deletions", "log_file_backfill"} {
		if _, err := db.Exec("DROP TABLE " + table); err != nil {
			t.Fatalf("Drop %s: %v", table, err)
		}
	}
}

func TestAnInstallFromBeforeTheManifestKeepsItsLogsAndItsStagedFilesAreSettled(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	ten := at(10, 0)
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env,
		logJSON(t, "ten past ten", at(10, 10), map[string]any{"user": "ada", "status": 200}),
		logJSON(t, "yesterday", testNow.AddDate(0, 0, -1), map[string]any{"user": "old"}),
	)
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t) // hour 10's data.parquet, and yesterday's

	// A late Log of hour 10 waits for its late file, and one of 12:10 is
	// hot.
	env.Clock.Set(at(12, 29))
	postAndProcess(t, env,
		logJSON(t, "late ten", at(10, 40), map[string]any{"user": "bob", "status": 500}),
		logJSON(t, "ten past twelve", at(12, 10), map[string]any{"user": "cy"}),
	)
	want := answersOf(t, env)
	if got := messages(want.logs); !slices.Equal(got, []string{"ten past twelve", "late ten", "ten past ten", "yesterday"}) {
		t.Fatalf("Unexpected Logs: %v", got)
	}

	// An older EventStore stops while it exports the late file, before its
	// commit: logs.duckdb still holds the late Log, and every file is on
	// disk under its staged name, the committed ones too.
	env.close()
	backup := t.TempDir()
	duckdbFiles := []string{"logs.duckdb", "logs.duckdb.wal"}
	copyFiles(t, env.dir, backup, duckdbFiles...)
	env.open(t)
	env.ExportLogs(t)
	env.close()
	copyFiles(t, backup, env.dir, duckdbFiles...)
	unpublishLogFiles(t, env)
	forgetLogFileManifest(t, env)

	env.open(t)
	assertAnswersUnchanged(t, env, want, "after an upgrade from before the manifest")
	assertHourFiles(t, env, 123, ten, 1)

	// Exports go on from the files on disk: the late Log gets a late file.
	env.ExportLogs(t)
	assertAnswersUnchanged(t, env, want, "after the late file")
	assertHourFiles(t, env, 123, ten, 2)

	// An older EventStore stops after its export commits, before it renames
	// its file.
	postAndProcess(t, env, logJSON(t, "later ten", at(10, 50), map[string]any{"user": "di"}))
	want = answersOf(t, env)
	env.ExportLogs(t)
	unpublishLogFiles(t, env)
	env.close()
	forgetLogFileManifest(t, env)
	env.open(t)
	assertAnswersUnchanged(t, env, want, "after an upgrade from before the manifest, past a commit")
	assertHourFiles(t, env, 123, ten, 3)

	// The next late file takes the next name, and replaces none.
	postAndProcess(t, env, logJSON(t, "latest ten", at(10, 55), nil))
	want = answersOf(t, env)
	env.Clock.Set(at(13, 0))
	env.ExportLogs(t)
	env.Reopen(t)
	assertAnswersUnchanged(t, env, want, "after the next late file and a restart")
	assertHourFiles(t, env, 123, ten, 4)
}

func TestAFileAReadIsReadingOutlivesRetentionUntilALaterPass(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 7)
	tenDaysAgo := testNow.AddDate(0, 0, -10)
	postAt(t, env, 123, "test_public_key", "ten days ago", tenDaysAgo)
	postAt(t, env, 123, "test_public_key", "today", testNow.Add(-30*time.Minute))
	env.Clock.Set(testNow)
	env.ExportLogs(t)
	files, err := env.LogStore.HourFiles(123, tenDaysAgo)
	if err != nil || len(files) != 1 {
		t.Fatalf("Expected one file of ten days ago, got %v (%v)", files, err)
	}
	dayDir := filepath.Dir(filepath.Dir(files[0]))

	// A read of all time is running when Retention deletes the day.
	cancel, longRead := startLongRead(t, env)
	defer cancel()
	if err := env.LogCleanup.DeleteLogsPastRetention(testNow); !errors.Is(err, storage.ErrLogFilesInUse) {
		t.Errorf("Expected Retention to wait for the read to delete the file, got %v", err)
	}
	if got, err := listLatest(env); err != nil || !slices.Equal(got, []string{"today"}) {
		t.Errorf("Expected later reads to see only today's Log, got %v (%v)", got, err)
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Errorf("Expected the file to stay while the read may read it, got %v", err)
	}

	// The next pass, after the read, deletes it, and its day's folder.
	cancel()
	<-longRead
	env.DeleteLogsPastRetention(t)
	if _, err := os.Stat(dayDir); !os.IsNotExist(err) {
		t.Errorf("Expected the day's folder deleted after the read, got %v", err)
	}
	if got, err := listLatest(env); err != nil || !slices.Equal(got, []string{"today"}) {
		t.Errorf("Expected only today's Log, got %v (%v)", got, err)
	}
}

// assertGone checks that a Log file or folder is gone from disk.
func assertGone(t *testing.T, path, when string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Expected %s deleted from disk %s, got %v", path, when, err)
	}
}

// hourFile is the path of a Project's only file of the hour of t.
func hourFile(t *testing.T, env *TestEnv, projectID uint32, at time.Time) string {
	t.Helper()
	files, err := env.LogStore.HourFiles(projectID, at)
	if err != nil || len(files) != 1 {
		t.Fatalf("Expected one file of Project %d at %v, got %v (%v)", projectID, at, files, err)
	}
	return files[0]
}

func TestAReadHoldsUpTheDeletionOfOnlyItsOwnProjectsFiles(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	keys := map[uint32]string{123: "test_public_key", 456: "other_public_key", 789: "third_public_key"}
	tenDaysAgo := testNow.AddDate(0, 0, -10)
	for _, projectID := range []uint32{123, 456, 789} {
		seedProject(t, env.SQLitePath, projectID, 7)
		if projectID != 123 {
			seedProjectKey(t, env.SQLitePath, projectID, keys[projectID])
		}
		postAt(t, env, projectID, keys[projectID], "ten days ago", tenDaysAgo)
	}
	env.Clock.Set(testNow)
	env.ExportLogs(t)
	files := map[uint32]string{}
	for projectID := range keys {
		files[projectID] = hourFile(t, env, projectID, tenDaysAgo)
	}

	// A read of Project 123 holds up the deletion of its own file only.
	cancel, longRead := startLongRead(t, env)
	defer cancel()
	cutoff := testNow.AddDate(0, 0, -7)
	if err := env.LogStore.DeleteLogDaysBefore(123, cutoff); !errors.Is(err, storage.ErrLogFilesInUse) {
		t.Errorf("Expected Retention of the read's Project to wait for the read, got %v", err)
	}
	if err := env.LogStore.DeleteLogDaysBefore(456, cutoff); err != nil {
		t.Errorf("Expected Retention of another Project to go through during the read, got %v", err)
	}
	assertGone(t, files[456], "by another Project's Retention during the read")
	if err := env.LogStore.DeleteProjectLogs(789); err != nil {
		t.Errorf("Expected another Project's deletion to go through during the read, got %v", err)
	}
	assertGone(t, filepath.Dir(filepath.Dir(filepath.Dir(files[789]))), "by another Project's deletion during the read")
	if _, err := os.Stat(files[123]); err != nil {
		t.Errorf("Expected the read's file to stay while the read may read it, got %v", err)
	}
	cancel()
	<-longRead
}

func TestADeletedProjectsFileThatAReadHoldsGoesWithTheNextCleanupPass(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	postAndProcess(t, env, logJSON(t, "exported", testNow.Add(-150*time.Minute), nil))
	env.ExportLogs(t)
	file := hourFile(t, env, 123, testNow.Add(-150*time.Minute))
	projectDir := filepath.Dir(filepath.Dir(filepath.Dir(file)))

	// Project 123 is deleted while a read of it runs.
	cancel, longRead := startLongRead(t, env)
	defer cancel()
	consoleExec(t, env, "DELETE FROM projects WHERE id = 123")
	if err := env.LogCleanup.DeleteLogsPastRetention(testNow); !errors.Is(err, storage.ErrLogFilesInUse) {
		t.Errorf("Expected the Project's deletion to wait for the read to delete its file, got %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("Expected the file to stay while the read may read it, got %v", err)
	}
	if got, err := listLatest(env); err != nil || len(got) != 0 {
		t.Errorf("Expected later reads to see no Logs of the deleted Project, got %v (%v)", got, err)
	}

	// The Project is no longer listed, and the next Cleanup pass after the
	// read deletes its file all the same.
	cancel()
	<-longRead
	env.DeleteLogsPastRetention(t)
	enforceLogDiskLimit(t, env, 1<<40)
	assertGone(t, projectDir, "by the next Cleanup pass after the read")
}

// findContent returns the path of a file in dir that holds content, or "".
func findContent(t *testing.T, dir string, content []byte) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Read %s: %v", dir, err)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if got, err := os.ReadFile(path); err == nil && bytes.Equal(got, content) {
			return path
		}
	}
	return ""
}

func TestAnUpgradeKeepsTheLogFilesItCannotRead(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 7)
	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	postWhileHot(t, env,
		logJSON(t, "ten past nine", at(9, 10), nil),
		logJSON(t, "ten past ten", at(10, 10), nil),
	)
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	nine, ten := hourFile(t, env, 123, at(9, 0)), hourFile(t, env, 123, at(10, 0))

	// An older EventStore leaves both files damaged, and a staged file cut
	// off by a crash during its COPY.
	env.close()
	damaged := map[string][]byte{
		nine: []byte("not a Parquet file: nine"),
		ten:  []byte("not a Parquet file: ten"),
	}
	staged := map[string][]byte{
		filepath.Join(filepath.Dir(nine), "late-1.parquet.tmp"): []byte("not a Parquet file: staged"),
	}
	for _, files := range []map[string][]byte{damaged, staged} {
		for path, content := range files {
			if err := os.WriteFile(path, content, 0644); err != nil {
				t.Fatalf("Damage %s: %v", path, err)
			}
		}
	}
	forgetLogFileManifest(t, env)
	env.open(t)

	assertKept := func(when string) {
		t.Helper()
		for path, content := range damaged {
			if findContent(t, filepath.Dir(path), content) == "" {
				t.Errorf("Expected the bytes of %s kept for an operator %s", path, when)
			}
		}
	}
	assertKept("after the upgrade")
	for path, content := range staged {
		if findContent(t, filepath.Dir(path), content) != "" {
			t.Errorf("Expected the damaged staged file %s deleted by the upgrade", path)
		}
	}

	// A late Log of hour 10 gets a file of its own.
	env.Clock.Set(at(12, 30))
	postAndProcess(t, env, logJSON(t, "late ten", at(10, 40), nil))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, at(10, 0), 2)
	env.Reopen(t)
	assertKept("after a late file and a restart")

	// Retention deletes the damaged files with their day.
	env.Clock.Set(at(12, 0).AddDate(0, 0, 9))
	env.DeleteLogsPastRetention(t)
	assertGone(t, filepath.Dir(nine), "by Retention")
	assertGone(t, filepath.Dir(ten), "by Retention")
}
