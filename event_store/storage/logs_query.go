package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
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
}

// BuiltIn is a built-in field's column, such as level.
func (c LogColumns) BuiltIn(name string) string {
	return sqlIdentifier(name)
}

// Map is the map that holds the Attributes of type t that aren't promoted.
func (c LogColumns) Map(t AttributeType) string {
	return t.Map()
}

// Attribute is the value of the Attribute key of type t, NULL for a Log
// without one: the key's column if it is promoted, coalesced with its map
// if it was promoted partway through the day, else its map.
func (c LogColumns) Attribute(t AttributeType, key string) string {
	inMap := fmt.Sprintf("map_extract_value(%s, %s)", t.Map(), sqlString(key))
	if c.late {
		return inMap
	}
	promoted, ok := c.set.Lookup(t, key)
	if !ok {
		return inMap
	}
	column := sqlIdentifier(promoted.Column())
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
			columns = append(columns, sqlIdentifier(key.Column()))
		}
	}
	return columns
}

// LogPosition is a Log's place in newest-first order: by time, then id.
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
}

// ScanLogs returns up to scan.Limit of a Project's Logs that meet the
// condition, newest first, from its hot table, its late Logs and its files.
// Promoted values are merged back into the Attributes.
//
// It reads hour by hour, newest first, and stops once it has scan.Limit Logs.
// Each hour is read in one query, while no export can publish, so it sees
// each of the hour's Logs exactly once; an export may publish between hours.
// Between hours it checks ctx.
func (s *LogStore) ScanLogs(ctx context.Context, scan LogScan) ([]models.Log, error) {
	from, to := scan.From.UTC(), scan.To.UTC()
	if scan.After != nil && scan.After.Time.Before(to) {
		// Logs at the same time as After, with a lower id, follow it.
		to = scan.After.Time.UTC().Add(time.Microsecond)
	}
	logs := []models.Log{}
	if !from.Before(to) || scan.Limit <= 0 {
		return logs, nil
	}
	hours, hot, err := s.hoursWithLogs(scan.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	for _, hour := range hours {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hourFrom, hourTo := hour, hour.Add(time.Hour)
		if from.After(hourFrom) {
			hourFrom = from
		}
		if to.Before(hourTo) {
			hourTo = to
		}
		found, err := s.scanHour(ctx, scan, hour, hourFrom, hourTo, hot, scan.Limit-len(logs))
		if err != nil {
			return nil, err
		}
		logs = append(logs, found...)
		if len(logs) >= scan.Limit {
			break
		}
	}
	return logs, nil
}

// hoursWithLogs returns, newest first, the hours of [from, to) that may hold
// a Project's Logs: those with files, and those between the oldest and the
// newest Log not yet exported. It also reports whether the Project has a
// hot table.
//
// An hour's Logs not yet exported are only ever exported into that hour's
// files, so an hour listed here stays listed while the scan runs.
func (s *LogStore) hoursWithLogs(projectID uint32, from, to time.Time) ([]time.Time, bool, error) {
	hours := map[time.Time]bool{}
	first, last := from.Truncate(time.Hour), to.Add(-time.Microsecond).Truncate(time.Hour)

	days, err := os.ReadDir(s.files.ProjectDir(projectID))
	if err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}
	for _, day := range days {
		d, ok := strings.CutPrefix(day.Name(), "day=")
		if !ok {
			continue
		}
		date, err := time.Parse(time.DateOnly, d)
		if err != nil || date.Before(LogDay(first)) || date.After(last) {
			continue
		}
		hourDirs, err := os.ReadDir(s.files.DayDir(projectID, date))
		if os.IsNotExist(err) {
			continue // deleted by Retention or the disk limit since
		} else if err != nil {
			return nil, false, err
		}
		for _, hourDir := range hourDirs {
			var h int
			if _, err := fmt.Sscanf(hourDir.Name(), "hour=%02d", &h); err != nil {
				continue
			}
			hour := date.Add(time.Duration(h) * time.Hour)
			if !hour.Before(first) && !hour.After(last) {
				hours[hour] = true
			}
		}
	}

	s.publishMu.RLock()
	defer s.publishMu.RUnlock()
	hot, err := s.tableExists(hotTable(projectID))
	if err != nil {
		return nil, false, err
	}
	sources := []string{fmt.Sprintf("SELECT time FROM %s.%s WHERE project_id = %d AND %s",
		logsCatalog, lateTable, projectID, timeRange(from, to))}
	if hot {
		sources = append(sources, fmt.Sprintf("SELECT time FROM %s.%s WHERE %s",
			logsCatalog, hotTable(projectID), timeRange(from, to)))
	}
	var oldest, newest sql.NullTime
	err = s.db.QueryRow(fmt.Sprintf("SELECT min(time), max(time) FROM (%s)", strings.Join(sources, " UNION ALL "))).
		Scan(&oldest, &newest)
	if err != nil {
		return nil, false, err
	}
	if oldest.Valid {
		for hour := oldest.Time.UTC().Truncate(time.Hour); !hour.After(newest.Time.UTC()); hour = hour.Add(time.Hour) {
			hours[hour] = true
		}
	}

	sorted := make([]time.Time, 0, len(hours))
	for hour := range hours {
		sorted = append(sorted, hour)
	}
	slices.SortFunc(sorted, func(a, b time.Time) int { return b.Compare(a) })
	return sorted, hot, nil
}

