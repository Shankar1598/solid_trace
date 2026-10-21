package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/pkg/sqltext"
)

// ExportLogs is the hourly export at now: for each Project, it moves every Log
// of the hours before the hot window, h-2 and earlier, out of its hot table
// and logs_late into that hour's files, then checkpoints logs.duckdb.
//
// An hour not yet exported gets data.parquet; an hour already exported gets
// its next late file. Each file is sorted by time, in row groups of 30,720,
// zstd, with the promoted columns of the hour's day, late Logs' values pulled
// out of their maps.
//
// Per Project, a transaction that only reads writes the files under staged
// names, outside writeMu, so that Log processing carries on meanwhile. Then a
// second one, under writeMu, adds the files' rows to the manifest, drops the
// Project's demoted columns and deletes the Logs the first one saw. Late Logs
// stored meanwhile wait for the next export. Only once it commits are the
// files renamed. Staged files a crash leaves behind are settled first.
func (s *LogStore) ExportLogs(now time.Time) error {
	s.exportMu.Lock()
	defer s.exportMu.Unlock()

	s.writeMu.Lock()
	err := s.recoverStagedFiles()
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("recover staged log files: %w", err)
	}
	cutoff, _ := HotWindow(now)
	projects, err := s.projectsToExport(cutoff)
	if err != nil {
		return err
	}
	var failed error
	for _, projectID := range projects {
		if err := s.exportProject(projectID, cutoff); err != nil {
			// The Logs stay where they are, and the next export takes them.
			logger.L.Error("Hourly Log export failed", "project_id", projectID, "error", err)
			if failed == nil {
				failed = fmt.Errorf("export logs of project %d: %w", projectID, err)
			}
		}
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	began := time.Now()
	if _, err := s.db.Exec("CHECKPOINT " + logsCatalog); err != nil {
		logger.L.Warn("Checkpoint of logs DuckDB after the hourly export failed", "error", err)
	} else {
		logger.L.Info("Checkpointed logs DuckDB after the hourly export", "took", time.Since(began).Round(time.Millisecond).String())
	}
	return failed
}

// mapType is the DuckDB type of the map that holds Attributes of this type.
func (t AttributeType) mapType() string {
	if t == JSONAttribute {
		return "MAP(VARCHAR, VARCHAR)"
	}
	return "MAP(VARCHAR, " + t.columnType() + ")"
}

// projectsToExport lists the Projects with a hot table, and those with late
// Logs before cutoff.
func (s *LogStore) projectsToExport(cutoff time.Time) ([]uint32, error) {
	rows, err := s.db.Query(`
		SELECT CAST(substr(table_name, length('logs_hot_') + 1) AS UINTEGER)
		FROM duckdb_tables() WHERE database_name = ? AND schema_name = 'main' AND table_name LIKE 'logs\_hot\_%' ESCAPE '\'
		UNION
		SELECT DISTINCT project_id FROM logs.logs_late WHERE time < CAST(? AS TIMESTAMP)`,
		logsCatalog, timestampString(cutoff))
	if err != nil {
		return nil, fmt.Errorf("list projects to export: %w", err)
	}
	defer rows.Close()
	var projects []uint32
	for rows.Next() {
		var projectID uint32
		if err := rows.Scan(&projectID); err != nil {
			return nil, err
		}
		projects = append(projects, projectID)
	}
	slices.Sort(projects)
	return projects, rows.Err()
}

// errLogsChanged says that the Logs a Project's export staged changed before
// it committed: some went, or some were stored out of batch order.
var errLogsChanged = errors.New("the Logs changed while their files were written")

// exportProject exports a Project's Logs before cutoff: stageExport writes
// their files, and commitExport commits them. If the Logs the files hold
// changed meanwhile, it exports them again holding writeMu throughout, as
// the export did before it wrote files outside writeMu, so that a change
// it can't follow never leaves Logs unexported hour after hour.
func (s *LogStore) exportProject(projectID uint32, cutoff time.Time) error {
	err := s.exportOnce(projectID, cutoff, false)
	if !errors.Is(err, errLogsChanged) {
		return err
	}
	logger.L.Warn("A Project's Logs changed while the hourly export wrote their files; exporting them again with Log processing held up",
		"project_id", projectID, "error", err)
	return s.exportOnce(projectID, cutoff, true)
}

