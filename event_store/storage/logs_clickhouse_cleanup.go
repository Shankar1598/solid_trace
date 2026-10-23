package storage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/solidtrace/event_store/pkg/logger"
)

// Retention, Project deletion and the disk limit delete Logs a (Project, day)
// at a time: every table of Logs is partitioned by Project and UTC day, so
// each drops whole partitions, which costs ClickHouse no rewrite.

// logDayTables are the tables partitioned by (Project, day) that hold a
// Project's Logs and what is counted from them.
var logDayTables = []string{"logs", "log_minute_counts", "log_key_counts"}

// cleanupTimeout bounds one pass's statements: listing the partitions, and
// dropping or moving one.
const cleanupTimeout = 30 * time.Minute

// logPartition is a table's partition of one Project's UTC day.
type logPartition struct {
	table string
	// id is ClickHouse's ID of the partition: for a key of whole numbers
	// and dates, their values joined by "-", as "123-20261002".
	id        string
	projectID uint32
	day       time.Time
	// localBytes is how much of the local disk its parts take; the rest of
	// them are in the bucket.
	localBytes int64
}

// logPartitions lists the partitions of the Log tables that hold any rows.
func (s *ClickHouseLogs) logPartitions(ctx context.Context) ([]logPartition, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT table, partition_id,
			toInt64(sumIf(bytes_on_disk, disk_name IN (SELECT name FROM system.disks WHERE NOT is_remote)))
		FROM system.parts
		WHERE database = currentDatabase() AND active AND table IN ('`+strings.Join(logDayTables, "', '")+`')
		GROUP BY table, partition_id`)
	if err != nil {
		return nil, fmt.Errorf("list the partitions of Logs: %w", err)
	}
	defer rows.Close()
	var partitions []logPartition
	for rows.Next() {
		var p logPartition
		if err := rows.Scan(&p.table, &p.id, &p.localBytes); err != nil {
			return nil, err
		}
		if p.projectID, p.day, err = parseLogPartitionID(p.id); err != nil {
			return nil, fmt.Errorf("partition %s of %s: %w", p.id, p.table, err)
		}
		partitions = append(partitions, p)
	}
	return partitions, rows.Err()
}

// parseLogPartitionID reads the Project and the day from the ID of a
// partition of a Log table.
func parseLogPartitionID(id string) (uint32, time.Time, error) {
	project, day, ok := strings.Cut(id, "-")
	if !ok {
		return 0, time.Time{}, errors.New("not a Project's day")
	}
	projectID, err := strconv.ParseUint(project, 10, 32)
	if err != nil {
		return 0, time.Time{}, err
	}
	at, err := time.Parse("20060102", day)
	if err != nil {
		return 0, time.Time{}, err
	}
	return uint32(projectID), at, nil
}

// LogProjects lists every Project that has any Logs in ClickHouse, or
// counts of them.
func (s *ClickHouseLogs) LogProjects() ([]uint32, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	partitions, err := s.logPartitions(ctx)
	if err != nil {
		return nil, err
	}
	var projects []uint32
	for _, p := range partitions {
		projects = append(projects, p.projectID)
	}
	slices.Sort(projects)
	return slices.Compact(projects), nil
}

// DeleteLogDaysBefore deletes a Project's Logs of every UTC day before that
// of day, with their counts.
func (s *ClickHouseLogs) DeleteLogDaysBefore(projectID uint32, day time.Time) error {
	day = LogDay(day)
	return s.dropLogDays(func(p logPartition) bool { return p.projectID == projectID && p.day.Before(day) })
}

// DeleteProjectLogs deletes every Log of a Project, with their counts. Its
// Processing cursor, in Pebble, stays, so that a Log batch it already stored
// is never stored again.
func (s *ClickHouseLogs) DeleteProjectLogs(projectID uint32) error {
	return s.dropLogDays(func(p logPartition) bool { return p.projectID == projectID })
}

// dropLogDays drops the partitions of the Log tables that drop selects.
func (s *ClickHouseLogs) dropLogDays(drop func(logPartition) bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	partitions, err := s.logPartitions(ctx)
	if err != nil {
		return err
	}
	return s.dropPartitions(ctx, slices.DeleteFunc(partitions, func(p logPartition) bool { return !drop(p) }))
}

// dropPartitions drops partitions of the Log tables. Then, whether or not
// every drop succeeded, it changes the generations of their days' hours, so
// that no bin the bin cache kept of them is taken again, and forgets their
// days' key counts kept in memory.
//
// countsMu is held throughout, so that no insert counts a day's keys while
// its counts are dropped.
func (s *ClickHouseLogs) dropPartitions(ctx context.Context, partitions []logPartition) error {
	if len(partitions) == 0 {
		return nil
	}
	s.countsMu.Lock()
	defer s.countsMu.Unlock()
	days := map[projectDay]bool{}
	defer func() {
		for day := range days {
			delete(s.counts, day)
			s.generations.bumpDay(day.projectID, day.day)
		}
	}()

	// A day of a busy Project can pass ClickHouse's 50 GB guard on drops.
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"max_partition_size_to_drop": 0}))
	slices.SortFunc(partitions, func(a, b logPartition) int {
		return cmp.Or(a.day.Compare(b.day), cmp.Compare(a.projectID, b.projectID), cmp.Compare(a.table, b.table))
	})
	for _, p := range partitions {
		days[projectDay{p.projectID, p.day}] = true
		if err := s.conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION ID '%s'", p.table, p.id)); err != nil {
			return fmt.Errorf("drop Project %d's %s of %s: %w", p.projectID, p.day.Format(time.DateOnly), p.table, err)
		}
	}
	return nil
}

// LogDayUse is how much local disk a Project's UTC day of Logs takes, with
// what is counted from them.
type LogDayUse struct {
	ProjectID uint32
	Day       time.Time
	Bytes     int64
}

// logDay is a Project's day of Logs on local disk, with its partitions.
type logDay struct {
	LogDayUse
	partitions []logPartition
}

// localLogDays groups partitions by Project and day, oldest day first and
// then by Project, leaving out days with nothing on local disk.
func localLogDays(partitions []logPartition) []logDay {
	byDay := map[projectDay]*logDay{}
	for _, p := range partitions {
		key := projectDay{p.projectID, p.day}
		if byDay[key] == nil {
			byDay[key] = &logDay{LogDayUse: LogDayUse{ProjectID: p.projectID, Day: p.day}}
		}
		byDay[key].Bytes += p.localBytes
		byDay[key].partitions = append(byDay[key].partitions, p)
	}
	var days []logDay
	for _, day := range byDay {
		if day.Bytes > 0 {
			days = append(days, *day)
		}
	}
	slices.SortFunc(days, func(a, b logDay) int {
		return cmp.Or(a.Day.Compare(b.Day), cmp.Compare(a.ProjectID, b.ProjectID))
	})
	return days
}

// LogDiskUse returns how much local disk each Project's day of Logs takes,
// oldest day first; a day wholly in the bucket is left out.
func (s *ClickHouseLogs) LogDiskUse() ([]LogDayUse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	partitions, err := s.logPartitions(ctx)
	if err != nil {
		return nil, err
	}
	var use []LogDayUse
	for _, day := range localLogDays(partitions) {
		use = append(use, day.LogDayUse)
	}
	return use, nil
}

// EnforceDiskLimit keeps the Logs on local disk, with what is counted from
// them, within limit, by deleting the oldest days of any Project.
func (s *ClickHouseLogs) EnforceDiskLimit(limit LogDiskLimit) error {
	if limit.Bytes <= 0 {
		return fmt.Errorf("no Log disk limit: %d bytes", limit.Bytes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	partitions, err := s.logPartitions(ctx)
	if err != nil {
		return err
	}
	days := localLogDays(partitions)
	var total int64
	for _, day := range days {
		total += day.Bytes
	}
	for _, day := range days {
		if total <= limit.Bytes {
			break
		}
		logger.L.Info("Deleting the oldest day of Logs past the disk limit",
			"project_id", day.ProjectID, "day", day.Day.Format(time.DateOnly), "bytes", day.Bytes, "limit", limit.Bytes)
		if err := s.dropPartitions(ctx, day.partitions); err != nil {
			return err
		}
		total -= day.Bytes
	}
	return nil
}