// scanHour returns up to limit of the Project's Logs of [from, to), within
// one hour, that meet the scan's condition, newest first.
func (s *LogStore) scanHour(ctx context.Context, scan LogScan, hour, from, to time.Time, hot bool, limit int) ([]models.Log, error) {
	s.publishMu.RLock()
	defer s.publishMu.RUnlock()

	files, err := s.files.Hour(scan.ProjectID, hour)
	if err != nil {
		return nil, err
	}
	set := s.PromotedSet(scan.ProjectID, hour)
	bounds := timeRange(from, to)
	if scan.After != nil {
		bounds += fmt.Sprintf(" AND (time < %[1]s OR (time = %[1]s AND id < CAST(%[2]s AS UUID)))",
			timestampLiteral(scan.After.Time), sqlString(scan.After.ID.String()))
	}
	where := func(cols LogColumns) string {
		condition := bounds
		if scan.Where != nil {
			if w := scan.Where(cols); w != "" {
				condition += " AND (" + w + ")"
			}
		}
		return condition
	}
	promoted := LogColumns{set: set}

	sources := []string{fmt.Sprintf("SELECT * EXCLUDE (project_id) FROM %s.%s WHERE project_id = %d AND %s",
		logsCatalog, lateTable, scan.ProjectID, where(LogColumns{late: true}))}
	if hot {
		sources = append(sources, fmt.Sprintf("SELECT * FROM (%s) WHERE %s",
			withColumns(fmt.Sprintf("SELECT * FROM %s.%s", logsCatalog, hotTable(scan.ProjectID)), set), where(promoted)))
	}
	if len(files) > 0 {
		sources = append(sources, fmt.Sprintf("SELECT * FROM (%s) WHERE %s",
			withColumns(readFiles(files), set), where(promoted)))
	}
	return s.queryLogs(ctx, fmt.Sprintf(`
		SELECT * FROM (%s)
		ORDER BY time DESC, id DESC
		LIMIT %d`, strings.Join(sources, "\nUNION ALL BY NAME\n"), limit))
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
		columns[i] = fmt.Sprintf("CAST(NULL AS %s) AS %s", key.Type.columnType(), sqlIdentifier(key.Column()))
	}
	return fmt.Sprintf("%s UNION ALL BY NAME SELECT %s WHERE false", source, strings.Join(columns, ", "))
}

// readFiles reads Log files, their columns matched by name.
func readFiles(files []string) string {
	quoted := make([]string, len(files))
	for i, file := range files {
		quoted[i] = sqlString(file)
	}
	return fmt.Sprintf("SELECT * FROM read_parquet([%s], union_by_name = true, hive_partitioning = false)",
		strings.Join(quoted, ", "))
}

// timeRange is the SQL condition time in [from, to).
func timeRange(from, to time.Time) string {
	return fmt.Sprintf("time >= %s AND time < %s", timestampLiteral(from), timestampLiteral(to))
}

func timestampLiteral(t time.Time) string {
	return fmt.Sprintf("CAST(%s AS TIMESTAMP)", sqlString(timestampString(t)))
}

// LogByID returns a Project's Log with the given id, if it has one. If at is
// set, only the Log files of at's hour are searched, else every file.
func (s *LogStore) LogByID(ctx context.Context, projectID uint32, id uuid.UUID, at *time.Time) (models.Log, bool, error) {
	s.publishMu.RLock()
	defer s.publishMu.RUnlock()

	hot, err := s.tableExists(hotTable(projectID))
	if err != nil {
		return models.Log{}, false, err
	}
	var files []string
	if at != nil {
		files, err = s.files.Hour(projectID, *at)
	} else {
		files, err = s.files.Project(projectID)
	}
	if err != nil {
		return models.Log{}, false, err
	}
	match := "id = CAST(" + sqlString(id.String()) + " AS UUID)"
	sources := []string{fmt.Sprintf("SELECT * EXCLUDE (project_id) FROM %s.%s WHERE project_id = %d AND %s",
		logsCatalog, lateTable, projectID, match)}
	if hot {
		sources = append(sources, fmt.Sprintf("SELECT * FROM %s.%s WHERE %s", logsCatalog, hotTable(projectID), match))
	}
	if len(files) > 0 {
		sources = append(sources, fmt.Sprintf("SELECT * FROM (%s) WHERE %s", readFiles(files), match))
	}
	logs, err := s.queryLogs(ctx, fmt.Sprintf("SELECT * FROM (%s) LIMIT 1", strings.Join(sources, "\nUNION ALL BY NAME\n")))
	if err != nil || len(logs) == 0 {
		return models.Log{}, false, err
	}
	return logs[0], true, nil
}

// queryLogs runs a query whose rows are Logs as a hot table holds them, with
// any promoted columns.
func (s *LogStore) queryLogs(ctx context.Context, query string) ([]models.Log, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var logs []models.Log
	for rows.Next() {
		log, err := scanHotRow(rows, columns)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}
