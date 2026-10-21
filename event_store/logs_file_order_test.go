package main_test

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// sourceLog is a Log sent from a Service's log file. An empty service or
// file leaves that Field out.
func sourceLog(t *testing.T, message string, at time.Time, service, file string) string {
	t.Helper()
	attributes := map[string]any{}
	if service != "" {
		attributes["service"] = service
	}
	if file != "" {
		attributes["file"] = file
	}
	return logJSON(t, message, at, attributes)
}

// fileMessages reads the messages of a Log file in the file's own row order.
func fileMessages(t *testing.T, path string) []string {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("Open DuckDB: %v", err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT message FROM read_parquet(?, file_row_number = true) ORDER BY file_row_number", path)
	if err != nil {
		t.Fatalf("Read %s: %v", path, err)
	}
	defer rows.Close()
	var messages []string
	for rows.Next() {
		var message string
		if err := rows.Scan(&message); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Read %s: %v", path, err)
	}
	return messages
}

func TestAnHoursFileIsSortedByServiceThenFileThenTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(10, 30))
	postAndProcess(t, env,
		sourceLog(t, "web a 1", at(10, 1), "web", "a.log"),
		sourceLog(t, "api b 1", at(10, 2), "api", "b.log"),
		sourceLog(t, "web c 1", at(10, 3), "web", "c.log"),
		sourceLog(t, "api a 1", at(10, 4), "api", "a.log"),
		sourceLog(t, "no service, a", at(10, 5), "", "a.log"),
		sourceLog(t, "api, no file", at(10, 6), "api", ""),
		sourceLog(t, "web a 2", at(10, 7), "web", "a.log"),
		sourceLog(t, "api b 2", at(10, 8), "api", "b.log"),
		sourceLog(t, "no source", at(10, 9), "", ""),
		sourceLog(t, "api a 2", at(10, 10), "api", "a.log"),
	)
	want := latestLogs(t, env, 123, 100)

	logs := captureLogs(t)
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the export")
	if line := exportLine(logs.String()); !strings.Contains(line, `order="service, file, time"`) {
		t.Errorf("Expected the export's log line to name the order, got %q", line)
	}

	// Logs without a service or a file come after those with one.
	got := fileMessages(t, hourFile(t, env, 123, at(10, 0)))
	if want := []string{
		"api a 1", "api a 2", "api b 1", "api b 2", "api, no file",
		"web a 1", "web a 2", "web c 1",
		"no service, a", "no source",
	}; !slices.Equal(got, want) {
		t.Errorf("Expected the hour's file in the order\n %v\ngot\n %v", want, got)
	}

	// Late Logs of the hour get a late file, sorted the same way.
	env.Clock.Set(at(13, 30))
	postAndProcess(t, env,
		sourceLog(t, "late web b", at(10, 40), "web", "b.log"),
		sourceLog(t, "late no source", at(10, 41), "", ""),
		sourceLog(t, "late api b", at(10, 42), "api", "b.log"),
		sourceLog(t, "late api a", at(10, 43), "api", "a.log"),
	)
	want = latestLogs(t, env, 123, 100)
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the late file")
	if got, want := fileMessages(t, lateFile(t, env, 123, at(10, 0))), []string{"late api a", "late api b", "late web b", "late no source"}; !slices.Equal(got, want) {
		t.Errorf("Expected the late file in the order\n %v\ngot\n %v", want, got)
	}
}

// exportLine is the last "Exported a Project's Logs" line of logs.
func exportLine(logs string) string {
	var line string
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, "Exported a Project's Logs") {
			line = l
		}
	}
	return line
}

// lateFile is the path of the only late file of a Project's hour.
func lateFile(t *testing.T, env *TestEnv, projectID uint32, hour time.Time) string {
	t.Helper()
	files, err := env.LogStore.HourFiles(projectID, hour)
	if err != nil || len(files) != 2 {
		t.Fatalf("Expected a file and a late file of Project %d at %v, got %v (%v)", projectID, hour, files, err)
	}
	for _, file := range files {
		if filepath.Base(file) != "data.parquet" {
			return file
		}
	}
	t.Fatalf("Expected a late file of Project %d at %v, got %v", projectID, hour, files)
	return ""
}
