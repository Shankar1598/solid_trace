package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/chsql"
)

// Events in ClickHouse's events table: one row for each Event that Event
// processing stored, holding what Event reads filter and show. The raw
// payload stays in Pebble, and Issues in Console's tables.

// eventsTableMigration makes the events table. Its version's hundreds are the
// ticket that added it (06), so that steps added on other branches meanwhile
// don't take its number.
var eventsTableMigration = clickHouseMigration{
	version: 601,
	statements: []string{fmt.Sprintf(`CREATE TABLE IF NOT EXISTS events (
		uuid UUID,
		project_id UInt32,
		issue_fingerprint_id Int64,
		timestamp DateTime64(6, 'UTC'),
		environment LowCardinality(String),
		server_name LowCardinality(String),
		release LowCardinality(String),
		level LowCardinality(String),
		tags Map(String, String)
	) ENGINE = MergeTree
	PARTITION BY toYYYYMM(timestamp)
	ORDER BY (project_id, issue_fingerprint_id, timestamp)
	SETTINGS storage_policy = '%s', non_replicated_deduplication_window = 1000`, ClickHouseStoragePolicy)},
}

// ErrEventsUnavailable is why an Event read fails while ClickHouse doesn't
// answer. A retry may succeed once it does.
var ErrEventsUnavailable = errors.New("Events can't be read now")

// eventsTimeout bounds an Event read or insert, so that a ClickHouse that
// stops answering fails it rather than holds it.
const eventsTimeout = time.Minute

// EventRange is one batch of a Project's Events, in UUID order: those after
// After ("" for the Project's first), up to and including Through. Event
// processing records it in Pebble before it inserts the batch's rows, and
// inserts them with a token made from it, so that ClickHouse drops the
// insert of a range it already holds (RecordEventRange).
type EventRange struct {
	ProjectID      uint32
	After, Through string
}

// token is the range's insert deduplication token.
func (r EventRange) token() string {
	return fmt.Sprintf("events/%d/%s/%s", r.ProjectID, r.After, r.Through)
}

// The times a DateTime64 holds.
var (
	earliestEventTime = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	latestEventTime   = time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)
)

// EventRowError says why the events table can't hold an Event's row, or is
// nil when it can. It depends on the row alone, so a replayed range skips the
// same rows.
func EventRowError(row models.Event) error {
	if _, err := uuid.Parse(row.EventUUID); err != nil {
		return fmt.Errorf("event UUID %q: %w", row.EventUUID, err)
	}
	if row.Timestamp.Before(earliestEventTime) || !row.Timestamp.Before(latestEventTime) {
		return fmt.Errorf("timestamp %s is outside the years 1900 to 2299 that ClickHouse holds", row.Timestamp.UTC().Format(time.RFC3339))
	}
	return nil
}

// InsertEvents inserts the rows of an Event range in one insert, with the
// range's deduplication token: inserting the same range again, as after a
// crash before Event processing's cursor moved, adds nothing. Each row must
// pass EventRowError.
func (ch *ClickHouse) InsertEvents(r EventRange, rows []models.Event) error {
	if len(rows) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), eventsTimeout)
	defer cancel()
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":               0,
		"insert_deduplication_token": r.token(),
		"max_insert_threads":         1,
		// Events carry the times their senders give, so a batch may span
		// many months. More parts beats a batch that never goes in.
		"throw_on_max_partitions_per_insert_block": 0,
	}))
	batch, err := ch.conn.PrepareBatch(ctx, "INSERT INTO events (uuid, project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags, trace_id)")
	if err != nil {
		return err
	}
	defer batch.Close()
	for _, row := range rows {
		if err := EventRowError(row); err != nil {
			return err
		}
		tags := row.Tags
		if tags == nil {
			tags = map[string]string{}
		}
		if err := batch.Append(uuid.MustParse(row.EventUUID), row.ProjectID, row.IssueFingerprintID, row.Timestamp,
			row.Environment, row.ServerName, row.Release, row.Level, tags, row.TraceID); err != nil {
			return err
		}
	}
	return batch.Send()
}

