package storage

import (
	"database/sql"
	"errors"

	_ "github.com/mattn/go-sqlite3"
)

// The instance settings until the operator first sets one, when Console's
// instance_settings table has no row. They mirror that table's column
// defaults in console/db/schema.rb, which owns them.
const (
	defaultSQLQueriesEnabled = false
	defaultLogRetentionDays  = 30
)

// instanceSettingsID is the one instance_settings row (InstanceSetting::ID in
// Console).
const instanceSettingsID = 1

// InstanceSettings are the install-wide settings the operator sets in Console.
type InstanceSettings struct {
	// SQLQueriesEnabled lets every member run raw DuckDB SQL (ADR 0003).
	SQLQueriesEnabled bool
	// DefaultLogRetentionDays is the Retention of a Project that sets none.
	DefaultLogRetentionDays int
}

// ConsoleSettings reads Console's instance settings and Projects' Retention
// from Console's SQLite database, read-only: Logs write no Console tables (ADR
// 0001). Every call reads the database, so a change in Console shows at once;
// callers that need a cache keep their own.
type ConsoleSettings struct {
	db *sql.DB
}

func NewConsoleSettings(sqlitePath string) (*ConsoleSettings, error) {
	// go-sqlite3 hands mode=ro to SQLite only for a file: URI; on a plain path
	// it drops the query string and opens the database read-write.
	db, err := sql.Open("sqlite3", "file:"+sqlitePath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &ConsoleSettings{db: db}, nil
}

func (s *ConsoleSettings) Close() error {
	return s.db.Close()
}

// Instance returns the instance settings.
func (s *ConsoleSettings) Instance() (InstanceSettings, error) {
	var settings InstanceSettings
	err := s.db.QueryRow(
		"SELECT sql_queries_enabled, default_log_retention_days FROM instance_settings WHERE id = ?",
		instanceSettingsID,
	).Scan(&settings.SQLQueriesEnabled, &settings.DefaultLogRetentionDays)
	if errors.Is(err, sql.ErrNoRows) {
		return InstanceSettings{
			SQLQueriesEnabled:       defaultSQLQueriesEnabled,
			DefaultLogRetentionDays: defaultLogRetentionDays,
		}, nil
	}
	if err != nil {
		return InstanceSettings{}, err
	}
	return settings, nil
}

// LogRetentionDays returns a Project's effective Retention in whole days: its
// own, else the instance default. A Project with no row in projects gets the
// instance default; telling a deleted Project apart is the caller's job.
func (s *ConsoleSettings) LogRetentionDays(projectID uint32) (int, error) {
	var days int
	err := s.db.QueryRow(`
		SELECT COALESCE(
			(SELECT log_retention_days FROM projects WHERE id = ?),
			(SELECT default_log_retention_days FROM instance_settings WHERE id = ?),
			?
		)`,
		projectID, instanceSettingsID, defaultLogRetentionDays,
	).Scan(&days)
	return days, err
}
