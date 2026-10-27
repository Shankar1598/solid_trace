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

// spanIngestCountsMigration counts Spans on arrival beside Logs, so that the
// Overview answers Spans received without reading spans. SummingMergeTree
// sums the new columns too. Traces 05, 25xx.
var spanIngestCountsMigration = clickHouseMigration{version: 2501, statements: []string{
	`ALTER TABLE log_ingest_counts
		ADD COLUMN IF NOT EXISTS spans_received UInt64,
		ADD COLUMN IF NOT EXISTS span_bytes_received UInt64`,
}}

// WriteIngestCounts inserts Ingest counts as one block with token as its
// dedup token, so that the same rows written again with it are dropped.
func (s *ClickHouseLogs) WriteIngestCounts(token string, counts []models.IngestCount) error {
	ctx := insertContext(token)
	block, err := s.conn.PrepareBatch(ctx, `INSERT INTO log_ingest_counts
		(project_id, second, logs_received, logs_unreadable, bytes_received, bytes_on_wire, requests, requests_overloaded,
		spans_received, span_bytes_received)`)
	if err != nil {
		return err
	}
	defer block.Abort()
	for _, c := range counts {
		if err := block.Append(c.ProjectID, c.Second, uint64(c.LogsReceived), uint64(c.LogsUnreadable),
			uint64(c.BytesReceived), uint64(c.BytesOnWire), uint64(c.Requests), uint64(c.RequestsOverloaded),
			uint64(c.SpansReceived), uint64(c.SpanBytesReceived)); err != nil {
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

// ingestSeconds is a scope's Ingest counts summed per second, and per
// Project when by is "project_id, ", as l, u, b, w, r, o, and s and sb for
// Spans and their bytes: a second's
// rows are summed, across Projects or the parts not yet merged, before a
// peak is taken over them. It takes the scope's ProjectIDs, From and To.
func ingestSeconds(by string) string {
	return `
		SELECT ` + by + `second, sum(logs_received) AS l, sum(logs_unreadable) AS u, sum(bytes_received) AS b,
			sum(bytes_on_wire) AS w, sum(requests) AS r, sum(requests_overloaded) AS o,
			sum(spans_received) AS s, sum(span_bytes_received) AS sb
		FROM log_ingest_counts
		WHERE project_id IN ? AND second >= ? AND second < ?
		GROUP BY ` + by + `second`
}

// peakOf selects the most of an ingestSeconds column in one second, and
// the second: of equal seconds the first wins.
func peakOf(column string) string {
	return fmt.Sprintf("toInt64(max(%[1]s)), argMax(second, (%[1]s, -toUnixTimestamp(second)))", column)
}

// settle clears a Peak's second when nothing arrived, and reads it in UTC.
func (p *Peak) settle() {
	if p.Count == 0 {
		p.At = time.Time{}
	} else {
		p.At = p.At.UTC()
	}
}

// IngestTotals sums the Ingest counts of a scope.
func (s *ClickHouseLogs) IngestTotals(ctx context.Context, scope OverviewScope) (IngestTotals, error) {
	row := s.conn.QueryRow(ctx, `
		SELECT toInt64(sum(l)), toInt64(sum(u)), toInt64(sum(b)), toInt64(sum(w)), toInt64(sum(r)), toInt64(sum(o)),
			toInt64(sum(s)), toInt64(sum(sb)), `+peakOf("l")+`, `+peakOf("b")+`, `+peakOf("r")+`
		FROM (`+ingestSeconds("")+`)`, scope.ProjectIDs, scope.From, scope.To)
	var t IngestTotals
	if err := row.Scan(&t.LogsReceived, &t.LogsUnreadable, &t.BytesReceived, &t.BytesOnWire, &t.Requests, &t.RequestsOverloaded,
		&t.SpansReceived, &t.SpanBytesReceived, &t.PeakLogs.Count, &t.PeakLogs.At, &t.PeakBytes.Count, &t.PeakBytes.At, &t.PeakRequests.Count, &t.PeakRequests.At); err != nil {
		return IngestTotals{}, fmt.Errorf("sum Ingest counts: %w", err)
	}
	for _, p := range []*Peak{&t.PeakLogs, &t.PeakBytes, &t.PeakRequests} {
		p.settle()
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

// localAndBucketBytes selects the bytes of a system.parts selection on
// local disk, then in the bucket.
const localAndBucketBytes = `
	toInt64(sumIf(bytes_on_disk, disk_name IN (SELECT name FROM system.disks WHERE NOT is_remote))),
	toInt64(sumIf(bytes_on_disk, disk_name IN (SELECT name FROM system.disks WHERE is_remote)))`

// StoredLogDays lists the stored days of each Project of projectIDs from
// the day of from to the day of to, by logs' parts, in no order.
func (s *ClickHouseLogs) StoredLogDays(ctx context.Context, projectIDs []uint32, from, to time.Time) ([]StoredLogDay, error) {
	return s.storedDays(ctx, "logs", projectIDs, from, to)
}

// StoredSpanDays lists the stored days of Spans as StoredLogDays does Logs,
// by spans' parts, with Logs counting the day's Spans.
func (s *ClickHouseLogs) StoredSpanDays(ctx context.Context, projectIDs []uint32, from, to time.Time) ([]StoredLogDay, error) {
	return s.storedDays(ctx, "spans", projectIDs, from, to)
}

// storedDays lists the stored days of a Project-day table by its parts.
func (s *ClickHouseLogs) storedDays(ctx context.Context, table string, projectIDs []uint32, from, to time.Time) ([]StoredLogDay, error) {
	var ids []string
	for _, projectID := range projectIDs {
		for day := LogDay(from); !day.After(to); day = day.AddDate(0, 0, 1) {
			ids = append(ids, logPartitionID(projectID, day))
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.conn.Query(ctx, `
		SELECT partition_id, toInt64(sum(rows)), `+localAndBucketBytes+`,
			toInt64(sum(data_uncompressed_bytes))
		FROM system.parts
		WHERE database = currentDatabase() AND table = ? AND active AND partition_id IN ?
		GROUP BY partition_id`, table, ids)
	if err != nil {
		return nil, fmt.Errorf("list the parts of %s: %w", table, err)
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
			return nil, fmt.Errorf("partition %s of %s: %w", id, table, err)
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
// by the logs table's parts, or Ingest counts of Logs received. Ingest
// counts of Spans alone aren't Logs.
func (s *ClickHouseLogs) HasLogs(ctx context.Context, projectIDs []uint32) (bool, error) {
	var has bool
	if err := s.conn.QueryRow(ctx, `
		SELECT (SELECT count() FROM system.parts
				WHERE database = currentDatabase() AND active AND table = 'logs'
					AND toUInt32OrZero(splitByChar('-', partition_id)[1]) IN ?) > 0
			OR (SELECT count() FROM (
				SELECT 1 FROM log_ingest_counts WHERE project_id IN ? AND logs_received > 0 LIMIT 1)) > 0`,
		projectIDs, projectIDs).Scan(&has); err != nil {
		return false, fmt.Errorf("find whether Projects have Logs: %w", err)
	}
	return has, nil
}

// IngestDay sums a day of a scope's Ingest counts, the Projects together or
// one Project's (ProjectID is 0 for the Projects together), with its
// busiest second for Logs.
type IngestDay struct {
	models.IngestCount
	Day      time.Time
	PeakLogs Peak
}

// IngestDays sums a scope's Ingest counts by UTC day of arrival, the
// Projects together, in no order. A day's peak sums the Projects in each
// second.
func (s *ClickHouseLogs) IngestDays(ctx context.Context, scope OverviewScope) ([]IngestDay, error) {
	return s.ingestDays(ctx, scope, false)
}

// ProjectIngestDays sums a scope's Ingest counts by Project and UTC day of
// arrival, in no order.
func (s *ClickHouseLogs) ProjectIngestDays(ctx context.Context, scope OverviewScope) ([]IngestDay, error) {
	return s.ingestDays(ctx, scope, true)
}

func (s *ClickHouseLogs) ingestDays(ctx context.Context, scope OverviewScope, byProject bool) ([]IngestDay, error) {
	project, by := "toUInt32(0)", ""
	if byProject {
		project, by = "project_id", "project_id, "
	}
	rows, err := s.conn.Query(ctx, `
		SELECT `+project+`, toDate(second) AS day, toInt64(sum(l)), toInt64(sum(u)), toInt64(sum(b)), toInt64(sum(w)),
			toInt64(sum(r)), toInt64(sum(o)), toInt64(sum(s)), toInt64(sum(sb)), `+peakOf("l")+`
		FROM (`+ingestSeconds(by)+`)
		GROUP BY `+by+`day`, scope.ProjectIDs, scope.From, scope.To)
	if err != nil {
		return nil, fmt.Errorf("sum Ingest counts by day: %w", err)
	}
	defer rows.Close()
	var days []IngestDay
	for rows.Next() {
		var d IngestDay
		if err := rows.Scan(&d.ProjectID, &d.Day, &d.LogsReceived, &d.LogsUnreadable, &d.BytesReceived, &d.BytesOnWire,
			&d.Requests, &d.RequestsOverloaded, &d.SpansReceived, &d.SpanBytesReceived, &d.PeakLogs.Count, &d.PeakLogs.At); err != nil {
			return nil, err
		}
		d.Day = LogDay(d.Day)
		d.PeakLogs.settle()
		days = append(days, d)
	}
	return days, rows.Err()
}

// StoredProject is what ClickHouse holds of a Project's Logs across every
// day it keeps, by logs' parts: its Logs, their bytes on local disk and in
// the bucket, and its oldest day.
type StoredProject struct {
	ProjectID uint32
	Logs      int64
	Bytes     int64
	OldestDay time.Time
}

// StoredProjects sums the stored Logs of each Project of projectIDs that
// has any, across every day, in no order.
func (s *ClickHouseLogs) StoredProjects(ctx context.Context, projectIDs []uint32) ([]StoredProject, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT toUInt32OrZero(splitByChar('-', partition_id)[1]) AS project_id, toInt64(sum(rows)), toInt64(sum(bytes_on_disk)),
			min(partition_id)
		FROM system.parts
		WHERE database = currentDatabase() AND table = 'logs' AND active AND project_id IN ?
		GROUP BY project_id`, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("sum the parts of stored Logs by Project: %w", err)
	}
	defer rows.Close()
	var projects []StoredProject
	for rows.Next() {
		var p StoredProject
		var oldest string
		if err := rows.Scan(&p.ProjectID, &p.Logs, &p.Bytes, &oldest); err != nil {
			return nil, err
		}
		// A Project's partitions differ only in their day, so the least is its oldest.
		if _, p.OldestDay, err = parseLogPartitionID(oldest); err != nil {
			return nil, fmt.Errorf("oldest partition %q of Project %d's Logs: %w", oldest, p.ProjectID, err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// LastLogArrivals is the second each Project of projectIDs last received a
// Log in, by its Ingest counts, for those that have.
func (s *ClickHouseLogs) LastLogArrivals(ctx context.Context, projectIDs []uint32) (map[uint32]time.Time, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT project_id, max(second) FROM log_ingest_counts
		WHERE project_id IN ? AND logs_received > 0
		GROUP BY project_id`, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("find when Projects' last Logs arrived: %w", err)
	}
	defer rows.Close()
	last := map[uint32]time.Time{}
	for rows.Next() {
		var projectID uint32
		var at time.Time
		if err := rows.Scan(&projectID, &at); err != nil {
			return nil, err
		}
		last[projectID] = at.UTC()
	}
	return last, rows.Err()
}