// DeleteProjectEventRows deletes a Project's rows from the events table.
func (ch *ClickHouse) DeleteProjectEventRows(projectID uint32) error {
	ctx, cancel := context.WithTimeout(context.Background(), eventsTimeout)
	defer cancel()
	return ch.conn.Exec(ctx, "DELETE FROM events WHERE project_id = ?", projectID)
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

// eventColumns are the events table's columns, as Event reads select them.
const eventColumns = "toString(uuid), project_id, issue_fingerprint_id, timestamp, environment, server_name, release, level, tags"

// eventOrder sorts Events by time. Events of the same time are sorted by
// UUID, so that pages and neighbours never disagree on their order.
func eventOrder(desc bool) string {
	if desc {
		return "timestamp DESC, uuid DESC"
	}
	return "timestamp ASC, uuid ASC"
}

// eventPredicate builds the WHERE clause, without the keyword, that selects
// the Events matching the params' filters: Project, Issue fingerprints, tags
// and the time window. The listing, the count and the Event-with-context query
// all use it, so they can't disagree on which Events a filter selects.
func eventPredicate(params QueryParams) (string, []any) {
	clauses := []string{"project_id = ?"}
	args := []any{params.ProjectID}

	if len(params.IssueFingerprintIDs) > 0 {
		placeholders := make([]string, len(params.IssueFingerprintIDs))
		for i, id := range params.IssueFingerprintIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		clauses = append(clauses, fmt.Sprintf("issue_fingerprint_id IN (%s)", strings.Join(placeholders, ",")))
	}

	for k, v := range params.Tags {
		clauses = append(clauses, "tags[?] = ?")
		args = append(args, k, v)
	}

	// Compared with DateTime64 values only: 26.8 skips a boundary second
	// when a DateTime64 key is compared with a DateTime (#123999).
	if !params.NewerThan.IsZero() {
		clauses = append(clauses, "timestamp > toDateTime64(?, 6, 'UTC')")
		args = append(args, chsql.TimeText(params.NewerThan))
	}
	if !params.OlderThan.IsZero() {
		clauses = append(clauses, "timestamp < toDateTime64(?, 6, 'UTC')")
		args = append(args, chsql.TimeText(params.OlderThan))
	}

	return strings.Join(clauses, " AND "), args
}

// eventsReadError is ErrEventsUnavailable, with why, for a read that failed
// because ClickHouse didn't answer it, and err for one ClickHouse refused.
func eventsReadError(err error) error {
	if _, ok := clickHouseException(err); ok {
		return err
	}
	return fmt.Errorf("%w: ClickHouse doesn't answer: %v", ErrEventsUnavailable, err)
}

// QueryEvents lists the Events matching the params, sorted by time, a page
// of them when Limit or Offset is set.
func (ch *ClickHouse) QueryEvents(params QueryParams) ([]models.Event, error) {
	where, args := eventPredicate(params)
	query := "SELECT " + eventColumns + " FROM events WHERE " + where
	if params.UUID != "" {
		query += " AND uuid = toUUIDOrNull(?)"
		args = append(args, params.UUID)
	}
	query += " ORDER BY " + eventOrder(params.SortDesc) + limitClause(params.Limit, params.Offset)

	ctx, cancel := context.WithTimeout(context.Background(), eventsTimeout)
	defer cancel()
	rows, err := ch.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, eventsReadError(err)
	}
	defer rows.Close()

	events := []models.Event{}
	for rows.Next() {
		var e models.Event
		if err := rows.Scan(&e.EventUUID, &e.ProjectID, &e.IssueFingerprintID, &e.Timestamp,
			&e.Environment, &e.ServerName, &e.Release, &e.Level, &e.Tags); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, eventsReadError(err)
	}
	return events, nil
}

// limitClause pages a read: limit rows (all when 0) after the first offset.
func limitClause(limit, offset int) string {
	clause := ""
	if limit > 0 {
		clause += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset > 0 {
		clause += fmt.Sprintf(" OFFSET %d", offset)
	}
	return clause
}

// CountEvents counts the Events matching the same filters as QueryEvents,
// including the time window. Paging, sort and UUID don't apply.
func (ch *ClickHouse) CountEvents(params QueryParams) (int64, error) {
	where, args := eventPredicate(params)
	ctx, cancel := context.WithTimeout(context.Background(), eventsTimeout)
	defer cancel()
	var count uint64
	if err := ch.conn.QueryRow(ctx, "SELECT count() FROM events WHERE "+where, args...).Scan(&count); err != nil {
		return 0, eventsReadError(err)
	}
	return int64(count), nil
}

// EventWithContext holds an event with its previous and next UUIDs
type EventWithContext struct {
	Event    models.Event
	PrevUUID string
	NextUUID string
}

// QueryEventWithContext returns Events with the UUIDs of the Events before
// and after each, over the Issue's whole history. With a UUID, it returns that
// Event; without, the Limit latest or earliest by SortDesc.
func (ch *ClickHouse) QueryEventWithContext(params QueryParams) ([]EventWithContext, error) {
	// prev/next run over the Issue's whole Event sequence, so only the Project
	// and fingerprint filters apply; a time window or tags would cut it short.
	// The events/context route rejects a time window rather than drop it here.
	where, args := eventPredicate(QueryParams{
		ProjectID:           params.ProjectID,
		IssueFingerprintIDs: params.IssueFingerprintIDs,
	})
	query := `SELECT ` + eventColumns + `, prev_uuid, next_uuid FROM (
		SELECT *,
			toString(lagInFrame(toNullable(uuid)) OVER sequence) AS prev_uuid,
			toString(leadInFrame(toNullable(uuid)) OVER sequence) AS next_uuid
		FROM events
		WHERE ` + where + `
		WINDOW sequence AS (ORDER BY ` + eventOrder(false) + ` ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)
	)`
	if params.UUID != "" {
		query += " WHERE uuid = toUUIDOrNull(?)"
		args = append(args, params.UUID)
	} else {
		query += " ORDER BY " + eventOrder(params.SortDesc) + limitClause(params.Limit, 0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), eventsTimeout)
	defer cancel()
	rows, err := ch.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, eventsReadError(err)
	}
	defer rows.Close()

	results := []EventWithContext{}
	for rows.Next() {
		var c EventWithContext
		var prev, next *string
		e := &c.Event
		if err := rows.Scan(&e.EventUUID, &e.ProjectID, &e.IssueFingerprintID, &e.Timestamp,
			&e.Environment, &e.ServerName, &e.Release, &e.Level, &e.Tags, &prev, &next); err != nil {
			return nil, err
		}
		if prev != nil {
			c.PrevUUID = *prev
		}
		if next != nil {
			c.NextUUID = *next
		}
		results = append(results, c)
	}
	if err := rows.Err(); err != nil {
		return nil, eventsReadError(err)
	}
	return results, nil
}
