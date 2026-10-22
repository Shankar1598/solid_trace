package main_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLateAndFutureLogsAreListedAndFoundByTheirKeys(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// The hot window is 12:15 to 13:00.
	now := testNow.Add(30 * time.Minute)
	env.Clock.Set(now)
	yesterday := testNow.AddDate(0, 0, -1)
	postAndProcess(t, env,
		logJSON(t, "yesterday", yesterday, map[string]any{"user": "ada", "status": 200}),
		logJSON(t, "before the window", time.Date(2026, 10, 2, 12, 14, 59, 0, time.UTC), map[string]any{"user": "bob"}),
		logJSON(t, "first in the window", time.Date(2026, 10, 2, 12, 15, 0, 0, time.UTC), map[string]any{"user": "cy"}),
		logJSON(t, "next hour", time.Date(2026, 10, 2, 13, 10, 0, 0, time.UTC), map[string]any{"user": "di"}),
	)

	logs := latestLogs(t, env, 123, 10)
	if got, want := messages(logs), []string{"next hour", "first in the window", "before the window", "yesterday"}; !slices.Equal(got, want) {
		t.Fatalf("Expected %v, got %v", want, got)
	}
	if got, want := attributesByMessage(logs)["yesterday"], map[string]any{"user": "ada", "status": 200.0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected a late Log's Attributes %v, got %v", want, got)
	}

	// Only the Log in the hot window promotes its keys, but each Log is
	// found by them, in logs_late and once the export at 14:00 moves the
	// Logs before 13:00 to files with their day's set.
	queries := map[string][]string{
		`where user == "ada"`: {"yesterday"},
		`where user == "bob"`: {"before the window"},
		`where user == "cy"`:  {"first in the window"},
		`where user == "di"`:  {"next hour"},
		`where status == 200`: {"yesterday"},
		`where user != "cy"`:  {"next hour", "before the window", "yesterday"},
	}
	assertQueries(t, env, queries, "with late and future Logs")
	env.Clock.Set(time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC), 1)
	assertQueries(t, env, queries, "after the export")

	// A Project whose only Log is late lists it too.
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	resp := sendLogs(t, env, "/api/456/logs", "application/json",
		logJSON(t, "only late", yesterday, map[string]any{"k": "v"}),
		map[string]string{"Authorization": "Bearer other_public_key"})
	decodeLogCounts(t, resp)
	env.ProcessLogs(t)
	if got, want := messages(latestLogs(t, env, 456, 10)), []string{"only late"}; !slices.Equal(got, want) {
		t.Errorf("Expected Project 456 to list %v, got %v", want, got)
	}
}

// assertListsUnchanged checks that a Project's latest Logs are still want,
// every field and Attribute alike.
func assertListsUnchanged(t *testing.T, env *TestEnv, projectID uint32, want []listedLog, when string) {
	t.Helper()
	if got := latestLogs(t, env, projectID, 100); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected the same Logs %s:\n want %+v\n got  %+v", when, want, got)
	}
}

// assertHourFiles checks how many files the compiler would read for a
// Project's hour.
func assertHourFiles(t *testing.T, env *TestEnv, projectID uint32, hour time.Time, want int) {
	t.Helper()
	files, err := env.LogStore.HourFiles(projectID, hour)
	if err != nil {
		t.Fatalf("List files of %v: %v", hour, err)
	}
	if len(files) != want {
		t.Errorf("Expected %d files for %v, got %v", want, hour, files)
	}
}

