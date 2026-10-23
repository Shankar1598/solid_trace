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
	"github.com/solidtrace/event_store/models"
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
		column_name String,
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
	if old.state, err = old.readState(ctx); err != nil {
		return err
	}
	if len(old.state.files) > 0 {
		s.old = old
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

	// mu guards state, which reads read and Retention changes.
	mu    sync.RWMutex
	state oldState
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

// preparedTables are the prepared files loaded into old tables, each with
// the select that reads it, from %s.
var preparedTables = []struct{ file, table, columns, read string }{
	{"log_files.parquet", "old_log_files",
		"project_id, day, hour, name, key, min_time, max_time, batch_max, local, remote",
		"SELECT project_id, day, hour, name, key, min_time, max_time, toUUIDOrNull(batch_max), local, remote FROM %s"},
	{"log_promoted_sets.parquet", "old_log_promoted_sets",
		"project_id, day, type, key, column_name, partway",
		"SELECT project_id, day, type, key, `column`, partway FROM %s"},
	{"log_key_counts.parquet", "old_log_key_counts",
		"project_id, day, type, key, logs",
		"SELECT project_id, day, type, key, logs FROM %s"},
}

// preparedCursors is the prepared file of the Processing cursors.
const preparedCursors = "log_processing_cursors.parquet"

// preparedData reads a prepared file in a query: ClickHouse can't see the
// prepared folder, so the file goes with the query, as a parameter.
const preparedData = "format(Parquet, base64Decode({prepared:String}))"

// withPrepared is ctx carrying a prepared file for preparedData.
func (o *oldLogFiles) withPrepared(ctx context.Context, file string) (context.Context, error) {
	raw, err := os.ReadFile(filepath.Join(o.cfg.PreparedDir, file))
	if err != nil {
		return nil, err
	}
	return clickhouse.Context(ctx, clickhouse.WithParameters(clickhouse.Parameters{"prepared": base64.StdEncoding.EncodeToString(raw)})), nil
}

// load loads the prepared files into the old tables, replacing what they
// held, and moves each Project's Processing cursor in Pebble up to the
// prepared one; then it records the marker. Stopped part way, it runs again
// whole at the next start.
func (o *oldLogFiles) load(ctx context.Context, pebble *PebbleWriter, marker oldLogFilesMarker) error {
	for _, prepared := range preparedTables {
		withFile, err := o.withPrepared(ctx, prepared.file)
		if err != nil {
			return err
		}
		if err := o.conn.Exec(ctx, "TRUNCATE TABLE "+prepared.table); err != nil {
			return err
		}
		insert := fmt.Sprintf("INSERT INTO %s (%s) %s", prepared.table, prepared.columns, fmt.Sprintf(prepared.read, preparedData))
		if err := o.conn.Exec(withFile, insert); err != nil {
			return fmt.Errorf("load %s: %w", prepared.file, err)
		}
	}
	if err := o.loadCursors(ctx, pebble); err != nil {
		return fmt.Errorf("load %s: %w", preparedCursors, err)
	}
	if err := o.recordMarker(ctx, &marker); err != nil {
		return err
	}
	logger.L.Info("Loaded the Logs from before the switch to ClickHouse", "release", marker.Release, "prepared_at", marker.PreparedAt)
	return nil
}

// loadCursors moves each Project's Processing cursor in Pebble up to the
// one prepared, the last Log batch the DuckDB release stored, unless it is
// already past it.
func (o *oldLogFiles) loadCursors(ctx context.Context, pebble *PebbleWriter) error {
	withFile, err := o.withPrepared(ctx, preparedCursors)
	if err != nil {
		return err
	}
	rows, err := o.conn.Query(withFile, "SELECT CAST(project_id AS UInt32), CAST(batch_id AS String) FROM "+preparedData)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			projectID uint32
			batch     string
		)
		if err := rows.Scan(&projectID, &batch); err != nil {
			return err
		}
		prepared, err := uuid.Parse(batch)
		if err != nil {
			return fmt.Errorf("Project %d's cursor %q: %w", projectID, batch, err)
		}
		cursor, err := pebble.LogProcessingCursor(projectID)
		if err != nil {
			return err
		}
		if bytes.Compare(prepared[:], cursor[:]) > 0 {
			if err := pebble.SetLogProcessingCursor(projectID, prepared); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

// oldLogFile is one old Log file of a Project.
type oldLogFile struct {
	day time.Time
	// key is its path under the Log file store, and under logs/ in the
	// bucket: project=<id>/day=<YYYY-MM-DD>/hour=<HH>/<name>.
	key string
	// from and to are the span of its Logs' times, [from, to); its hour
	// when the DuckDB release couldn't read it.
	from, to time.Time
	// local is whether a copy is in the Log file store: then it is read
	// there, and else from the bucket.
	local bool
}

// oldPromotedKey is an Attribute key promoted on a day: its values are in
// column of that day's files, and when partway, in its map too.
type oldPromotedKey struct {
	typ     AttributeType
	key     string
	column  string
	partway bool
}

// oldKeyPattern is what an old Log file's key looks like. Keys go into
// file() and s3() paths as they are, so nothing else is read.
var oldKeyPattern = regexp.MustCompile(`^project=[0-9]+/day=[0-9]{4}-[0-9]{2}-[0-9]{2}/hour=[0-9]{2}/[a-z0-9-]+\.parquet$`)

// oldState is the old Log files and promoted sets as the tables hold them.
type oldState struct {
	// files are each Project's, by day, hour and name.
	files map[uint32][]oldLogFile
	sets  map[projectDay][]oldPromotedKey
}

// readState reads the old tables.
func (o *oldLogFiles) readState(ctx context.Context) (oldState, error) {
	state := oldState{files: map[uint32][]oldLogFile{}, sets: map[projectDay][]oldPromotedKey{}}
	rows, err := o.conn.Query(ctx, `SELECT project_id, day, hour, key, min_time, max_time, local
		FROM old_log_files ORDER BY project_id, day, hour, name`)
	if err != nil {
		return state, fmt.Errorf("read the old Log files: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			projectID  uint32
			file       oldLogFile
			hour       uint8
			minT, maxT *time.Time
		)
		if err := rows.Scan(&projectID, &file.day, &hour, &file.key, &minT, &maxT, &file.local); err != nil {
			return state, err
		}
		if !oldKeyPattern.MatchString(file.key) {
			return state, fmt.Errorf("old Log file %q isn't named as prepare-clickhouse names them", file.key)
		}
		file.day = LogDay(file.day)
		file.from = file.day.Add(time.Duration(hour) * time.Hour)
		file.to = file.from.Add(time.Hour)
		if minT != nil && maxT != nil {
			file.from, file.to = minT.UTC(), maxT.UTC().Add(time.Microsecond)
		}
		state.files[projectID] = append(state.files[projectID], file)
	}
	if err := rows.Err(); err != nil {
		return state, err
	}

	sets, err := o.conn.Query(ctx, `SELECT project_id, day, type, key, column_name, partway
		FROM old_log_promoted_sets ORDER BY project_id, day, type, key`)
	if err != nil {
		return state, fmt.Errorf("read the old promoted sets: %w", err)
	}
	defer sets.Close()
	for sets.Next() {
		var (
			day      projectDay
			promoted oldPromotedKey
			typ      string
		)
		if err := sets.Scan(&day.projectID, &day.day, &typ, &promoted.key, &promoted.column, &promoted.partway); err != nil {
			return state, err
		}
		promoted.typ = AttributeType(typ)
		day.day = LogDay(day.day)
		state.sets[day] = append(state.sets[day], promoted)
	}
	return state, sets.Err()
}

// oldSource is old Log files of one Project's day that one table function
// reads: all on local disk, or all in the bucket.
type oldSource struct {
	day   time.Time
	set   []oldPromotedKey
	local bool
	keys  []string
}

// sources returns the sources of a Project's old files that keep selects.
func (o *oldLogFiles) sources(projectID uint32, keep func(oldLogFile) bool) []oldSource {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	var files []oldLogFile
	for _, file := range o.state.files[projectID] {
		if keep(file) {
			files = append(files, file)
		}
	}
	// A day's local and bucket files may alternate by hour.
	slices.SortStableFunc(files, func(a, b oldLogFile) int {
		return cmp.Or(a.day.Compare(b.day), compareBool(a.local, b.local))
	})
	var sources []oldSource
	for _, file := range files {
		if n := len(sources); n > 0 && sources[n-1].day.Equal(file.day) && sources[n-1].local == file.local {
			sources[n-1].keys = append(sources[n-1].keys, file.key)
			continue
		}
		set := o.state.sets[projectDay{projectID, file.day}]
		sources = append(sources, oldSource{day: file.day, set: set, local: file.local, keys: []string{file.key}})
	}
	return sources
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

// inSpans keeps the files whose Logs' span overlaps one of spans.
func inSpans(spans ...timeSpan) func(oldLogFile) bool {
	return func(file oldLogFile) bool {
		for _, span := range spans {
			if file.from.Before(span.to) && span.from.Before(file.to) {
				return true
			}
		}
		return false
	}
}

// oldFileSettings are the settings each read of old files runs with. A
// file deleted while the read runs, as by Retention, is skipped. Paths look
// hive-partitioned, but their parts aren't columns.
const oldFileSettings = " SETTINGS use_hive_partitioning = 0, s3_ignore_file_doesnt_exist = 1, input_format_parquet_allow_missing_columns = 1"

// table is the table function that reads a source's files. Each is read
// with the columns of its day's set, and a column a file lacks reads NULL.
// The paths are a glob of the keys, which reads the files that exist and
// lists nothing.
func (o *oldLogFiles) table(src oldSource) string {
	glob := "{" + strings.Join(src.keys, ",") + "}"
	structure := chsql.String(oldFileStructure(src.set))
	if src.local {
		return fmt.Sprintf("file(%s, 'Parquet', %s)", chsql.String(path.Join(o.cfg.ClickHouseDir, glob)), structure)
	}
	key := "NOSIGN"
	if o.cfg.Bucket.KeyID != "" {
		key = "{old_files_key_id:String}, {old_files_secret:String}"
	}
	return fmt.Sprintf("s3(%s, %s, 'Parquet', %s)", chsql.String(oldBucketURL(o.cfg.Bucket)+glob), key, structure)
}

// withBucketKey is ctx carrying the bucket's key for table's s3(), as
// parameters, so that it is in no statement's text.
func (o *oldLogFiles) withBucketKey(ctx context.Context) context.Context {
	if o == nil || o.cfg.Bucket == nil || o.cfg.Bucket.KeyID == "" {
		return ctx
	}
	return clickhouse.Context(ctx, clickhouse.WithParameters(clickhouse.Parameters{
		"old_files_key_id": o.cfg.Bucket.KeyID,
		"old_files_secret": o.cfg.Bucket.Secret,
	}))
}

// oldBucketURL is where the bucket keeps the old Log files: the keys go
// after it.
func oldBucketURL(bucket *config.ObjectStore) string {
	key := bucket.Prefix + logObjectPrefix
	if bucket.Endpoint == "" {
		return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket.Bucket, bucket.Region, key)
	}
	return bucket.Endpoint + "/" + bucket.Bucket + "/" + key
}

// oldFileStructure is the columns an old Log file of a day is read with:
// the fixed ones, then the day's promoted columns.
func oldFileStructure(set []oldPromotedKey) string {
	columns := []string{
		"id UUID", "time DateTime64(6, 'UTC')",
		"level Nullable(String)", "message Nullable(String)", "service Nullable(String)",
		"environment Nullable(String)", "trace_id Nullable(String)", "span_id Nullable(String)",
		"batch Nullable(UUID)",
		"attrs_string Map(String, String)", "attrs_number Map(String, Float64)",
		"attrs_bool Map(String, Bool)", "attrs_json Map(String, String)",
	}
	for _, promoted := range set {
		columns = append(columns, chsql.Identifier(promoted.column)+" Nullable("+attributeTypes[promoted.typ]+")")
	}
	return strings.Join(columns, ", ")
}

// oldDay is how a day's old files keep each Field, for ClickHouseColumns:
// a promoted key in its column, coalesced with its map when it was
// promoted partway through the day, any other in its map. The built-in
// fields read as the logs table's do: service and message never NULL.
type oldDay struct {
	set []oldPromotedKey
}

func (d *oldDay) builtIn(name string) string {
	switch name {
	case "service", "message":
		return "ifNull(" + chsql.Identifier(name) + ", '')"
	}
	return chsql.Identifier(name)
}

func (d *oldDay) attribute(t AttributeType, key string) string {
	m := "attrs_" + string(t)
	inMap := "if(mapContains(" + m + ", " + chsql.String(key) + "), " + m + "[" + chsql.String(key) + "], NULL)"
	for _, promoted := range d.set {
		if promoted.typ != t || promoted.key != key {
			continue
		}
		if promoted.partway {
			return "coalesce(" + chsql.Identifier(promoted.column) + ", " + inMap + ")"
		}
		return chsql.Identifier(promoted.column)
	}
	return inMap
}

// searchText is every string Attribute's value, joined by \0: the map's
// and the promoted columns'.
func (d *oldDay) searchText() string {
	parts := []string{"arrayStringConcat(mapValues(attrs_string), '\\0')"}
	for _, promoted := range d.set {
		if promoted.typ == StringAttribute {
			parts = append(parts, "ifNull("+chsql.Identifier(promoted.column)+", '')")
		}
	}
	return "concatWithSeparator('\\0', " + strings.Join(parts, ", ") + ")"
}

// columns is how the source's files keep each Field.
func (src oldSource) columns() ClickHouseColumns {
	return ClickHouseColumns{old: &oldDay{set: src.set}}
}

// timeSpans is the condition that time is in one of spans.
func timeSpans(spans []timeSpan) string {
	inSpans := make([]string, len(spans))
	for i, span := range spans {
		inSpans[i] = fmt.Sprintf("(time >= %s AND time < %s)", chsql.Time(span.from), chsql.Time(span.to))
	}
	return "(" + strings.Join(inSpans, " OR ") + ")"
}

// oldWhere is the WHERE of a read of a source: conditions, and the Logs
// where keeps, written for the source's day.
func (src oldSource) where(conditions []string, where func(ClickHouseColumns) Filter) string {
	if where != nil {
		filter := where(src.columns())
		for _, part := range []string{filter.Rest, filter.Message} {
			if part != "" {
				conditions = append(conditions, "("+part+")")
			}
		}
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

// span returns the times of the oldest and newest Logs a Project's old files
// may hold in [from, to), and false if none of its files overlaps it.
func (o *oldLogFiles) span(projectID uint32, from, to time.Time) (oldest, newest time.Time, ok bool) {
	if o == nil {
		return
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	keep := inSpans(timeSpan{from, to})
	for _, file := range o.state.files[projectID] {
		if !keep(file) {
			continue
		}
		first, last := latestTime(file.from, from), earliestTime(file.to, to).Add(-time.Microsecond)
		if !ok || first.Before(oldest) {
			oldest = first
		}
		if !ok || last.After(newest) {
			newest = last
		}
		ok = true
	}
	return oldest, newest, ok
}

// oldLogColumns are the columns old Logs are read whole with, in every
// source alike: the fixed ones, then the day's promoted keys and values of
// each type as arrays, since each day has its own.
func (src oldSource) logColumns() string {
	list := []string{"id", "time", "level", "ifNull(message, '')", "ifNull(service, '')", "environment", "trace_id", "span_id",
		"attrs_string", "attrs_number", "attrs_bool", "attrs_json"}
	for i, t := range []AttributeType{StringAttribute, NumberAttribute, BoolAttribute} {
		var keys, values []string
		for _, promoted := range src.set {
			if promoted.typ == t {
				keys = append(keys, chsql.String(promoted.key))
				values = append(values, chsql.Identifier(promoted.column))
			}
		}
		list = append(list,
			fmt.Sprintf("CAST([%s] AS Array(String)) AS promoted_keys_%d", strings.Join(keys, ", "), i),
			fmt.Sprintf("CAST([%s] AS Array(Nullable(%s))) AS promoted_values_%d", strings.Join(values, ", "), attributeTypes[t], i))
	}
	return strings.Join(list, ", ")
}

// scanLogs returns up to limit of a Project's old Logs of [from, to) that
// meet conditions and where, newest first.
func (o *oldLogFiles) scanLogs(s *ClickHouseLogs, ctx context.Context, projectID uint32, from, to time.Time, conditions []string, where func(ClickHouseColumns) Filter, limit int) ([]models.Log, error) {
	sources := o.sources(projectID, inSpans(timeSpan{from, to}))
	if len(sources) == 0 || limit <= 0 {
		return nil, nil
	}
	conditions = append(conditions, timeSpans([]timeSpan{{from, to}}))
	return o.queryLogs(s, ctx, sources, conditions, where, fmt.Sprintf("ORDER BY %s LIMIT %d", logsNewestFirst, limit))
}

// queryLogs reads the Logs of sources that meet conditions and where,
// whole, with tail after them: an order and a limit.
func (o *oldLogFiles) queryLogs(s *ClickHouseLogs, ctx context.Context, sources []oldSource, conditions []string, where func(ClickHouseColumns) Filter, tail string) (logs []models.Log, err error) {
	selects := make([]string, len(sources))
	for i, src := range sources {
		selects[i] = fmt.Sprintf("(SELECT %s FROM %s %s%s)", src.logColumns(), o.table(src), src.where(conditions, where), oldFileSettings)
	}
	query := "SELECT * FROM (" + strings.Join(selects, "\nUNION ALL\n") + ") " + tail
	began := time.Now()
	defer func() { recordStatement(ctx, query, began, err) }()
	rows, err := s.conn.Query(o.withBucketKey(ctx), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		log, err := scanOldLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// scanOldLog reads a row of logColumns as a Log: a promoted column's value
// where it has one, else the map's.
func scanOldLog(rows driver.Rows) (models.Log, error) {
	var (
		log                                 models.Log
		level, environment, traceID, spanID *string
		strs, jsons                         map[string]string
		nums                                map[string]float64
		bools                               map[string]bool
		strKeys, numKeys, boolKeys          []string
		strValues                           []*string
		numValues                           []*float64
		boolValues                          []*bool
	)
	if err := rows.Scan(&log.ID, &log.Time, &level, &log.Message, &log.Service, &environment, &traceID, &spanID,
		&strs, &nums, &bools, &jsons, &strKeys, &strValues, &numKeys, &numValues, &boolKeys, &boolValues); err != nil {
		return models.Log{}, err
	}
	log.Time = log.Time.UTC()
	log.Level, log.Environment, log.TraceID, log.SpanID = stringOf(level), stringOf(environment), stringOf(traceID), stringOf(spanID)
	log.Attributes = models.Attributes{String: orEmpty(strs), Number: orEmpty(nums), Bool: orEmpty(bools), JSON: orEmpty(jsons)}
	setPromoted(log.Attributes.String, strKeys, strValues)
	setPromoted(log.Attributes.Number, numKeys, numValues)
	setPromoted(log.Attributes.Bool, boolKeys, boolValues)
	return log, nil
}

func orEmpty[V any](m map[string]V) map[string]V {
	if m == nil {
		return map[string]V{}
	}
	return m
}

// setPromoted sets each key whose promoted column has a value.
func setPromoted[V any](m map[string]V, keys []string, values []*V) {
	for i, key := range keys {
		if i < len(values) && values[i] != nil {
			m[key] = *values[i]
		}
	}
}

// mergeNewestFirst merges two lists of Logs, each newest first, into one,
// newest first, of at most limit Logs.
func mergeNewestFirst(a, b []models.Log, limit int) []models.Log {
	merged := make([]models.Log, 0, min(len(a)+len(b), limit))
	for len(merged) < limit && (len(a) > 0 || len(b) > 0) {
		if len(b) == 0 || (len(a) > 0 && newerFirst(a[0], b[0]) <= 0) {
			merged, a = append(merged, a[0]), a[1:]
		} else {
			merged, b = append(merged, b[0]), b[1:]
		}
	}
	return merged
}

// newerFirst orders Logs newest first: by time, then id, in the order of
// its bytes.
func newerFirst(a, b models.Log) int {
	return cmp.Or(b.Time.Compare(a.Time), bytes.Compare(b.ID[:], a.ID[:]))
}

