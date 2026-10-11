package storage

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/pkg/sqltext"
)

// The Log file manifest lists every published Log file in logs.duckdb, so
// that reads, Retention, Project deletion and the disk limit find files
// without listing folders. Only the recovery of staged files lists them.
//
//   - log_files has a row per published file, by its key: Project, UTC day,
//     hour and name. The export inserts it in the transaction that deletes
//     the file's Logs from DuckDB, so a row marks a committed export. The
//     key is unique because an export names a file after every name its hour
//     has in either table (nextHourFile).
//   - log_file_deletions has a tombstone per file whose row Retention,
//     Project deletion or the disk limit deleted while its bytes may still
//     be on disk. Each deleter then deletes the bytes of every tombstoned
//     file no read has pinned, and their tombstones (deleteTombstonedFiles).
//   - log_file_backfill gets one row once the manifest of an install from
//     before it has been built from the files on disk.
var manifestTables = []string{
	`CREATE TABLE IF NOT EXISTS logs.log_files (
		project_id UINTEGER,
		day DATE,
		hour UTINYINT,
		name VARCHAR,
		bytes BIGINT,
		min_time TIMESTAMP,
		max_time TIMESTAMP,
		log_count BIGINT,
		-- The highest Log batch id in the file; NULL until it is recorded.
		batch_max UUID,
		local BOOLEAN,
		remote BOOLEAN
	)`,
	`CREATE TABLE IF NOT EXISTS logs.log_file_deletions (
		project_id UINTEGER,
		day DATE,
		hour UTINYINT,
		name VARCHAR,
		reason VARCHAR,
		deleted_at TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS logs.log_file_backfill (
		done_at TIMESTAMP
	)`,
}

// Why a file's row was deleted, as its tombstone says.
const (
	deletedForRetention    = "retention"
	deletedWithProject     = "project deleted"
	deletedForTheDiskLimit = "disk limit"
)

// A file's key in the manifest's tables: its columns, and the condition that
// matches it, with logFile.keyArgs.
const (
	manifestColumnsOfAKey   = "project_id, day, hour, name"
	manifestConditionOfAKey = "project_id = ? AND day = CAST(? AS DATE) AND hour = ? AND name = ?"
)

// logFile is a Log file as the manifest knows it: a Project's file of one
// UTC hour.
type logFile struct {
	projectID uint32
	// hour is the start of the file's UTC hour.
	hour time.Time
	// name is data.parquet or late-<n>.parquet.
	name string
}

// key is the file's logical key, project=<id>/day=<YYYY-MM-DD>/hour=<HH>/<name>,
// which is also its path under the files folder. Pins and tombstones go by
// it, wherever the file's bytes are.
func (f logFile) key() string {
	return fmt.Sprintf("project=%d/day=%s/hour=%02d/%s", f.projectID, dayString(LogDay(f.hour)), f.hour.UTC().Hour(), f.name)
}

// keyArgs are the arguments of manifestConditionOfAKey.
func (f logFile) keyArgs() []any {
	return []any{f.projectID, dayString(LogDay(f.hour)), f.hour.UTC().Hour(), f.name}
}

// path is where a Log file is on local disk.
func (f LogFiles) path(file logFile) string {
	return filepath.Join(f.Dir, filepath.FromSlash(file.key()))
}

