package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

// logsCatalog is the name logs.duckdb is attached under.
const logsCatalog = "logs"

// LogStore is logs.duckdb: a hot table per Project, and each Project's Log
// processing cursor. It is attached to the DuckDB instance of events.duckdb,
// so the two share one memory limit, but it has its own WAL and checkpoints.
//
// A DuckDB transaction can write only one attached database, so everything
// Log processing commits together lives here.
type LogStore struct {
	db *sql.DB
}

// NewLogStore attaches the logs.duckdb at path to the DuckDB instance of
// events, creating the file if it doesn't exist.
func NewLogStore(events *DuckDBWriter, path string) (*LogStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	// An ATTACH on one pooled connection is seen by every connection.
	if _, err := events.db.Exec(fmt.Sprintf("ATTACH IF NOT EXISTS %s AS %s", sqlString(path), logsCatalog)); err != nil {
		return nil, fmt.Errorf("attach %s: %w", path, err)
	}
	if _, err := events.db.Exec(`
		CREATE TABLE IF NOT EXISTS logs.log_processing_cursors (
			project_id UINTEGER,
			batch_id UUID
		)
	`); err != nil {
		return nil, err
	}
	return &LogStore{db: events.db}, nil
}

// hotTable is the name of a Project's hot table in logs.duckdb.
func hotTable(projectID uint32) string {
	return fmt.Sprintf("logs_hot_%d", projectID)
}

// hotTableColumns are a hot table's columns: the id, the seven built-in fields
// and one map per Attribute type. attrs_json holds JSON text.
const hotTableColumns = `
	id UUID,
	time TIMESTAMP,
	level VARCHAR,
	message VARCHAR,
	service VARCHAR,
	environment VARCHAR,
	trace_id VARCHAR,
	span_id VARCHAR,
	attrs_string MAP(VARCHAR, VARCHAR),
	attrs_number MAP(VARCHAR, DOUBLE),
	attrs_bool MAP(VARCHAR, BOOLEAN),
	attrs_json MAP(VARCHAR, VARCHAR)
`

