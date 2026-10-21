package storage

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
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

// LogPosition is a Log's place in newest-first order: by time, then id. With
// the nil id, it is the place after every Log at its time.
type LogPosition struct {
	Time time.Time
	ID   uuid.UUID
}

// LogScan asks for a Project's newest Logs in a time range.
type LogScan struct {
	ProjectID uint32
	// From and To are the time range, [From, To).
	From, To time.Time
	// After, if set, continues a page: only Logs after it in newest-first
	// order, strictly older, are scanned.
	After *LogPosition
	// Where, if set, returns the SQL condition a Log must meet, for a source
	// whose columns are given. "" keeps every Log.
	Where func(LogColumns) string
	// Limit is how many Logs to return at most.
	Limit int
	// Budget, if set, is how long the scan may go on reading hours once it
	// has read one: past it, it returns what it has found.
	Budget time.Duration
}

// LogScanPage is what a scan found: Logs, newest first, and where the scan of
// older Logs goes on, or nil once there are none in the range.
type LogScanPage struct {
	Logs []models.Log
	Next *LogPosition
}

// ScanLogs returns up to scan.Limit of a Project's Logs that meet the
// condition, newest first, from its hot table, its late Logs and its files.
// Promoted values are merged back into the Attributes.
//
// It reads hour by hour, newest first, and stops once it has scan.Limit Logs,
// or once it has spent scan.Budget, so that a search for rare Logs answers
// with what it has found so far: then Next is the start of the last hour it
// read, and the scan goes on from there.
//
// It reads the Logs as they were when it began (logRead), so it sees each
// exactly once, and holds up no export. Between hours it checks ctx, and
// cancelling ctx stops a query.
func (s *LogStore) ScanLogs(ctx context.Context, scan LogScan) (LogScanPage, error) {
	started := time.Now()
	from, to := scan.From.UTC(), scan.To.UTC()
	if scan.After != nil && scan.After.Time.Before(to) {
		to = scan.After.Time.UTC()
		if scan.After.ID != uuid.Nil {
			// Logs at the same time as After, with a lower id, follow it.
			to = to.Add(time.Microsecond)
		}
	}
	page := LogScanPage{Logs: []models.Log{}}
	if !from.Before(to) || scan.Limit <= 0 {
		return page, nil
	}
	read, err := s.beginRead(ctx, scan.ProjectID, from, to, false)
	if err != nil {
		return LogScanPage{}, err
	}
	defer read.end()
	logs := page.Logs
	oneStep := false
	for i, hour := range read.hours {
		if err := ctx.Err(); err != nil {
			return LogScanPage{}, err
		}
		hourFrom, hourTo := hour, hour.Add(time.Hour)
		if from.After(hourFrom) {
			hourFrom = from
		}
		if to.Before(hourTo) {
			hourTo = to
		}
		found, err := read.scanHour(scan, hour, hourFrom, hourTo, scan.Limit-len(logs), oneStep)
		if err != nil {
			return LogScanPage{}, err
		}
		logs = append(logs, found...)
		if len(logs) >= scan.Limit {
			last := logs[len(logs)-1]
			return LogScanPage{Logs: logs, Next: &LogPosition{Time: last.Time, ID: last.ID}}, nil
		}
		// A whole hour held fewer Logs than the page still wanted, so they
		// are rare: later hours are read in one step, as a second would read
		// again most of the row groups holding them. An hour the range or
		// the cursor cuts short says nothing of that: the hour before it may
		// be busy.
		if hourFrom.Equal(hour) && hourTo.Equal(hour.Add(time.Hour)) {
			oneStep = true
		}
		if scan.Budget > 0 && time.Since(started) > scan.Budget && i < len(read.hours)-1 {
			return LogScanPage{Logs: logs, Next: &LogPosition{Time: hourFrom}}, nil
		}
	}
	return LogScanPage{Logs: logs}, nil
}

// scanHour returns up to limit of the Project's Logs of [from, to), within
// one hour, that meet the scan's condition, newest first.
//
// It reads in two steps. The first (newestInHour) reads only the time and id
// of the newest limit Logs that meet the condition. The second (hourLogsByID)
// reads those Logs whole, by id, from the span of time they lie in: Log files
// are sorted by time, and a hot table is close to it, so for the newest Logs
// of a busy hour it skips all but the newest row groups. The condition isn't
// run again, and an hour with no such Logs isn't read again.
//
// With oneStep, it reads whole Logs at once instead, as ScanLogs asks once a
// whole hour held fewer Logs than the page still wanted: such Logs are rare,
// and a second step would read again most of the row groups that hold them.
func (r *logRead) scanHour(scan LogScan, hour, from, to time.Time, limit int, oneStep bool) ([]models.Log, error) {
	bounds := timeRange(from, to)
	if scan.After != nil {
		bounds += fmt.Sprintf(" AND (time < %[1]s OR (time = %[1]s AND id < CAST(%[2]s AS UUID)))",
			timestampLiteral(scan.After.Time), sqltext.String(scan.After.ID.String()))
	}
	matching := func(cols LogColumns) string {
		condition := bounds
		if scan.Where != nil {
			if w := scan.Where(cols); w != "" {
				condition += " AND (" + w + ")"
			}
		}
		return condition
	}
	if oneStep {
		return r.queryLogs(fmt.Sprintf(`
			SELECT * FROM (%s)
			ORDER BY time DESC, id DESC
			LIMIT %d`, r.hourSources(hour, "*", matching), limit))
	}
	found, err := r.newestInHour(hour, matching, limit)
	if err != nil || len(found.ids) == 0 {
		return nil, err
	}
	return r.hourLogsByID(hour, found)
}

