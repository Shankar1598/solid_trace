package storage

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/chcol"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// logsDedupWindow is how many of the last inserts' blocks the logs table and
// the key counts table remember, so that a range of Log batches inserted
// again after a crash or a failed insert is dropped. A range is inserted
// again within seconds, while each Project inserts about once a second.
const logsDedupWindow = 10000

// logsTableMigration makes the logs table, every Project's Logs, and the key
// counts table, how many Logs of each Project's day carried each Attribute.
// Its version's hundreds are the ticket that added it (03).
var logsTableMigration = clickHouseMigration{version: 301, statements: []string{
	// Partitioned so that no part mixes Projects or days, and sorted by
	// Service and the file Attribute, so that a filter on them reads only
	// their Logs whatever the range.
	//
	// file is the file Attribute when it is a string, else '': the
	// Attribute itself stays in attributes. attributes holds every string,
	// number (Float64) and bool Attribute at its own path, typed;
	// attrs_json holds arrays and objects as JSON text. search_text is
	// every string Attribute's value, joined by \0, for search; being
	// MATERIALIZED keeps it out of SELECT *. batch is the Log batch the Log
	// came in, for Match monitors: its index orders batches by their bytes,
	// as they are made, which ClickHouse's order of UUIDs doesn't.
	`CREATE TABLE IF NOT EXISTS logs (
		project_id UInt32,
		id UUID,
		time DateTime64(6, 'UTC'),
		level LowCardinality(Nullable(String)),
		service LowCardinality(String),
		environment LowCardinality(Nullable(String)),
		message String CODEC(ZSTD(1)),
		trace_id Nullable(String),
		span_id Nullable(String),
		batch UUID,
		file LowCardinality(String),
		attributes JSON,
		attrs_json Map(LowCardinality(String), String),
		search_text String MATERIALIZED '' CODEC(ZSTD(1)),
		INDEX batch_order toUInt128(batch) TYPE minmax GRANULARITY 1
	) ENGINE = MergeTree
	PARTITION BY (project_id, toDate(time))
	ORDER BY (project_id, service, file, time)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(logsDedupWindow),
	// Summed per Project, day and Attribute. Log processing inserts how
	// each batch changes a day's top keys: a key evicted from them gets
	// minus its count, so that its sum is 0 and the merge drops it.
	`CREATE TABLE IF NOT EXISTS log_key_counts (
		project_id UInt32,
		day Date,
		type LowCardinality(String),
		key String,
		logs Int64
	) ENGINE = SummingMergeTree(logs)
	PARTITION BY (project_id, day)
	ORDER BY (project_id, day, type, key)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(logsDedupWindow),
	// Takes the inserts TryLogBatches makes, and keeps nothing.
	`CREATE TABLE IF NOT EXISTS logs_try AS logs ENGINE = Null`,
}}

// logsColumnList is the columns Log processing inserts into logs.
const logsColumnList = "project_id, id, time, level, service, environment, message, trace_id, span_id, batch, file, attributes, attrs_json, search_text"

// ClickHouseLogs is every Project's Logs in ClickHouse: the logs table, which
// Log processing inserts each Project's Logs into, and the key counts table,
// which the Fields list reads. Log processing's cursors are in Pebble, which
// a read of the Logs ingested after a batch reads too.
type ClickHouseLogs struct {
	conn   driver.Conn
	pebble *PebbleWriter

	// countsMu guards counts: each (Project, day)'s key counts as the key
	// counts table holds them, read once and then kept up to date by the
	// inserts. Log processing is the only writer, so they stay right.
	countsMu sync.Mutex
	counts   map[projectDay]map[typedKey]int64

	// bins is the bin cache, or nil when it is off, and generations its
	// generations of (Project, hour)s (logs_clickhouse_table.go).
	bins        atomic.Pointer[binCache]
	generations generations
}

type projectDay struct {
	projectID uint32
	day       time.Time
}

// keptCountDays is how many (Project, day)s' key counts stay in memory. Past
// it, those an insert doesn't touch are read again when next needed.
const keptCountDays = 64

// NewClickHouseLogs reads and writes Logs in ch, with Log processing's
// cursors in pebble.
func NewClickHouseLogs(ch *ClickHouse, pebble *PebbleWriter) *ClickHouseLogs {
	return &ClickHouseLogs{conn: ch.Conn(), pebble: pebble, counts: map[projectDay]map[typedKey]int64{}}
}

// Ping checks that ClickHouse answers.
func (s *ClickHouseLogs) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), clickHousePingTimeout)
	defer cancel()
	return s.conn.Ping(ctx)
}

// LogProcessingCursor returns the id of the last Log batch whose Logs Log
// processing has stored for a Project, or uuid.Nil if it has stored none.
func (s *ClickHouseLogs) LogProcessingCursor(projectID uint32) (uuid.UUID, error) {
	return s.pebble.LogProcessingCursor(projectID)
}

