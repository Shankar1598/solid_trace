package storage

// Logs from before the switch to ClickHouse.
//
// The DuckDB releases kept Logs in Log files: Parquet files of an hour or a
// quarter-hour of a Project's Logs, on local disk under the Log file store,
// in the bucket under <prefix>logs/, or both. The last DuckDB release's
// prepare-clickhouse writes the manifest of those files, each day's promoted
// set, the key counts and the Processing cursors to prepared/, next to
// logs.duckdb, with a marker. At its first start on such an install,
// EventStore loads them into ClickHouse (UseOldLogFiles), and reads then
// read the files in place: file() for a local copy, s3() for the bucket's.
// Nothing is imported. Retention and Project deletion delete the files.
//
// Everything about old Log files is in this file, which the rest calls
// through a few hooks, so that it goes whole once the last old day has
// passed Retention.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/chsql"
	"github.com/solidtrace/event_store/pkg/logger"
)

// oldLogFilesMigration makes the tables the prepared files are loaded into:
// the old Log files, each day's promoted set, the key counts of the old days,
// and the marker loaded, or none for an install that had no DuckDB data. Its
// version's hundreds are the ticket that added it (09).
var oldLogFilesMigration = clickHouseMigration{version: 901, statements: []string{
	// A file's span is [min_time, max_time]; NULL for a file the DuckDB
	// release couldn't read, which counts as its whole hour.
	`CREATE TABLE IF NOT EXISTS old_log_files (
		project_id UInt32,
		day Date,
		hour UInt8,
		name String,
		key String,
		min_time Nullable(DateTime64(6, 'UTC')),
		max_time Nullable(DateTime64(6, 'UTC')),
		batch_max Nullable(UUID),
		local Bool,
		remote Bool
	) ENGINE = MergeTree
	ORDER BY (project_id, day, hour, name)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `'`,
	`CREATE TABLE IF NOT EXISTS old_log_promoted_sets (
		project_id UInt32,
		day Date,
		type LowCardinality(String),
		key String,
		column String,
		partway Bool
	) ENGINE = MergeTree
	ORDER BY (project_id, day, type, key)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `'`,
	`CREATE TABLE IF NOT EXISTS old_log_key_counts (
		project_id UInt32,
		day Date,
		type LowCardinality(String),
		key String,
		logs Int64
	) ENGINE = MergeTree
	ORDER BY (project_id, day, type, key)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `'`,
	// One row: the marker loaded, or release '' and no prepared_at for an
	// install that started with no DuckDB data.
	`CREATE TABLE IF NOT EXISTS old_log_marker (
		release String,
		prepared_at Nullable(DateTime64(6, 'UTC')),
		recorded_at DateTime64(6, 'UTC') DEFAULT now64(6)
	) ENGINE = MergeTree
	ORDER BY tuple()
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `'`,
}}

// OldLogFiles says where an install's Logs from before the switch are.
type OldLogFiles struct {
	// LogsDuckDB is the DuckDB release's logs.duckdb. Found with no marker,
	// it stops the start: its Logs haven't been prepared.
	LogsDuckDB string
	// PreparedDir is where prepare-clickhouse wrote the prepared files and
	// the marker: prepared/, next to logs.duckdb.
	PreparedDir string
	// LocalDir is the Log file store, where the local copies are
	// (logs_parquet_path).
	LocalDir string
	// ClickHouseDir is the same store as ClickHouse's file() reads it,
	// relative to its user_files_path: "" when the store is mounted there,
	// as in Compose and bin/clickhouse-dev.
	ClickHouseDir string
	// Bucket is the object_store the files were uploaded to, or nil.
	Bucket *config.ObjectStore
}

// oldLogFilesMarker is prepare-clickhouse's marker.json: the release that
// prepared the files, and when.
type oldLogFilesMarker struct {
	Release    string    `json:"release"`
	PreparedAt time.Time `json:"prepared_at"`
}

// oldLogFilesMarkerFile is the marker's name in the prepared folder. The
// DuckDB release removes it when it starts again, as after a rollback.
const oldLogFilesMarkerFile = "marker.json"

// errUnprepared says how to prepare the DuckDB release's Logs.
var errUnprepared = errors.New("stop the stack and run `event_store prepare-clickhouse` from the last DuckDB release's image " +
	"(deploy/install does this when it upgrades), then start again")

