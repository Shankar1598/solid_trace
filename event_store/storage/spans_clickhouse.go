package storage

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// spansDedupWindow is how many of the last inserts' blocks the Span tables
// remember, as logsDedupWindow is for Logs.
const spansDedupWindow = 10000

// spansTablesMigration makes the spans table, every Project's Spans, and the
// trace index, one row per Transaction. Its version's hundreds are the
// ticket that added it (Traces 02, numbered past the Logs effort's).
var spansTablesMigration = clickHouseMigration{version: 2101, statements: []string{
	// Partitioned so that no part mixes Projects or days, as logs is, and
	// sorted by Transaction, so that a Transaction's Spans are read alone.
	// group_hash is a hash of the op and the description with literals
	// replaced; repeats is how many Spans of the Transaction share it.
	// Times are microseconds.
	`CREATE TABLE IF NOT EXISTS spans (
		project_id UInt32,
		trace_id String,
		span_id String,
		parent_span_id String,
		segment_id String,
		is_segment Bool,
		transaction LowCardinality(String),
		op LowCardinality(String),
		description String CODEC(ZSTD(1)),
		group_hash String,
		repeats UInt32,
		status LowCardinality(String),
		start DateTime64(6, 'UTC'),
		duration_us UInt64,
		self_time_us UInt64,
		sample_rate Float64,
		environment LowCardinality(String),
		release LowCardinality(String),
		server_name LowCardinality(String),
		attributes Map(LowCardinality(String), String) CODEC(ZSTD(1))
	) ENGINE = MergeTree
	PARTITION BY (project_id, toDate(start))
	ORDER BY (project_id, transaction, start)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(spansDedupWindow),
	// Sorted by trace id, so that a trace's Transactions are found in any
	// of the Organization's Projects without reading spans.
	`CREATE TABLE IF NOT EXISTS trace_index (
		trace_id String,
		project_id UInt32,
		start DateTime64(6, 'UTC'),
		finish DateTime64(6, 'UTC'),
		transaction String
	) ENGINE = MergeTree
	PARTITION BY (project_id, toDate(start))
	ORDER BY (trace_id, start)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(spansDedupWindow),
}}

// SpanDays are a Project's Spans and the trace index of them, kept for its
// trace Retention.
var SpanDays = DayFamily{Name: "Spans", Tables: []string{"spans", "trace_index"}, TimeColumn: "start"}

// SpanInsert is the Spans of a range of a Project's Transaction batches, the
// batches after After up to UpTo, which name the insert to ClickHouse.
type SpanInsert struct {
	After, UpTo uuid.UUID
	Spans       []models.Span
}

func (i SpanInsert) token(projectID uint32) string {
	return fmt.Sprintf("spans-%d-%s-%s", projectID, i.After, i.UpTo)
}

// InsertSpans inserts a range's Spans into spans, and each segment's row into
// trace_index, with the range's dedup token: a range inserted again, whole
// or in part, is stored once.
func (ch *ClickHouse) InsertSpans(projectID uint32, insert SpanInsert) error {
	if len(insert.Spans) == 0 {
		return nil
	}
	ctx := insertContext(insert.token(projectID))
	block, err := ch.conn.PrepareBatch(ctx, `INSERT INTO spans (project_id, trace_id, span_id, parent_span_id, segment_id,
		is_segment, transaction, op, description, group_hash, repeats, status, start, duration_us, self_time_us,
		sample_rate, environment, release, server_name, attributes)`)
	if err != nil {
		return fmt.Errorf("insert spans: %w", err)
	}
	defer block.Abort()
	for _, s := range insert.Spans {
		if err := block.Append(projectID, s.TraceID, s.SpanID, s.ParentSpanID, s.SegmentID,
			s.IsSegment, s.Transaction, s.Op, s.Description, s.Group, s.Repeats, s.Status, s.Start,
			uint64(s.Duration.Microseconds()), uint64(s.SelfTime.Microseconds()),
			s.SampleRate, s.Environment, s.Release, s.ServerName, s.Attributes); err != nil {
			return fmt.Errorf("insert spans: %w", err)
		}
	}
	if err := block.Send(); err != nil {
		return fmt.Errorf("insert spans: %w", err)
	}

	index, err := ch.conn.PrepareBatch(ctx, "INSERT INTO trace_index (trace_id, project_id, start, finish, transaction)")
	if err != nil {
		return fmt.Errorf("insert trace index: %w", err)
	}
	defer index.Abort()
	for _, s := range insert.Spans {
		if s.IsSegment {
			if err := index.Append(s.TraceID, projectID, s.Start, s.Start.Add(s.Duration), s.Transaction); err != nil {
				return fmt.Errorf("insert trace index: %w", err)
			}
		}
	}
	if err := index.Send(); err != nil {
		return fmt.Errorf("insert trace index: %w", err)
	}
	return nil
}

