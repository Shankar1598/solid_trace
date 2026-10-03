package storage

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
// Per Project, one transaction writes the files under staged names, deletes
// the Logs and drops the Project's demoted columns. Only once it commits are
// the files renamed. Staged files a crash leaves behind are settled first.
func (s *LogStore) ExportLogs(now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := s.recoverStagedFiles(); err != nil {
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
	if _, err := s.db.Exec("CHECKPOINT " + logsCatalog); err != nil {
		logger.L.Warn("Checkpoint of logs DuckDB after the hourly export failed", "error", err)
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

// exportProject exports a Project's Logs before cutoff in one transaction.
func (s *LogStore) exportProject(projectID uint32, cutoff time.Time) error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	var staged []string
	committed, rolledBack := false, false
	defer func() {
		if committed {
			return
		}
		if !rolledBack {
			if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
				logger.L.Error("DuckDB rollback failed", "error", err)
				discardConn(conn)
			}
		}
		for _, file := range staged {
			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				logger.L.Warn("Failed to remove staged log file", "file", file, "error", err)
			}
		}
	}()

	export, err := s.newProjectExport(ctx, conn, projectID)
	if err != nil {
		return err
	}
	hours, err := export.hours(cutoff)
	if err != nil {
		return err
	}
	for _, hour := range hours {
		file, err := export.stageHour(hour)
		if file != "" {
			staged = append(staged, file)
		}
		if err != nil {
			return fmt.Errorf("export hour %s: %w", hour.Format(time.RFC3339), err)
		}
	}
	// DuckDB can't commit a DELETE made before an ALTER of the same table,
	// so the columns go first.
	if err := export.dropDemotedColumns(cutoff); err != nil {
		return err
	}
	if err := export.delete(cutoff); err != nil {
		return err
	}

	// No read begins between the commit and the renames, so none sees the
	// Logs both in a file and in DuckDB, nor in neither.
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		// The commit may have happened anyway, so leave the staged files
		// for the next export's recovery to settle.
		discardConn(conn)
		rolledBack, staged = true, nil
		return err
	}
	committed = true
	for _, file := range staged {
		if err := publishStagedLogFile(file); err != nil {
			// Its Logs are gone from DuckDB, so the next export or startup
			// publishes it.
			return fmt.Errorf("publish %s: %w", file, err)
		}
	}
	return nil
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
// logs_late, to the hour's next file under its staged name, and returns that
// name once the file may exist.
func (e *projectExport) stageHour(hour time.Time) (string, error) {
	set, err := e.daySet(hour)
	if err != nil {
		return "", err
	}
	dir := e.files.HourDir(e.projectID, hour)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	name, err := nextHourFile(dir)
	if err != nil {
		return "", err
	}
	staged := filepath.Join(dir, name+stagedSuffix)

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
		strings.Join(sources, " UNION ALL BY NAME "), sqltext.String(staged)))
	if err != nil {
		return staged, err
	}
	// A committed export's files must survive a crash before their rename.
	return staged, syncFile(staged)
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
const fixedColumns = "id, time, level, message, service, environment, trace_id, span_id"

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

// delete deletes the Project's Logs before cutoff from its hot table and
// logs_late.
func (e *projectExport) delete(cutoff time.Time) error {
	if _, err := e.conn.ExecContext(e.ctx,
		fmt.Sprintf("DELETE FROM %s.%s WHERE project_id = ? AND time < CAST(? AS TIMESTAMP)", logsCatalog, lateTable),
		e.projectID, timestampString(cutoff)); err != nil {
		return fmt.Errorf("delete exported late logs: %w", err)
	}
	if e.hotColumns == nil {
		return nil
	}
	if _, err := e.conn.ExecContext(e.ctx,
		fmt.Sprintf("DELETE FROM %s WHERE time < CAST(? AS TIMESTAMP)", e.hotTable()),
		timestampString(cutoff)); err != nil {
		return fmt.Errorf("delete exported logs: %w", err)
	}
	return nil
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
// when it stopped before renaming it. An export's commit deletes all of its
// file's Logs from DuckDB or none, so one Log decides: still stored means
// the export didn't commit, and the file is deleted; gone means it did, and
// the file is renamed. A file that can't be read is left for an operator, as
// readers never see staged files. Exports never overlap, so this runs only
// when none is in progress.
func (s *LogStore) recoverStagedFiles() error {
	staged, err := s.files.stagedFiles()
	if err != nil {
		return err
	}
	for file, projectID := range staged {
		var id string
		err := s.db.QueryRow(fmt.Sprintf("SELECT CAST(id AS VARCHAR) FROM read_parquet(%s) LIMIT 1", sqltext.String(file))).Scan(&id)
		if err != nil {
			logger.L.Error("Skipping unreadable staged log file", "file", file, "error", err)
			continue
		}
		stored, err := s.logStored(projectID, id)
		if err != nil {
			return err
		}
		if stored {
			logger.L.Info("Deleting staged log file of an export that didn't commit", "file", file)
			err = os.Remove(file)
		} else {
			logger.L.Info("Publishing staged log file of an export that committed", "file", file)
			err = publishStagedLogFile(file)
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
