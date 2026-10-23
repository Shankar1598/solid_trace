package storage

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/chcol"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/chsql"
)

// ClickHouseColumns says where the logs table keeps each Field, so that a
// Query can be written as SQL against it. Its answers are SQL expressions.
type ClickHouseColumns struct{}

// BuiltIn is a built-in field's column, such as level.
func (ClickHouseColumns) BuiltIn(name string) string {
	return chsql.Identifier(name)
}

// Attribute is the value of the Attribute key of type t, NULL for a Log
// without one: the typed subcolumn of its path, never the path's Dynamic
// value, which throws on a path that holds values of more than one type, or
// its JSON text in attrs_json.
func (ClickHouseColumns) Attribute(t AttributeType, key string) string {
	path := "attributes." + chsql.Identifier(key)
	switch t {
	case StringAttribute:
		return path + ".:String"
	case NumberAttribute:
		return path + ".:Float64"
	case BoolAttribute:
		return path + ".:Bool"
	}
	name := chsql.String(key)
	return "if(mapContains(attrs_json, " + name + "), attrs_json[" + name + "], NULL)"
}

// SearchText is every string Attribute's value, joined by \0, which search
// reads besides message.
func (ClickHouseColumns) SearchText() string {
	return "search_text"
}

// StringColumn is a column that holds the string Attribute key's value, or ”
// for a Log whose key isn't a string, if there is one: file, a sort column,
// so that a filter on it skips other files.
func (ClickHouseColumns) StringColumn(key string) (string, bool) {
	if key == "file" {
		return "file", true
	}
	return "", false
}

// logSelectList is the columns a Log is read whole from.
const logSelectList = "id, time, level, message, service, environment, trace_id, span_id, attributes, attrs_json"

// logsNewestFirst orders Logs newest first: by time, then by id, in the order of
// its bytes.
var logsNewestFirst = "time DESC, " + chsql.UUIDOrder("id") + " DESC"

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
	// Where, if set, returns the SQL condition a Log must meet. "" keeps
	// every Log.
	Where func(ClickHouseColumns) string
	// Limit is how many Logs to return at most.
	Limit int
	// Budget, if set, is how long the scan may go on reading spans of time
	// once it has read one: past it, it returns what it has found.
	Budget time.Duration
}

// LogScanPage is what a scan found: Logs, newest first, and where the scan of
// older Logs goes on, or nil once there are none in the range.
type LogScanPage struct {
	Logs []models.Log
	Next *LogPosition
}

// ScanLogs returns up to scan.Limit of a Project's Logs that meet the
// condition, newest first.
//
// It reads spans of time, newest first: the hour of the newest Log in range,
// then spans of one hour, two, four and so on, each one query that reads the
// newest Logs whole at once (lazy materialization). It stops once it has
// scan.Limit Logs, or once it has spent scan.Budget, so that a search for rare
// Logs answers with what it has found so far: then Next is the start of the
// last span it read, and the scan goes on from there. Cancelling ctx stops
// it.
func (s *ClickHouseLogs) ScanLogs(ctx context.Context, scan LogScan) (LogScanPage, error) {
	started := time.Now()
	from, to := storableRange(scan.From, scan.To)
	page := LogScanPage{Logs: []models.Log{}}
	if scan.After != nil && scan.After.Time.Before(to) {
		to = scan.After.Time.UTC()
		if scan.After.ID != uuid.Nil {
			// Logs at the same time as After, with a lower id, follow it.
			to = to.Add(time.Microsecond)
		}
	}
	if !from.Before(to) || scan.Limit <= 0 {
		return page, nil
	}
	oldest, newest, ok, err := s.timeSpan(ctx, scan.ProjectID, from, to)
	if err != nil || !ok {
		return page, err
	}
	from = latestTime(from, oldest)
	to = earliestTime(to, newest.Add(time.Microsecond))

	condition := func(spanFrom, spanTo time.Time) string {
		conditions := []string{
			fmt.Sprintf("project_id = %d", scan.ProjectID),
			"time >= " + chsql.Time(spanFrom), "time < " + chsql.Time(spanTo),
		}
		if scan.After != nil && scan.After.ID != uuid.Nil {
			at := chsql.Time(scan.After.Time)
			conditions = append(conditions, fmt.Sprintf("(time < %s OR %s < %s)",
				at, chsql.UUIDOrder("id"), chsql.UUIDOrder(chsql.UUID(scan.After.ID))))
		}
		if scan.Where != nil {
			if w := scan.Where(ClickHouseColumns{}); w != "" {
				conditions = append(conditions, "("+w+")")
			}
		}
		return strings.Join(conditions, " AND ")
	}
	span := time.Hour
	for spanTo := to; spanTo.After(from); {
		spanFrom := spanTo.Add(-time.Microsecond).Truncate(time.Hour)
		if spanTo != to {
			spanFrom = spanTo.Add(-span)
			span = min(2*span, maxScanSpan)
		}
		spanFrom = latestTime(spanFrom, from)
		found, err := s.queryLogs(ctx, fmt.Sprintf("SELECT %s FROM logs WHERE %s ORDER BY %s LIMIT %d",
			logSelectList, condition(spanFrom, spanTo), logsNewestFirst, scan.Limit-len(page.Logs)))
		if err != nil {
			return LogScanPage{}, err
		}
		page.Logs = append(page.Logs, found...)
		if len(page.Logs) >= scan.Limit {
			last := page.Logs[len(page.Logs)-1]
			page.Next = &LogPosition{Time: last.Time, ID: last.ID}
			return page, nil
		}
		if scan.Budget > 0 && time.Since(started) > scan.Budget && spanFrom.After(from) {
			page.Next = &LogPosition{Time: spanFrom}
			return page, nil
		}
		spanTo = spanFrom
	}
	return page, nil
}

