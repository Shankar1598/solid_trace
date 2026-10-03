package storage

import (
	"database/sql"
	"errors"
	"fmt"

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
// and slugs from Console's SQLite database, read-only: Logs write no Console tables (ADR
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
// instance default; telling a deleted Project apart is the caller's job. A
// Retention below one day, which only Console's validations prevent, is an
// error.
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
	if err != nil {
		return 0, err
	}
	if days < 1 {
		return 0, fmt.Errorf("project %d has a Retention of %d days", projectID, days)
	}
	return days, nil
}

// ProjectSlug returns a Project's slug, which Queries name its Logs by, or ""
// for a Project Console doesn't know or that has none.
func (s *ConsoleSettings) ProjectSlug(projectID uint32) (string, error) {
	var slug sql.NullString
	err := s.db.QueryRow("SELECT slug FROM projects WHERE id = ?", projectID).Scan(&slug)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return slug.String, err
}

// ProjectLogRetention returns the effective Retention, in whole days, of every
// Project in Console's projects table, by id. A Project missing from it has
// been deleted. A Project whose Retention is below one day, which only
// Console's validations prevent, has 0.
func (s *ConsoleSettings) ProjectLogRetention() (map[uint32]int, error) {
	rows, err := s.db.Query(`
		SELECT id, COALESCE(
			log_retention_days,
			(SELECT default_log_retention_days FROM instance_settings WHERE id = ?),
			?
		)
		FROM projects`,
		instanceSettingsID, defaultLogRetentionDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	retention := make(map[uint32]int)
	for rows.Next() {
		var (
			projectID uint32
			days      int
		)
		if err := rows.Scan(&projectID, &days); err != nil {
			return nil, err
		}
		retention[projectID] = max(days, 0)
	}
	return retention, rows.Err()
}
