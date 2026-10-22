package main_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// setupBusyTestEnv is setupTestEnv with a LogStore that takes a Project
// holding 2 hot Logs of the quarter-hour before the quarter cutoff as busy.
func setupBusyTestEnv(t *testing.T) *TestEnv {
	return newTestEnv(t, func(env *TestEnv) { env.BusyProjectLogs = 2 })
}

// assertHourFileNames checks the names of a Project's files of an hour, in
// the order the manifest sorts them.
func assertHourFileNames(t *testing.T, env *TestEnv, projectID uint32, hour time.Time, want ...string) {
	t.Helper()
	files, err := env.LogStore.HourFiles(projectID, hour)
	if err != nil {
		t.Fatalf("List files of %v: %v", hour, err)
	}
	var got []string
	for _, file := range files {
		got = append(got, filepath.Base(file))
	}
	if !slices.Equal(got, want) {
		t.Errorf("Expected Project %d's files of %v to be %v, got %v", projectID, hour.Format("15:04"), want, got)
	}
}

// hourFileNamed is the path of a Project's file of an hour by its name.
func hourFileNamed(t *testing.T, env *TestEnv, projectID uint32, hour time.Time, name string) string {
	t.Helper()
	files, err := env.LogStore.HourFiles(projectID, hour)
	if err != nil {
		t.Fatalf("List files of %v: %v", hour, err)
	}
	for _, file := range files {
		if filepath.Base(file) == name {
			return file
		}
	}
	t.Fatalf("Expected Project %d's hour %v to have %s, got %v", projectID, hour.Format("15:04"), name, files)
	return ""
}

func TestABusyProjectsQuarterHoursGetTheirOwnFilesAndAQuietProjectsHourOneFile(t *testing.T) {
	env := setupBusyTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	quiet := func(logs ...string) { postAndProcessTo(t, env, 456, "other_public_key", logs...) }

	// Project 123 sends 2 Logs a quarter-hour, Project 456 one an hour.
	env.Clock.Set(at(10, 14))
	postAndProcess(t, env,
		logJSON(t, "10:01", at(10, 1), map[string]any{"user": "ada"}),
		logJSON(t, "10:02", at(10, 2), map[string]any{"user": "bob"}))
	quiet(logJSON(t, "quiet 10:05", at(10, 5), map[string]any{"user": "cy"}))

	type run struct {
		at   time.Time
		post []string
		// files are Project 123's files of hour 10 after the run.
		files []string
	}
	for _, r := range []run{
		{at: at(10, 15), files: nil},
		{at: at(10, 30), files: []string{"data-00.parquet"}, post: []string{
			logJSON(t, "10:16", at(10, 16), nil), logJSON(t, "10:17", at(10, 17), nil)}},
		{at: at(10, 45), files: []string{"data-00.parquet", "data-15.parquet"}, post: []string{
			logJSON(t, "10:31", at(10, 31), nil), logJSON(t, "10:32", at(10, 32), nil)}},
		{at: at(11, 0), files: []string{"data-00.parquet", "data-15.parquet", "data-30.parquet"}, post: []string{
			logJSON(t, "10:46", at(10, 46), nil), logJSON(t, "10:47", at(10, 47), nil)}},
		{at: at(11, 15), files: []string{"data-00.parquet", "data-15.parquet", "data-30.parquet", "data-45.parquet"}, post: []string{
			logJSON(t, "11:01", at(11, 1), nil), logJSON(t, "11:02", at(11, 2), nil)}},
		{at: at(11, 30), files: []string{"data-00.parquet", "data-15.parquet", "data-30.parquet", "data-45.parquet"}, post: []string{
			logJSON(t, "11:16", at(11, 16), nil), logJSON(t, "11:17", at(11, 17), nil)}},
		{at: at(11, 45), files: []string{"data-00.parquet", "data-15.parquet", "data-30.parquet", "data-45.parquet"}, post: []string{
			logJSON(t, "11:31", at(11, 31), nil), logJSON(t, "11:32", at(11, 32), nil)}},
	} {
		// Each quarter-hour's Logs are sent during it.
		if r.post != nil {
			env.Clock.Set(r.at.Add(-time.Minute))
			postAndProcess(t, env, r.post...)
		}
		busyWant, quietWant := latestLogs(t, env, 123, 100), latestLogs(t, env, 456, 100)
		env.Clock.Set(r.at)
		env.ExportLogs(t)
		when := "after the run at " + r.at.Format("15:04")
		assertListsUnchanged(t, env, 123, busyWant, when)
		assertListsUnchanged(t, env, 456, quietWant, when)
		assertHourFileNames(t, env, 123, at(10, 0), r.files...)
		assertHourFileNames(t, env, 456, at(10, 0))
	}

	// Each quarter-hour's file holds its Logs.
	for name, want := range map[string][]string{
		"data-00.parquet": {"10:01", "10:02"},
		"data-15.parquet": {"10:16", "10:17"},
		"data-30.parquet": {"10:31", "10:32"},
		"data-45.parquet": {"10:46", "10:47"},
	} {
		if got := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), name)); !slices.Equal(got, want) {
			t.Errorf("Expected %s to hold %v, got %v", name, want, got)
		}
	}

	// The quiet Project's hour 10 goes once it reaches the hour cutoff, at
	// 12:00, in the same run as the busy Project's 11:30 quarter-hour.
	busyWant, quietWant := latestLogs(t, env, 123, 100), latestLogs(t, env, 456, 100)
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, busyWant, "after the run at 12:00")
	assertListsUnchanged(t, env, 456, quietWant, "after the run at 12:00")
	assertHourFileNames(t, env, 456, at(10, 0), "data.parquet")
	assertHourFileNames(t, env, 123, at(11, 0), "data-00.parquet", "data-15.parquet", "data-30.parquet")

	env.Reopen(t)
	assertListsUnchanged(t, env, 123, busyWant, "after a restart")
	assertListsUnchanged(t, env, 456, quietWant, "after a restart")
}