// parsePath reads the Log file at a path under the files folder,
// <Dir>/project=<id>/day=<YYYY-MM-DD>/hour=<HH>/<name>. A staged file's name
// keeps its staged suffix.
func (f LogFiles) parsePath(path string) (logFile, bool) {
	rel, err := filepath.Rel(f.Dir, path)
	if err != nil {
		return logFile{}, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 4 {
		return logFile{}, false
	}
	var projectID uint32
	if _, err := fmt.Sscanf(parts[0], "project=%d", &projectID); err != nil || parts[0] != fmt.Sprintf("project=%d", projectID) {
		return logFile{}, false
	}
	day, err := time.Parse(time.DateOnly, strings.TrimPrefix(parts[1], "day="))
	if err != nil || !strings.HasPrefix(parts[1], "day=") {
		return logFile{}, false
	}
	var h int
	if _, err := fmt.Sscanf(parts[2], "hour=%02d", &h); err != nil || h < 0 || h > 23 || parts[2] != fmt.Sprintf("hour=%02d", h) {
		return logFile{}, false
	}
	return logFile{projectID: projectID, hour: day.Add(time.Duration(h) * time.Hour), name: parts[3]}, true
}

// sortLogFiles sorts files by hour, then in the order each hour's were
// written: data.parquet, then late files by number.
func sortLogFiles(files []logFile) {
	slices.SortFunc(files, func(a, b logFile) int {
		return cmp.Or(
			a.hour.Compare(b.hour),
			cmp.Compare(a.projectID, b.projectID),
			cmp.Compare(lateFileNumber(a.name), lateFileNumber(b.name)),
			cmp.Compare(a.name, b.name),
		)
	})
}

// sqlRunner runs SQL: a *sql.DB, or a *sql.Conn inside its transaction.
type sqlRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanLogFiles reads rows of project_id, day, hour and name.
func scanLogFiles(rows *sql.Rows) ([]logFile, error) {
	defer rows.Close()
	var files []logFile
	for rows.Next() {
		var (
			file logFile
			day  time.Time
			hour int
		)
		if err := rows.Scan(&file.projectID, &day, &hour, &file.name); err != nil {
			return nil, err
		}
		file.hour = day.UTC().Add(time.Duration(hour) * time.Hour)
		files = append(files, file)
	}
	return files, rows.Err()
}

// listLogFiles returns a Project's published files of the hours that start in
// [from, to), by hour, each hour's in the order they were written.
func listLogFiles(ctx context.Context, db sqlRunner, projectID uint32, from, to time.Time) (map[time.Time][]logFile, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+manifestColumnsOfAKey+` FROM logs.log_files
		WHERE project_id = ?
			AND day + to_hours(CAST(hour AS BIGINT)) >= CAST(? AS TIMESTAMP)
			AND day + to_hours(CAST(hour AS BIGINT)) < CAST(? AS TIMESTAMP)`,
		projectID, timestampString(from.UTC().Truncate(time.Hour)), timestampString(to))
	if err != nil {
		return nil, fmt.Errorf("list log files: %w", err)
	}
	files, err := scanLogFiles(rows)
	if err != nil {
		return nil, err
	}
	sortLogFiles(files)
	byHour := make(map[time.Time][]logFile)
	for _, file := range files {
		byHour[file.hour] = append(byHour[file.hour], file)
	}
	return byHour, nil
}

// HourFiles returns the paths of a Project's published Log files of the UTC
// hour of t: data.parquet, if the hour has been exported, then its late files
// in the order they were written.
func (s *LogStore) HourFiles(projectID uint32, t time.Time) ([]string, error) {
	hour := t.UTC().Truncate(time.Hour)
	files, err := listLogFiles(context.Background(), s.db, projectID, hour, hour.Add(time.Hour))
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(files[hour]))
	for i, file := range files[hour] {
		paths[i] = s.files.path(file)
	}
	return paths, nil
}

// nextHourFile is the name the next file of a Project's hour gets:
// data.parquet if the hour has never had a file, else the next late file. A
// tombstoned file's name counts, so its bytes are never mistaken for a new
// file's.
func nextHourFile(ctx context.Context, db sqlRunner, projectID uint32, hour time.Time) (string, error) {
	where := "project_id = ? AND day = CAST(? AS DATE) AND hour = ?"
	args := []any{projectID, dayString(LogDay(hour)), hour.UTC().Hour()}
	rows, err := db.QueryContext(ctx,
		"SELECT name FROM logs.log_files WHERE "+where+" UNION ALL SELECT name FROM logs.log_file_deletions WHERE "+where,
		append(args, args...)...)
	if err != nil {
		return "", fmt.Errorf("list names of the hour's files: %w", err)
	}
	defer rows.Close()
	used, last := false, 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", err
		}
		used, last = true, max(last, lateFileNumber(name))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if !used {
		return hourDataFile, nil
	}
	return fmt.Sprintf("%s%d%s", lateFilePrefix, last+1, parquetSuffix), nil
}