// exportOnce stages a Project's files, then commits them. With locked, it
// holds writeMu throughout, and the commit deletes every Log before cutoff.
func (s *LogStore) exportOnce(projectID uint32, cutoff time.Time, locked bool) error {
	if locked {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
	}
	began := time.Now()
	var staged *stagedExport
	// settled is set once the staged files are renamed, or left for the next
	// export's recovery.
	settled := false
	defer func() {
		if settled || staged == nil {
			return
		}
		for _, file := range staged.files {
			if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
				logger.L.Warn("Failed to remove staged log file", "file", file.path, "error", err)
			}
		}
	}()

	staged, err := s.stageExport(projectID, cutoff)
	if err != nil {
		return err
	}
	staged.locked = locked
	copied := time.Since(began)
	settled, err = s.commitExport(projectID, cutoff, staged)
	if err != nil {
		return err
	}
	if len(staged.files) > 0 {
		logger.L.Info("Exported a Project's Logs", "project_id", projectID, "logs", staged.logs, "files", len(staged.files),
			"copy", copied.Round(time.Millisecond).String(), "commit", (time.Since(began) - copied).Round(time.Millisecond).String())
	}
	return nil
}

// stagedExport is what an export's first transaction wrote: its staged files,
// how many Logs they hold, and the Project's Log processing cursor, which no
// Log in them was stored after. locked says the export holds writeMu
// throughout, so the files hold every Log before the cutoff.
type stagedExport struct {
	files  []stagedFile
	logs   int64
	cursor uuid.UUID
	locked bool
}

// stagedFile is a Log file written under its staged name, with the facts of
// its manifest row.
type stagedFile struct {
	path  string
	file  logFile
	stats logFileStats
}

// stageExport writes the files of a Project's Logs before cutoff under their
// staged names, from one snapshot of logs.duckdb, in a transaction that only
// reads. It returns the files it wrote even when it fails.
func (s *LogStore) stageExport(projectID uint32, cutoff time.Time) (*stagedExport, error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return nil, err
	}
	defer func() {
		if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
			logger.L.Error("DuckDB rollback failed", "error", err)
			discardConn(conn)
		}
	}()

	staged := &stagedExport{}
	// DuckDB fixes the transaction's view of logs.duckdb when it first reads
	// it, so the files hold exactly the Logs stored up to this cursor.
	if staged.cursor, err = logProcessingCursor(ctx, conn, projectID); err != nil {
		return staged, err
	}
	export, err := s.newProjectExport(ctx, conn, projectID)
	if err != nil {
		return staged, err
	}
	hours, err := export.hours(cutoff)
	if err != nil {
		return staged, err
	}
	for _, hour := range hours {
		file, err := export.stageHour(hour)
		if file.path != "" {
			staged.files = append(staged.files, file)
		}
		if err != nil {
			return staged, fmt.Errorf("export hour %s: %w", hour.Format(time.RFC3339), err)
		}
		staged.logs += file.stats.logs.Int64
	}
	if s.staged != nil {
		s.staged(projectID)
	}
	return staged, nil
}

// commitExport adds the staged files' rows to the manifest, drops the
// Project's demoted columns and deletes the Logs the files hold, in one
// transaction under writeMu, then commits it and renames the files. Log
// processing may have stored late Logs of the exported hours since the files
// were written; they come after staged.cursor, so they stay for the next
// export. It fails with errLogsChanged if it would delete other Logs than the
// files hold. settled reports whether the files are no longer the caller's
// to remove: renamed, or left for the next export's recovery.
func (s *LogStore) commitExport(projectID uint32, cutoff time.Time, staged *stagedExport) (settled bool, err error) {
	if !staged.locked {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
	}
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return false, err
	}
	defer func() {
		if settled {
			return
		}
		if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
			logger.L.Error("DuckDB rollback failed", "error", err)
			discardConn(conn)
		}
	}()

	for _, file := range staged.files {
		if err := insertLogFile(ctx, conn, file.file, file.stats); err != nil {
			return false, err
		}
	}
	// Log processing may have added columns since the files were written.
	export, err := s.newProjectExport(ctx, conn, projectID)
	if err != nil {
		return false, err
	}
	// DuckDB can't commit a DELETE made before an ALTER of the same table,
	// so the columns go first.
	if err := export.dropDemotedColumns(cutoff); err != nil {
		return false, err
	}
	upTo := &staged.cursor
	if staged.locked {
		upTo = nil
	}
	deleted, err := export.delete(cutoff, upTo)
	if err != nil {
		return false, err
	}
	// A deletion, such as the Project's, takes Logs out meanwhile; a Log
	// stored out of batch order is in the files but not deleted.
	if deleted != staged.logs {
		return false, fmt.Errorf("%w: the files hold %d Logs, and %d of them were deleted", errLogsChanged, staged.logs, deleted)
	}

	// No read begins between the commit and the renames, so none sees the
	// Logs both in a file and in DuckDB, nor in neither.
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		// The commit may have happened anyway, so leave the staged files
		// for the next export's recovery to settle.
		discardConn(conn)
		return true, err
	}
	for _, file := range staged.files {
		if err := publishStagedLogFile(file.path); err != nil {
			// Its Logs are gone from DuckDB, so the next export or startup
			// publishes it.
			return true, fmt.Errorf("publish %s: %w", file.path, err)
		}
	}
	if len(staged.files) > 0 {
		s.notifyPublished()
	}
	return true, nil
}

