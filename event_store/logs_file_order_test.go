package main_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// serviceFileLog is a Log sent from a Service's log file. An empty service or
// file leaves that Field out.
func serviceFileLog(t *testing.T, message string, at time.Time, service, file string) string {
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
		serviceFileLog(t, "web a 1", at(10, 1), "web", "a.log"),
		serviceFileLog(t, "api b 1", at(10, 2), "api", "b.log"),
		serviceFileLog(t, "web c 1", at(10, 3), "web", "c.log"),
		serviceFileLog(t, "api a 1", at(10, 4), "api", "a.log"),
		serviceFileLog(t, "no service, a", at(10, 5), "", "a.log"),
		serviceFileLog(t, "api, no file", at(10, 6), "api", ""),
		serviceFileLog(t, "web a 2", at(10, 7), "web", "a.log"),
		serviceFileLog(t, "api b 2", at(10, 8), "api", "b.log"),
		serviceFileLog(t, "no service or file", at(10, 9), "", ""),
		serviceFileLog(t, "api a 2", at(10, 10), "api", "a.log"),
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
		"no service, a", "no service or file",
	}; !slices.Equal(got, want) {
		t.Errorf("Expected the hour's file in the order\n %v\ngot\n %v", want, got)
	}

	// Late Logs of the hour get a late file, sorted the same way.
	env.Clock.Set(at(13, 30))
	postAndProcess(t, env,
		serviceFileLog(t, "late web b", at(10, 40), "web", "b.log"),
		serviceFileLog(t, "late, no service or file", at(10, 41), "", ""),
		serviceFileLog(t, "late api b", at(10, 42), "api", "b.log"),
		serviceFileLog(t, "late api a", at(10, 43), "api", "a.log"),
	)
	want = latestLogs(t, env, 123, 100)
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the late file")
	if got, want := fileMessages(t, lateFile(t, env, 123, at(10, 0))), []string{"late api a", "late api b", "late web b", "late, no service or file"}; !slices.Equal(got, want) {
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

func TestAFilePromotedForTheDayPartwayOrOnlyInTheMapSortsTheSame(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 456, "partway_key")
	seedProjectKey(t, env.SQLitePath, 789, "map_only_key")

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	serviceFileLogs := []string{
		serviceFileLog(t, "web a 1", at(12, 1), "web", "a.log"),
		serviceFileLog(t, "api b 1", at(12, 2), "api", "b.log"),
		serviceFileLog(t, "no service or file", at(12, 3), "", ""),
		serviceFileLog(t, "api a 1", at(12, 4), "api", "a.log"),
		serviceFileLog(t, "no service, a", at(12, 5), "", "a.log"),
		serviceFileLog(t, "web a 2", at(12, 6), "web", "a.log"),
	}
	sorted := []string{"api a 1", "api b 1", "web a 1", "web a 2", "no service, a", "no service or file"}

	// Project 123's day starts with these Logs, so file is promoted for
	// the day.
	env.Clock.Set(at(12, 30))
	// Project 456's day has a file before its first hot Log: a late Log's,
	// with file in its map. So its set, made by the hot Logs, promotes file
	// partway.
	postAndProcessTo(t, env, 456, "partway_key", serviceFileLog(t, "late", at(10, 30), "", "a.log"))
	env.ExportLogs(t)
	assertHourFiles(t, env, 456, at(10, 0), 1)
	// Project 789's day starts with a Log that fills its set, so file stays
	// in the map.
	postAndProcessTo(t, env, 789, "map_only_key", logJSON(t, "full", at(12, 0), numberedKeys("k", 100, "v")))

	postAndProcess(t, env, serviceFileLogs...)
	postAndProcessTo(t, env, 456, "partway_key", serviceFileLogs...)
	postAndProcessTo(t, env, 789, "map_only_key", serviceFileLogs...)
	want := map[uint32][]listedLog{}
	for _, projectID := range []uint32{123, 456, 789} {
		want[projectID] = latestLogs(t, env, projectID, 100)
	}

	env.Clock.Set(at(14, 0))
	env.ExportLogs(t)
	for projectID, wantSorted := range map[uint32][]string{
		123: sorted,
		456: sorted,
		789: {"api a 1", "api b 1", "web a 1", "web a 2", "no service, a", "full", "no service or file"},
	} {
		assertListsUnchanged(t, env, projectID, want[projectID], "after the export")
		if got := fileMessages(t, hourFile(t, env, projectID, at(12, 0))); !slices.Equal(got, wantSorted) {
			t.Errorf("Project %d: expected the hour's file in the order\n %v\ngot\n %v", projectID, wantSorted, got)
		}
	}
}