// logFileStats is what the manifest records of a file's contents.
type logFileStats struct {
	bytes            int64
	minTime, maxTime sql.NullTime
	logs             int64
}

// readLogFileStats reads a Log file's size, the times of its first and last
// Logs and how many it holds.
func readLogFileStats(ctx context.Context, db sqlRunner, path string) (logFileStats, error) {
	info, err := os.Stat(path)
	if err != nil {
		return logFileStats{}, err
	}
	stats := logFileStats{bytes: info.Size()}
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT min(time), max(time), count(*) FROM read_parquet(%s)", sqltext.String(path))).
		Scan(&stats.minTime, &stats.maxTime, &stats.logs)
	return stats, err
}

// insertLogFile adds a local file's row to the manifest.
func insertLogFile(ctx context.Context, db sqlRunner, file logFile, stats logFileStats) error {
	timestamp := func(t sql.NullTime) any {
		if !t.Valid {
			return nil
		}
		return timestampString(t.Time)
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO logs.log_files (project_id, day, hour, name, bytes, min_time, max_time, log_count, local, remote)
		VALUES (?, CAST(? AS DATE), ?, ?, ?, CAST(? AS TIMESTAMP), CAST(? AS TIMESTAMP), ?, true, false)`,
		append(file.keyArgs(), stats.bytes, timestamp(stats.minTime), timestamp(stats.maxTime), stats.logs)...)
	if err != nil {
		return fmt.Errorf("add %s to the manifest: %w", file.key(), err)
	}
	return nil
}

// logFileListed reports whether the manifest has a file's row.
func (s *LogStore) logFileListed(file logFile) (bool, error) {
	var listed bool
	err := s.db.QueryRow("SELECT count(*) > 0 FROM logs.log_files WHERE "+manifestConditionOfAKey, file.keyArgs()...).Scan(&listed)
	return listed, err
}

// tombstoneLogFiles deletes the manifest's rows that meet where, inside the
// connection's transaction, and leaves a tombstone for each, so that the
// deleter that commits it deletes their bytes once no read has them pinned.
func tombstoneLogFiles(ctx context.Context, conn *sql.Conn, reason, where string, args ...any) error {
	_, err := conn.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO logs.log_file_deletions
		SELECT %s, ?, CAST(? AS TIMESTAMP) FROM logs.log_files WHERE %s`, manifestColumnsOfAKey, where),
		append([]any{reason, timestampString(time.Now())}, args...)...)
	if err != nil {
		return fmt.Errorf("tombstone log files: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "DELETE FROM logs.log_files WHERE "+where, args...); err != nil {
		return fmt.Errorf("delete log files from the manifest: %w", err)
	}
	return nil
}

// deleteTombstonedFiles deletes the bytes of every tombstoned file that no
// read has pinned, then its tombstone. A pinned one stays for a later pass,
// and is returned. No read pins a tombstoned file anew, as none sees its row
// any more, so this needs no publishMu. The caller holds writeMu.
func (s *LogStore) deleteTombstonedFiles() (pinned []logFile, err error) {
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT "+manifestColumnsOfAKey+" FROM logs.log_file_deletions")
	if err != nil {
		return nil, fmt.Errorf("list tombstoned log files: %w", err)
	}
	files, err := scanLogFiles(rows)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	sortLogFiles(files)

	var deleted []logFile
	for _, file := range files {
		if s.pinned(file.key()) {
			pinned = append(pinned, file)
			continue
		}
		path := s.files.path(file)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, errors.Join(fmt.Errorf("delete %s: %w", path, err), s.dropTombstones(deleted))
		}
		// Each folder goes once it is empty.
		for dir := filepath.Dir(path); dir != filepath.Clean(s.files.Dir); dir = filepath.Dir(dir) {
			if os.Remove(dir) != nil {
				break
			}
		}
		deleted = append(deleted, file)
	}
	return pinned, s.dropTombstones(deleted)
}