// projectExport is one Project's export inside its transaction.
type projectExport struct {
	ctx       context.Context
	conn      *sql.Conn
	files     LogFiles
	projectID uint32
	// hotColumns are the hot table's columns, or nil if it has none.
	hotColumns map[string]bool
}

func (s *LogStore) newProjectExport(ctx context.Context, conn *sql.Conn, projectID uint32) (*projectExport, error) {
	e := &projectExport{ctx: ctx, conn: conn, files: s.files, projectID: projectID}
	var exists bool
	err := conn.QueryRowContext(ctx,
		"SELECT count(*) > 0 FROM duckdb_tables() WHERE database_name = ? AND schema_name = 'main' AND table_name = ?",
		logsCatalog, hotTable(projectID)).Scan(&exists)
	if err != nil || !exists {
		return e, err
	}
	columns, err := tableColumns(ctx, conn, hotTable(projectID))
	if err != nil {
		return nil, err
	}
	e.hotColumns = make(map[string]bool, len(columns))
	for _, column := range columns {
		e.hotColumns[column] = true
	}
	return e, nil
}

func (e *projectExport) hotTable() string {
	return logsCatalog + "." + hotTable(e.projectID)
}

// hours lists the hours of the Project's Logs before cutoff, oldest first.
func (e *projectExport) hours(cutoff time.Time) ([]time.Time, error) {
	sources := []string{fmt.Sprintf("SELECT time FROM %s.%s WHERE project_id = %d AND time < CAST(? AS TIMESTAMP)", logsCatalog, lateTable, e.projectID)}
	args := []any{timestampString(cutoff)}
	if e.hotColumns != nil {
		sources = append(sources, fmt.Sprintf("SELECT time FROM %s WHERE time < CAST(? AS TIMESTAMP)", e.hotTable()))
		args = append(args, timestampString(cutoff))
	}
	rows, err := e.conn.QueryContext(e.ctx, fmt.Sprintf(
		"SELECT DISTINCT date_trunc('hour', time) AS hour FROM (%s) ORDER BY hour",
		strings.Join(sources, " UNION ALL ")), args...)
	if err != nil {
		return nil, fmt.Errorf("list hours to export: %w", err)
	}
	defer rows.Close()
	var hours []time.Time
	for rows.Next() {
		var hour time.Time
		if err := rows.Scan(&hour); err != nil {
			return nil, err
		}
		hours = append(hours, hour.UTC())
	}
	return hours, rows.Err()
}

// stageHour writes the Project's Logs of an hour, from its hot table and
// logs_late, to the hour's next file under its staged name. It returns the
// file once it may exist, with the facts of its manifest row once it is whole.
func (e *projectExport) stageHour(hour time.Time) (stagedFile, error) {
	set, err := e.daySet(hour)
	if err != nil {
		return stagedFile{}, err
	}
	if err := os.MkdirAll(e.files.HourDir(e.projectID, hour), 0755); err != nil {
		return stagedFile{}, err
	}
	name, err := nextHourFile(e.ctx, e.conn, e.projectID, hour)
	if err != nil {
		return stagedFile{}, err
	}
	file := logFile{projectID: e.projectID, hour: hour, name: name}
	staged := stagedFile{path: e.files.path(file) + stagedSuffix, file: file}

	from, to := timestampString(hour), timestampString(hour.Add(time.Hour))
	sources := []string{e.lateRows(set, from, to)}
	if e.hotColumns != nil {
		sources = append(sources, e.hotRows(set, from, to))
	}
	_, err = e.conn.ExecContext(e.ctx, fmt.Sprintf(`
		COPY (
			SELECT * FROM (%s)
			ORDER BY time, id
		) TO %s (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE 30720)`,
		strings.Join(sources, " UNION ALL BY NAME "), sqltext.String(staged.path)))
	if err != nil {
		return staged, err
	}
	// A committed export's files must survive a crash before their rename.
	if err := syncFile(staged.path); err != nil {
		return staged, err
	}
	if staged.stats, err = readLogFileStats(e.ctx, e.conn, staged.path, true); err != nil {
		return staged, fmt.Errorf("read the staged file: %w", err)
	}
	return staged, nil
}

