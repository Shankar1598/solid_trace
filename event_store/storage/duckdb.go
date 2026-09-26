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

func (w *DuckDBWriter) WriteBatch(events []models.Event) error {
	conn, err := w.db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()

	return conn.Raw(func(c interface{}) error {
		dConn, ok := c.(driver.Conn)
		if !ok {
			return fmt.Errorf("not a driver connection")
		}

		appender, err := duckdb.NewAppenderFromConn(dConn, "", "events_hot")
		if err != nil {
			return err
		}
		defer appender.Close()

		for _, event := range events {
			tags := make(duckdb.Map)
			for k, v := range event.Tags {
				tags[k] = v
			}

			// Log event (optional, keeping existing behavior of logging but maybe lighter)
			// Keeping it simple as previous code logged the full JSON.
			// Replicating logic without JSON marshal overhead for the insert itself.
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
				return err
			}
		}

		return appender.Flush()
	})
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
		var tagsMap duckdb.Map
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

		e.Tags = make(map[string]string, len(tagsMap))
		for k, v := range tagsMap {
			if keyStr, ok := k.(string); ok {
				if valStr, ok := v.(string); ok {
					e.Tags[keyStr] = valStr
				}
			}
		}
		events = append(events, e)
	}
	return events, nil
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
		var tagsMap duckdb.Map
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

		ctx.Event.Tags = make(map[string]string, len(tagsMap))
		for k, v := range tagsMap {
			if keyStr, ok := k.(string); ok {
				if valStr, ok := v.(string); ok {
					ctx.Event.Tags[keyStr] = valStr
				}
			}
		}

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

// TODO: Need to refactor this completely. AI slop.
// ArchiveEventsForDate moves events for a specific date from events_hot to Parquet.
// The date parameter specifies which day's events to archive.
func (w *DuckDBWriter) ArchiveEventsForDate(date time.Time) error {
	dateStr := date.Format("2006-01-02")
	partitionPath := filepath.Join(w.parquetPath, fmt.Sprintf("event_date=%s", dateStr))

	// Ensure partition directory exists
	if err := os.MkdirAll(partitionPath, 0755); err != nil {
		return fmt.Errorf("failed to create partition directory: %w", err)
	}

	parquetFile := filepath.Join(partitionPath, "data.parquet")

	// Use a transaction for atomicity
	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Step 1: Create temp table with data for the specific date
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TEMP TABLE move_batch AS
		SELECT uuid, project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags
		FROM events_hot
		WHERE CAST(timestamp AS DATE) = '%s'
	`, dateStr))
	if err != nil {
		return fmt.Errorf("failed to create temp table: %w", err)
	}

	// Step 2: Check if there's any data to move
	var count int
	err = tx.QueryRow("SELECT COUNT(*) FROM move_batch").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count batch: %w", err)
	}

	if count == 0 {
		// Nothing to archive
		_, _ = tx.Exec("DROP TABLE IF EXISTS move_batch")
		return nil
	}

	// Step 3: Export to Parquet
	_, err = tx.Exec(fmt.Sprintf(`
		COPY move_batch
		TO '%s'
		(FORMAT PARQUET, COMPRESSION 'ZSTD')
	`, parquetFile))
	if err != nil {
		return fmt.Errorf("failed to export to parquet: %w", err)
	}

	// Step 4: Delete from hot table
	_, err = tx.Exec(fmt.Sprintf(`
		DELETE FROM events_hot
		WHERE CAST(timestamp AS DATE) = '%s'
	`, dateStr))
	if err != nil {
		return fmt.Errorf("failed to delete from hot table: %w", err)
	}

	// Step 5: Drop temp table
	_, err = tx.Exec("DROP TABLE IF EXISTS move_batch")
	if err != nil {
		return fmt.Errorf("failed to drop temp table: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Recreate view to include new parquet files
	if err := w.recreateEventsView(); err != nil {
		return fmt.Errorf("failed to recreate events view: %w", err)
	}

	return nil
}

// GetParquetPath returns the parquet storage path
func (w *DuckDBWriter) GetParquetPath() string {
	return w.parquetPath
}