func TestAProjectWhoseQuarterHoursEachHoldTooFewLogsGoesByWholeHours(t *testing.T) {
	env := setupBusyTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	// The Project sends one Log a quarter-hour from 10:00 to 11:59: each hour
	// holds 4, more than the 2 that make it busy, and each quarter-hour 1.
	// At a :00 run its hot table holds 7 Logs before the quarter cutoff, but
	// only the last quarter-hour before it counts.
	for run := at(10, 15); !run.After(at(13, 0)); run = run.Add(15 * time.Minute) {
		if quarter := run.Add(-15 * time.Minute); quarter.Before(at(12, 0)) {
			sent := quarter.Add(time.Minute)
			env.Clock.Set(sent)
			postAndProcess(t, env, logJSON(t, sent.Format("15:04"), sent, nil))
		}
		want := latestLogs(t, env, 123, 100)
		env.Clock.Set(run)
		env.ExportLogs(t)
		when := "after the run at " + run.Format("15:04")
		assertListsUnchanged(t, env, 123, want, when)
		var hour10, hour11 []string
		if !run.Before(at(12, 0)) {
			hour10 = []string{"data.parquet"}
		}
		if !run.Before(at(13, 0)) {
			hour11 = []string{"data.parquet"}
		}
		assertHourFileNames(t, env, 123, at(10, 0), hour10...)
		assertHourFileNames(t, env, 123, at(11, 0), hour11...)
	}
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), "data.parquet")), []string{"10:01", "10:16", "10:31", "10:46"}; !slices.Equal(got, want) {
		t.Errorf("Expected hour 10's data.parquet to hold %v, got %v", want, got)
	}
}

