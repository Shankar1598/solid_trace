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
	"sync"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

// logsCatalog is the name logs.duckdb is attached under.
const logsCatalog = "logs"

// LogStore is logs.duckdb: a hot table per Project, each Project's Log
// processing cursor, its promoted sets and its key popularity counts. It is
// attached to the DuckDB instance of events.duckdb, so the two share one
// memory limit, but it has its own WAL and checkpoints.
//
// A DuckDB transaction can write only one attached database, so everything
// Log processing commits together lives here.
//
// The promoted-set table is also held in memory, and read from there.
//
// The hourly export moves Logs out to files under files.Dir.
type LogStore struct {
	db       *sql.DB
	promoted *promotedSetCache
	files    LogFiles

	// writeMu lets one write to logs.duckdb run at a time: a Log processing
	// commit or try, or a Project's export. It is held only for each of
	// them, never across Log processing's waits.
	writeMu sync.Mutex
	// publishMu keeps a read from beginning while an export or a deletion
	// commits and publishes its files, so that a read sees each Log exactly
	// once. A read holds it only while it begins (logRead).
	publishMu sync.RWMutex
	// pins counts, for each Log file, the reads that may still read it.
	pinMu sync.Mutex
	pins  map[string]int
}

// NewLogStore attaches the logs.duckdb at path to the DuckDB instance of
// events, creating the file if it doesn't exist. Exported Logs go under
// filesDir. Files an export staged before a crash are settled.
func NewLogStore(events *DuckDBWriter, path, filesDir string) (*LogStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	// An ATTACH on one pooled connection is seen by every connection.
	if _, err := events.db.Exec(fmt.Sprintf("ATTACH IF NOT EXISTS %s AS %s", sqlString(path), logsCatalog)); err != nil {
		return nil, fmt.Errorf("attach %s: %w", path, err)
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS logs.log_processing_cursors (
			project_id UINTEGER,
			batch_id UUID
		)`,
		// One row per (type, key) pair of a Project's promoted set for a
		// UTC day.
		`CREATE TABLE IF NOT EXISTS logs.log_promoted_sets (
			project_id UINTEGER,
			day DATE,
			type VARCHAR,
			key VARCHAR,
			added_at TIMESTAMP,
			partway BOOLEAN
		)`,
		// How many Logs of a Project's UTC day carried each (type, key)
		// pair: a top-K per Project and day.
		`CREATE TABLE IF NOT EXISTS logs.log_key_counts (
			project_id UINTEGER,
			day DATE,
			type VARCHAR,
			key VARCHAR,
			logs BIGINT
		)`,
		// Every Project's Logs whose time was outside the hot window when
		// Log processing stored them: a hot table's fixed columns, maps only.
		`CREATE TABLE IF NOT EXISTS logs.logs_late (
			project_id UINTEGER,` + hotTableColumns + `)`,
	} {
		if _, err := events.db.Exec(ddl); err != nil {
			return nil, err
		}
	}
	promoted, err := loadPromotedSets(events.db)
	if err != nil {
		return nil, fmt.Errorf("load promoted sets: %w", err)
	}
	store := &LogStore{db: events.db, promoted: promoted, files: LogFiles{Dir: filesDir}, pins: map[string]int{}}
	if err := store.recoverStagedFiles(); err != nil {
		return nil, fmt.Errorf("recover staged log files: %w", err)
	}
	return store, nil
}

// Files is where the Log files are.
func (s *LogStore) Files() LogFiles {
	return s.files
}

// hotTable is the name of a Project's hot table in logs.duckdb.
func hotTable(projectID uint32) string {
	return fmt.Sprintf("logs_hot_%d", projectID)
}

// hotTableColumns are a hot table's fixed columns: the id, the seven built-in
// fields and one map per Attribute type. attrs_json holds JSON text. Promoted
// columns follow them.
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

// lateTable is the table that holds every Project's late Logs.
const lateTable = "logs_late"

// HotWindow is the span of time whose Logs Log processing appends to a
// Project's hot table at now: from the start of the previous hour to the
// start of the next. A Log outside it is late, and goes to logs_late.
func HotWindow(now time.Time) (from, to time.Time) {
	hour := now.UTC().Truncate(time.Hour)
	return hour.Add(-time.Hour), hour.Add(time.Hour)
}

// hotTableFixedColumns are the names of hotTableColumns, in order.
var hotTableFixedColumns = []string{
	"id", "time", "level", "message", "service", "environment", "trace_id", "span_id",
	"attrs_string", "attrs_number", "attrs_bool", "attrs_json",
}

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

// AppendLogBatches stores the Logs of a Project's Log batches, all in one
// transaction:
//  1. it makes the promoted set of each of the hot Logs' days that has none;
//  2. it promotes the hot Logs' new keys while their day's set has room,
//     adding their columns to the Project's hot table;
//  3. it appends the Logs in the hot window of now to the hot table, created
//     with its first Log, each promoted value in its column and not in its
//     map, and the rest to logs_late, maps only;
//  4. it adds the hot Logs' keys to the popularity counts;
//  5. it moves the Project's Log processing cursor to the last batch.
//
// Either all of it commits or none does, so a batch at or below the cursor is
// never appended twice. now is when promoted keys are recorded as added.
//
// A batch may hold no Logs, so that the cursor still moves past it.
func (s *LogStore) AppendLogBatches(projectID uint32, batches []LogBatch, now time.Time) error {
	return s.appendLogBatchesTx(projectID, batches, now, true)
}

// TryLogBatches does all that AppendLogBatches does, then rolls it back. It
// fails as AppendLogBatches would, but stores nothing.
func (s *LogStore) TryLogBatches(projectID uint32, batches []LogBatch, now time.Time) error {
	return s.appendLogBatchesTx(projectID, batches, now, false)
}

// Ping reports whether logs.duckdb answers a query.
func (s *LogStore) Ping() error {
	var n int
	return s.db.QueryRow("SELECT count(*) FROM (SELECT 1 FROM logs.log_processing_cursors LIMIT 1)").Scan(&n)
}

// appendLogBatchesTx runs appendLogBatches in a transaction, and commits it
// only if commit is set.
func (s *LogStore) appendLogBatchesTx(projectID uint32, batches []LogBatch, now time.Time, commit bool) error {
	if len(batches) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	promoted, err := appendLogBatches(ctx, conn, projectID, batches, now.UTC())
	if err != nil || !commit {
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
	s.promoted.put(projectID, promoted)
	return nil
}

// appendLogBatches does AppendLogBatches' work inside the connection's open
// transaction, and returns the promoted sets of the Logs' days as it leaves
// them.
func appendLogBatches(ctx context.Context, conn *sql.Conn, projectID uint32, batches []LogBatch, now time.Time) ([]PromotedSet, error) {
	table := hotTable(projectID)
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (%s)", logsCatalog, table, hotTableColumns)); err != nil {
		return nil, fmt.Errorf("create hot table: %w", err)
	}

	// Late Logs are never promoted, and don't count towards popularity:
	// their days' sets are made or past, and a burst of old Logs shouldn't
	// choose today's columns.
	var hot, late []models.Log
	from, to := HotWindow(now)
	for _, batch := range batches {
		for _, log := range batch.Logs {
			if log.Time.Before(from) || !log.Time.Before(to) {
				late = append(late, log)
			} else {
				hot = append(hot, log)
			}
		}
	}
	promotion, err := promote(ctx, conn, projectID, hot, now)
	if err != nil {
		return nil, err
	}
	if err := appendHotRows(conn, table, hot, promotion); err != nil {
		return nil, fmt.Errorf("append logs: %w", err)
	}
	if err := appendLateRows(conn, projectID, late); err != nil {
		return nil, fmt.Errorf("append late logs: %w", err)
	}
	if err := countKeys(ctx, conn, projectID, hot); err != nil {
		return nil, err
	}

	cursor := batches[len(batches)-1].ID
	result, err := conn.ExecContext(ctx, "UPDATE logs.log_processing_cursors SET batch_id = ? WHERE project_id = ?", cursor.String(), projectID)
	if err != nil {
		return nil, fmt.Errorf("move log processing cursor: %w", err)
	}
	if moved, err := result.RowsAffected(); err != nil || moved > 0 {
		return promotion.sets(), err
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO logs.log_processing_cursors VALUES (?, ?)", projectID, cursor.String()); err != nil {
		return nil, fmt.Errorf("create log processing cursor: %w", err)
	}
	return promotion.sets(), nil
}

// appendHotRows appends Logs to a hot table through an appender opened after
// the promotion's ALTER TABLEs, as an appender opened before one fails.
func appendHotRows(conn *sql.Conn, table string, logs []models.Log, promotion *promotion) error {
	promoted := promotion.promotedColumns()
	columns := slices.Clone(hotTableFixedColumns)
	for _, key := range promoted {
		columns = append(columns, key.column())
	}
	return conn.Raw(func(c any) error {
		dConn, ok := c.(driver.Conn)
		if !ok {
			return fmt.Errorf("not a driver connection")
		}
		appender, err := duckdb.NewAppenderWithColumns(dConn, logsCatalog, "main", table, columns)
		if err != nil {
			return err
		}
		for _, log := range logs {
			set := promotion.days[LogDay(log.Time)].keys
			if err := appender.AppendRow(hotTableRow(log, set, promoted)...); err != nil {
				appender.Close()
				return err
			}
		}
		return appender.Close()
	})
}

// appendLateRows appends a Project's late Logs to logs_late, every Attribute
// in its map.
func appendLateRows(conn *sql.Conn, projectID uint32, logs []models.Log) error {
	if len(logs) == 0 {
		return nil
	}
	return conn.Raw(func(c any) error {
		dConn, ok := c.(driver.Conn)
		if !ok {
			return fmt.Errorf("not a driver connection")
		}
		appender, err := duckdb.NewAppender(dConn, logsCatalog, "main", lateTable)
		if err != nil {
			return err
		}
		for _, log := range logs {
			row := append([]driver.Value{projectID}, hotTableRow(log, nil, nil)...)
			if err := appender.AppendRow(row...); err != nil {
				appender.Close()
				return err
			}
		}
		return appender.Close()
	})
}

// hotTableRow is a Log's row: the fixed columns, then the promoted columns.
// A value whose key is in the Log's day's set goes to its column and not its
// map; the columns of other keys are NULL.
func hotTableRow(log models.Log, set map[typedKey]PromotedKey, promoted []typedKey) []driver.Value {
	inMap := func(t AttributeType) func(string) bool {
		return func(key string) bool {
			_, ok := set[typedKey{t, key}]
			return !ok
		}
	}
	row := []driver.Value{
		duckdb.UUID(log.ID),
		log.Time,
		nullIfEmpty(log.Level),
		log.Message,
		nullIfEmpty(log.Service),
		nullIfEmpty(log.Environment),
		nullIfEmpty(log.TraceID),
		nullIfEmpty(log.SpanID),
		orderedMap(log.Attributes.String, inMap(StringAttribute)),
		orderedMap(log.Attributes.Number, inMap(NumberAttribute)),
		orderedMap(log.Attributes.Bool, inMap(BoolAttribute)),
		orderedMap(log.Attributes.JSON, inMap(JSONAttribute)),
	}
	for _, key := range promoted {
		var value driver.Value
		if _, ok := set[key]; ok {
			if v, ok := attributeValue(log.Attributes, key); ok {
				value = v
			}
		}
		row = append(row, value)
	}
	return row
}

func nullIfEmpty(s string) driver.Value {
	if s == "" {
		return nil
	}
	return s
}

// orderedMap converts the entries of a Go map whose keys keep accepts to a
// DuckDB MAP value, keys sorted.
func orderedMap[V any](m map[string]V, keep func(string) bool) duckdb.OrderedMap {
	var om duckdb.OrderedMap
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if keep(k) {
			om.Set(k, m[k])
		}
	}
	return om
}

// tableExists reports whether logs.duckdb has a table named table.
func (s *LogStore) tableExists(table string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(
		"SELECT count(*) > 0 FROM duckdb_tables() WHERE database_name = ? AND schema_name = 'main' AND table_name = ?",
		logsCatalog, table,
	).Scan(&exists)
	return exists, err
}

// scanHotRow scans a hot-table row of every column into a Log, merging each
// non-NULL promoted column into its map.
func scanHotRow(rows *sql.Rows, columns []string) (models.Log, error) {
	var (
		id                                           []byte
		ts                                           time.Time
		level, service, environment, traceID, spanID sql.NullString
		message                                      string
		attrsString, attrsNumber, attrsBool, attrsJS any
	)
	fixed := map[string]any{
		"id": &id, "time": &ts, "level": &level, "message": &message, "service": &service,
		"environment": &environment, "trace_id": &traceID, "span_id": &spanID,
		"attrs_string": &attrsString, "attrs_number": &attrsNumber, "attrs_bool": &attrsBool, "attrs_json": &attrsJS,
	}
	values := make([]any, len(columns))
	dest := make([]any, len(columns))
	for i, column := range columns {
		if d, ok := fixed[column]; ok {
			dest[i] = d
		} else {
			dest[i] = &values[i]
		}
	}
	if err := rows.Scan(dest...); err != nil {
		return models.Log{}, err
	}

	logID, err := uuid.FromBytes(id)
	if err != nil {
		return models.Log{}, fmt.Errorf("log id %x: %w", id, err)
	}
	log := models.Log{
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
	}
	for i, column := range columns {
		key, ok := promotedColumnKey(column)
		if !ok || values[i] == nil {
			continue
		}
		switch v := values[i].(type) {
		case string:
			log.Attributes.String[key.Key] = v
		case float64:
			log.Attributes.Number[key.Key] = v
		case bool:
			log.Attributes.Bool[key.Key] = v
		}
	}
	return log, nil
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