// TracedSpan is a stored Span with its Project.
type TracedSpan struct {
	ProjectID uint32
	models.Span
}

// traceSpanMargin is how far before its Transaction's start, or after its
// end, a Span is still looked for.
const traceSpanMargin = time.Minute

// Trace reads a trace's Spans among projectIDs' in start order, each
// segment before the Spans that start with it. It finds the trace's
// Transactions in the trace index first, so that it reads only their parts
// of spans.
func (ch *ClickHouse) Trace(ctx context.Context, projectIDs []uint32, traceID string) ([]TracedSpan, error) {
	ctx = TagRead(ctx, projectIDs[0], ReadTrace)
	rows, err := ch.conn.Query(ctx, `
		SELECT project_id, groupUniqArray(transaction), min(start), max(finish)
		FROM trace_index
		WHERE trace_id = {trace:String} AND project_id IN {projects:Array(UInt32)}
		GROUP BY project_id ORDER BY project_id`,
		clickhouse.Named("trace", traceID), clickhouse.Named("projects", projectIDs))
	if err != nil {
		return nil, fmt.Errorf("read the trace index: %w", err)
	}
	var (
		found      []uint32
		names      []string
		start, end time.Time
	)
	for rows.Next() {
		var (
			projectID    uint32
			transactions []string
			from, to     time.Time
		)
		if err := rows.Scan(&projectID, &transactions, &from, &to); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, projectID)
		names = append(names, transactions...)
		if start.IsZero() || from.Before(start) {
			start = from
		}
		end = later(end, to)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	slices.Sort(names)

	rows, err = ch.conn.Query(ctx, `
		SELECT project_id, trace_id, span_id, parent_span_id, segment_id, is_segment, transaction, op, description,
			group_hash, repeats, status, start, duration_us, self_time_us, sample_rate, environment, release,
			server_name, attributes
		FROM spans
		WHERE project_id IN {projects:Array(UInt32)} AND transaction IN {transactions:Array(String)}
			AND start >= {from:DateTime64(6, 'UTC')} AND start <= {to:DateTime64(6, 'UTC')}
			AND trace_id = {trace:String}
		ORDER BY start, is_segment DESC, span_id`,
		clickhouse.Named("projects", found), clickhouse.Named("transactions", slices.Compact(names)),
		clickhouse.Named("from", start.Add(-traceSpanMargin)), clickhouse.Named("to", end.Add(traceSpanMargin)),
		clickhouse.Named("trace", traceID))
	if err != nil {
		return nil, fmt.Errorf("read the trace's spans: %w", err)
	}
	defer rows.Close()
	var spans []TracedSpan
	for rows.Next() {
		var (
			s              TracedSpan
			duration, self uint64
		)
		if err := rows.Scan(&s.ProjectID, &s.TraceID, &s.SpanID, &s.ParentSpanID, &s.SegmentID, &s.IsSegment,
			&s.Transaction, &s.Op, &s.Description, &s.Group, &s.Repeats, &s.Status, &s.Start, &duration, &self,
			&s.SampleRate, &s.Environment, &s.Release, &s.ServerName, &s.Attributes); err != nil {
			return nil, err
		}
		s.Start = s.Start.UTC()
		s.Duration = time.Duration(duration) * time.Microsecond
		s.SelfTime = time.Duration(self) * time.Microsecond
		spans = append(spans, s)
	}
	return spans, rows.Err()
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