func TestALogOfAnExportedQuarterHourGoesToTheHoursLateFileAtTheNextRun(t *testing.T) {
	env := setupBusyTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(10, 14))
	postAndProcess(t, env,
		logJSON(t, "10:01", at(10, 1), map[string]any{"user": "ada"}),
		logJSON(t, "10:02", at(10, 2), map[string]any{"user": "bob"}))
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env,
		logJSON(t, "10:16", at(10, 16), map[string]any{"user": "ada"}),
		logJSON(t, "10:17", at(10, 17), nil))
	env.Clock.Set(at(10, 30))
	env.ExportLogs(t)
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet")

	// The hot window starts at the quarter cutoff, 10:15 at 10:35, so a Log
	// of 10:05 sent then is late: it waits in logs_late, and isn't counted.
	// A Log of the current quarter-hour, sent on time, is hot and counted,
	// and stays hot through the next run.
	env.Clock.Set(at(10, 35))
	postAndProcess(t, env,
		logJSON(t, "late 10:05", at(10, 5), map[string]any{"user": "cy"}),
		logJSON(t, "on time 10:34", at(10, 34), map[string]any{"user": "di"}))
	queries := map[string][]string{
		`where user == "cy"`:  {"late 10:05"},
		`where user == "ada"`: {"10:16", "10:01"},
		`where user == "di"`:  {"on time 10:34"},
	}
	assertQueries(t, env, queries, "before the run at 10:45")
	assertAttributes(t, getFields(t, env, 123, at(10, 0), at(11, 0), ""),
		[]attributeCount{{Name: "user", Type: "string", Logs: 4}}, "with the late Log in logs_late")
	want := latestLogs(t, env, 123, 100)

	logs := captureLogs(t)
	env.Clock.Set(at(10, 45))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the run at 10:45")
	assertQueries(t, env, queries, "after the run at 10:45")
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet", "data-15.parquet", "late-1.parquet")
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), "late-1.parquet")), []string{"late 10:05"}; !slices.Equal(got, want) {
		t.Errorf("Expected the late file to hold %v, got %v", want, got)
	}
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), "data-15.parquet")), []string{"10:16", "10:17"}; !slices.Equal(got, want) {
		t.Errorf("Expected data-15.parquet to hold %v, got %v", want, got)
	}
	// The log line says which span each file holds.
	line := exportLine(logs.String())
	for _, file := range []string{"hour=10/data-15.parquet (10:15-10:30)", "hour=10/late-1.parquet (10:00-10:15)"} {
		if !strings.Contains(line, file) {
			t.Errorf("Expected the export's log line to name %q, got %q", file, line)
		}
	}

	env.Reopen(t)
	assertListsUnchanged(t, env, 123, want, "after a restart")
	assertQueries(t, env, queries, "after a restart")
}

func TestAnHourThatWentByQuarterHoursKeepsGoingByThemOnceTheProjectIsQuiet(t *testing.T) {
	env := setupBusyTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env,
		logJSON(t, "10:01", at(10, 1), nil),
		logJSON(t, "10:02", at(10, 2), nil),
		logJSON(t, "10:16", at(10, 16), nil))
	env.Clock.Set(at(10, 30))
	env.ExportLogs(t)
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet")

	// From now on the Project sends one Log a quarter-hour, too few to be
	// busy, and the rest of hour 10 still goes a quarter-hour at a time.
	for _, r := range []struct {
		at    time.Time
		post  time.Time
		files []string
	}{
		{at(10, 45), at(10, 31), []string{"data-00.parquet", "data-15.parquet"}},
		{at(11, 0), at(10, 46), []string{"data-00.parquet", "data-15.parquet", "data-30.parquet"}},
		{at(11, 15), at(11, 1), []string{"data-00.parquet", "data-15.parquet", "data-30.parquet", "data-45.parquet"}},
	} {
		env.Clock.Set(r.at.Add(-time.Minute))
		postAndProcess(t, env, logJSON(t, r.post.Format("15:04"), r.post, nil))
		want := latestLogs(t, env, 123, 100)
		env.Clock.Set(r.at)
		env.ExportLogs(t)
		assertListsUnchanged(t, env, 123, want, "after the run at "+r.at.Format("15:04"))
		assertHourFileNames(t, env, 123, at(10, 0), r.files...)
	}

	// Hour 11 began quiet, so it goes whole, at 13:00.
	want := latestLogs(t, env, 123, 100)
	env.Clock.Set(at(12, 45))
	env.ExportLogs(t)
	assertHourFileNames(t, env, 123, at(11, 0))
	env.Clock.Set(at(13, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the run at 13:00")
	assertHourFileNames(t, env, 123, at(11, 0), "data.parquet")
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), "data-15.parquet")), []string{"10:16"}; !slices.Equal(got, want) {
		t.Errorf("Expected data-15.parquet to hold %v, got %v", want, got)
	}
}

