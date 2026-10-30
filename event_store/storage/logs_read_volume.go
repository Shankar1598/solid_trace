package storage

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/solidtrace/event_store/pkg/chsql"
)

// LogVolume is what the counts say of a Project's Logs of a range, read
// from Source counts and the counts by minute, never from the Logs: each
// day's Logs and bytes by Service and file, and the share of the day's Logs
// that the range holds.
type LogVolume struct {
	From, To time.Time
	// Days are the days of the range with Source counts, oldest first.
	Days []DayVolume
}

// DayVolume is one UTC day of a LogVolume.
type DayVolume struct {
	Day time.Time
	// Sources are the whole day's Source counts, by Service and file.
	Sources []SourceVolume
	// Logs is the whole day's Logs by the counts by minute, and Share the
	// part of them in the range: 1 for a day the range covers, else by the
	// counts by minute, or by time for a day they hold none of.
	Logs  int64
	Share float64
}

// SourceVolume is the Logs and kept bytes of one Service and file.
type SourceVolume struct {
	Service, File string
	Logs, Bytes   int64
}

// MinuteLogs is how many of a Project's Logs one minute holds, by the
// counts by minute.
type MinuteLogs struct {
	Minute time.Time
	Logs   int64
}

// LogVolume reads what the counts say of a Project's Logs of [from, to). A
// minute partly in the range counts whole. Its reads aren't tagged as Log
// reads: they size a read rather than make one.
func (s *ClickHouseLogs) LogVolume(ctx context.Context, projectID uint32, from, to time.Time) (LogVolume, error) {
	from, to = storableRange(from, to)
	volume := LogVolume{From: from, To: to}
	if !from.Before(to) {
		return volume, nil
	}
	ctx = untagged(ctx)
	firstDay, lastDay := LogDay(from), LogDay(to.Add(-time.Microsecond))

	sources := fmt.Sprintf(`SELECT day, service, file, toInt64(sum(logs)), toInt64(sum(bytes))
		FROM log_source_counts
		WHERE project_id = %d AND day >= toDate('%s') AND day <= toDate('%s')
		GROUP BY day, service, file ORDER BY day, service, file`, projectID, dayString(firstDay), dayString(lastDay))
	days := map[time.Time]*DayVolume{}
	err := s.scanVolume(ctx, sources, func(rows interface{ Scan(...any) error }) error {
		var day time.Time
		var source SourceVolume
		if err := rows.Scan(&day, &source.Service, &source.File, &source.Logs, &source.Bytes); err != nil {
			return err
		}
		day = LogDay(day)
		d, ok := days[day]
		if !ok {
			d = &DayVolume{Day: day}
			days[day] = d
		}
		d.Sources = append(d.Sources, source)
		return nil
	})
	if err != nil {
		return LogVolume{}, fmt.Errorf("read the Source counts of the range: %w", err)
	}

	byMinute := fmt.Sprintf(`SELECT toDate(minute) AS day, toInt64(sum(logs)), toInt64(sumIf(logs, minute >= %s AND minute < %s))
		FROM log_minute_counts
		WHERE project_id = %d AND minute >= %s AND minute < %s
		GROUP BY day`, chsql.Time(from.Truncate(time.Minute)), chsql.Time(to),
		projectID, chsql.Time(firstDay), chsql.Time(lastDay.Add(24*time.Hour)))
	inRange := map[time.Time]int64{}
	err = s.scanVolume(ctx, byMinute, func(rows interface{ Scan(...any) error }) error {
		var day time.Time
		var logs, in int64
		if err := rows.Scan(&day, &logs, &in); err != nil {
			return err
		}
		if d, ok := days[LogDay(day)]; ok {
			d.Logs = logs
			inRange[d.Day] = in
		}
		return nil
	})
	if err != nil {
		return LogVolume{}, fmt.Errorf("read the counts by minute of the range: %w", err)
	}

	for _, d := range days {
		start, end := latestTime(d.Day, from), earliestTime(d.Day.Add(24*time.Hour), to)
		switch {
		case start.Equal(d.Day) && end.Equal(d.Day.Add(24*time.Hour)):
			d.Share = 1
		case d.Logs > 0:
			d.Share = float64(inRange[d.Day]) / float64(d.Logs)
		default:
			d.Share = float64(end.Sub(start)) / float64(24*time.Hour)
		}
		volume.Days = append(volume.Days, *d)
	}
	slices.SortFunc(volume.Days, func(a, b DayVolume) int { return a.Day.Compare(b.Day) })
	return volume, nil
}

// LogsByMinute reads a Project's Logs of each minute of [from, to) that
// holds any, by the counts by minute, oldest first. A minute partly in the
// range counts whole. Like LogVolume, its read isn't tagged.
func (s *ClickHouseLogs) LogsByMinute(ctx context.Context, projectID uint32, from, to time.Time) ([]MinuteLogs, error) {
	from, to = storableRange(from, to)
	query := fmt.Sprintf(`SELECT minute, toInt64(sum(logs)) FROM log_minute_counts
		WHERE project_id = %d AND minute >= %s AND minute < %s
		GROUP BY minute ORDER BY minute`, projectID, chsql.Time(from.Truncate(time.Minute)), chsql.Time(to))
	minutes := []MinuteLogs{}
	err := s.scanVolume(untagged(ctx), query, func(rows interface{ Scan(...any) error }) error {
		var m MinuteLogs
		if err := rows.Scan(&m.Minute, &m.Logs); err != nil {
			return err
		}
		m.Minute = m.Minute.UTC()
		minutes = append(minutes, m)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the counts by minute: %w", err)
	}
	return minutes, nil
}

// scanVolume runs a read of the counts, scanning each row, and records it
// as the run's statement.
func (s *ClickHouseLogs) scanVolume(ctx context.Context, query string, scan func(rows interface{ Scan(...any) error }) error) (err error) {
	began := time.Now()
	defer func() { recordStatement(ctx, query, began, err) }()
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// untagged is ctx with no ClickHouse settings, so that its statements carry
// no read's log_comment, and the Overview doesn't count them as Log reads.
func untagged(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(nil))
}