// filesInUse is ErrLogFilesInUse, naming the first of files, or nil if there
// are none.
func filesInUse(files []logFile) error {
	if len(files) == 0 {
		return nil
	}
	return fmt.Errorf("delete %s: %w", files[0].key(), ErrLogFilesInUse)
}

// dropTombstones deletes the tombstones of files whose bytes are gone, in
// one transaction.
func (s *LogStore) dropTombstones(files []logFile) error {
	if len(files) == 0 {
		return nil
	}
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	for _, file := range files {
		if _, err := conn.ExecContext(ctx, "DELETE FROM logs.log_file_deletions WHERE "+manifestConditionOfAKey, file.keyArgs()...); err != nil {
			if _, rollbackErr := conn.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
				discardConn(conn)
			}
			return fmt.Errorf("delete tombstone of %s: %w", file.key(), err)
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		discardConn(conn)
		return err
	}
	return nil
}

// backfillManifest builds the manifest of an install from before it, once:
// a row for each published file on disk. Its staged files are settled first,
// by whether their Logs are still in DuckDB, as the manifest has no rows to
// tell. The rows and the marker row commit together, so a crash part-way
// leaves the whole backfill for the next start.
func (s *LogStore) backfillManifest() error {
	var done bool
	if err := s.db.QueryRow("SELECT count(*) > 0 FROM logs.log_file_backfill").Scan(&done); err != nil || done {
		return err
	}
	if err := s.settleStagedFilesByTheirLogs(); err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(s.files.Dir, "project=*", "day=*", "hour=*", "*"+parquetSuffix))
	if err != nil {
		return err
	}
	ctx := context.Background()
	type found struct {
		file  logFile
		stats logFileStats
	}
	var files []found
	for _, path := range paths {
		file, ok := s.files.parsePath(path)
		if !ok {
			continue // not a file the export writes
		}
		stats, err := readLogFileStats(ctx, s.db, path)
		if err != nil {
			// Reads failed on it before; now they leave it out.
			logger.L.Error("Leaving an unreadable Log file out of the manifest", "file", path, "error", err)
			continue
		}
		files = append(files, found{file, stats})
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	insert := func() error {
		for _, f := range files {
			if err := insertLogFile(ctx, conn, f.file, f.stats); err != nil {
				return err
			}
		}
		_, err := conn.ExecContext(ctx, "INSERT INTO logs.log_file_backfill VALUES (CAST(? AS TIMESTAMP))", timestampString(time.Now()))
		return err
	}
	if err := insert(); err != nil {
		if _, rollbackErr := conn.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
			discardConn(conn)
		}
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		discardConn(conn)
		return err
	}
	if len(files) > 0 {
		logger.L.Info("Listed the Log files on disk in the manifest", "files", len(files))
	}
	return nil
}

// settleStagedFilesByTheirLogs settles the staged files of an install from
// before the manifest. An export's commit deleted all of its file's Logs
// from DuckDB or none, so one Log decides: still stored means the export
// didn't commit, and the file is deleted; gone means it did, and the file is
// renamed. A file that can't be read is left for an operator.
func (s *LogStore) settleStagedFilesByTheirLogs() error {
	staged, err := s.files.stagedFiles()
	if err != nil {
		return err
	}
	for path, file := range staged {
		var id string
		err := s.db.QueryRow(fmt.Sprintf("SELECT CAST(id AS VARCHAR) FROM read_parquet(%s) LIMIT 1", sqltext.String(path))).Scan(&id)
		if err != nil {
			logger.L.Error("Skipping unreadable staged log file", "file", path, "error", err)
			continue
		}
		stored, err := s.logStored(file.projectID, id)
		if err != nil {
			return err
		}
		if stored {
			logger.L.Info("Deleting staged log file of an export that didn't commit", "file", path)
			err = os.Remove(path)
		} else {
			logger.L.Info("Publishing staged log file of an export that committed", "file", path)
			err = publishStagedLogFile(path)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