func TestARunAfterAGapWritesEachMissedQuarterHoursFile(t *testing.T) {
	env := setupBusyTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	// The export doesn't run from 10:15 to 11:15, as when EventStore is down.
	for _, quarter := range []time.Time{at(10, 0), at(10, 15), at(10, 30), at(10, 45), at(11, 0)} {
		env.Clock.Set(quarter.Add(14 * time.Minute))
		postAndProcess(t, env,
			logJSON(t, quarter.Add(time.Minute).Format("15:04"), quarter.Add(time.Minute), nil),
			logJSON(t, quarter.Add(2*time.Minute).Format("15:04"), quarter.Add(2*time.Minute), nil))
	}
	want := latestLogs(t, env, 123, 100)
	env.Reopen(t)
	env.Clock.Set(at(11, 30))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the run at 11:30")
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet", "data-15.parquet", "data-30.parquet", "data-45.parquet")
	assertHourFileNames(t, env, 123, at(11, 0), "data-00.parquet")
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), "data-30.parquet")), []string{"10:31", "10:32"}; !slices.Equal(got, want) {
		t.Errorf("Expected data-30.parquet to hold %v, got %v", want, got)
	}
}

func TestStagedQuarterHourFilesACrashLeavesAreSettledAsAnHoursAre(t *testing.T) {
	env := setupBusyTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env,
		logJSON(t, "10:01", at(10, 1), map[string]any{"user": "ada"}),
		logJSON(t, "10:02", at(10, 2), nil),
		logJSON(t, "10:16", at(10, 16), nil))
	want := latestLogs(t, env, 123, 100)
	env.Clock.Set(at(10, 30))

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
	assertHourFileNames(t, env, 123, at(10, 0))

	// The export stops after it commits, before it renames its file.
	env.ExportLogs(t)
	unpublishLogFiles(t, env)
	env.Reopen(t)
	assertListsUnchanged(t, env, 123, want, "after a crash before the export renamed its file")
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet")
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), "data-00.parquet")), []string{"10:01", "10:02"}; !slices.Equal(got, want) {
		t.Errorf("Expected data-00.parquet to hold %v, got %v", want, got)
	}
}

