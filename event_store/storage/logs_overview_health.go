package storage

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// The Overview's Health reads: Log processing's oldest waiting Log batches,
// the Log reads ClickHouse's query_log holds, and the Events of a range.

// OldestWaitingLogBatches is the oldest Log batch still waiting for Log
// processing of each Project of projectIDs that has one, read from Pebble.
func (s *ClickHouseLogs) OldestWaitingLogBatches(projectIDs []uint32) ([]OldestLogBatch, error) {
	oldest, err := s.pebble.OldestLogBatches()
	if err != nil {
		return nil, fmt.Errorf("find the oldest waiting Log batches: %w", err)
	}
	return slices.DeleteFunc(oldest, func(b OldestLogBatch) bool { return !slices.Contains(projectIDs, b.ProjectID) }), nil
}

// ProjectReadTimes is how long a Project's Log reads took over the last day:
// how many there were, and the median and 95th percentile, in milliseconds.
type ProjectReadTimes struct {
	ProjectID uint32
	Reads     int64
	MedianMs  float64
	P95Ms     float64
}

// LogRead is one Log read: its Project, its kind, when it began and how long
// it took, in milliseconds.
type LogRead struct {
	ProjectID  uint32
	Kind       string
	At         time.Time
	DurationMs float64
}

// LogReadTimes is how fast the Log reads of some Projects ran over the last
// day: each Project's, and the slowest reads, slowest first.
type LogReadTimes struct {
	Projects []ProjectReadTimes
	Slowest  []LogRead
}

// logReads is the Log reads of the last day by ClickHouse's own clock, as
// query_log keeps them, a row each: TagRead's runs, whose statements make
// one read from the first's start to the last's end, and each SQL query, by
// its Project's SQL user (<database>_sql_<id>). Reads that failed while
// running count too: a slow read that times out is slow.
const logReads = `
	WITH {prefix:String} AS prefix
	SELECT project_id, kind, min(started) AS at,
		(max(finished) - min(started)) / 1000 AS duration_ms
	FROM (
		SELECT
			if(log_comment != '', JSONExtractUInt(log_comment, 'project_id'),
				toUInt64OrZero(substring(user, length(prefix) + 1))) AS project_id,
			if(log_comment != '', JSONExtractString(log_comment, 'read'), {sql:String}) AS kind,
			if(log_comment != '', JSONExtractString(log_comment, 'run'), query_id) AS run,
			toUnixTimestamp64Micro(query_start_time_microseconds) AS started,
			toUnixTimestamp64Micro(event_time_microseconds) AS finished
		FROM system.query_log
		WHERE event_date >= yesterday() AND event_time >= now() - INTERVAL 1 DAY
			AND current_database = currentDatabase() AND is_initial_query
			AND type IN ('QueryFinish', 'ExceptionWhileProcessing')
			AND (JSONExtractString(log_comment, 'run') != '' OR startsWith(user, prefix))
	)
	WHERE project_id IN {projects:Array(UInt32)}
	GROUP BY project_id, kind, run`

// LogReadTimes reads how fast projectIDs' Log reads ran over the last day,
// with the slowest limit reads.
func (s *ClickHouseLogs) LogReadTimes(ctx context.Context, projectIDs []uint32, limit int) (LogReadTimes, error) {
	var times LogReadTimes
	var database string
	if err := s.conn.QueryRow(ctx, "SELECT currentDatabase()").Scan(&database); err != nil {
		return times, err
	}
	args := []any{
		clickhouse.Named("prefix", database+"_sql_"), clickhouse.Named("sql", ReadSQLQuery),
		clickhouse.Named("projects", projectIDs),
	}

	rows, err := s.conn.Query(ctx, `
		SELECT toUInt32(project_id), toInt64(count()), quantileExact(0.5)(duration_ms), quantileExact(0.95)(duration_ms)
		FROM (`+logReads+`)
		GROUP BY project_id ORDER BY project_id`, args...)
	if err != nil {
		return times, fmt.Errorf("read the Log reads' times: %w", err)
	}
	for rows.Next() {
		var p ProjectReadTimes
		if err := rows.Scan(&p.ProjectID, &p.Reads, &p.MedianMs, &p.P95Ms); err != nil {
			rows.Close()
			return times, err
		}
		times.Projects = append(times.Projects, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return times, err
	}

	rows, err = s.conn.Query(ctx, `
		SELECT toUInt32(project_id), kind, fromUnixTimestamp64Micro(toInt64(at), 'UTC'), duration_ms
		FROM (`+logReads+`)
		ORDER BY duration_ms DESC, at DESC LIMIT {limit:UInt32}`, append(args, clickhouse.Named("limit", limit))...)
	if err != nil {
		return times, fmt.Errorf("read the slowest Log reads: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r LogRead
		if err := rows.Scan(&r.ProjectID, &r.Kind, &r.At, &r.DurationMs); err != nil {
			return times, err
		}
		r.At = r.At.UTC()
		times.Slowest = append(times.Slowest, r)
	}
	return times, rows.Err()
}

// ProjectEvents is how many Events of a Project are of a range.
type ProjectEvents struct {
	ProjectID uint32
	Events    int64
}

// CountEventsBetween counts the Events of each Project of projectIDs whose
// time is from from up to to, leaving out Projects with none.
func (s *ClickHouseLogs) CountEventsBetween(ctx context.Context, projectIDs []uint32, from, to time.Time) ([]ProjectEvents, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT project_id, toInt64(count()) FROM events
		WHERE project_id IN ? AND timestamp >= ? AND timestamp < ?
		GROUP BY project_id ORDER BY project_id`, projectIDs, from, to)
	if err != nil {
		return nil, fmt.Errorf("count Events: %w", err)
	}
	defer rows.Close()
	var counts []ProjectEvents
	for rows.Next() {
		var c ProjectEvents
		if err := rows.Scan(&c.ProjectID, &c.Events); err != nil {
			return nil, err
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}
