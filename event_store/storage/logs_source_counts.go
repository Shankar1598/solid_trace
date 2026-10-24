package storage

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/solidtrace/event_store/models"
)

// logSourceCountsMigration makes the Source counts table: the Logs and bytes
// Log processing kept of each Project's day, Service and file, and the Logs
// the Log transform dropped of each. Partitioned and deduplicated like the
// key counts, it is one of the day tables. Its version's hundreds are the
// ticket that added it (Overview 03, 10xx).
var logSourceCountsMigration = clickHouseMigration{version: 1003, statements: []string{
	`CREATE TABLE IF NOT EXISTS log_source_counts (
		project_id UInt32,
		day Date,
		service LowCardinality(String),
		file String,
		logs Int64,
		bytes Int64,
		logs_dropped_by_transform Int64
	) ENGINE = SummingMergeTree
	PARTITION BY (project_id, day)
	ORDER BY (project_id, day, service, file)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(logsDedupWindow),
}}

// LogSource is where a Log comes from: its own UTC day, its Service and its
// file Attribute, "" for none.
type LogSource struct {
	Day           time.Time
	Service, File string
}

// SourceOf is where log comes from.
func SourceOf(log models.Log) LogSource {
	return LogSource{Day: LogDay(log.Time), Service: log.Service, File: log.Attributes.String["file"]}
}

func (s LogSource) compare(o LogSource) int {
	return cmp.Or(s.Day.Compare(o.Day), cmp.Compare(s.Service, o.Service), cmp.Compare(s.File, o.File))
}

// logSize measures Logs as Source counts do: a Log's message and its
// Attributes' JSON. It counts bytes and keeps none.
type logSize struct {
	n   int64
	enc *jsontext.Encoder
}

func (w *logSize) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// of is log's size.
func (w *logSize) of(log models.Log) int64 {
	if w.enc == nil {
		w.enc = jsontext.NewEncoder(w)
	}
	w.n = 0
	w.enc.Reset(w)
	if err := log.Attributes.MarshalJSONTo(w.enc); err != nil {
		return int64(len(log.Message))
	}
	// The encoder ends each value with a newline, which isn't the JSON's.
	return int64(len(log.Message)) + w.n - 1
}

// sourceCount is a row of the Source counts table.
type sourceCount struct {
	logs, bytes, dropped int64
}

// insertSourceCounts inserts the Logs an insert keeps and the Logs the Log
// transform dropped of its range, by source, sorted, so that the range
// inserted again makes the same block.
func (s *ClickHouseLogs) insertSourceCounts(ctx context.Context, projectID uint32, insert LogInsert) error {
	counts := map[LogSource]sourceCount{}
	var size logSize
	for _, batch := range insert.Batches {
		for _, log := range batch.Logs {
			source := SourceOf(log)
			c := counts[source]
			c.logs++
			c.bytes += size.of(log)
			counts[source] = c
		}
	}
	for source, n := range insert.Dropped {
		c := counts[source]
		c.dropped += n
		counts[source] = c
	}
	if len(counts) == 0 {
		return nil
	}
	block, err := s.conn.PrepareBatch(ctx, "INSERT INTO log_source_counts (project_id, day, service, file, logs, bytes, logs_dropped_by_transform)")
	if err != nil {
		return err
	}
	defer block.Abort()
	for _, source := range slices.SortedFunc(maps.Keys(counts), LogSource.compare) {
		c := counts[source]
		if err := block.Append(projectID, source.Day, source.Service, source.File, c.logs, c.bytes, c.dropped); err != nil {
			return err
		}
	}
	return block.Send()
}

// SourceCount is what a Project's Service and file sent over a range of the
// Logs' own days: the Logs kept and their bytes, and the Logs the Log
// transform dropped.
type SourceCount struct {
	ProjectID          uint32
	Service, File      string
	Logs, Bytes        int64
	DroppedByTransform int64
}

// SourceCounts sums the Source counts of each Project of projectIDs, Service
// and file over the Logs' days from the day of from to the day of to, in no
// order.
func (s *ClickHouseLogs) SourceCounts(ctx context.Context, projectIDs []uint32, from, to time.Time) ([]SourceCount, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT project_id, service, file, sum(logs), sum(bytes), sum(logs_dropped_by_transform)
		FROM log_source_counts
		WHERE project_id IN ? AND day >= toDate(?) AND day <= toDate(?)
		GROUP BY project_id, service, file`, projectIDs, dayString(from), dayString(to))
	if err != nil {
		return nil, fmt.Errorf("sum Source counts: %w", err)
	}
	defer rows.Close()
	var counts []SourceCount
	for rows.Next() {
		var c SourceCount
		if err := rows.Scan(&c.ProjectID, &c.Service, &c.File, &c.Logs, &c.Bytes, &c.DroppedByTransform); err != nil {
			return nil, err
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}