func TestLogsListTheSameBeforeAndAfterTheHourlyExport(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }

	// Each Log is sent while the hot window takes it: at 10:29 the window
	// is 10:00 to 11:00, and at 11:29 11:00 to 12:00.
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env, logJSON(t, "ten past ten", at(10, 10), map[string]any{"user": "ada", "status": 200, "cached": true}))
	env.Clock.Set(at(11, 29))
	postAndProcess(t, env,
		logJSON(t, "ten past eleven", at(11, 10), map[string]any{"user": "bob", "roles": []any{"a"}}),
		logJSON(t, "yesterday", testNow.AddDate(0, 0, -1), map[string]any{"user": "old"}),
	)
	// At 12:05 the window has moved on to 11:45 to 13:00, so a Log of 10:20
	// is late, and its hour hasn't been exported yet. So is one of 13:01.
	env.Clock.Set(at(12, 5))
	postAndProcess(t, env,
		logJSON(t, "ten twenty", at(10, 20), map[string]any{"user": "cy", "status": 404}),
		logJSON(t, "noon", at(12, 0), map[string]any{"flag": false}),
		logJSON(t, "next hour", at(13, 1), map[string]any{"user": "di"}),
	)
	want := latestLogs(t, env, 123, 100)
	if got := messages(want); !slices.Equal(got, []string{"next hour", "noon", "ten past eleven", "ten twenty", "ten past ten", "yesterday"}) {
		t.Fatalf("Unexpected Logs before the export: %v", got)
	}

	env.ExportLogs(t) // hour 10, and yesterday's Log
	assertListsUnchanged(t, env, 123, want, "after hour 10 is exported")
	assertHourFiles(t, env, 123, at(10, 0), 1)
	assertHourFiles(t, env, 123, at(11, 0), 0)

	env.Clock.Set(at(13, 0))
	env.ExportLogs(t) // hour 11
	assertListsUnchanged(t, env, 123, want, "after hour 11 is exported")
	env.ExportLogs(t) // nothing new
	assertListsUnchanged(t, env, 123, want, "after an export with nothing to do")

	env.Reopen(t)
	assertListsUnchanged(t, env, 123, want, "after a restart")

	// Later Logs still go to the hot table, and are exported in turn.
	postAndProcess(t, env, logJSON(t, "one o'clock", at(13, 0), map[string]any{"user": "ed"}))
	env.Clock.Set(at(16, 0))
	want = latestLogs(t, env, 123, 100)
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after hours 12 to 14 are exported")
	if got := messages(want); len(got) != 7 {
		t.Errorf("Expected 7 Logs, got %v", got)
	}
}

// unpublishLogFiles renames every Log file back to its staged name, as an
// export leaves its files when it stops between its commit and its renames.
func unpublishLogFiles(t *testing.T, env *TestEnv) {
	t.Helper()
	err := filepath.Walk(filepath.Join(env.dir, "logs_parquet"), func(path string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, ".parquet") {
			return err
		}
		return os.Rename(path, path+".tmp")
	})
	if err != nil {
		t.Fatalf("Unpublish Log files: %v", err)
	}
}

// copyFiles copies each existing file in names from one folder to another.
func copyFiles(t *testing.T, from, to string, names ...string) {
	t.Helper()
	for _, name := range names {
		src, err := os.Open(filepath.Join(from, name))
		if os.IsNotExist(err) {
			os.Remove(filepath.Join(to, name))
			continue
		}
		if err != nil {
			t.Fatalf("Open %s: %v", name, err)
		}
		dst, err := os.Create(filepath.Join(to, name))
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if _, err := io.Copy(dst, src); err != nil {
			t.Fatalf("Copy %s: %v", name, err)
		}
		src.Close()
		dst.Close()
	}
}

func TestACrashBetweenStagingAndRenameLeavesNoHalfWrittenFileAndLosesNoLog(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postWhileHot(t, env,
		logJSON(t, "ten past ten", time.Date(2026, 10, 2, 10, 10, 0, 0, time.UTC), map[string]any{"user": "ada"}),
		logJSON(t, "eleven", time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC), map[string]any{"user": "bob"}),
	)
	env.Clock.Set(time.Date(2026, 10, 2, 12, 5, 0, 0, time.UTC))
	postAndProcess(t, env, logJSON(t, "late ten", time.Date(2026, 10, 2, 10, 50, 0, 0, time.UTC), map[string]any{"user": "cy"}))
	want := latestLogs(t, env, 123, 100)
	ten := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	// The export stops before it commits: logs.duckdb still holds the Logs,
	// and the staged file is on disk.
	env.close()
	backup := t.TempDir()
	duckdbFiles := []string{"logs.duckdb", "logs.duckdb.wal"}
	copyFiles(t, env.dir, backup, duckdbFiles...)
	env.open(t)
	env.ExportLogs(t)
	env.close()
	copyFiles(t, backup, env.dir, duckdbFiles...)
	unpublishLogFiles(t, env)

	env.open(t)
	assertListsUnchanged(t, env, 123, want, "after a crash before the export committed")
	assertHourFiles(t, env, 123, ten, 0)

	// The export stops after it commits, before it renames its file.
	env.ExportLogs(t)
	unpublishLogFiles(t, env)
	env.Reopen(t)
	assertListsUnchanged(t, env, 123, want, "after a crash before the export renamed its file")
	assertHourFiles(t, env, 123, ten, 1)

	// Exports carry on as before.
	env.Clock.Set(time.Date(2026, 10, 2, 13, 0, 0, 0, time.UTC))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the next export")
}