func TestAQuietProjectsLogTwentyMinutesLateIsInItsHoursDataFile(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env, logJSON(t, "on time 10:05", at(10, 5), map[string]any{"user": "ada"}))

	// At 10:45 the hot window starts at 10:30, so a Log of 10:25 is late:
	// it waits in logs_late, and isn't counted. One of 10:44 is hot.
	env.Clock.Set(at(10, 45))
	postAndProcess(t, env,
		logJSON(t, "late 10:25", at(10, 25), map[string]any{"user": "bob"}),
		logJSON(t, "on time 10:44", at(10, 44), map[string]any{"user": "cy"}))
	assertAttributes(t, getFields(t, env, 123, at(10, 0), at(11, 0), ""),
		[]attributeCount{{Name: "user", Type: "string", Logs: 2}}, "with the late Log in logs_late")
	queries := map[string][]string{
		`where user == "bob"`: {"late 10:25"},
		`where user != "bob"`: {"on time 10:44", "on time 10:05"},
	}
	assertQueries(t, env, queries, "before the export")
	want := latestLogs(t, env, 123, 100)

	// The quarter-hour runs leave the quiet Project's hour 10 alone.
	for _, run := range []time.Time{at(11, 0), at(11, 15), at(11, 30), at(11, 45)} {
		env.Clock.Set(run)
		env.ExportLogs(t)
		assertHourFileNames(t, env, 123, at(10, 0))
	}

	// The hour's export takes the late Log into data.parquet, sorted with
	// the hour's other Logs, and writes no late file.
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the hour's export")
	assertQueries(t, env, queries, "after the hour's export")
	assertHourFileNames(t, env, 123, at(10, 0), "data.parquet")
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), "data.parquet")), []string{"on time 10:05", "late 10:25", "on time 10:44"}; !slices.Equal(got, want) {
		t.Errorf("Expected data.parquet to hold %v, got %v", want, got)
	}
}

// assertBeforeCutoffAllInFiles checks that every Log a Project lists in
// [hour, cutoff) is in a file of the hour, so none is left in the hot table
// or logs_late.
func assertBeforeCutoffAllInFiles(t *testing.T, env *TestEnv, hour, cutoff time.Time, when string) {
	t.Helper()
	listed := messages(runQueryPages(t, env, 123, queryRequest{From: hour, To: cutoff, Limit: 1000}))
	files, err := env.LogStore.HourFiles(123, hour)
	if err != nil {
		t.Fatalf("List files of %v: %v", hour, err)
	}
	var inFiles []string
	for _, file := range files {
		inFiles = append(inFiles, fileMessages(t, file)...)
	}
	slices.Sort(listed)
	slices.Sort(inFiles)
	if !slices.Equal(listed, inFiles) {
		t.Errorf("%s: expected the Logs before %s, %v, all in files, got %v in files", when, cutoff.Format("15:04"), listed, inFiles)
	}
}