// foundLogs are Logs newestInHour found: their ids, as SQL values, newest
// first, and the times of the oldest and the newest.
type foundLogs struct {
	ids            []string
	oldest, newest time.Time
}

// newestInHour finds the newest limit of hour's Logs that meet where, reading
// only their time and id.
func (r *logRead) newestInHour(hour time.Time, where func(LogColumns) string, limit int) (foundLogs, error) {
	rows, err := queryRead(r.ctx, r.conn, fmt.Sprintf(`
		SELECT time, id FROM (%s)
		ORDER BY time DESC, id DESC
		LIMIT %d`, r.hourSources(hour, "time, id", where), limit))
	if err != nil {
		return foundLogs{}, err
	}
	defer rows.Close()
	var found foundLogs
	for rows.Next() {
		var at time.Time
		var id uuid.UUID
		if err := rows.Scan(&at, &id); err != nil {
			return foundLogs{}, err
		}
		if len(found.ids) == 0 {
			found.newest = at
		}
		found.oldest = at
		found.ids = append(found.ids, "CAST("+sqltext.String(id.String())+" AS UUID)")
	}
	return found, rows.Err()
}

// hourLogsByID reads the Logs newestInHour found in hour whole, newest first.
func (r *logRead) hourLogsByID(hour time.Time, found foundLogs) ([]models.Log, error) {
	// The span skips row groups. The ids pick the Logs with list_contains,
	// which DuckDB runs in the scan, so it reads the other columns only
	// where they match: an IN list becomes a join after the scan, which
	// reads every column of the span. The LIMIT keeps to the Logs found,
	// should an id be in two sources.
	span := fmt.Sprintf("time >= %s AND time <= %s", timestampLiteral(found.oldest), timestampLiteral(found.newest))
	return r.queryLogs(fmt.Sprintf(`
		SELECT * FROM (%s)
		WHERE list_contains([%s], id)
		ORDER BY time DESC, id DESC
		LIMIT %d`,
		r.hourSources(hour, "*", func(LogColumns) string { return span }), strings.Join(found.ids, ", "), len(found.ids)))
}

// hourSources returns, as SQL, hour's Logs that meet where, from the
// Project's late Logs, its hot table and the hour's Log files. Each source is
// read with the select list sel; "*" reads whole Logs.
func (r *logRead) hourSources(hour time.Time, sel string, where func(LogColumns) string) string {
	lateSel := sel
	if sel == "*" {
		// The late Logs' table also holds each Log's project_id.
		lateSel = "* EXCLUDE (project_id)"
	}
	sources := []string{fmt.Sprintf("SELECT %s FROM %s.%s WHERE project_id = %d AND %s",
		lateSel, logsCatalog, lateTable, r.projectID, where(LogColumns{late: true}))}
	set := r.promotedSet(hour)
	promoted := LogColumns{set: set}
	if r.hot {
		sources = append(sources, fmt.Sprintf("SELECT %s FROM (%s) WHERE %s",
			sel, withColumns(fmt.Sprintf("SELECT * FROM %s.%s", logsCatalog, hotTable(r.projectID)), set), where(promoted)))
	}
	if files := r.files[hour]; len(files) > 0 {
		sources = append(sources, fmt.Sprintf("SELECT %s FROM (%s) WHERE %s",
			sel, withColumns(readFiles(files), set), where(promoted)))
	}
	return strings.Join(sources, "\nUNION ALL BY NAME\n")
}

// IngestedLogScan asks for a Project's Logs ingested after a Log batch: those
// of the later batches Log processing has stored, whatever their time.
type IngestedLogScan struct {
	ProjectID uint32
	// After is the Log batch the scan starts after, the nil UUID for the
	// first.
	After uuid.UUID
	// Where, if set, returns the SQL condition a Log must meet, for a source
	// whose columns are given. "" keeps every Log.
	Where func(LogColumns) string
	// Limit is how many Logs to return at most.
	Limit int
}

// IngestedLogs is what an IngestedLogScan found.
type IngestedLogs struct {
	// Logs are up to Limit of the Logs, newest first.
	Logs []models.Log
	// Total counts every Log the scan found, past Limit too.
	Total int
	// Cursor is the Project's Log processing cursor when the scan began:
	// the scan read the batches after After up to it, and the next scan
	// starts after it. It is the nil UUID while Log processing has stored
	// nothing for the Project.
	Cursor uuid.UUID
}