func TestLateLogsOfAnExportedHourGetLateFilesAndListTheSame(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env, logJSON(t, "ten past ten", at(10, 10), map[string]any{"user": "ada", "status": 200}))
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, at(10, 0), 1)

	// Late Logs of hour 10: one with the day's promoted keys, one with a
	// key the day never promoted.
	env.Clock.Set(at(13, 30))
	postAndProcess(t, env,
		logJSON(t, "late ten", at(10, 40), map[string]any{"user": "bob", "status": 500}),
		logJSON(t, "late ten, new key", at(10, 41), map[string]any{"region": "eu"}),
	)
	want := latestLogs(t, env, 123, 100)
	if got := messages(want); !slices.Equal(got, []string{"late ten, new key", "late ten", "ten past ten"}) {
		t.Fatalf("Unexpected Logs before the late file: %v", got)
	}
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the first late file")
	assertHourFiles(t, env, 123, at(10, 0), 2)

	postAndProcess(t, env, logJSON(t, "later still", at(10, 50), map[string]any{"user": "cy"}))
	want = latestLogs(t, env, 123, 100)
	env.Clock.Set(at(14, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the second late file")
	assertHourFiles(t, env, 123, at(10, 0), 3)

	env.Reopen(t)
	assertListsUnchanged(t, env, 123, want, "after a restart")
}

func TestDemotedColumnsAreDroppedAt0100AndTheirValuesStillReadBack(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC) }

	// Yesterday's set fills with k000…k099, then most Logs carry k000…k089
	// and x000…x009, so today's set demotes k090…k099.
	env.Clock.Set(at(1, 23, 29))
	postAndProcess(t, env, logJSON(t, "first", at(1, 23, 10), numberedKeys("k", 100, "v")))
	common := numberedKeys("k", 90, "v")
	for name, value := range numberedKeys("x", 10, 1) {
		common[name] = value
	}
	for i := range 3 {
		postAndProcess(t, env, logJSON(t, fmt.Sprintf("common %d", i), at(1, 23, 20+i), common))
	}
	env.Clock.Set(at(2, 0, 10))
	postAndProcess(t, env, logJSON(t, "after midnight", at(2, 0, 5), map[string]any{"k095": "v", "x000": 7}))
	queries := map[string][]string{
		`where k095 == "v"`: {"after midnight", "first"},
		`where x000 == 7`:   {"after midnight"},
		`where k001 == "v"`: {"common 2", "common 1", "common 0", "first"},
	}
	assertQueries(t, env, queries, "with k095 demoted today")
	want := latestLogs(t, env, 123, 100)

	// The quarter-hour runs before 01:00 keep yesterday's last hour, and
	// with it the demoted columns.
	for _, minute := range []int{15, 30, 45} {
		env.Clock.Set(at(2, 0, minute))
		env.ExportLogs(t)
		assertListsUnchanged(t, env, 123, want, fmt.Sprintf("after the export at 00:%d", minute))
		assertQueries(t, env, queries, fmt.Sprintf("after the export at 00:%d", minute))
	}

	// The export at 01:00 deletes yesterday's last hour.
	env.Clock.Set(at(2, 1, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the export at 01:00")
	assertQueries(t, env, queries, "after the export at 01:00")

	// Today's Logs and late Logs of yesterday still keep the demoted key.
	env.Clock.Set(at(2, 1, 10))
	postAndProcess(t, env,
		logJSON(t, "later today", at(2, 1, 5), map[string]any{"k095": "w", "k001": "v"}),
		logJSON(t, "late yesterday", at(1, 23, 50), map[string]any{"k095": "y"}),
	)
	want = latestLogs(t, env, 123, 100)
	queries[`where k095 == "w"`] = []string{"later today"}
	queries[`where k095 == "y"`] = []string{"late yesterday"}
	queries[`where k001 == "v"`] = []string{"later today", "common 2", "common 1", "common 0", "first"}
	assertQueries(t, env, queries, "with today's and yesterday's late Logs")
	attributes := attributesByMessage(want)
	if got, want := attributes["later today"], map[string]any{"k095": "w", "k001": "v"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got, want := attributes["first"], numberedKeys("k", 100, "v"); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected the first Log's 100 Attributes back, got %v", got)
	}
	env.Clock.Set(at(2, 3, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the next exports")
	assertQueries(t, env, queries, "after the next exports")
	env.Reopen(t)
	assertListsUnchanged(t, env, 123, want, "after a restart")
	assertQueries(t, env, queries, "after a restart")
}
