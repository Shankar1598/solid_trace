package storage

import (
	"context"
	"database/sql/driver"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
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
// values (see jsonValue).
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

// RunSQL runs a SQL query exactly as written (ADR 0003), with the logs view
// holding a Project's Logs of [from, to): from its hot table, its late Logs
// and its files, promoted values merged back into their maps. It reads at
// most limit rows. If the query itself fails, the error is a *SQLError.
//
// The view is a temporary view on the query's own connection, which is
// closed afterwards, so nothing the query leaves on it (temporary objects,
// an open transaction) reaches another query. Like ScanLogTable, it runs while
// no export can publish, so that the view sees each Log exactly once.
func (s *LogStore) RunSQL(ctx context.Context, projectID uint32, from, to time.Time, query string, limit int) (SQLResult, error) {
	from, to = from.UTC(), to.UTC()
	if !to.After(from) {
		to = from
	}
	s.publishMu.RLock()
	defer s.publishMu.RUnlock()

	logs, err := s.logSourcesLocked(projectID, from, to, func(cols LogColumns) string {
		return viewSelect(projectID, cols)
	}, nil)
	if err != nil {
		return SQLResult{}, err
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return SQLResult{}, err
	}
	// Returning ErrBadConn takes the connection out of the pool and closes it.
	defer conn.Raw(func(any) error { return driver.ErrBadConn })

	if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE TEMPORARY VIEW %s AS %s", LogsView, logs)); err != nil {
		return SQLResult{}, err
	}
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return SQLResult{}, &SQLError{err}
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
			row[i] = jsonValue(v)
		}
	}
	return result, nil
}

// jsonValue turns a value as DuckDB gives it into one that encodes as JSON: a
// time as RFC 3339 in UTC, a decimal as a number, a map as an object with its
// keys as text, NaN and infinities as null. A JSON column's values come
// decoded already.
func jsonValue(v any) any {
	switch v := v.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
	case float32:
		return jsonValue(float64(v))
	case duckdb.Decimal:
		return v.Float64()
	case duckdb.Union:
		return jsonValue(v.Value)
	case duckdb.Map:
		object := make(map[string]any, len(v))
		for key, value := range v {
			object[fmt.Sprint(jsonValue(key))] = jsonValue(value)
		}
		return object
	case map[string]any:
		object := make(map[string]any, len(v))
		for key, value := range v {
			object[key] = jsonValue(value)
		}
		return object
	case []any:
		list := make([]any, len(v))
		for i, value := range v {
			list[i] = jsonValue(value)
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
		list = append(list, sqlIdentifier(name))
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
			entries = append(entries, fmt.Sprintf("{'key': %s, 'value': %s}", sqlString(key.Key), sqlIdentifier(key.Column())))
		}
	}
	if len(entries) == 0 {
		return t.Map()
	}
	return fmt.Sprintf("map_concat(%s, map_from_entries(list_filter([%s], lambda e: e.value IS NOT NULL)))",
		t.Map(), strings.Join(entries, ", "))
}