func TestABusyProjectsHotTableHoldsNoLogBeforeTheQuarterCutoffAfterARun(t *testing.T) {
	env := setupBusyTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env,
		logJSON(t, "10:01", at(10, 1), nil), logJSON(t, "10:02", at(10, 2), nil),
		logJSON(t, "10:16", at(10, 16), nil), logJSON(t, "10:17", at(10, 17), nil))
	env.Clock.Set(at(10, 30))
	env.ExportLogs(t)
	assertBeforeCutoffAllInFiles(t, env, at(10, 0), at(10, 15), "after the run at 10:30")

	// Sent after the run at 10:30: a Log of 10:14, before the window, and
	// Logs of the quarter-hour 10:15 and after, inside it. Just before the
	// run at 10:45, a Log of 10:29, still inside it.
	env.Clock.Set(at(10, 31))
	postAndProcess(t, env,
		logJSON(t, "10:14", at(10, 14), nil), logJSON(t, "10:20", at(10, 20), nil),
		logJSON(t, "10:32", at(10, 32), nil))
	env.Clock.Set(at(10, 44))
	postAndProcess(t, env, logJSON(t, "10:29", at(10, 29), nil))
	want := latestLogs(t, env, 123, 100)
	env.Clock.Set(at(10, 45))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the run at 10:45")
	assertBeforeCutoffAllInFiles(t, env, at(10, 0), at(10, 30), "after the run at 10:45")
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet", "data-15.parquet", "late-1.parquet")
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), "data-15.parquet")), []string{"10:16", "10:17", "10:20", "10:29"}; !slices.Equal(got, want) {
		t.Errorf("Expected data-15.parquet to hold %v, got %v", want, got)
	}

	// Between runs too: a Log of 10:25 sent at 10:46 isn't stored hot, so
	// it isn't counted, and the next run writes it to a late file.
	env.Clock.Set(at(10, 46))
	postAndProcess(t, env, logJSON(t, "10:25", at(10, 25), map[string]any{"user": "ada"}))
	assertAttributes(t, getFields(t, env, 123, at(10, 0), at(11, 0), ""), nil, "with a Log of 10:25 sent at 10:46")
	want = latestLogs(t, env, 123, 100)
	env.Clock.Set(at(11, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the run at 11:00")
	assertBeforeCutoffAllInFiles(t, env, at(10, 0), at(10, 45), "after the run at 11:00")
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet", "data-15.parquet", "data-30.parquet", "late-1.parquet", "late-2.parquet")
}

func TestAfterAGapDelayedLogsGoToTheirRangesOwnFileOrALateFile(t *testing.T) {
	env := setupBusyTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(10, 29))
	postAndProcess(t, env,
		logJSON(t, "10:01", at(10, 1), nil), logJSON(t, "10:02", at(10, 2), nil),
		logJSON(t, "10:16", at(10, 16), nil), logJSON(t, "10:17", at(10, 17), nil))
	env.Clock.Set(at(10, 30))
	env.ExportLogs(t)
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet")

	// Nothing arrives from 10:30 to 11:31, as when a shipper falls behind,
	// and the export doesn't run. Then the delayed Logs come, more than 15
	// to 30 minutes late, with Logs sent on time.
	env.Clock.Set(at(11, 31))
	postAndProcess(t, env,
		logJSON(t, "10:05", at(10, 5), map[string]any{"user": "ada"}),
		logJSON(t, "10:20", at(10, 20), map[string]any{"user": "bob"}),
		logJSON(t, "10:35", at(10, 35), nil),
		logJSON(t, "10:50", at(10, 50), nil),
		logJSON(t, "11:05", at(11, 5), map[string]any{"user": "cy"}),
		logJSON(t, "11:20", at(11, 20), nil),
		logJSON(t, "11:25", at(11, 25), nil),
		logJSON(t, "11:31", at(11, 31), nil))
	queries := map[string][]string{
		`where user == "ada"`: {"10:05"},
		`where user == "bob"`: {"10:20"},
		`where user == "cy"`:  {"11:05"},
	}
	assertQueries(t, env, queries, "before the run at 11:45")
	want := latestLogs(t, env, 123, 100)

	// A range with a file gets a late file; each range without one gets
	// its own file, with the hot Logs it holds.
	env.Clock.Set(at(11, 45))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the run at 11:45")
	assertQueries(t, env, queries, "after the run at 11:45")
	assertBeforeCutoffAllInFiles(t, env, at(10, 0), at(11, 0), "after the run at 11:45")
	assertBeforeCutoffAllInFiles(t, env, at(11, 0), at(11, 30), "after the run at 11:45")
	assertHourFileNames(t, env, 123, at(10, 0), "data-00.parquet", "data-15.parquet", "data-30.parquet", "data-45.parquet", "late-1.parquet")
	assertHourFileNames(t, env, 123, at(11, 0), "data-00.parquet", "data-15.parquet")
	if got, want := fileMessages(t, hourFileNamed(t, env, 123, at(11, 0), "data-00.parquet")), []string{"11:05"}; !slices.Equal(got, want) {
		t.Errorf("Expected hour 11's data-00.parquet to hold %v, got %v", want, got)
	}
	for name, want := range map[string][]string{
		"data-15.parquet": {"10:16", "10:17", "10:20"},
		"data-30.parquet": {"10:35"},
		"data-45.parquet": {"10:50"},
		"late-1.parquet":  {"10:05"},
	} {
		if got := fileMessages(t, hourFileNamed(t, env, 123, at(10, 0), name)); !slices.Equal(got, want) {
			t.Errorf("Expected %s to hold %v, got %v", name, want, got)
		}
	}
}
