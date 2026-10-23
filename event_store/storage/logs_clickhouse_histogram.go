package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/solidtrace/event_store/pkg/chsql"
)

// logMinuteCountsMigration makes the histogram's counts: how many of each
// Project's Logs of each Level are in each minute, which a materialized view
// adds to as Log processing inserts into logs. Partitioned as logs is, and
// with its dedup window, so that a range of Log batches inserted again is
// counted once. The Logs logs already holds are counted afresh. Its version's
// hundreds are the ticket that added it (04).
var logMinuteCountsMigration = clickHouseMigration{version: 401, statements: []string{
	`CREATE TABLE IF NOT EXISTS log_minute_counts (
		project_id UInt32,
		minute DateTime64(6, 'UTC'),
		level LowCardinality(String),
		logs UInt64
	) ENGINE = SummingMergeTree(logs)
	PARTITION BY (project_id, toDate(minute))
	ORDER BY (project_id, minute, level)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(logsDedupWindow),
	`CREATE MATERIALIZED VIEW IF NOT EXISTS log_minute_counts_mv TO log_minute_counts AS ` + logMinuteCountsSelect,
	// Nothing inserts into logs while EventStore migrates, so counting
	// afresh counts each stored Log once, however often it runs.
	`TRUNCATE TABLE log_minute_counts`,
	`INSERT INTO log_minute_counts ` + logMinuteCountsSelect,
}}

// logMinuteCountsSelect counts Logs by Project, minute and Level, '' for
// none.
const logMinuteCountsSelect = `SELECT project_id, toStartOfMinute(time) AS minute, ifNull(level, '') AS level, count() AS logs
	FROM logs GROUP BY project_id, minute, level`

// LevelCount is how many of a Project's Logs of one Level, "" for none, are
// in a bucket of time.
type LevelCount struct {
	Bucket time.Time
	Level  string
	Logs   uint64
}

// CountLogsByLevel counts a Project's Logs of [from, to) by Level in buckets
// of size, each a whole number of sizes after 1970; size is a whole number
// of minutes. A bucket and Level may be counted in more than one LevelCount.
//
// It reads the histogram's counts by minute for the whole minutes of the
// range, and the Logs themselves only for the part-minutes at either end.
// Cancelling ctx stops it.
func (s *ClickHouseLogs) CountLogsByLevel(ctx context.Context, projectID uint32, from, to time.Time, size time.Duration) ([]LevelCount, error) {
	if size <= 0 || size%time.Minute != 0 {
		return nil, fmt.Errorf("count Logs by Level: buckets of %s aren't whole minutes", size)
	}
	from, to = storableRange(from, to)
	if !from.Before(to) {
		return []LevelCount{}, nil
	}
	bucket := func(column string) string {
		return fmt.Sprintf("toStartOfInterval(%s, toIntervalMicrosecond(%d))", column, size.Microseconds())
	}
	inLogs := func(conditions string) string {
		return fmt.Sprintf(`SELECT %s AS bucket, ifNull(level, '') AS level, count() AS logs FROM logs
			WHERE project_id = %d AND (%s) GROUP BY bucket, level`, bucket("time"), projectID, conditions)
	}
	timeIn := func(a, b time.Time) string {
		return fmt.Sprintf("time >= %s AND time < %s", chsql.Time(a), chsql.Time(b))
	}
	// The whole minutes of the range are [first, last).
	first, last := from.Truncate(time.Minute), to.Truncate(time.Minute)
	if first.Before(from) {
		first = first.Add(time.Minute)
	}
	query := inLogs(timeIn(from, to))
	if first.Before(last) {
		query = fmt.Sprintf(`SELECT %s AS bucket, level, sum(logs) AS logs FROM log_minute_counts
			WHERE project_id = %d AND minute >= %s AND minute < %s GROUP BY bucket, level`,
			bucket("minute"), projectID, chsql.Time(first), chsql.Time(last))
		if from.Before(first) || last.Before(to) {
			query += "\nUNION ALL\n" + inLogs("("+timeIn(from, first)+") OR ("+timeIn(last, to)+")")
		}
	}

	began := time.Now()
	counts, err := s.scanLevelCounts(ctx, query)
	recordStatement(ctx, query, began, err)
	return counts, err
}

func (s *ClickHouseLogs) scanLevelCounts(ctx context.Context, query string) ([]LevelCount, error) {
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := []LevelCount{}
	for rows.Next() {
		var count LevelCount
		if err := rows.Scan(&count.Bucket, &count.Level, &count.Logs); err != nil {
			return nil, err
		}
		count.Bucket = count.Bucket.UTC()
		counts = append(counts, count)
	}
	return counts, rows.Err()
}
