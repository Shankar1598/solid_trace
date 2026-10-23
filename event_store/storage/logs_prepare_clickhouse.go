package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/pkg/sqltext"
)

// prepare-clickhouse readies an install of the last DuckDB release for the
// ClickHouse release (.scratch/clickhouse/spec.md, "Cutover"). With the
// stack stopped, it leaves every Log in a Log file, every file in the bucket
// if there is one, and writes what the ClickHouse release loads as the
// prepared files, then the marker, to the prepared folder:
//
//   - log_files.parquet: the manifest's rows, one per Log file;
//   - log_promoted_sets.parquet: each Project's promoted set of each day;
//   - log_key_counts.parquet: each Project's key counts of each day;
//   - log_processing_cursors.parquet: each Project's Log processing cursor;
//   - marker.json: the release that wrote them, and when.
//
// The marker comes last, so a folder with one holds every prepared file. Their
// columns are the contract with the ClickHouse release, which reads them
// (ticket 09); the fixtures in event_store/testdata/prepared_clickhouse are
// written by this code.

const (
	// preparedFolder is the name of the prepared folder, next to
	// logs.duckdb.
	preparedFolder = "prepared"
	// markerFile is the name of the marker in the prepared folder.
	markerFile = "marker.json"
)

// PreparedDir is the prepared folder of the install whose logs.duckdb is at
// logsDuckDBPath: prepared/, next to it.
func PreparedDir(logsDuckDBPath string) string {
	return filepath.Join(filepath.Dir(logsDuckDBPath), preparedFolder)
}

// ClickHouseMarker is the marker prepare-clickhouse writes once the prepared
// files are whole: the release that wrote them, and when.
type ClickHouseMarker struct {
	Release    string    `json:"release"`
	PreparedAt time.Time `json:"prepared_at"`
}

// ReadClickHouseMarker reads the marker in the prepared folder dir, or
// returns nil if there is none.
func ReadClickHouseMarker(dir string) (*ClickHouseMarker, error) {
	data, err := os.ReadFile(filepath.Join(dir, markerFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var marker ClickHouseMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Join(dir, markerFile), err)
	}
	return &marker, nil
}

// NewLogsDuckDB is a DuckDB instance for a LogStore alone: in memory, with
// none of events.duckdb's tables, so that prepare-clickhouse attaches
// logs.duckdb to it and never opens events.duckdb. tempDir is where DuckDB
// spills, if not "".
func NewLogsDuckDB(tempDir, memoryLimit string) (*DuckDBWriter, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, err
	}
	settings := []string{
		"SET GLOBAL autoinstall_known_extensions = false",
		"SET GLOBAL autoload_known_extensions = false",
	}
	if tempDir != "" {
		if err := os.MkdirAll(tempDir, 0755); err != nil {
			db.Close()
			return nil, err
		}
		settings = append(settings, "SET temp_directory = "+sqltext.String(tempDir))
	}
	if memoryLimit != "" {
		settings = append(settings, "SET memory_limit = "+sqltext.String(memoryLimit))
	}
	for _, setting := range settings {
		if _, err := db.Exec(setting); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", setting, err)
		}
	}
	return &DuckDBWriter{db: db}, nil
}

// PrepareForClickHouse readies logs.duckdb and the Log files for the
// ClickHouse release, as the release named release, at now. Nothing else may
// use the store meanwhile: the stack is stopped. It
//  1. exports every Log in the hot tables and logs_late to Log files,
//     whatever the busy rule and the cutoffs say;
//  2. writes the prepared files, then the marker, to the prepared folder dir.
func (s *LogStore) PrepareForClickHouse(ctx context.Context, dir, release string, now time.Time) error {
	if err := s.exportEveryLog(now); err != nil {
		return fmt.Errorf("export every Log to Log files: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := s.writePreparedFiles(ctx, dir); err != nil {
		return err
	}
	return writeMarker(dir, ClickHouseMarker{Release: release, PreparedAt: now.UTC()})
}

// preparedFiles are the prepared files, by name, each the query of its rows.
var preparedFiles = []struct{ name, query string }{
	{"log_files.parquet", `
		SELECT project_id, day, hour, name,
			format('project={}/day={}/hour={}/{}', project_id, strftime(day, '%Y-%m-%d'), lpad(CAST(hour AS VARCHAR), 2, '0'), name) AS key,
			bytes, min_time, max_time, log_count, CAST(batch_max AS VARCHAR) AS batch_max, local, remote, generation
		FROM logs.log_files
		ORDER BY project_id, day, hour, name`},
}

// writePreparedFiles writes each prepared file to dir, synced.
func (s *LogStore) writePreparedFiles(ctx context.Context, dir string) error {
	for _, file := range preparedFiles {
		path := filepath.Join(dir, file.name)
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("COPY (%s) TO %s (FORMAT parquet)", file.query, sqltext.String(path))); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := syncFile(path); err != nil {
			return err
		}
	}
	return nil
}

// writeMarker writes the marker to dir, whole or not at all.
func writeMarker(dir string, marker ClickHouseMarker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, markerFile)
	staged := path + stagedSuffix
	if err := os.WriteFile(staged, append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := syncFile(staged); err != nil {
		return err
	}
	if err := os.Rename(staged, path); err != nil {
		return err
	}
	if err := syncFile(dir); err != nil {
		return err
	}
	logger.L.Info("Prepared the install for the ClickHouse release", "dir", dir, "release", marker.Release)
	return nil
}