// LogProcessingCursor returns the id of the last Log batch that Log processing
// stored for a Project, or uuid.Nil if it has stored none.
func (s *LogStore) LogProcessingCursor(projectID uint32) (uuid.UUID, error) {
	var batchID []byte
	err := s.db.QueryRow("SELECT batch_id FROM logs.log_processing_cursors WHERE project_id = ?", projectID).Scan(&batchID)
	if err == sql.ErrNoRows {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.FromBytes(batchID)
}

// AppendLogBatches appends the Logs of a Project's Log batches to its hot
// table, creating the table with its first Log, and moves the Project's Log
// processing cursor to the last batch, all in one transaction. Either the
// Logs and the cursor both commit or neither does, so a batch at or below the
// cursor is never appended twice.
func (s *LogStore) AppendLogBatches(projectID uint32, batches []LogBatch) error {
	if len(batches) == 0 {
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
	if err := appendLogBatches(ctx, conn, projectID, batches); err != nil {
		// After a failed statement DuckDB's COMMIT returns nil but commits
		// nothing, so roll back rather than try it.
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

// appendLogBatches does AppendLogBatches' work inside the connection's open
// transaction.
func appendLogBatches(ctx context.Context, conn *sql.Conn, projectID uint32, batches []LogBatch) error {
	table := hotTable(projectID)
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (%s)", logsCatalog, table, hotTableColumns)); err != nil {
		return fmt.Errorf("create hot table: %w", err)
	}

	err := conn.Raw(func(c any) error {
		dConn, ok := c.(driver.Conn)
		if !ok {
			return fmt.Errorf("not a driver connection")
		}
		appender, err := duckdb.NewAppender(dConn, logsCatalog, "main", table)
		if err != nil {
			return err
		}
		for _, batch := range batches {
			for _, log := range batch.Logs {
				if err := appender.AppendRow(hotTableRow(log)...); err != nil {
					appender.Close()
					return err
				}
			}
		}
		return appender.Close()
	})
	if err != nil {
		return fmt.Errorf("append logs: %w", err)
	}

	cursor := batches[len(batches)-1].ID
	result, err := conn.ExecContext(ctx, "UPDATE logs.log_processing_cursors SET batch_id = ? WHERE project_id = ?", cursor.String(), projectID)
	if err != nil {
		return fmt.Errorf("move log processing cursor: %w", err)
	}
	if moved, err := result.RowsAffected(); err != nil || moved > 0 {
		return err
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO logs.log_processing_cursors VALUES (?, ?)", projectID, cursor.String()); err != nil {
		return fmt.Errorf("create log processing cursor: %w", err)
	}
	return nil
}

func hotTableRow(log models.Log) []driver.Value {
	return []driver.Value{
		duckdb.UUID(log.ID),
		log.Time,
		nullIfEmpty(log.Level),
		log.Message,
		nullIfEmpty(log.Service),
		nullIfEmpty(log.Environment),
		nullIfEmpty(log.TraceID),
		nullIfEmpty(log.SpanID),
		orderedMap(log.Attributes.String),
		orderedMap(log.Attributes.Number),
		orderedMap(log.Attributes.Bool),
		orderedMap(log.Attributes.JSON),
	}
}

func nullIfEmpty(s string) driver.Value {
	if s == "" {
		return nil
	}
	return s
}

// orderedMap converts a Go map to a DuckDB MAP value, keys sorted.
func orderedMap[V any](m map[string]V) duckdb.OrderedMap {
	var om duckdb.OrderedMap
	for _, k := range slices.Sorted(maps.Keys(m)) {
		om.Set(k, m[k])
	}
	return om
}

// LatestLogs returns up to limit of a Project's newest Logs, by time, newest
// first.
func (s *LogStore) LatestLogs(projectID uint32, limit int) ([]models.Log, error) {
	table := hotTable(projectID)
	var exists bool
	err := s.db.QueryRow(
		"SELECT count(*) > 0 FROM duckdb_tables() WHERE database_name = ? AND schema_name = 'main' AND table_name = ?",
		logsCatalog, table,
	).Scan(&exists)
	if err != nil || !exists {
		return []models.Log{}, err
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT id, time, level, message, service, environment, trace_id, span_id,
			attrs_string, attrs_number, attrs_bool, attrs_json
		FROM %s.%s
		ORDER BY time DESC, id DESC
		LIMIT ?`, logsCatalog, table), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []models.Log{}
	for rows.Next() {
		var (
			id                                           []byte
			ts                                           time.Time
			level, service, environment, traceID, spanID sql.NullString
			message                                      string
			attrsString, attrsNumber, attrsBool, attrsJS any
		)
		if err := rows.Scan(&id, &ts, &level, &message, &service, &environment, &traceID, &spanID,
			&attrsString, &attrsNumber, &attrsBool, &attrsJS); err != nil {
			return nil, err
		}
		logID, err := uuid.FromBytes(id)
		if err != nil {
			return nil, fmt.Errorf("log id %x: %w", id, err)
		}
		logs = append(logs, models.Log{
			ID:          logID,
			Time:        ts.UTC(),
			Level:       level.String,
			Message:     message,
			Service:     service.String,
			Environment: environment.String,
			TraceID:     traceID.String,
			SpanID:      spanID.String,
			Attributes: models.Attributes{
				String: goMap[string](attrsString),
				Number: goMap[float64](attrsNumber),
				Bool:   goMap[bool](attrsBool),
				JSON:   goMap[string](attrsJS),
			},
		})
	}
	return logs, rows.Err()
}

// goMap converts a scanned DuckDB MAP(VARCHAR, V), or NULL, to a Go map.
func goMap[V any](scanned any) map[string]V {
	m := map[string]V{}
	om, ok := scanned.(duckdb.OrderedMap)
	if !ok {
		return m
	}
	values := om.Values()
	for i, k := range om.Keys() {
		key, keyOK := k.(string)
		value, valueOK := values[i].(V)
		if keyOK && valueOK {
			m[key] = value
		}
	}
	return m
}

// sqlString quotes s as a SQL string literal.
func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