// ScanLogsIngestedAfter returns the Logs of a Project's Log batches after
// scan.After, up to its Log processing cursor, that meet the condition: from
// its hot table, its late Logs and the files that hold a later batch, as
// batch_max says. A Log stored before the batch column has no batch, and is
// never read.
//
// Within a Project, Log processing stores batches in id order, and commits
// its cursor with them, so a scan that starts after the last one's cursor
// reads each Log exactly once, even one Log processing stored after a Log
// with a later id. The cursor is read in the transaction that reads the Logs
// (logRead), so a batch past it, stored while the scan runs, waits for the
// next scan. Cancelling ctx stops it.
func (s *LogStore) ScanLogsIngestedAfter(ctx context.Context, scan IngestedLogScan) (IngestedLogs, error) {
	read, err := s.beginIngestedRead(ctx, scan.ProjectID, scan.After)
	if err != nil {
		return IngestedLogs{}, err
	}
	defer read.end()
	found := IngestedLogs{Logs: []models.Log{}, Cursor: read.cursor}
	if !batchesBetween(scan.After, read.cursor) {
		return found, nil
	}
	window := batchWindow(scan.After, read.cursor)
	where := func(cols LogColumns) string {
		condition := window
		if scan.Where != nil {
			if w := scan.Where(cols); w != "" {
				condition += " AND (" + w + ")"
			}
		}
		return condition
	}
	from, to := time.Unix(0, 0).UTC(), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	logs := read.sources(from, to, func(cols LogColumns) string {
		if cols.late {
			return "* EXCLUDE (project_id)"
		}
		return "*"
	}, where)
	if scan.Limit > 0 {
		if found.Logs, err = read.queryLogs(fmt.Sprintf("SELECT * FROM (%s) ORDER BY time DESC, id DESC LIMIT %d", logs, scan.Limit)); err != nil {
			return IngestedLogs{}, err
		}
		if found.Logs == nil {
			found.Logs = []models.Log{}
		}
	}
	if found.Total = len(found.Logs); found.Total < scan.Limit {
		return found, nil
	}
	counted := read.sources(from, to, func(LogColumns) string { return "id" }, where)
	if err := queryReadRow(ctx, read.conn, fmt.Sprintf("SELECT count(*) FROM (%s)", counted), &found.Total); err != nil {
		return IngestedLogs{}, err
	}
	return found, nil
}

// batchesBetween reports whether a Log batch can come after the batch after
// and no later than upTo. Log batch ids are made in the order of their
// bytes, which is how DuckDB orders UUIDs too.
func batchesBetween(after, upTo uuid.UUID) bool {
	return bytes.Compare(after[:], upTo[:]) < 0
}

// batchWindow is the SQL condition that a Log's batch comes after the batch
// after and no later than upTo.
func batchWindow(after, upTo uuid.UUID) string {
	return fmt.Sprintf("batch > CAST(%s AS UUID) AND batch <= CAST(%s AS UUID)",
		sqltext.String(after.String()), sqltext.String(upTo.String()))
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

// timeSpan is the time range [from, to).
type timeSpan struct {
	from, to time.Time
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

// LogByID returns a Project's Log with the given id, if it has one. If at is
// set, only the Log files of at's hour are searched, wherever they are; else
// every file on local disk, as searching every file in the bucket would read
// them all.
func (s *LogStore) LogByID(ctx context.Context, projectID uint32, id uuid.UUID, at *time.Time) (models.Log, bool, error) {
	from, to := time.Unix(0, 0).UTC(), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if at != nil {
		from = at.UTC().Truncate(time.Hour)
		to = from.Add(time.Hour)
	}
	read, err := s.beginRead(ctx, projectID, from, to, at == nil)
	if err != nil {
		return models.Log{}, false, err
	}
	defer read.end()

	var files []string
	for _, hour := range read.hours {
		files = append(files, read.files[hour]...)
	}
	match := "id = CAST(" + sqltext.String(id.String()) + " AS UUID)"
	sources := []string{fmt.Sprintf("SELECT * EXCLUDE (project_id) FROM %s.%s WHERE project_id = %d AND %s",
		logsCatalog, lateTable, projectID, match)}
	if read.hot {
		sources = append(sources, fmt.Sprintf("SELECT * FROM %s.%s WHERE %s", logsCatalog, hotTable(projectID), match))
	}
	if len(files) > 0 {
		sources = append(sources, fmt.Sprintf("SELECT * FROM (%s) WHERE %s", readFiles(files), match))
	}
	logs, err := read.queryLogs(fmt.Sprintf("SELECT * FROM (%s) LIMIT 1", strings.Join(sources, "\nUNION ALL BY NAME\n")))
	if err != nil || len(logs) == 0 {
		return models.Log{}, false, err
	}
	return logs[0], true, nil
}
