package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/solidtrace/event_store/models"
)

// The Overview's reads: an Organization's Ingest counts and the parts of its
// stored Logs. None reads logs itself.

// logIngestCountsMigration makes the Ingest counts table: what arrived at
// each Project's Log routes in each second, written by ingest's flusher.
// Partitioned by the day of arrival, it is one of the day tables, and has
// the logs table's dedup window, so that a write retried is counted once.
// Its version's hundreds are the ticket that added it (Overview 01, 10xx).
var logIngestCountsMigration = clickHouseMigration{version: 1001, statements: []string{
	`CREATE TABLE IF NOT EXISTS log_ingest_counts (
		project_id UInt32,
		second DateTime('UTC'),
		logs_received UInt64,
		logs_unreadable UInt64,
		bytes_received UInt64,
		bytes_on_wire UInt64,
		requests UInt64,
		requests_overloaded UInt64
	) ENGINE = SummingMergeTree
	PARTITION BY (project_id, toDate(second))
	ORDER BY (project_id, second)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(logsDedupWindow),
}}

// WriteIngestCounts inserts Ingest counts as one block with token as its
// dedup token, so that the same rows written again with it are dropped.
func (s *ClickHouseLogs) WriteIngestCounts(token string, counts []models.IngestCount) error {
	ctx := insertContext(token)
	block, err := s.conn.PrepareBatch(ctx, `INSERT INTO log_ingest_counts
		(project_id, second, logs_received, logs_unreadable, bytes_received, bytes_on_wire, requests, requests_overloaded)`)
	if err != nil {
		return err
	}
	defer block.Abort()
	for _, c := range counts {
		if err := block.Append(c.ProjectID, c.Second, uint64(c.LogsReceived), uint64(c.LogsUnreadable),
			uint64(c.BytesReceived), uint64(c.BytesOnWire), uint64(c.Requests), uint64(c.RequestsOverloaded)); err != nil {
			return err
		}
	}
	return block.Send()
}

// OverviewScope is what an Overview covers: an Organization's Projects, and
// the seconds from From up to To.
type OverviewScope struct {
	ProjectIDs []uint32
	From, To   time.Time
}

// Peak is the most of something in any one second, and the first second it
// happened in; At is zero when there was none.
type Peak struct {
	Count int64
	At    time.Time
}

// IngestTotals sums a scope's Ingest counts, with the busiest second of the
// Projects together for Logs, bytes and requests.
type IngestTotals struct {
	models.IngestCount
	PeakLogs, PeakBytes, PeakRequests Peak
}

// IngestTotals sums the Ingest counts of a scope.
func (s *ClickHouseLogs) IngestTotals(ctx context.Context, scope OverviewScope) (IngestTotals, error) {
	// A second's rows are summed across Projects, and the parts not yet
	// merged, before its peak is taken. Of equal seconds the first wins.
	peak := func(column string) string {
		return fmt.Sprintf("toInt64(max(%[1]s)), argMax(second, (%[1]s, -toUnixTimestamp(second)))", column)
	}
	row := s.conn.QueryRow(ctx, `
		SELECT toInt64(sum(l)), toInt64(sum(u)), toInt64(sum(b)), toInt64(sum(w)), toInt64(sum(r)), toInt64(sum(o)),
			`+peak("l")+`, `+peak("b")+`, `+peak("r")+`
		FROM (
			SELECT second, sum(logs_received) AS l, sum(logs_unreadable) AS u, sum(bytes_received) AS b,
				sum(bytes_on_wire) AS w, sum(requests) AS r, sum(requests_overloaded) AS o
			FROM log_ingest_counts
			WHERE project_id IN ? AND second >= ? AND second < ?
			GROUP BY second
		)`, scope.ProjectIDs, scope.From, scope.To)
	var t IngestTotals
	if err := row.Scan(&t.LogsReceived, &t.LogsUnreadable, &t.BytesReceived, &t.BytesOnWire, &t.Requests, &t.RequestsOverloaded,
		&t.PeakLogs.Count, &t.PeakLogs.At, &t.PeakBytes.Count, &t.PeakBytes.At, &t.PeakRequests.Count, &t.PeakRequests.At); err != nil {
		return IngestTotals{}, fmt.Errorf("sum Ingest counts: %w", err)
	}
	for _, p := range []*Peak{&t.PeakLogs, &t.PeakBytes, &t.PeakRequests} {
		if p.Count == 0 {
			p.At = time.Time{}
		} else {
			p.At = p.At.UTC()
		}
	}
	return t, nil
}

// IngestCountsSince is the first second the install's Ingest counts hold,
// from the parts' own bounds, or false when they hold none.
func (s *ClickHouseLogs) IngestCountsSince(ctx context.Context) (time.Time, bool, error) {
	var since time.Time
	var parts uint64
	if err := s.conn.QueryRow(ctx, `
		SELECT min(min_time), count()
		FROM system.parts
		WHERE database = currentDatabase() AND table = 'log_ingest_counts' AND active`).Scan(&since, &parts); err != nil {
		return time.Time{}, false, fmt.Errorf("find when Ingest counts start: %w", err)
	}
	return since.UTC(), parts > 0, nil
}

// StoredLogDay is what ClickHouse holds of a Project's day of Logs, by its
// parts: bytes on local disk and in the bucket, as compressed, and the
// bytes and count of its Logs before compression.
type StoredLogDay struct {
	ProjectID   uint32
	Day         time.Time
	Logs        int64
	LocalBytes  int64
	BucketBytes int64
	RawBytes    int64
}

// StoredLogDays lists the stored days of each Project of projectIDs from
// the day of from to the day of to, by logs' parts, in no order.
func (s *ClickHouseLogs) StoredLogDays(ctx context.Context, projectIDs []uint32, from, to time.Time) ([]StoredLogDay, error) {
	var ids []string
	for _, projectID := range projectIDs {
		for day := LogDay(from); !day.After(to); day = day.AddDate(0, 0, 1) {
			ids = append(ids, fmt.Sprintf("%d-%s", projectID, day.Format("20060102")))
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.conn.Query(ctx, `
		SELECT partition_id, toInt64(sum(rows)),
			toInt64(sumIf(bytes_on_disk, disk_name IN (SELECT name FROM system.disks WHERE NOT is_remote))),
			toInt64(sumIf(bytes_on_disk, disk_name IN (SELECT name FROM system.disks WHERE is_remote))),
			toInt64(sum(data_uncompressed_bytes))
		FROM system.parts
		WHERE database = currentDatabase() AND table = 'logs' AND active AND partition_id IN ?
		GROUP BY partition_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("list the parts of stored Logs: %w", err)
	}
	defer rows.Close()
	var days []StoredLogDay
	for rows.Next() {
		var id string
		var d StoredLogDay
		if err := rows.Scan(&id, &d.Logs, &d.LocalBytes, &d.BucketBytes, &d.RawBytes); err != nil {
			return nil, err
		}
		if d.ProjectID, d.Day, err = parseLogPartitionID(id); err != nil {
			return nil, fmt.Errorf("partition %s of logs: %w", id, err)
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// IngestLogsBetween counts the Logs received by projectIDs in the seconds
// from from up to to.
func (s *ClickHouseLogs) IngestLogsBetween(ctx context.Context, projectIDs []uint32, from, to time.Time) (int64, error) {
	var logs int64
	if err := s.conn.QueryRow(ctx, `
		SELECT toInt64(sum(logs_received)) FROM log_ingest_counts
		WHERE project_id IN ? AND second >= ? AND second < ?`, projectIDs, from, to).Scan(&logs); err != nil {
		return 0, fmt.Errorf("count Logs received: %w", err)
	}
	return logs, nil
}

// HasLogs is whether any Project of projectIDs has stored Logs of any day,
// or Ingest counts of any, by the tables' parts.
func (s *ClickHouseLogs) HasLogs(ctx context.Context, projectIDs []uint32) (bool, error) {
	var parts uint64
	if err := s.conn.QueryRow(ctx, `
		SELECT count() FROM system.parts
		WHERE database = currentDatabase() AND active AND table IN ('logs', 'log_ingest_counts')
			AND toUInt32OrZero(splitByChar('-', partition_id)[1]) IN ?`, projectIDs).Scan(&parts); err != nil {
		return false, fmt.Errorf("find whether Projects have Logs: %w", err)
	}
	return parts > 0, nil
}
