package storage

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/sqltext"
)

// LogsView is the name of the view SQL queries read a Project's Logs from.
// Its columns are project_id, id, the seven built-in fields and the four
// Attribute maps, every Attribute in its map: users' SQL never sees promoted
// columns, so storage changes don't break it. docs/logs/sql.md documents it.
const LogsView = "logs"

// LogViewColumns describes the logs view's columns, so that a Query can be
// written as SQL against it: every Attribute is in its map.
func LogViewColumns() LogColumns {
	return LogColumns{late: true, view: true}
}

// SQLColumn is a column of a SQL query's result: its name, and its DuckDB
// type, such as VARCHAR or MAP(VARCHAR, DOUBLE).
type SQLColumn struct {
	Name string
	Type string
}

// SQLResult is what a SQL query returned: its columns, and its rows as JSON
// values (see jsonEncodable).
type SQLResult struct {
	Columns []SQLColumn
	Rows    [][]any
	// Truncated is set when the query returned more rows than were read.
	Truncated bool
}

// SQLError is a SQL query's own failure, such as a mistake in it, as opposed
// to a failure to run it.
type SQLError struct {
	Err error
}

func (e *SQLError) Error() string { return e.Err.Error() }
func (e *SQLError) Unwrap() error { return e.Err }

// RunSQL runs a read-only SQL query as written (ADR 0003), with the logs
// view holding a Project's Logs of [from, to): from its hot table, its late
// Logs and its files, promoted values merged back into their maps. The
// events view holds the Project's Events of [from, to). It reads at most
// limit rows. SQL that isn't one SELECT, that calls a function that changes
// something (checkReadOnly), or that reads anything but the two views
// (checkScope), is refused before any of it runs. If the query is refused
// or itself fails, the error is a *SQLError.
//
// The views are temporary views on the read's own connection, which is
// closed afterwards, so nothing the query leaves on it (temporary objects,
// an open transaction) reaches another query. Like ScanLogTable, the query
// runs in the read's transaction, so that the view sees each Log exactly
// once and the query holds up no export. Cancelling ctx stops it.
func (s *LogStore) RunSQL(ctx context.Context, projectID uint32, from, to time.Time, query string, limit int) (SQLResult, error) {
	statement, err := s.checkReadOnly(ctx, query)
	if err != nil {
		return SQLResult{}, err
	}
	readsEvents, err := s.checkScope(ctx, statement)
	if err != nil {
		return SQLResult{}, err
	}
	from, to = from.UTC(), to.UTC()
	if !to.After(from) {
		to = from
	}
	// The query may read the events view, so it holds off the deletion of
	// the Event archive's uploaded local copies until it ends.
	defer s.events.readingArchive()()
	read, err := s.beginRead(ctx, projectID, from, to, false)
	if err != nil {
		return SQLResult{}, err
	}
	read.discard = true
	defer read.end()

	logs := read.sources(from, to, func(cols LogColumns) string {
		return viewSelect(projectID, cols)
	}, nil)
	conn := read.conn
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE TEMPORARY VIEW %s AS %s", LogsView, logs)); err != nil {
		return SQLResult{}, err
	}
	// Only a query that reads the events view gets one, so a query that
	// doesn't still runs while there is no global events view.
	if readsEvents {
		if err := createEventsView(ctx, conn, projectID, from, to); err != nil {
			if s.events.eventsUnavailable() != nil {
				return SQLResult{}, &SQLError{s.withEventsUnavailable(err)}
			}
			return SQLResult{}, err
		}
	}
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		// The query may have failed for want of the events view.
		return SQLResult{}, &SQLError{s.withEventsUnavailable(err)}
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return SQLResult{}, &SQLError{err}
	}
	result := SQLResult{Columns: make([]SQLColumn, len(types))}
	for i, t := range types {
		result.Columns[i] = SQLColumn{Name: t.Name(), Type: t.DatabaseTypeName()}
	}
	if result.Rows, err = scanTable(rows, limit); err != nil {
		return SQLResult{}, &SQLError{err}
	}
	result.Truncated = rows.Next()
	if err := rows.Err(); err != nil {
		return SQLResult{}, &SQLError{err}
	}
	for _, row := range result.Rows {
		for i, v := range row {
			if b, ok := v.([]byte); ok && result.Columns[i].Type == "UUID" {
				if id, err := uuid.FromBytes(b); err == nil {
					row[i] = id.String()
					continue
				}
			}
			row[i] = jsonEncodable(v)
		}
	}
	return result, nil
}

// withEventsUnavailable is err with why Events can't be read now, while
// they can't.
func (s *LogStore) withEventsUnavailable(err error) error {
	if unavailable := s.events.eventsUnavailable(); unavailable != nil {
		return fmt.Errorf("%w\n%v", err, unavailable)
	}
	return err
}

// jsonEncodable turns any value DuckDB gives into one that encodes as JSON: a
// time as RFC 3339 in UTC, a decimal as a number, a map as an object with its
// keys as text, NaN and infinities as null. A JSON column's values come
// decoded already.
func jsonEncodable(v any) any {
	switch v := v.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
	case float32:
		return jsonEncodable(float64(v))
	case duckdb.Decimal:
		return v.Float64()
	case duckdb.Union:
		return jsonEncodable(v.Value)
	case duckdb.Map:
		object := make(map[string]any, len(v))
		for key, value := range v {
			object[fmt.Sprint(jsonEncodable(key))] = jsonEncodable(value)
		}
		return object
	case map[string]any:
		object := make(map[string]any, len(v))
		for key, value := range v {
			object[key] = jsonEncodable(value)
		}
		return object
	case []any:
		list := make([]any, len(v))
		for i, value := range v {
			list[i] = jsonEncodable(value)
		}
		return list
	}
	return v
}

// viewSelect is the select list that reads a source of a Project's Logs, whose
// columns cols describes, as the logs view's columns.
func viewSelect(projectID uint32, cols LogColumns) string {
	list := []string{fmt.Sprintf("CAST(%d AS UINTEGER) AS project_id", projectID), "id"}
	for _, name := range []string{"time", "level", "message", "service", "environment", "trace_id", "span_id"} {
		list = append(list, sqltext.Identifier(name))
	}
	for _, t := range []AttributeType{StringAttribute, NumberAttribute, BoolAttribute, JSONAttribute} {
		list = append(list, mergedMap(t, cols)+" AS "+t.Map())
	}
	return strings.Join(list, ", ")
}

// mergedMap is the map of the Attributes of type t, with the values of
// promoted keys taken from their columns.
func mergedMap(t AttributeType, cols LogColumns) string {
	if cols.late {
		return t.Map()
	}
	var entries []string
	for _, key := range cols.set.Keys {
		if key.Type == t {
			entries = append(entries, fmt.Sprintf("{'key': %s, 'value': %s}", sqltext.String(key.Key), sqltext.Identifier(key.Column())))
		}
	}
	if len(entries) == 0 {
		return t.Map()
	}
	return fmt.Sprintf("map_concat(%s, map_from_entries(list_filter([%s], lambda e: e.value IS NOT NULL)))",
		t.Map(), strings.Join(entries, ", "))
}
