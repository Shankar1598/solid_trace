package storage

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/solidtrace/event_store/pkg/chsql"
)

// TableScan asks for a table worked out from a Project's Logs in a time
// range.
type TableScan struct {
	ProjectID uint32
	// From and To are the time range, [From, To).
	From, To time.Time
	// Where returns the SQL condition a Log must meet; "" keeps every Log.
	Where func(ClickHouseColumns) string
	// Select returns the select list the Logs are read with.
	Select func(ClickHouseColumns) string
	// Query returns the query that works the table out from logs, the
	// query that reads the Logs.
	Query func(logs string) string
}

// ScanLogTable runs a query over a Project's Logs in a time range and
// returns its rows. Each value is as ClickHouse gives it, with NULL as nil:
// a string, a number, a bool, a time, or a tuple's values as a []any.
// Cancelling ctx stops it.
func (s *ClickHouseLogs) ScanLogTable(ctx context.Context, scan TableScan) ([][]any, error) {
	from, to := storableRange(scan.From, scan.To)
	if !to.After(from) {
		to = from
	}
	return s.queryTable(ctx, scan.Query(tableSource(scan, from, to)))
}

// tableSource is the query that reads a table's Logs of [from, to).
func tableSource(scan TableScan, from, to time.Time) string {
	conditions := []string{
		fmt.Sprintf("project_id = %d", scan.ProjectID),
		"time >= " + chsql.Time(from), "time < " + chsql.Time(to),
	}
	if scan.Where != nil {
		if w := scan.Where(ClickHouseColumns{}); w != "" {
			conditions = append(conditions, "("+w+")")
		}
	}
	return fmt.Sprintf("SELECT %s FROM logs WHERE %s", scan.Select(ClickHouseColumns{}), strings.Join(conditions, " AND "))
}

// queryTable runs a query and reads its rows, as ScanLogTable returns them,
// recording it in ctx's ReadStatements.
func (s *ClickHouseLogs) queryTable(ctx context.Context, query string) (table [][]any, err error) {
	began := time.Now()
	defer func() { recordStatement(ctx, query, began, err) }()
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types := rows.ColumnTypes()
	table = [][]any{}
	for rows.Next() {
		dest := make([]any, len(types))
		for i, t := range types {
			dest[i] = reflect.New(t.ScanType()).Interface()
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := make([]any, len(dest))
		for i, d := range dest {
			row[i] = plainValue(reflect.ValueOf(d).Elem())
		}
		table = append(table, row)
	}
	return table, rows.Err()
}

// plainValue is a value clickhouse-go scanned, with pointers followed and
// NULL as nil, in a tuple's values too.
func plainValue(v reflect.Value) any {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Interface {
		values := make([]any, v.Len())
		for i := range values {
			values[i] = plainValue(v.Index(i))
		}
		return values
	}
	return v.Interface()
}
