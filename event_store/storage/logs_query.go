package storage

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/solidtrace/event_store/pkg/sqltext"
)

// LogColumns says where one source of a Project's Logs keeps each Field, so
// that a Query can be written as SQL against it without reading any table's
// or file's schema. Its answers are SQL expressions.
//
// A hot table and the files hold a Log's promoted Attributes in the columns
// of its day's promoted set, and the rest in the maps; logs_late holds every
// Attribute in the maps.
type LogColumns struct {
	set  PromotedSet
	late bool
	// view is set for the logs view, read by people: maps only, written
	// map['key'].
	view bool
}

// BuiltIn is a built-in field's column, such as level.
func (c LogColumns) BuiltIn(name string) string {
	return sqltext.Identifier(name)
}

// Map is the map that holds the Attributes of type t that aren't promoted.
func (c LogColumns) Map(t AttributeType) string {
	return t.Map()
}

// Attribute is the value of the Attribute key of type t, NULL for a Log
// without one: the key's column if it is promoted, coalesced with its map
// if it was promoted partway through the day, else its map.
func (c LogColumns) Attribute(t AttributeType, key string) string {
	if c.view {
		return fmt.Sprintf("%s[%s]", t.Map(), sqltext.String(key))
	}
	inMap := fmt.Sprintf("map_extract_value(%s, %s)", t.Map(), sqltext.String(key))
	if c.late {
		return inMap
	}
	promoted, ok := c.set.Lookup(t, key)
	if !ok {
		return inMap
	}
	column := sqltext.Identifier(promoted.Column())
	if promoted.Partway {
		return fmt.Sprintf("coalesce(%s, %s)", column, inMap)
	}
	return column
}

// Promoted lists the columns of the promoted Attributes of type t.
func (c LogColumns) Promoted(t AttributeType) []string {
	if c.late {
		return nil
	}
	var columns []string
	for _, key := range c.set.Keys {
		if key.Type == t {
			columns = append(columns, sqltext.Identifier(key.Column()))
		}
	}
	return columns
}

// SearchText is every string Attribute's value, joined by \0: the map's
// values and the promoted string columns.
func (c LogColumns) SearchText() string {
	parts := append([]string{fmt.Sprintf("array_to_string(map_values(%s), chr(0))", c.Map(StringAttribute))}, c.Promoted(StringAttribute)...)
	return "concat_ws(chr(0), " + strings.Join(parts, ", ") + ")"
}

// LogTableScan asks for a table worked out from a Project's Logs in a time
// range.
type LogTableScan struct {
	ProjectID uint32
	// From and To are the time range, [From, To).
	From, To time.Time
	// Where returns the SQL condition a Log must meet, for a source whose
	// columns are given; "" keeps every Log.
	Where func(LogColumns) string
	// Select returns the select list each source's Logs are read with. Every
	// source's list must give the same columns.
	Select func(LogColumns) string
	// Query returns the query that works the table out from logs, the Logs
	// read, as SQL to use as a subquery.
	Query func(logs string) string
	// Bins, if set, says the table is made bin by bin, so that bins of
	// Logs all in files can come from the bin cache.
	Bins *TableBins
}

// ScanLogTable runs a query over a Project's Logs in a time range, from its
// hot table, its late Logs and its files, and returns its rows.
//
// The query runs once over the whole range, as the Logs were when it began
// (logRead), so it sees each Log exactly once and holds up no export. With
// Bins and the bin cache on, over a range with a file, it runs over only the
// bins the cache lacks (scanBinnedTable). Cancelling ctx stops it.
func (s *LogStore) ScanLogTable(ctx context.Context, scan LogTableScan) ([][]any, error) {
	from, to := scan.From.UTC(), scan.To.UTC()
	if !to.After(from) {
		to = from
	}
	read, err := s.beginRead(ctx, scan.ProjectID, from, to, false)
	if err != nil {
		return nil, err
	}
	defer read.end()

	// A bin the cache keeps holds no hot or late Log, so in a range with no
	// file, such as the explorer's last hour, it holds no Log at all: the
	// query works it out for nothing, and the cache is skipped, with the
	// query of which bins hold hot or late Logs.
	if cache := s.bins.Load(); cache != nil && scan.Bins != nil && from.Before(to) && len(read.files) > 0 {
		return read.scanBinnedTable(scan, cache, from, to)
	}
	return read.queryTable(scan, []timeSpan{{from, to}})
}

// sources returns, as SQL, the Project's Logs of [from, to) from its hot
// table, its late Logs and its files. Each source is read with the select
// list sel(cols), and filtered by where(cols) if where is set, for its
// columns cols.
func (r *logRead) sources(from, to time.Time, sel, where func(LogColumns) string) string {
	return r.sourcesIn([]timeSpan{{from, to}}, sel, where)
}