// setLogFileOrder sets a Project's Log file order in Console, adding its row
// if it has none: order is the column's JSON text, or "" for NULL.
func setLogFileOrder(t *testing.T, sqlitePath string, projectID uint32, order string) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqlitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite for seeding: %v", err)
	}
	defer db.Close()
	value := sql.NullString{String: order, Valid: order != ""}
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, organization_id, log_file_order, created_at, updated_at) VALUES (?, ?, 1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET log_file_order = excluded.log_file_order`,
		projectID, fmt.Sprintf("Project %d", projectID), value, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to set the Log file order of Project %d: %v", projectID, err)
	}
}

// leveledLog is a Log of a level from a Service's log file. An empty level
// leaves it out.
func leveledLog(t *testing.T, message string, at time.Time, level, service, file string) string {
	t.Helper()
	attributes := map[string]any{"service": service, "file": file}
	if level != "" {
		attributes["level"] = level
	}
	return logJSON(t, message, at, attributes)
}

func TestAnHoursFileIsSortedByItsProjectsLogFileOrder(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	keys := map[uint32]string{456: "time_only_key", 789: "missing_key", 321: "null_key"}
	for projectID, key := range keys {
		seedProjectKey(t, env.SQLitePath, projectID, key)
	}
	setLogFileOrder(t, env.SQLitePath, 123, `["level"]`)
	setLogFileOrder(t, env.SQLitePath, 456, `[]`)
	setLogFileOrder(t, env.SQLitePath, 789, `["region"]`)
	setLogFileOrder(t, env.SQLitePath, 321, "")

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	logs := []string{
		leveledLog(t, "web a info", at(10, 1), "info", "web", "a.log"),
		leveledLog(t, "api b error", at(10, 2), "error", "api", "b.log"),
		leveledLog(t, "web c warn", at(10, 3), "warn", "web", "c.log"),
		leveledLog(t, "api a info", at(10, 4), "info", "api", "a.log"),
		leveledLog(t, "api a no level", at(10, 5), "", "api", "a.log"),
		leveledLog(t, "web a error", at(10, 6), "error", "web", "a.log"),
	}
	inTime := []string{"web a info", "api b error", "web c warn", "api a info", "api a no level", "web a error"}
	env.Clock.Set(at(10, 30))
	postAndProcess(t, env, logs...)
	for projectID, key := range keys {
		postAndProcessTo(t, env, projectID, key, logs...)
	}
	want := map[uint32][]listedLog{}
	for _, projectID := range []uint32{123, 456, 789, 321} {
		want[projectID] = latestLogs(t, env, projectID, 100)
	}

	captured := captureLogs(t)
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	for projectID, wantOrder := range map[uint32][]string{
		123: {"api b error", "web a error", "web a info", "api a info", "web c warn", "api a no level"},
		456: inTime,
		789: inTime,
		321: {"api a info", "api a no level", "api b error", "web a info", "web a error", "web c warn"},
	} {
		assertListsUnchanged(t, env, projectID, want[projectID], "after the export")
		if got := fileMessages(t, hourFile(t, env, projectID, at(10, 0))); !slices.Equal(got, wantOrder) {
			t.Errorf("Project %d: expected the hour's file in the order\n %v\ngot\n %v", projectID, wantOrder, got)
		}
	}
	for _, order := range []string{`order="level, time"`, `order=time`, `order="region, time"`, `order="service, file, time"`} {
		if !strings.Contains(captured.String(), order) {
			t.Errorf("Expected an export's log line to name %s, got\n%s", order, captured.String())
		}
	}
}

func TestAChangedLogFileOrderAppliesFromTheNextExport(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	hourLogs := func(hour int, prefix string) []string {
		return []string{
			leveledLog(t, prefix+"web a info", at(hour, 1), "info", "web", "a.log"),
			leveledLog(t, prefix+"api b error", at(hour, 2), "error", "api", "b.log"),
			leveledLog(t, prefix+"web a error", at(hour, 3), "error", "web", "a.log"),
		}
	}
	byDefault := func(prefix string) []string {
		return []string{prefix + "api b error", prefix + "web a info", prefix + "web a error"}
	}
	byLevel := func(prefix string) []string {
		return []string{prefix + "api b error", prefix + "web a error", prefix + "web a info"}
	}

	env.Clock.Set(at(10, 30))
	postAndProcess(t, env, hourLogs(10, "")...)
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	if got := fileMessages(t, hourFile(t, env, 123, at(10, 0))); !slices.Equal(got, byDefault("")) {
		t.Errorf("Expected the first hour's file in the default order\n %v\ngot\n %v", byDefault(""), got)
	}

	// The order changes; the next export writes a new hour and a late file
	// of the first.
	setLogFileOrder(t, env.SQLitePath, 123, `["level"]`)
	env.Clock.Set(at(12, 30))
	postAndProcess(t, env, hourLogs(12, "")...)
	postAndProcess(t, env, hourLogs(10, "late ")...)
	want := latestLogs(t, env, 123, 100)
	env.Clock.Set(at(14, 0))
	env.ExportLogs(t)
	assertListsUnchanged(t, env, 123, want, "after the export")

	if got := fileMessages(t, hourFile(t, env, 123, at(12, 0))); !slices.Equal(got, byLevel("")) {
		t.Errorf("Expected the next hour's file in the new order\n %v\ngot\n %v", byLevel(""), got)
	}
	late := lateFile(t, env, 123, at(10, 0))
	if got := fileMessages(t, late); !slices.Equal(got, byLevel("late ")) {
		t.Errorf("Expected the first hour's late file in the new order\n %v\ngot\n %v", byLevel("late "), got)
	}
	first := filepath.Join(filepath.Dir(late), "data.parquet")
	if got := fileMessages(t, first); !slices.Equal(got, byDefault("")) {
		t.Errorf("Expected the first hour's file to keep its order\n %v\ngot\n %v", byDefault(""), got)
	}
}

func TestALogFileOrderEventStoreCantReadExportsInTheDefaultOrder(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	// Orders written by hand that Console would refuse, by Project.
	unreadable := map[uint32]string{
		123: `"level"`,
		456: `["level", 3]`,
		457: `["level", "service", "environment", "file"]`,
		458: `["level", "level"]`,
	}
	for i, field := range []string{"time", "_time", "id", "message", "trace_id", "span_id"} {
		unreadable[uint32(460+i)] = fmt.Sprintf(`["level", %q]`, field)
	}
	// The JSON null reads as no order, as Console reads it.
	const nullOrder = 470
	projects := []uint32{nullOrder}
	for projectID, order := range unreadable {
		if projectID != 123 {
			seedProjectKey(t, env.SQLitePath, projectID, fmt.Sprintf("key_%d", projectID))
		}
		setLogFileOrder(t, env.SQLitePath, projectID, order)
		projects = append(projects, projectID)
	}
	seedProjectKey(t, env.SQLitePath, nullOrder, fmt.Sprintf("key_%d", nullOrder))
	setLogFileOrder(t, env.SQLitePath, nullOrder, `null`)

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	logs := []string{
		leveledLog(t, "web a info", at(10, 1), "info", "web", "a.log"),
		leveledLog(t, "api b error", at(10, 2), "error", "api", "b.log"),
		leveledLog(t, "web a error", at(10, 3), "error", "web", "a.log"),
	}
	env.Clock.Set(at(10, 30))
	want := map[uint32][]listedLog{}
	for _, projectID := range projects {
		if projectID == 123 {
			postAndProcess(t, env, logs...)
		} else {
			postAndProcessTo(t, env, projectID, fmt.Sprintf("key_%d", projectID), logs...)
		}
		want[projectID] = latestLogs(t, env, projectID, 100)
	}

	captured := captureLogs(t)
	env.Clock.Set(at(12, 0))
	env.ExportLogs(t)
	for _, projectID := range projects {
		assertListsUnchanged(t, env, projectID, want[projectID], "after the export")
		if got, want := fileMessages(t, hourFile(t, env, projectID, at(10, 0))), []string{"api b error", "web a info", "web a error"}; !slices.Equal(got, want) {
			t.Errorf("Project %d: expected the hour's file in the default order\n %v\ngot\n %v", projectID, want, got)
		}
		warning := fmt.Sprintf(`level=warn message="Failed to read a Project's Log file order; exporting its Logs in the default order" project_id=%d`, projectID)
		if warned := strings.Contains(captured.String(), warning); warned != (projectID != nullOrder) {
			t.Errorf("Project %d (order %s): expected a warning %v, got\n%s", projectID, unreadable[projectID], projectID != nullOrder, captured.String())
		}
	}
}
