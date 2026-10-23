package storage

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/solidtrace/event_store/pkg/sqltext"
)

// DuckDBWriter is the DuckDB database that LogStore attaches logs.duckdb to.
// Events are in ClickHouse (clickhouse_events.go); an events.duckdb from
// before keeps its events_hot table, which nothing reads.
type DuckDBWriter struct {
	db *sql.DB
}

// configureDuckDB applies to db the settings of every DuckDB instance that
// EventStore opens. tempDir is where DuckDB spills and memoryLimit its memory
// limit, each left to DuckDB if "".
func configureDuckDB(db *sql.DB, tempDir, memoryLimit string) error {
	// DuckDB never installs or loads an extension by itself, so a SQL query
	// can't bring in one whose functions change things (ADR 0003). The ones
	// built in are loaded already, and UseBucket loads httpfs by name.
	settings := []string{
		"SET GLOBAL autoinstall_known_extensions = false",
		"SET GLOBAL autoload_known_extensions = false",
	}
	if tempDir != "" {
		if err := os.MkdirAll(tempDir, 0755); err != nil {
			return err
		}
		settings = append(settings, "SET temp_directory = "+sqltext.String(tempDir))
	}
	if memoryLimit != "" {
		settings = append(settings, "SET memory_limit = "+sqltext.String(memoryLimit))
	}
	for _, setting := range settings {
		if _, err := db.Exec(setting); err != nil {
			return fmt.Errorf("%s: %w", setting, err)
		}
	}
	return nil
}

func NewDuckDBWriter(dbPath, tempDir, memoryLimit string) (*DuckDBWriter, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, err
	}
	if err := configureDuckDB(db, tempDir, memoryLimit); err != nil {
		return nil, err
	}

	// SQL mode's events view reads this one, which has the events table's
	// columns and no rows, until SQL queries run on ClickHouse (ticket 07).
	if _, err := db.Exec(`
		CREATE OR REPLACE VIEW events AS
		SELECT CAST(NULL AS VARCHAR) AS uuid, CAST(NULL AS BIGINT) AS project_id,
			CAST(NULL AS BIGINT) AS issue_fingerprint_id, CAST(NULL AS TIMESTAMP) AS timestamp,
			CAST(NULL AS VARCHAR) AS environment, CAST(NULL AS VARCHAR) AS server_name,
			CAST(NULL AS VARCHAR) AS release, CAST(NULL AS VARCHAR) AS level,
			CAST(NULL AS MAP(VARCHAR, VARCHAR)) AS tags
		WHERE false
	`); err != nil {
		return nil, err
	}

	return &DuckDBWriter{db: db}, nil
}

// discardConn keeps a connection whose transaction may still be open out of
// the pool: returning driver.ErrBadConn from Raw makes database/sql close it.
func discardConn(conn *sql.Conn) {
	_ = conn.Raw(func(interface{}) error { return driver.ErrBadConn })
}

func (w *DuckDBWriter) Close() {
	w.db.Close()
}

// Ping checks if DuckDB is healthy by running a simple query
func (w *DuckDBWriter) Ping() error {
	var result int
	return w.db.QueryRow("SELECT 1").Scan(&result)
}