// LogInsert is one insert of a Project's Log batches: those after the
// Processing cursor After, up to and including UpTo, as the Log transform
// returned them. Its range is recorded in Pebble before it is inserted, and
// names the insert to ClickHouse, which drops it if it comes again.
type LogInsert struct {
	After, UpTo uuid.UUID
	Batches     []LogBatch
	// Again is set when the range may have been inserted before, whole or
	// in part, as after a crash or a failed insert.
	Again bool
}

// token is the insert's dedup token, the same each time its range is
// inserted.
func (i LogInsert) token(projectID uint32) string {
	return fmt.Sprintf("logs-%d-%s-%s", projectID, i.After, i.UpTo)
}

// ErrUnstorableLog marks a Log that can't be stored for what it holds.
var ErrUnstorableLog = errors.New("log can't be stored")

// Logs' times must lie in the days a partition can name: a Date holds days
// from 1970 to 2149.
var (
	firstStorableTime = time.Unix(0, 0).UTC()
	endOfStorableTime = time.Date(2149, 6, 1, 0, 0, 0, 0, time.UTC)
)

// checkStorable returns an ErrUnstorableLog for the first Log of batches
// that can't be stored.
func checkStorable(batches []LogBatch) error {
	for _, batch := range batches {
		for _, log := range batch.Logs {
			if log.Time.Before(firstStorableTime) || !log.Time.Before(endOfStorableTime) {
				return fmt.Errorf("%w: its time %s is outside %s to %s", ErrUnstorableLog,
					log.Time.Format(time.RFC3339), firstStorableTime.Format(time.DateOnly), endOfStorableTime.Format(time.DateOnly))
			}
		}
	}
	return nil
}

// InsertLogBatches inserts the Logs of a Project's Log batches into logs,
// and how many of them carried each Attribute into the key counts table,
// each with the range's dedup token: ClickHouse drops an insert whose range
// it has already taken, so a range inserted again after a crash, whole or in
// part, is stored once. The batches may hold no Logs.
func (s *ClickHouseLogs) InsertLogBatches(projectID uint32, insert LogInsert) error {
	if err := checkStorable(insert.Batches); err != nil {
		return err
	}
	ctx := insertContext(insert.token(projectID))
	rows := 0
	for _, batch := range insert.Batches {
		rows += len(batch.Logs)
	}
	if rows == 0 {
		return nil
	}
	err := s.insertLogs(ctx, "logs", projectID, insert.Batches)
	// Whether or not ClickHouse answered, the Logs may be in logs now, so
	// no bin of their hours the bin cache kept is taken again.
	s.generations.bump(projectID, insert.Batches)
	if err != nil {
		return fmt.Errorf("insert logs: %w", err)
	}
	if err := s.insertKeyCounts(ctx, projectID, insert); err != nil {
		return fmt.Errorf("insert key counts: %w", err)
	}
	return nil
}

// TryLogBatches fails as InsertLogBatches would on the Logs of a Project's
// Log batches, but stores nothing: it inserts them into logs_try.
func (s *ClickHouseLogs) TryLogBatches(projectID uint32, batches []LogBatch) error {
	if err := checkStorable(batches); err != nil {
		return err
	}
	return s.insertLogs(insertContext(""), "logs_try", projectID, batches)
}

// insertContext carries an insert's settings: synchronous, so that a Log is
// in ClickHouse once the insert returns; one thread, so that the insert's
// blocks, and their dedup tokens, are the same each time; and the dedup
// token, if any.
func insertContext(token string) context.Context {
	settings := clickhouse.Settings{
		"async_insert":                      0,
		"max_insert_threads":                1,
		"insert_allow_materialized_columns": 1,
	}
	if token != "" {
		settings["insert_deduplication_token"] = token
	}
	return clickhouse.Context(context.Background(), clickhouse.WithSettings(settings))
}

// insertLogs inserts the Logs of a Project's batches into table, logs or
// logs_try, as one block.
func (s *ClickHouseLogs) insertLogs(ctx context.Context, table string, projectID uint32, batches []LogBatch) error {
	block, err := s.conn.PrepareBatch(ctx, "INSERT INTO "+table+" ("+logsColumnList+")")
	if err != nil {
		return err
	}
	defer block.Abort()
	for _, batch := range batches {
		for _, log := range batch.Logs {
			if err := block.Append(logRow(projectID, batch.ID, log)...); err != nil {
				return err
			}
		}
	}
	return block.Send()
}

// logRow is a Log's row in logs, in logsColumnList's order. Its string,
// number and bool Attributes go to their paths as typed values, never as
// JSON text, which ClickHouse would read as Int64 or dates.
func logRow(projectID uint32, batch uuid.UUID, log models.Log) []any {
	attributes := chcol.NewJSON()
	strs := make([]string, 0, len(log.Attributes.String))
	for _, key := range slices.Sorted(maps.Keys(log.Attributes.String)) {
		value := log.Attributes.String[key]
		attributes.SetValueAtPath(key, chcol.NewDynamicWithType(value, "String"))
		strs = append(strs, value)
	}
	for key, value := range log.Attributes.Number {
		attributes.SetValueAtPath(key, chcol.NewDynamicWithType(value, "Float64"))
	}
	for key, value := range log.Attributes.Bool {
		attributes.SetValueAtPath(key, chcol.NewDynamicWithType(value, "Bool"))
	}
	json := log.Attributes.JSON
	if json == nil {
		json = map[string]string{}
	}
	return []any{
		projectID,
		log.ID,
		log.Time,
		nullIfEmptyString(log.Level),
		log.Service,
		nullIfEmptyString(log.Environment),
		log.Message,
		nullIfEmptyString(log.TraceID),
		nullIfEmptyString(log.SpanID),
		batch,
		log.Attributes.String["file"],
		attributes,
		json,
		strings.Join(strs, "\x00"),
	}
}

