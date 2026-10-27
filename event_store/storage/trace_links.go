package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/solidtrace/event_store/models"
)

// A trace's Logs and Events are those that carry its id: a Log's trace_id,
// and an Event's, which Event processing reads from its trace context.

// traceLinksMigration indexes Logs by trace id and gives Events one. Its
// version's hundreds are the ticket that added it (Traces 03). The indexes
// apply to parts made from now on: existing parts aren't rewritten, so a
// read of them scans their trace ids, and Events stored before have none.
var traceLinksMigration = clickHouseMigration{version: 2301, statements: []string{
	`ALTER TABLE logs ADD INDEX IF NOT EXISTS trace_id trace_id TYPE bloom_filter(0.01) GRANULARITY 1`,
	`ALTER TABLE events ADD COLUMN IF NOT EXISTS trace_id String DEFAULT ''`,
	`ALTER TABLE events ADD INDEX IF NOT EXISTS trace_id trace_id TYPE bloom_filter(0.01) GRANULARITY 1`,
}}

// TraceLog is a Log of a trace, with its Project.
type TraceLog struct {
	ProjectID uint32 `json:"project_id"`
	models.Log
}

// TraceLogs reads at most limit of a trace's Logs among projectIDs', from
// from to to, in time order.
func (ch *ClickHouse) TraceLogs(ctx context.Context, projectIDs []uint32, traceID string, from, to time.Time, limit int) ([]TraceLog, error) {
	ctx = TagRead(ctx, projectIDs[0], ReadTrace)
	rows, err := ch.conn.Query(ctx, `
		SELECT project_id, `+logSelectList+`
		FROM logs
		WHERE project_id IN {projects:Array(UInt32)} AND trace_id = {trace:String}
			AND time >= {from:DateTime64(6, 'UTC')} AND time <= {to:DateTime64(6, 'UTC')}
		ORDER BY time, id
		LIMIT {limit:UInt32}`,
		clickhouse.Named("projects", projectIDs), clickhouse.Named("trace", traceID),
		clickhouse.Named("from", from), clickhouse.Named("to", to), clickhouse.Named("limit", limit))
	if err != nil {
		return nil, fmt.Errorf("read the trace's logs: %w", err)
	}
	defer rows.Close()
	var logs []TraceLog
	for rows.Next() {
		var l TraceLog
		if l.Log, err = scanLog(rows, &l.ProjectID); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// TracedEvent is an Event of a trace.
type TracedEvent struct {
	ProjectID          uint32
	UUID               string
	IssueFingerprintID int64
	Timestamp          time.Time
}

// TraceEvents reads at most limit of a trace's Events among projectIDs', in
// time order: those from from to to, or of any time when from is zero.
func (ch *ClickHouse) TraceEvents(ctx context.Context, projectIDs []uint32, traceID string, from, to time.Time, limit int) ([]TracedEvent, error) {
	ctx = TagRead(ctx, projectIDs[0], ReadTrace)
	within := ""
	if !from.IsZero() {
		within = "AND timestamp >= {from:DateTime64(6, 'UTC')} AND timestamp <= {to:DateTime64(6, 'UTC')}"
	}
	rows, err := ch.conn.Query(ctx, `
		SELECT project_id, toString(uuid), issue_fingerprint_id, timestamp
		FROM events
		WHERE project_id IN {projects:Array(UInt32)} AND trace_id = {trace:String} `+within+`
		ORDER BY timestamp, uuid
		LIMIT {limit:UInt32}`,
		clickhouse.Named("projects", projectIDs), clickhouse.Named("trace", traceID),
		clickhouse.Named("from", from), clickhouse.Named("to", to), clickhouse.Named("limit", limit))
	if err != nil {
		return nil, fmt.Errorf("read the trace's events: %w", err)
	}
	defer rows.Close()
	var events []TracedEvent
	for rows.Next() {
		var e TracedEvent
		if err := rows.Scan(&e.ProjectID, &e.UUID, &e.IssueFingerprintID, &e.Timestamp); err != nil {
			return nil, err
		}
		e.Timestamp = e.Timestamp.UTC()
		events = append(events, e)
	}
	return events, rows.Err()
}
