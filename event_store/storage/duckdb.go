package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

type DuckDBWriter struct {
	db          *sql.DB
	parquetPath string
}

type QueryParams struct {
	ProjectID           uint32
	IssueFingerprintIDs []int64
	UUID                string
	Tags                map[string]string
	NewerThan           time.Time
	OlderThan           time.Time
	Limit               int
	Offset              int
	SortDesc            bool
}

func NewDuckDBWriter(dbPath, parquetPath, tempDir, memoryLimit string) (*DuckDBWriter, error) {
	// Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(parquetPath, 0755); err != nil {
		return nil, err
	}
	if tempDir != "" {
		if err := os.MkdirAll(tempDir, 0755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, err
	}

	if tempDir != "" {
		_, err = db.Exec(fmt.Sprintf("SET temp_directory='%s'", tempDir))
		if err != nil {
			return nil, err
		}
	}

	if memoryLimit != "" {
		_, err = db.Exec(fmt.Sprintf("SET memory_limit='%s'", memoryLimit))
		if err != nil {
			return nil, err
		}
	}

	// Create hot table for recent events
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS events_hot (
            uuid VARCHAR,
            project_id BIGINT,
            issue_fingerprint_id BIGINT,
            timestamp TIMESTAMP,
            environment VARCHAR,
            server_name VARCHAR,
            release VARCHAR,
            level VARCHAR,
            tags MAP(VARCHAR, VARCHAR)
        )
    `)
	if err != nil {
		return nil, err
	}

	writer := &DuckDBWriter{db: db, parquetPath: parquetPath}

	if err := writer.recoverStagedFiles(); err != nil {
		return nil, fmt.Errorf("failed to recover staged archive files: %w", err)
	}

	// Create the unified view
	if err := writer.recreateEventsView(); err != nil {
		return nil, err
	}

	return writer, nil
}

// recreateEventsView rebuilds the events view to include any new parquet files.
// This must be called after archiving to pick up newly created parquet files.
func (w *DuckDBWriter) recreateEventsView() error {
	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DROP VIEW IF EXISTS events`)
	if err != nil {
		return err
	}

	parquetGlob := strings.ReplaceAll(filepath.Join(w.parquetPath, "*", "*.parquet"), "\\", "/")

	// Check if any parquet files exist
	var fileCount int
	err = tx.QueryRow(fmt.Sprintf("SELECT count(*) FROM glob('%s')", parquetGlob)).Scan(&fileCount)
	if err != nil {
		fileCount = 0
	}

	if fileCount > 0 {
		// Create view with hot + parquet union
		_, err = tx.Exec(fmt.Sprintf(`
            CREATE VIEW events AS
            SELECT uuid, project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags
            FROM events_hot
            UNION ALL
            SELECT uuid, project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags
            FROM read_parquet('%s', hive_partitioning = true, union_by_name = true)
        `, parquetGlob))
	} else {
		// No parquet files yet, just use hot table
		_, err = tx.Exec(`
            CREATE VIEW events AS
            SELECT uuid, project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags
            FROM events_hot
        `)
	}

	if err != nil {
		return err
	}

	return tx.Commit()
}

// WriteBatch appends the events to events_hot in one transaction: either
// every row commits or none does.
func (w *DuckDBWriter) WriteBatch(events []models.Event) error {
	ctx := context.Background()
	conn, err := w.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	if err := appendEvents(conn, events); err != nil {
		if _, rollbackErr := conn.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
			logger.L.Error("DuckDB rollback failed", "error", rollbackErr)
			discardConn(conn)
		}
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		discardConn(conn)
		return err
	}
	return nil
}

// discardConn keeps a connection whose transaction may still be open out of
// the pool: returning driver.ErrBadConn from Raw makes database/sql close it.
func discardConn(conn *sql.Conn) {
	_ = conn.Raw(func(interface{}) error { return driver.ErrBadConn })
}

// appendEvents appends the events inside the connection's open transaction.
func appendEvents(conn *sql.Conn, events []models.Event) error {
	return conn.Raw(func(c interface{}) error {
		dConn, ok := c.(driver.Conn)
		if !ok {
			return fmt.Errorf("not a driver connection")
		}

		appender, err := duckdb.NewAppenderFromConn(dConn, "", "events_hot")
		if err != nil {
			return err
		}

		for _, event := range events {
			var tags duckdb.OrderedMap
			for k, v := range event.Tags {
				tags.Set(k, v)
			}

			eventJSON, _ := json.Marshal(event)
			logger.L.Debug("DuckDB insert event", "event", string(eventJSON))

			err := appender.AppendRow(
				event.EventUUID,
				event.ProjectID,
				event.IssueFingerprintID,
				event.Timestamp,
				event.Environment,
				event.ServerName,
				event.Release,
				event.Level,
				tags,
			)
			if err != nil {
				appender.Close()
				return err
			}
		}

		return appender.Close()
	})
}