// sourcesIn is sources over the Logs of spans, which are in time order and
// apart, reading only the files of the hours they cover.
func (r *logRead) sourcesIn(spans []timeSpan, sel, where func(LogColumns) string) string {
	projectID := r.projectID
	// within is the condition that time is in one of the spans, clipped to
	// [from, to), or "" for none. It is in parentheses, so a where can be
	// added to it with AND.
	within := func(from, to time.Time) string {
		var ranges []string
		for _, span := range spans {
			if spanFrom, spanTo := latestTime(span.from, from), earliestTime(span.to, to); spanFrom.Before(spanTo) {
				ranges = append(ranges, timeRange(spanFrom, spanTo))
			}
		}
		if len(ranges) == 0 {
			return ""
		}
		return "((" + strings.Join(ranges, ") OR (") + "))"
	}
	source := func(table string, cols LogColumns, times string) string {
		condition := times
		if where != nil {
			if w := where(cols); w != "" {
				condition += " AND (" + w + ")"
			}
		}
		return fmt.Sprintf("SELECT %s FROM (%s) WHERE %s", sel(cols), table, condition)
	}
	// The late Logs are always read, so the query is valid SQL even when
	// the spans hold no time, as when a where on time leaves no range.
	first, last := spans[0].from, spans[len(spans)-1].to
	sources := []string{source(fmt.Sprintf("SELECT * FROM %s.%s WHERE project_id = %d", logsCatalog, lateTable, projectID),
		LogColumns{late: true}, cmp.Or(within(first, last), "false"))}

	// The hot table and the files, a day at a time: each day by its own
	// promoted set.
	days := map[time.Time][]time.Time{}
	var dayOrder []time.Time
	for _, hour := range r.hours {
		day := LogDay(hour)
		if _, ok := days[day]; !ok {
			dayOrder = append(dayOrder, day)
		}
		days[day] = append(days[day], hour)
	}
	for _, day := range dayOrder {
		times := within(day, day.Add(24*time.Hour))
		if times == "" {
			continue
		}
		set := r.promotedSet(day)
		cols := LogColumns{set: set}
		if r.hot {
			sources = append(sources, source(withColumns(fmt.Sprintf("SELECT * FROM %s.%s", logsCatalog, hotTable(projectID)), set),
				cols, times))
		}
		var files []string
		for _, hour := range days[day] {
			if within(hour, hour.Add(time.Hour)) != "" {
				files = append(files, r.files[hour]...)
			}
		}
		if len(files) > 0 {
			sources = append(sources, source(withColumns(readFiles(files), set), cols, times))
		}
	}
	return strings.Join(sources, "\nUNION ALL BY NAME\n")
}

// latestTime and earliestTime are the later and the earlier of two times.
func latestTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func earliestTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// scanTable reads every row of rows as DuckDB gives its values, or at most
// limit rows if limit isn't negative.
func scanTable(rows *sql.Rows, limit int) ([][]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	table := [][]any{}
	for (limit < 0 || len(table) < limit) && rows.Next() {
		row := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range row {
			pointers[i] = &row[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		table = append(table, row)
	}
	return table, rows.Err()
}

// withColumns makes sure the rows of source have a column for each key of a
// promoted set, NULL where source has none. A hot table drops a column when
// the key is demoted, and a file written before a key joined its day's set
// lacks it; neither holds a value for it then.
func withColumns(source string, set PromotedSet) string {
	if len(set.Keys) == 0 {
		return source
	}
	columns := make([]string, len(set.Keys))
	for i, key := range set.Keys {
		columns[i] = fmt.Sprintf("CAST(NULL AS %s) AS %s", key.Type.columnType(), sqltext.Identifier(key.Column()))
	}
	return fmt.Sprintf("%s UNION ALL BY NAME SELECT %s WHERE false", source, strings.Join(columns, ", "))
}

// readFiles reads Log files, their columns matched by name.
func readFiles(files []string) string {
	quoted := make([]string, len(files))
	for i, file := range files {
		quoted[i] = sqltext.String(file)
	}
	return fmt.Sprintf("SELECT * FROM read_parquet([%s], union_by_name = true, hive_partitioning = false)",
		strings.Join(quoted, ", "))
}

// timeRange is the SQL condition time in [from, to).
func timeRange(from, to time.Time) string {
	return fmt.Sprintf("time >= %s AND time < %s", timestampLiteral(from), timestampLiteral(to))
}

func timestampLiteral(t time.Time) string {
	return fmt.Sprintf("CAST(%s AS TIMESTAMP)", sqltext.String(timestampString(t)))
}