// daySet reads the promoted set of the day of t, by type then key.
func (e *projectExport) daySet(t time.Time) ([]typedKey, error) {
	rows, err := e.conn.QueryContext(e.ctx,
		"SELECT type, key FROM logs.log_promoted_sets WHERE project_id = ? AND day = CAST(? AS DATE)",
		e.projectID, dayString(LogDay(t)))
	if err != nil {
		return nil, fmt.Errorf("read promoted set: %w", err)
	}
	defer rows.Close()
	var keys []typedKey
	for rows.Next() {
		var key typedKey
		if err := rows.Scan(&key.Type, &key.Key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, typedKey.compare)
	return keys, rows.Err()
}

// fixedColumns are the fixed columns but the maps, to select as they are.
const fixedColumns = "id, time, level, message, service, environment, trace_id, span_id, batch"

// hotRows selects the hot table's Logs of [from, to) with the promoted
// columns of set. Their values are in those columns already.
func (e *projectExport) hotRows(set []typedKey, from, to string) string {
	columns := []string{fixedColumns, "attrs_string, attrs_number, attrs_bool, attrs_json"}
	for _, key := range set {
		column := sqltext.Identifier(key.column())
		if !e.hotColumns[key.column()] {
			column = fmt.Sprintf("CAST(NULL AS %s)", key.Type.columnType())
		}
		columns = append(columns, column+" AS "+sqltext.Identifier(key.column()))
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE time >= CAST(%s AS TIMESTAMP) AND time < CAST(%s AS TIMESTAMP)",
		strings.Join(columns, ", "), e.hotTable(), sqltext.String(from), sqltext.String(to))
}

// lateRows selects the Project's late Logs of [from, to) with the promoted
// columns of set, each value pulled out of its map into its column.
func (e *projectExport) lateRows(set []typedKey, from, to string) string {
	columns := []string{fixedColumns}
	for _, t := range []AttributeType{StringAttribute, NumberAttribute, BoolAttribute, JSONAttribute} {
		var keys []string
		for _, key := range set {
			if key.Type == t {
				keys = append(keys, sqltext.String(key.Key))
			}
		}
		if len(keys) == 0 {
			columns = append(columns, t.Map())
			continue
		}
		columns = append(columns, fmt.Sprintf(
			"CAST(map_from_entries(list_filter(map_entries(%[1]s), lambda e: NOT list_contains([%[2]s], e.key))) AS %[3]s) AS %[1]s",
			t.Map(), strings.Join(keys, ", "), t.mapType()))
	}
	for _, key := range set {
		columns = append(columns, fmt.Sprintf("CAST(map_extract_value(%s, %s) AS %s) AS %s",
			key.Type.Map(), sqltext.String(key.Key), key.Type.columnType(), sqltext.Identifier(key.column())))
	}
	return fmt.Sprintf("SELECT %s FROM %s.%s WHERE project_id = %d AND time >= CAST(%s AS TIMESTAMP) AND time < CAST(%s AS TIMESTAMP)",
		strings.Join(columns, ", "), logsCatalog, lateTable, e.projectID, sqltext.String(from), sqltext.String(to))
}

// delete deletes the Project's Logs before cutoff that were stored no later
// than the batch upTo from its hot table and logs_late, and returns how many
// it deleted. A Log stored before the batch column has no batch, and goes
// too. A nil upTo deletes every Log before cutoff.
func (e *projectExport) delete(cutoff time.Time, upTo *uuid.UUID) (int64, error) {
	stored := "true"
	if upTo != nil {
		stored = fmt.Sprintf("(batch IS NULL OR batch <= CAST(%s AS UUID))", sqltext.String(upTo.String()))
	}
	result, err := e.conn.ExecContext(e.ctx,
		fmt.Sprintf("DELETE FROM %s.%s WHERE project_id = ? AND time < CAST(? AS TIMESTAMP) AND %s", logsCatalog, lateTable, stored),
		e.projectID, timestampString(cutoff))
	if err != nil {
		return 0, fmt.Errorf("delete exported late logs: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil || e.hotColumns == nil {
		return deleted, err
	}
	result, err = e.conn.ExecContext(e.ctx,
		fmt.Sprintf("DELETE FROM %s WHERE time < CAST(? AS TIMESTAMP) AND %s", e.hotTable(), stored),
		timestampString(cutoff))
	if err != nil {
		return 0, fmt.Errorf("delete exported logs: %w", err)
	}
	hot, err := result.RowsAffected()
	return deleted + hot, err
}

// dropDemotedColumns drops each promoted column of the Project's hot table
// that the sets of the days it keeps after cutoff don't have, nor the sets of the
// days of the hot window from cutoff. Those are the keys the day's set
// demoted, so the drop happens at the export that deletes the previous day's
// last hour, 01:00 UTC. Log processing adds a column back if a set needs it.
func (e *projectExport) dropDemotedColumns(cutoff time.Time) error {
	if e.hotColumns == nil {
		return nil
	}
	kept := make(map[string]bool)
	keep := func(day time.Time) error {
		set, err := e.daySet(day)
		for _, key := range set {
			kept[key.column()] = true
		}
		return err
	}
	if err := keep(cutoff); err != nil {
		return err
	}
	if err := keep(cutoff.Add(2*time.Hour - time.Nanosecond)); err != nil {
		return err
	}
	demoted := func() []string {
		var columns []string
		for _, column := range slices.Sorted(maps.Keys(e.hotColumns)) {
			if _, promoted := promotedColumnKey(column); promoted && !kept[column] {
				columns = append(columns, column)
			}
		}
		return columns
	}
	if len(demoted()) == 0 {
		return nil
	}

	// Only near midnight can the table keep another day's Logs.
	rows, err := e.conn.QueryContext(e.ctx, fmt.Sprintf(
		"SELECT DISTINCT CAST(time AS DATE) FROM %s WHERE time >= CAST(? AS TIMESTAMP)", e.hotTable()),
		timestampString(cutoff))
	if err != nil {
		return fmt.Errorf("list days in hot table: %w", err)
	}
	var days []time.Time
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			rows.Close()
			return err
		}
		days = append(days, day.UTC())
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, day := range days {
		if err := keep(day); err != nil {
			return err
		}
	}
	for _, column := range demoted() {
		if _, err := e.conn.ExecContext(e.ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", e.hotTable(), sqltext.Identifier(column))); err != nil {
			return fmt.Errorf("drop demoted column %s: %w", column, err)
		}
		logger.L.Debug("Dropping demoted Log column", "project_id", e.projectID, "column", column)
	}
	return nil
}

// publishStagedLogFile renames a staged Log file to its final name.
func publishStagedLogFile(file string) error {
	return os.Rename(file, strings.TrimSuffix(file, stagedSuffix))
}

// recoverStagedFiles settles every staged Log file an export left behind
// when it stopped before renaming it. The export inserted the file's row in
// the manifest in its transaction, so the row decides: there means the
// export committed, and the file is renamed; missing means it didn't, and
// the file is deleted. Exports never overlap, so this runs only when none is
// in progress.
func (s *LogStore) recoverStagedFiles() error {
	staged, err := s.files.stagedFiles()
	if err != nil {
		return err
	}
	for path, file := range staged {
		listed, err := s.logFileListed(file)
		if err != nil {
			return err
		}
		if listed {
			logger.L.Info("Publishing staged log file of an export that committed", "file", path)
			err = publishStagedLogFile(path)
		} else {
			logger.L.Info("Deleting staged log file of an export that didn't commit", "file", path)
			err = os.Remove(path)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// logStored reports whether a Log is in a Project's hot table or logs_late.
func (s *LogStore) logStored(projectID uint32, id string) (bool, error) {
	sources := []string{fmt.Sprintf("SELECT 1 FROM %s.%s WHERE project_id = %d AND id = CAST(? AS UUID)", logsCatalog, lateTable, projectID)}
	args := []any{id}
	hot, err := s.tableExists(hotTable(projectID))
	if err != nil {
		return false, err
	}
	if hot {
		sources = append(sources, fmt.Sprintf("SELECT 1 FROM %s.%s WHERE id = CAST(? AS UUID)", logsCatalog, hotTable(projectID)))
		args = append(args, id)
	}
	var stored bool
	err = s.db.QueryRow(fmt.Sprintf("SELECT EXISTS (%s)", strings.Join(sources, " UNION ALL ")), args...).Scan(&stored)
	return stored, err
}

// timestampString is t as a DuckDB TIMESTAMP literal in UTC, so no time zone
// conversion applies.
func timestampString(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.999999")
}
