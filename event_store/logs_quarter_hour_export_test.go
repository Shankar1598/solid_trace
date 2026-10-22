package main_test

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// setupBusyTestEnv is setupTestEnv with a LogStore that takes a Project
// holding 2 hot Logs before the quarter cutoff as busy.
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
	// 12:00, in the same run as the busy Project's 11:00 quarter-hour.
	busyWant, quietWant := latestLogs(t, env, 123, 100), latestLogs(t, env, 456, 100)
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, busyWant, "after the run at 12:00")
	assertListsUnchanged(t, env, 456, quietWant, "after the run at 12:00")
	assertHourFileNames(t, env, 456, at(10, 0), "data.parquet")
	assertHourFileNames(t, env, 123, at(11, 0), "data-00.parquet")

	env.Reopen(t)
	assertListsUnchanged(t, env, 123, busyWant, "after a restart")
	assertListsUnchanged(t, env, 456, quietWant, "after a restart")
}
