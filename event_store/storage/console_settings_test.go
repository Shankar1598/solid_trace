package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// settingsTablesDDL copies the tables ConsoleSettings reads from
// console/db/schema.rb, which owns them.
const settingsTablesDDL = `
PRAGMA journal_mode=WAL;

CREATE TABLE instance_settings (
	id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
	created_at DATETIME NOT NULL,
	default_log_retention_days INTEGER DEFAULT 30 NOT NULL,
	sql_queries_enabled BOOLEAN DEFAULT FALSE NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
	created_at DATETIME NOT NULL,
	log_retention_days INTEGER,
	name VARCHAR,
	organization_id INTEGER NOT NULL,
	slug VARCHAR,
	updated_at DATETIME NOT NULL
);
`

// newConsoleDB creates a Console-shaped database and returns its path and a
// read-write connection standing in for Console.
func newConsoleDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "solid_trace.sqlite3")
	console, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("Failed to open SQLite: %v", err)
	}
	t.Cleanup(func() { console.Close() })

	if _, err := console.Exec(settingsTablesDDL); err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}
	return path, console
}

func openConsoleSettings(t *testing.T, path string) *ConsoleSettings {
	t.Helper()
	settings, err := NewConsoleSettings(path)
	if err != nil {
		t.Fatalf("Failed to open Console settings: %v", err)
	}
	t.Cleanup(func() { settings.Close() })
	return settings
}

func consoleExec(t *testing.T, console *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := console.Exec(query, args...); err != nil {
		t.Fatalf("Console write failed: %v", err)
	}
}

func addProject(t *testing.T, console *sql.DB, id int, logRetentionDays any) {
	t.Helper()
	consoleExec(t, console, `INSERT INTO projects (id, created_at, log_retention_days, name, organization_id, slug, updated_at)
		VALUES (?, datetime('now'), ?, 'Project', 1, 'project', datetime('now'))`, id, logRetentionDays)
}

func setInstanceSettings(t *testing.T, console *sql.DB, sqlQueriesEnabled bool, defaultLogRetentionDays int) {
	t.Helper()
	consoleExec(t, console, `INSERT INTO instance_settings (id, created_at, default_log_retention_days, sql_queries_enabled, updated_at)
		VALUES (1, datetime('now'), ?, ?, datetime('now'))
		ON CONFLICT (id) DO UPDATE SET default_log_retention_days = excluded.default_log_retention_days,
			sql_queries_enabled = excluded.sql_queries_enabled`, defaultLogRetentionDays, sqlQueriesEnabled)
}

func assertInstanceSettings(t *testing.T, settings *ConsoleSettings, want InstanceSettings) {
	t.Helper()
	got, err := settings.Instance()
	if err != nil {
		t.Fatalf("Instance: %v", err)
	}
	if got != want {
		t.Errorf("instance settings = %+v, want %+v", got, want)
	}
}

func assertLogRetentionDays(t *testing.T, settings *ConsoleSettings, projectID uint32, want int) {
	t.Helper()
	got, err := settings.LogRetentionDays(projectID)
	if err != nil {
		t.Fatalf("LogRetentionDays(%d): %v", projectID, err)
	}
	if got != want {
		t.Errorf("LogRetentionDays(%d) = %d, want %d", projectID, got, want)
	}
}

func TestConsoleSettings_FreshInstallHasSQLOffAndThirtyDayRetention(t *testing.T) {
	path, console := newConsoleDB(t)
	addProject(t, console, 1, nil)
	// Console isn't running: its connection is closed and the WAL files are gone.
	console.Close()

	settings := openConsoleSettings(t, path)

	assertInstanceSettings(t, settings, InstanceSettings{SQLQueriesEnabled: false, DefaultLogRetentionDays: 30})
	assertLogRetentionDays(t, settings, 1, 30)
}

func TestConsoleSettings_ReadsWhatTheOperatorAndAdminsSet(t *testing.T) {
	path, console := newConsoleDB(t)
	setInstanceSettings(t, console, true, 14)
	addProject(t, console, 1, nil)
	addProject(t, console, 2, 90)

	settings := openConsoleSettings(t, path)

	assertInstanceSettings(t, settings, InstanceSettings{SQLQueriesEnabled: true, DefaultLogRetentionDays: 14})
	assertLogRetentionDays(t, settings, 1, 14)
	assertLogRetentionDays(t, settings, 2, 90)
}

func TestConsoleSettings_SeesChangesWithoutReopening(t *testing.T) {
	path, console := newConsoleDB(t)
	addProject(t, console, 1, nil)
	settings := openConsoleSettings(t, path)
	assertLogRetentionDays(t, settings, 1, 30)

	setInstanceSettings(t, console, true, 7)
	assertInstanceSettings(t, settings, InstanceSettings{SQLQueriesEnabled: true, DefaultLogRetentionDays: 7})
	assertLogRetentionDays(t, settings, 1, 7)

	consoleExec(t, console, `UPDATE projects SET log_retention_days = 3 WHERE id = 1`)
	assertLogRetentionDays(t, settings, 1, 3)

	setInstanceSettings(t, console, false, 7)
	assertInstanceSettings(t, settings, InstanceSettings{SQLQueriesEnabled: false, DefaultLogRetentionDays: 7})
}

func TestConsoleSettings_ProjectWithoutRowGetsTheInstanceDefault(t *testing.T) {
	path, console := newConsoleDB(t)
	setInstanceSettings(t, console, false, 21)

	settings := openConsoleSettings(t, path)

	assertLogRetentionDays(t, settings, 123, 21)
}