func nullIfEmptyString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// insertKeyCounts inserts how the Logs of an insert change their Project's
// key counts for each of their days: each day's top keyCountsKept keys by how
// many Logs carried them (mergeKeyCounts). The counts kept in memory change
// only once ClickHouse has the insert; for a range inserted again they are
// read afresh, since ClickHouse may have dropped it.
func (s *ClickHouseLogs) insertKeyCounts(ctx context.Context, projectID uint32, insert LogInsert) error {
	byDay := map[time.Time]map[typedKey]int64{}
	for _, batch := range insert.Batches {
		for _, log := range batch.Logs {
			day := LogDay(log.Time)
			for _, key := range logKeys(log) {
				if byDay[day] == nil {
					byDay[day] = map[typedKey]int64{}
				}
				byDay[day][key]++
			}
		}
	}
	if len(byDay) == 0 {
		return nil
	}

	s.countsMu.Lock()
	defer s.countsMu.Unlock()
	merged := map[time.Time]map[typedKey]int64{}
	block, err := s.conn.PrepareBatch(ctx, "INSERT INTO log_key_counts (project_id, day, type, key, logs)")
	if err != nil {
		return err
	}
	defer block.Abort()
	for _, day := range slices.SortedFunc(maps.Keys(byDay), time.Time.Compare) {
		counts, err := s.dayCounts(ctx, projectID, day)
		if err != nil {
			return err
		}
		before := maps.Clone(counts)
		changed, evicted := mergeKeyCounts(counts, byDay[day], keyCountsKept)
		merged[day] = counts
		slices.SortFunc(changed, typedKey.compare)
		for _, key := range changed {
			if err := block.Append(projectID, day, string(key.Type), key.Key, counts[key]-before[key]); err != nil {
				return err
			}
		}
		slices.SortFunc(evicted, typedKey.compare)
		for _, key := range evicted {
			if n := before[key]; n != 0 {
				if err := block.Append(projectID, day, string(key.Type), key.Key, -n); err != nil {
					return err
				}
			}
		}
	}
	if err := block.Send(); err != nil {
		return err
	}
	for day, counts := range merged {
		if insert.Again {
			delete(s.counts, projectDay{projectID, day})
		} else {
			s.counts[projectDay{projectID, day}] = counts
		}
	}
	if len(s.counts) > keptCountDays {
		for kept := range s.counts {
			if _, touched := merged[kept.day]; !touched || kept.projectID != projectID {
				delete(s.counts, kept)
			}
		}
	}
	return nil
}

// dayCounts returns a copy of a Project's key counts for a day, read from
// the key counts table unless they are in memory.
func (s *ClickHouseLogs) dayCounts(ctx context.Context, projectID uint32, day time.Time) (map[typedKey]int64, error) {
	if counts, ok := s.counts[projectDay{projectID, day}]; ok {
		return maps.Clone(counts), nil
	}
	rows, err := s.conn.Query(ctx, `
		SELECT type, key, sum(logs) AS logs FROM log_key_counts
		WHERE project_id = ? AND day = toDate(?)
		GROUP BY type, key
		HAVING logs != 0`, projectID, dayString(day))
	if err != nil {
		return nil, fmt.Errorf("read key counts: %w", err)
	}
	defer rows.Close()
	counts := map[typedKey]int64{}
	for rows.Next() {
		var (
			kind, key string
			logs      int64
		)
		if err := rows.Scan(&kind, &key, &logs); err != nil {
			return nil, err
		}
		counts[typedKey{AttributeType(kind), key}] = logs
	}
	return counts, rows.Err()
}

// clickHouseStoreFailureCodes are the codes of ClickHouse's errors that are
// failures of the store, not of what was stored: they clear by themselves.
var clickHouseStoreFailureCodes = map[int32]bool{
	159: true, // TIMEOUT_EXCEEDED
	202: true, // TOO_MANY_SIMULTANEOUS_QUERIES
	209: true, // SOCKET_TIMEOUT
	210: true, // NETWORK_ERROR
	236: true, // ABORTED
	241: true, // MEMORY_LIMIT_EXCEEDED
	243: true, // NOT_ENOUGH_SPACE
	252: true, // TOO_MANY_PARTS
	394: true, // QUERY_WAS_CANCELLED
}

// StoreFailure reports whether err says the store failed, rather than what
// was stored: ClickHouse is short of memory or disk, too busy, or gone.
func StoreFailure(err error) bool {
	var exception *clickhouse.Exception
	if errors.As(err, &exception) {
		return clickHouseStoreFailureCodes[exception.Code]
	}
	return false
}