// maxScanSpan is the longest span of time ScanLogs reads in one query.
const maxScanSpan = 24 * time.Hour

// storableRange is [from, to) within the times a Log can be stored at.
func storableRange(from, to time.Time) (time.Time, time.Time) {
	return latestTime(from.UTC(), firstStorableTime), earliestTime(to.UTC(), endOfStorableTime)
}

// timeSpan returns the times of the oldest and the newest of a Project's Logs
// on the days that [from, to) touches, and false if it has none. It reads
// only the parts' metadata.
func (s *ClickHouseLogs) timeSpan(ctx context.Context, projectID uint32, from, to time.Time) (oldest, newest time.Time, ok bool, err error) {
	var count uint64
	err = s.conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(), min(time), max(time) FROM logs
		WHERE project_id = %d AND toDate(time) >= toDate(%s) AND toDate(time) <= toDate(%s)`,
		projectID, chsql.Time(from), chsql.Time(to.Add(-time.Microsecond)))).Scan(&count, &oldest, &newest)
	return oldest.UTC(), newest.UTC(), count > 0, err
}

// queryLogs runs a query of logSelectList's columns and reads its Logs.
func (s *ClickHouseLogs) queryLogs(ctx context.Context, query string) ([]models.Log, error) {
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []models.Log
	for rows.Next() {
		log, err := scanLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// scanLog reads a row of logSelectList's columns as a Log.
func scanLog(rows driver.Rows) (models.Log, error) {
	var (
		log                                 models.Log
		level, environment, traceID, spanID *string
		attributes                          chcol.JSON
		attrsJSON                           map[string]string
	)
	if err := rows.Scan(&log.ID, &log.Time, &level, &log.Message, &log.Service, &environment, &traceID, &spanID, &attributes, &attrsJSON); err != nil {
		return models.Log{}, err
	}
	log.Time = log.Time.UTC()
	log.Level, log.Environment, log.TraceID, log.SpanID = stringOf(level), stringOf(environment), stringOf(traceID), stringOf(spanID)
	log.Attributes = models.Attributes{String: map[string]string{}, Number: map[string]float64{}, Bool: map[string]bool{}, JSON: attrsJSON}
	if log.Attributes.JSON == nil {
		log.Attributes.JSON = map[string]string{}
	}
	for path, value := range attributes.ValuesByPath() {
		if dynamic, ok := value.(chcol.Dynamic); ok {
			value = dynamic.Any()
		}
		switch v := value.(type) {
		case string:
			log.Attributes.String[path] = v
		case float64:
			log.Attributes.Number[path] = v
		case bool:
			log.Attributes.Bool[path] = v
		}
	}
	return log, nil
}

func stringOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// IngestedLogScan asks for a Project's Logs ingested after a Log batch: those
// of the later batches Log processing has stored, whatever their time.
type IngestedLogScan struct {
	ProjectID uint32
	// After is the Log batch the scan starts after, the nil UUID for the
	// first.
	After uuid.UUID
	// Where, if set, returns the SQL condition a Log must meet. "" keeps
	// every Log.
	Where func(ClickHouseColumns) string
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
// scan.After, up to its Log processing cursor, that meet the condition,
// whatever their time. The batch index skips the parts and granules of Logs
// ingested before After.
//
// Within a Project, Log processing inserts batches in id order, and moves its
// cursor only once their insert has returned, so a scan that reads the
// cursor first, and then the batches up to it, reads each Log exactly once,
// even one Log processing stored after a Log with a later id. A batch past
// the cursor, stored while the scan runs, waits for the next scan. Cancelling
// ctx stops it.
func (s *ClickHouseLogs) ScanLogsIngestedAfter(ctx context.Context, scan IngestedLogScan) (IngestedLogs, error) {
	cursor, err := s.pebble.LogProcessingCursor(scan.ProjectID)
	if err != nil {
		return IngestedLogs{}, err
	}
	found := IngestedLogs{Logs: []models.Log{}, Cursor: cursor}
	if bytes.Compare(scan.After[:], cursor[:]) >= 0 {
		return found, nil
	}
	conditions := []string{
		fmt.Sprintf("project_id = %d", scan.ProjectID),
		chsql.UUIDOrder("batch") + " > " + chsql.UUIDOrder(chsql.UUID(scan.After)),
		chsql.UUIDOrder("batch") + " <= " + chsql.UUIDOrder(chsql.UUID(cursor)),
	}
	if scan.Where != nil {
		if w := scan.Where(ClickHouseColumns{}); w != "" {
			conditions = append(conditions, "("+w+")")
		}
	}
	where := strings.Join(conditions, " AND ")
	if scan.Limit > 0 {
		logs, err := s.queryLogs(ctx, fmt.Sprintf("SELECT %s FROM logs WHERE %s ORDER BY %s LIMIT %d",
			logSelectList, where, logsNewestFirst, scan.Limit))
		if err != nil {
			return IngestedLogs{}, err
		}
		if logs != nil {
			found.Logs = logs
		}
	}
	if found.Total = len(found.Logs); found.Total < scan.Limit {
		return found, nil
	}
	var total uint64
	if err := s.conn.QueryRow(ctx, "SELECT count() FROM logs WHERE "+where).Scan(&total); err != nil {
		return IngestedLogs{}, err
	}
	found.Total = int(total)
	return found, nil
}

// LogByID returns a Project's Log with the given id, if it has one. If at is
// set, the Log's own time, only its day's partition is read. Else the days
// around the id's time are read first, since a Log's id is a UUIDv7 made when
// it arrived, and most Logs arrive soon after their time; then every day of
// the Project.
func (s *ClickHouseLogs) LogByID(ctx context.Context, projectID uint32, id uuid.UUID, at *time.Time) (models.Log, bool, error) {
	match := fmt.Sprintf("project_id = %d AND id = %s", projectID, chsql.UUID(id))
	days := func(from, to time.Time) string {
		from, to = storableRange(from, to)
		return fmt.Sprintf(" AND toDate(time) >= toDate(%s) AND toDate(time) <= toDate(%s)", chsql.Time(from), chsql.Time(to))
	}
	var tries []string
	if at != nil {
		tries = []string{match + days(*at, *at)}
	} else {
		arrived := time.Unix(id.Time().UnixTime()).UTC()
		tries = []string{match + days(arrived.Add(-time.Hour), arrived.Add(time.Hour)), match}
	}
	for _, where := range tries {
		logs, err := s.queryLogs(ctx, fmt.Sprintf("SELECT %s FROM logs WHERE %s LIMIT 1", logSelectList, where))
		if err != nil || len(logs) > 0 {
			return firstLog(logs), len(logs) > 0, err
		}
	}
	return models.Log{}, false, nil
}

func firstLog(logs []models.Log) models.Log {
	if len(logs) == 0 {
		return models.Log{}
	}
	return logs[0]
}

// AttributeCounts returns the Attributes of a Project's Logs over the UTC
// days that [from, to) touches, the days of from and of to's last instant
// and those between: at most limit, most Logs first, then by name and type,
// and whether there were more. An empty range counts from's day.
//
// The counts are those Log processing keeps in the key counts table, summed
// over the days. So they leave out JSON Attributes, which are never counted,
// and a Project's day with more Attributes than keyCountsKept keeps only its
// top ones, with approximate counts.
func (s *ClickHouseLogs) AttributeCounts(ctx context.Context, projectID uint32, from, to time.Time, limit int) ([]AttributeCount, bool, error) {
	first, last := LogDay(from), LogDay(from)
	if to.After(from) {
		last = LogDay(to.Add(-time.Nanosecond))
	}
	rows, err := s.conn.Query(ctx, `
		SELECT key, type, sum(logs) AS total FROM log_key_counts
		WHERE project_id = ? AND day >= toDate(?) AND day <= toDate(?)
		GROUP BY key, type
		HAVING total > 0
		ORDER BY total DESC, key, type
		LIMIT ?`,
		projectID, dayString(first), dayString(last), limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("count Attributes: %w", err)
	}
	defer rows.Close()
	counts := []AttributeCount{}
	for rows.Next() {
		var count AttributeCount
		var kind string
		if err := rows.Scan(&count.Name, &kind, &count.Logs); err != nil {
			return nil, false, err
		}
		count.Type = AttributeType(kind)
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(counts) > limit {
		return counts[:limit], true, nil
	}
	return counts, false, nil
}