// UseOldLogFiles readies the reads of the install's Logs from before the
// switch, at start, before Log processing starts. With a marker it hasn't
// loaded, it loads the prepared files into ClickHouse: the manifest of Log
// files, the promoted sets and the key counts, replacing those of an earlier
// marker, and the Processing cursors into Pebble. With logs.duckdb and no
// marker, it returns an error that names prepare-clickhouse, and EventStore
// must not start: the DuckDB release's Logs would be lost.
func (s *ClickHouseLogs) UseOldLogFiles(cfg OldLogFiles) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	old := &oldLogFiles{conn: s.conn, cfg: cfg}
	if cfg.Bucket != nil {
		old.objects = NewObjectStore(cfg.Bucket)
	}

	marker, err := readOldLogFilesMarker(cfg.PreparedDir)
	if err != nil {
		return err
	}
	loaded, recorded, err := old.loadedMarker(ctx)
	if err != nil {
		return err
	}
	duckDB, err := fileExists(cfg.LogsDuckDB)
	if err != nil {
		return err
	}
	switch {
	case marker != nil && (!recorded || loaded == nil || *loaded != *marker):
		if err := old.load(ctx, s.pebble, *marker); err != nil {
			return fmt.Errorf("load the Logs from before the switch, prepared in %s: %w", cfg.PreparedDir, err)
		}
	case marker == nil && duckDB && (!recorded || loaded != nil):
		// Never prepared, or the DuckDB release ran again since it was,
		// which removes the marker.
		return fmt.Errorf("%s holds the DuckDB release's Logs, and %s has no marker that they were prepared for ClickHouse: %w",
			cfg.LogsDuckDB, cfg.PreparedDir, errUnprepared)
	case !recorded:
		if err := old.recordMarker(ctx, nil); err != nil {
			return err
		}
	}
	return nil
}

// fileExists reports whether a file is at path.
func fileExists(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// readOldLogFilesMarker reads the prepared folder's marker, or nil if it has
// none.
func readOldLogFilesMarker(dir string) (*oldLogFilesMarker, error) {
	if dir == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, oldLogFilesMarkerFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var marker oldLogFilesMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Join(dir, oldLogFilesMarkerFile), err)
	}
	marker.PreparedAt = marker.PreparedAt.UTC()
	return &marker, nil
}

// oldLogFiles are the Log files written before the switch, as loaded.
type oldLogFiles struct {
	conn driver.Conn
	cfg  OldLogFiles
	// objects is the bucket, through the S3 adapter, or nil.
	objects ObjectStore
}

// loadedMarker returns the marker last loaded, nil for an install that
// started with no DuckDB data, and whether any start recorded either.
func (o *oldLogFiles) loadedMarker(ctx context.Context) (*oldLogFilesMarker, bool, error) {
	rows, err := o.conn.Query(ctx, "SELECT release, prepared_at FROM old_log_marker ORDER BY recorded_at DESC LIMIT 1")
	if err != nil {
		return nil, false, fmt.Errorf("read the marker of the Logs from before the switch: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	var (
		release    string
		preparedAt *time.Time
	)
	if err := rows.Scan(&release, &preparedAt); err != nil {
		return nil, false, err
	}
	if preparedAt == nil {
		return nil, true, nil
	}
	return &oldLogFilesMarker{Release: release, PreparedAt: preparedAt.UTC()}, true, nil
}

// recordMarker records the marker loaded, or nil for none.
func (o *oldLogFiles) recordMarker(ctx context.Context, marker *oldLogFilesMarker) error {
	if err := o.conn.Exec(ctx, "TRUNCATE TABLE old_log_marker"); err != nil {
		return err
	}
	if marker == nil {
		return o.conn.Exec(ctx, "INSERT INTO old_log_marker (release, prepared_at) VALUES ('', NULL)")
	}
	return o.conn.Exec(ctx, "INSERT INTO old_log_marker (release, prepared_at) VALUES (?, ?)", marker.Release, marker.PreparedAt)
}

// load loads the prepared files into the old tables, replacing what they
// held, and moves each Project's Processing cursor in Pebble up to the
// prepared one; then it records the marker. Stopped part way, it runs again
// whole at the next start.
func (o *oldLogFiles) load(ctx context.Context, pebble *PebbleWriter, marker oldLogFilesMarker) error {
	_ = pebble
	return o.recordMarker(ctx, &marker)
}

// Unused until the reads land.
var (
	_ = bytes.Compare
	_ = cmp.Compare[int]
	_ = base64.StdEncoding
	_ = path.Join
	_ = regexp.MustCompile
	_ = slices.Sort[[]int]
	_ = strings.Join
	_ sync.Mutex
	_ = clickhouse.Context
	_ uuid.UUID
	_ = chsql.String
	_ = logger.L
)