// NewestHotUUIDAfter returns the newest UUID in events_hot for a Project that
// comes after afterUUID, or "" if there is none. Rows are appended a batch per
// transaction in UUID order, so it shows how far a Project's appends got.
func (w *DuckDBWriter) NewestHotUUIDAfter(projectID uint32, afterUUID string) (string, error) {
	var newest sql.NullString
	err := w.db.QueryRow("SELECT max(uuid) FROM events_hot WHERE project_id = ? AND uuid > ?", projectID, afterUUID).Scan(&newest)
	return newest.String, err
}

// HotUUIDsBetween returns the UUIDs in events_hot for a Project from first to
// last, inclusive.
func (w *DuckDBWriter) HotUUIDsBetween(projectID uint32, first, last string) (map[string]bool, error) {
	rows, err := w.db.Query("SELECT uuid FROM events_hot WHERE project_id = ? AND uuid BETWEEN ? AND ?", projectID, first, last)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	uuids := make(map[string]bool)
	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			return nil, err
		}
		uuids[uuid] = true
	}
	return uuids, rows.Err()
}

func (w *DuckDBWriter) QueryEvents(params QueryParams) ([]models.Event, error) {
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString("SELECT uuid, project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags FROM events WHERE project_id = ?")
	args := []interface{}{params.ProjectID}

	if params.UUID != "" {
		queryBuilder.WriteString(" AND uuid = ?")
		args = append(args, params.UUID)
	}

	if len(params.IssueFingerprintIDs) > 0 {
		placeholders := make([]string, len(params.IssueFingerprintIDs))
		for i, id := range params.IssueFingerprintIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		queryBuilder.WriteString(fmt.Sprintf(" AND issue_fingerprint_id IN (%s)", strings.Join(placeholders, ",")))
	}

	for k, v := range params.Tags {
		queryBuilder.WriteString(" AND tags[?] = ?")
		args = append(args, k, v)
	}

	if !params.NewerThan.IsZero() {
		queryBuilder.WriteString(" AND timestamp > ?")
		args = append(args, params.NewerThan)
	}

	if !params.OlderThan.IsZero() {
		queryBuilder.WriteString(" AND timestamp < ?")
		args = append(args, params.OlderThan)
	}

	if params.SortDesc {
		queryBuilder.WriteString(" ORDER BY timestamp DESC")
	} else {
		queryBuilder.WriteString(" ORDER BY timestamp ASC")
	}

	if params.Limit > 0 {
		queryBuilder.WriteString(" LIMIT ?")
		args = append(args, params.Limit)
	}

	if params.Offset > 0 {
		queryBuilder.WriteString(" OFFSET ?")
		args = append(args, params.Offset)
	}

	rows, err := w.db.Query(queryBuilder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []models.Event{}
	for rows.Next() {
		var e models.Event
		// Note: We don't have IssueID (stored in SQLite) or RawJSON (stored in Pebble)
		// We only populate what we have in DuckDB
		var tagsMap duckdb.OrderedMap
		err := rows.Scan(
			&e.EventUUID,
			&e.ProjectID,
			&e.IssueFingerprintID,
			&e.Timestamp,
			&e.Environment,
			&e.ServerName,
			&e.Release,
			&e.Level,
			&tagsMap,
		)
		if err != nil {
			return nil, err
		}

		e.Tags = tagsFromMap(tagsMap)
		events = append(events, e)
	}
	return events, nil
}

// tagsFromMap converts a scanned MAP(VARCHAR, VARCHAR) into Go tags.
func tagsFromMap(m duckdb.OrderedMap) map[string]string {
	tags := make(map[string]string, m.Len())
	vals := m.Values()
	for i, k := range m.Keys() {
		if keyStr, ok := k.(string); ok {
			if valStr, ok := vals[i].(string); ok {
				tags[keyStr] = valStr
			}
		}
	}
	return tags
}

func (w *DuckDBWriter) CountEvents(params QueryParams) (int64, error) {
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString("SELECT COUNT(*) FROM events WHERE project_id = ?")
	args := []interface{}{params.ProjectID}

	if len(params.IssueFingerprintIDs) > 0 {
		placeholders := make([]string, len(params.IssueFingerprintIDs))
		for i, id := range params.IssueFingerprintIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		queryBuilder.WriteString(fmt.Sprintf(" AND issue_fingerprint_id IN (%s)", strings.Join(placeholders, ",")))
	}

	for k, v := range params.Tags {
		queryBuilder.WriteString(" AND tags[?] = ?")
		args = append(args, k, v)
	}

	var count int64
	err := w.db.QueryRow(queryBuilder.String(), args...).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// EventWithContext holds an event with its previous and next UUIDs
type EventWithContext struct {
	Event    models.Event
	PrevUUID string
	NextUUID string
}

// QueryEventWithContext returns an event with its prev/next UUIDs using window functions.
// If uuid is provided, returns that specific event with context.
// If uuid is empty, returns the latest event(s) based on limit with context.
func (w *DuckDBWriter) QueryEventWithContext(params QueryParams) ([]EventWithContext, error) {
	// Build the base CTE with window functions
	queryBuilder := strings.Builder{}
	args := []interface{}{}

	// CTE to get events with prev/next using window functions
	queryBuilder.WriteString(`
		WITH ordered_events AS (
			SELECT
				uuid,
				project_id,
				issue_fingerprint_id,
				timestamp,
				environment,
				server_name,
				release,
				level,
				tags,
				LAG(uuid) OVER (ORDER BY timestamp ASC) as prev_uuid,
				LEAD(uuid) OVER (ORDER BY timestamp ASC) as next_uuid
			FROM events
			WHERE project_id = ?
	`)
	args = append(args, params.ProjectID)

	if len(params.IssueFingerprintIDs) > 0 {
		placeholders := make([]string, len(params.IssueFingerprintIDs))
		for i, id := range params.IssueFingerprintIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		queryBuilder.WriteString(fmt.Sprintf(" AND issue_fingerprint_id IN (%s)", strings.Join(placeholders, ",")))
	}

	queryBuilder.WriteString(")")

	// Select from CTE
	queryBuilder.WriteString(" SELECT uuid, project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags, prev_uuid, next_uuid FROM ordered_events")

	if params.UUID != "" {
		// Get specific event by UUID
		queryBuilder.WriteString(" WHERE uuid = ?")
		args = append(args, params.UUID)
	} else {
		// Get latest events
		if params.SortDesc {
			queryBuilder.WriteString(" ORDER BY timestamp DESC")
		} else {
			queryBuilder.WriteString(" ORDER BY timestamp ASC")
		}
		if params.Limit > 0 {
			queryBuilder.WriteString(" LIMIT ?")
			args = append(args, params.Limit)
		}
	}

	rows, err := w.db.Query(queryBuilder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []EventWithContext{}
	for rows.Next() {
		var ctx EventWithContext
		var tagsMap duckdb.OrderedMap
		var prevUUID, nextUUID sql.NullString

		err := rows.Scan(
			&ctx.Event.EventUUID,
			&ctx.Event.ProjectID,
			&ctx.Event.IssueFingerprintID,
			&ctx.Event.Timestamp,
			&ctx.Event.Environment,
			&ctx.Event.ServerName,
			&ctx.Event.Release,
			&ctx.Event.Level,
			&tagsMap,
			&prevUUID,
			&nextUUID,
		)
		if err != nil {
			return nil, err
		}

		ctx.Event.Tags = tagsFromMap(tagsMap)

		if prevUUID.Valid {
			ctx.PrevUUID = prevUUID.String
		}
		if nextUUID.Valid {
			ctx.NextUUID = nextUUID.String
		}

		results = append(results, ctx)
	}
	return results, nil
}

func (w *DuckDBWriter) Close() {
	w.db.Close()
}

// Ping checks if DuckDB is healthy by running a simple query
func (w *DuckDBWriter) Ping() error {
	var result int
	return w.db.QueryRow("SELECT 1").Scan(&result)
}

// archiveRunIDFormat names archive files so they sort by run: fixed width, UTC.
const archiveRunIDFormat = "20060102T150405.000000000Z"

// stagingSuffix marks an archive file whose run may not have committed yet.
// The events view reads only *.parquet, so staged files are never queried.
const stagingSuffix = ".tmp"

// ArchiveEventsUpTo moves every row in events_hot dated on or before cutoff
// into Parquet. Each date gets a new file event_date=<date>/data-<run id>.parquet,
// so a date archived again keeps its earlier files.
//
// Files are written under a .tmp name, the rows are deleted in one transaction,
// and only then are the files renamed. recoverStagedFiles settles any .tmp file
// a failed run leaves behind.
func (w *DuckDBWriter) ArchiveEventsUpTo(cutoff time.Time) error {
	if err := w.recoverStagedFiles(); err != nil {
		return fmt.Errorf("failed to recover staged archive files: %w", err)
	}

	runID := time.Now().UTC().Format(archiveRunIDFormat)
	cutoffStr := cutoff.Format("2006-01-02")

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TEMP TABLE move_batch AS
		SELECT uuid, project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags
		FROM events_hot
		WHERE CAST(timestamp AS DATE) <= '%s'
	`, cutoffStr))
	if err != nil {
		return fmt.Errorf("failed to create temp table: %w", err)
	}

	dates, err := moveBatchDates(tx)
	if err != nil {
		return err
	}
	if len(dates) == 0 {
		// Recovery may have published files, so the view still needs rebuilding.
		if err := w.recreateEventsView(); err != nil {
			return fmt.Errorf("failed to recreate events view: %w", err)
		}
		return nil
	}

	var staged []string
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, f := range staged {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				logger.L.Warn("Failed to remove staged archive file", "file", f, "error", err)
			}
		}
	}()

	for _, date := range dates {
		partitionPath := filepath.Join(w.parquetPath, "event_date="+date)
		if err := os.MkdirAll(partitionPath, 0755); err != nil {
			return fmt.Errorf("failed to create partition directory: %w", err)
		}
		stagedFile := filepath.Join(partitionPath, "data-"+runID+".parquet"+stagingSuffix)
		staged = append(staged, stagedFile)
		_, err = tx.Exec(fmt.Sprintf(`
			COPY (SELECT * FROM move_batch WHERE CAST(timestamp AS DATE) = '%s')
			TO '%s'
			(FORMAT PARQUET, COMPRESSION 'ZSTD')
		`, date, stagedFile))
		if err != nil {
			return fmt.Errorf("failed to export %s to parquet: %w", date, err)
		}
	}

	// Same predicate as move_batch: the transaction's snapshot sees the same rows.
	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM events_hot
		WHERE CAST(timestamp AS DATE) <= '%s'
	`, cutoffStr))
	if err != nil {
		return fmt.Errorf("failed to delete from hot table: %w", err)
	}
	if _, err := tx.Exec("DROP TABLE move_batch"); err != nil {
		return fmt.Errorf("failed to drop temp table: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	for _, f := range staged {
		if err := publishStagedFile(f); err != nil {
			return fmt.Errorf("failed to publish archive file: %w", err)
		}
	}

	if err := w.recreateEventsView(); err != nil {
		return fmt.Errorf("failed to recreate events view: %w", err)
	}
	return nil
}

// publishStagedFile renames a staged archive file to its final .parquet name.
func publishStagedFile(f string) error {
	return os.Rename(f, strings.TrimSuffix(f, stagingSuffix))
}

// moveBatchDates lists the distinct dates in move_batch, oldest first.
func moveBatchDates(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`SELECT DISTINCT strftime(CAST(timestamp AS DATE), '%Y-%m-%d') AS d FROM move_batch ORDER BY d`)
	if err != nil {
		return nil, fmt.Errorf("failed to list archive dates: %w", err)
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		dates = append(dates, d)
	}
	return dates, rows.Err()
}

// recoverStagedFiles settles every .tmp archive file left by a run that has
// ended. A run's commit removes all of its rows from events_hot or none, so one
// uuid decides: still in events_hot means the commit didn't happen and the file
// is deleted; gone means it did and the file is renamed to its final name.
// Runs never overlap, so this must only be called when no run is in progress.
func (w *DuckDBWriter) recoverStagedFiles() error {
	staged, err := filepath.Glob(filepath.Join(w.parquetPath, "*", "*.parquet"+stagingSuffix))
	if err != nil {
		return err
	}

	for _, f := range staged {
		var uuid string
		err := w.db.QueryRow(fmt.Sprintf("SELECT uuid FROM read_parquet('%s') LIMIT 1", f)).Scan(&uuid)
		if err != nil {
			// Can't tell whether its run committed. Leave it for an operator: the
			// view never reads .tmp files, so it can't cause duplicates meanwhile.
			logger.L.Error("Skipping unreadable staged archive file", "file", f, "error", err)
			continue
		}

		var stillHot bool
		if err := w.db.QueryRow("SELECT EXISTS (SELECT 1 FROM events_hot WHERE uuid = ?)", uuid).Scan(&stillHot); err != nil {
			return err
		}
		if stillHot {
			logger.L.Info("Deleting staged archive file from uncommitted run", "file", f)
			err = os.Remove(f)
		} else {
			logger.L.Info("Publishing staged archive file from committed run", "file", f)
			err = publishStagedFile(f)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// GetParquetPath returns the parquet storage path
func (w *DuckDBWriter) GetParquetPath() string {
	return w.parquetPath
}
