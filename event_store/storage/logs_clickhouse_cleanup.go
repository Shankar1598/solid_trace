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

// Retention, Project deletion and the disk limit delete a (Project, day) at a
// time: every table they clean up is partitioned by Project and UTC day, so
// each drops whole partitions, which costs ClickHouse no rewrite.

// A DayFamily is a family of tables partitioned by (Project, day) whose days
// go together: past the family's own Retention, with their Project, and past
// the disk limit.
type DayFamily struct {
	// Name is what its rows are called in messages, as "Logs".
	Name string
	// Tables are its tables, its main one first. The disk limit counts the
	// main table's local disk and moves its days to the bucket, where its
	// parts also move once local_cache_hours old (useBucket). The others
	// hold what is counted from it, which takes far less and stays on local
	// disk.
	Tables []string
	// TimeColumn is the main table's time, which its parts move to the
	// bucket by.
	TimeColumn string
	// logs is set on LogDays alone, whose days are also in the old Log
	// files from before the switch.
	logs bool
}

// LogDays are a Project's Logs and what is counted from them. The Ingest
// counts' day is the day of arrival, not the Logs' own.
var LogDays = DayFamily{
	Name:       "Logs",
	Tables:     []string{"logs", "log_minute_counts", "log_key_counts", "log_ingest_counts", "log_source_counts"},
	TimeColumn: "time",
	logs:       true,
}

// dayFamilies are EventStore's families of day tables.
var dayFamilies = []DayFamily{LogDays, SpanDays}

// cleanupTimeout bounds a deletion, or a pass of the disk limit, whose moves
// of days to the bucket can take minutes each.
const cleanupTimeout = 30 * time.Minute

// logPartition is a day table's partition of one Project's UTC day.
type logPartition struct {
	table string
	// main is whether the table is its family's main one.
	main bool
	// id is ClickHouse's ID of the partition: for a key of whole numbers
	// and dates, their values joined by "-", as "123-20261002".
	id        string
	projectID uint32
	day       time.Time
	// localBytes is how much of the local disk its parts take; the rest of
	// them are in the bucket.
	localBytes int64
}

// logPartitions lists the partitions of the families' tables that hold any
// rows.
func (s *ClickHouseLogs) logPartitions(ctx context.Context, families []DayFamily) ([]logPartition, error) {
	var tables, names []string
	main := map[string]bool{}
	for _, f := range families {
		tables = append(tables, f.Tables...)
		names = append(names, f.Name)
		main[f.Tables[0]] = true
	}
	rows, err := s.conn.Query(ctx, `
		SELECT table, partition_id,
			toInt64(sumIf(bytes_on_disk, disk_name IN (SELECT name FROM system.disks WHERE NOT is_remote)))
		FROM system.parts
		WHERE database = currentDatabase() AND active AND table IN ('`+strings.Join(tables, "', '")+`')
		GROUP BY table, partition_id`)
	if err != nil {
		return nil, fmt.Errorf("list the partitions of %s: %w", strings.Join(names, " and "), err)
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
		p.main = main[p.table]
		partitions = append(partitions, p)
	}
	return partitions, rows.Err()
}

// logPartitionDay is how a day table's partition ID writes its day.
const logPartitionDay = "20060102"

// logPartitionID is the ID of a Project's day's partition of a day table,
// as parseLogPartitionID reads it.
func logPartitionID(projectID uint32, day time.Time) string {
	return fmt.Sprintf("%d-%s", projectID, day.Format(logPartitionDay))
}

// parseLogPartitionID reads the Project and the day from the ID of a
// partition of a day table.
func parseLogPartitionID(id string) (uint32, time.Time, error) {
	project, day, ok := strings.Cut(id, "-")
	if !ok {
		return 0, time.Time{}, errors.New("not a Project's day")
	}
	projectID, err := strconv.ParseUint(project, 10, 32)
	if err != nil {
		return 0, time.Time{}, err
	}
	at, err := time.Parse(logPartitionDay, day)
	if err != nil {
		return 0, time.Time{}, err
	}
	return uint32(projectID), at, nil
}

// DayProjects lists every Project that has any days of a family: of Logs,
// in ClickHouse or the old Log files, or counts of them.
func (s *ClickHouseLogs) DayProjects(f DayFamily) ([]uint32, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	partitions, err := s.logPartitions(ctx, []DayFamily{f})
	if err != nil {
		return nil, err
	}
	var projects []uint32
	if f.logs {
		if projects, err = s.old.projects(ctx); err != nil {
			return nil, err
		}
	}
	for _, p := range partitions {
		projects = append(projects, p.projectID)
	}
	slices.Sort(projects)
	return slices.Compact(projects), nil
}

// DeleteDaysBefore deletes a Project's days of a family before that of day:
// of Logs, with their counts and its old Log files of those days.
func (s *ClickHouseLogs) DeleteDaysBefore(f DayFamily, projectID uint32, day time.Time) error {
	day = LogDay(day)
	return s.deleteDays(f, projectID, func(d time.Time) bool { return d.Before(day) })
}

// DeleteProjectDays deletes every day of a family of a Project: of Logs,
// with their counts and its old Log files. Its Processing cursor, in
// Pebble, stays, so that a Log batch it already stored is never stored
// again.
func (s *ClickHouseLogs) DeleteProjectDays(f DayFamily, projectID uint32) error {
	return s.deleteDays(f, projectID, func(time.Time) bool { return true })
}

// deleteDays deletes the days of a family of a Project that drop selects.
func (s *ClickHouseLogs) deleteDays(f DayFamily, projectID uint32, drop func(time.Time) bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	partitions, err := s.logPartitions(ctx, []DayFamily{f})
	if err == nil {
		err = s.dropPartitions(ctx, slices.DeleteFunc(partitions, func(p logPartition) bool {
			return p.projectID != projectID || !drop(p.day)
		}))
	}
	if !f.logs {
		return err
	}
	return errors.Join(err, s.deleteOldDays(projectID, drop))
}

// dropPartitions drops partitions of day tables, oldest day first. Then,
// whether or not every drop succeeded, it changes the generations of their
// days' hours, so that no bin the bin cache kept of them is taken again, and
// forgets their days' key counts kept in memory.
//
// The key counts' partitions go last, under countsMu, so that no insert
// counts a day's keys from the counts in memory while they are dropped.
func (s *ClickHouseLogs) dropPartitions(ctx context.Context, partitions []logPartition) error {
	if len(partitions) == 0 {
		return nil
	}
	days := map[projectDay]bool{}
	defer func() {
		for day := range days {
			s.generations.bumpDay(day.projectID, day.day)
		}
	}()

	// A day of a busy Project can pass ClickHouse's 50 GB guard on drops.
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"max_partition_size_to_drop": 0}))
	slices.SortFunc(partitions, func(a, b logPartition) int {
		return cmp.Or(a.day.Compare(b.day), cmp.Compare(a.projectID, b.projectID), cmp.Compare(a.table, b.table))
	})
	drop := func(p logPartition) error {
		days[projectDay{p.projectID, p.day}] = true
		if err := s.conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION ID '%s'", p.table, p.id)); err != nil {
			return fmt.Errorf("drop Project %d's %s of %s: %w", p.projectID, p.day.Format(time.DateOnly), p.table, err)
		}
		return nil
	}
	var counts []logPartition
	for _, p := range partitions {
		if p.table == "log_key_counts" {
			counts = append(counts, p)
		} else if err := drop(p); err != nil {
			return err
		}
	}

	s.countsMu.Lock()
	defer s.countsMu.Unlock()
	defer func() {
		for day := range days {
			delete(s.counts, day)
		}
	}()
	for _, p := range counts {
		if err := drop(p); err != nil {
			return err
		}
	}
	return nil
}

// defaultDiskShare is the share of ClickHouse's local disk that Logs may
// take when no limit is configured. Logs are the largest thing on the disk,
// but Events, Pebble and the system share it; half leaves them room to
// grow, and doesn't move when they do.
const defaultDiskShare = 50 // percent

// LogDiskLimit caps how much local disk all Projects' days take together,
// those of every family of day tables.
type LogDiskLimit struct {
	// Bytes is the cap, more than 0.
	Bytes int64
}

// DiskLimit is the disk limit: configured bytes if more than 0, else
// defaultDiskShare of the size of ClickHouse's local disk, read now.
func (s *ClickHouseLogs) DiskLimit(configured int64) (LogDiskLimit, error) {
	if configured > 0 {
		return LogDiskLimit{Bytes: configured}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	var size int64
	err := s.conn.QueryRow(ctx, `
		SELECT toInt64(sum(total_space)) FROM system.disks WHERE name IN (`+logsLocalDisks+`)`).Scan(&size)
	if err != nil {
		return LogDiskLimit{}, fmt.Errorf("read the size of ClickHouse's local disk: %w", err)
	}
	if size <= 0 {
		return LogDiskLimit{}, errors.New("read the size of ClickHouse's local disk: the logs table has no local disk")
	}
	return LogDiskLimit{Bytes: size * defaultDiskShare / 100}, nil
}

// LogDayUse is how much local disk a Project's UTC day takes.
type LogDayUse struct {
	ProjectID uint32
	Day       time.Time
	Bytes     int64
}

// logDay is a Project's day on local disk, with its partitions of every day
// table.
type logDay struct {
	LogDayUse
	partitions []logPartition
}

// localLogDays groups partitions by Project and day, oldest day first and
// then by Project, leaving out the days whose main tables' partitions are
// wholly in the bucket. A day's size is that of those partitions alone: what
// is counted from them takes far less, and stays on local disk.
func localLogDays(partitions []logPartition) ([]logDay, int64) {
	byDay := map[projectDay]*logDay{}
	for _, p := range partitions {
		key := projectDay{p.projectID, p.day}
		if byDay[key] == nil {
			byDay[key] = &logDay{LogDayUse: LogDayUse{ProjectID: p.projectID, Day: p.day}}
		}
		if p.main {
			byDay[key].Bytes += p.localBytes
		}
		byDay[key].partitions = append(byDay[key].partitions, p)
	}
	var days []logDay
	var total int64
	for _, day := range byDay {
		if day.Bytes > 0 {
			days = append(days, *day)
			total += day.Bytes
		}
	}
	slices.SortFunc(days, func(a, b logDay) int {
		return cmp.Or(a.Day.Compare(b.Day), cmp.Compare(a.ProjectID, b.ProjectID))
	})
	return days, total
}

// LogDiskUse returns how much local disk each Project's day of Logs takes,
// old Log files' local copies included, oldest day first; a day wholly in
// the bucket is left out.
func (s *ClickHouseLogs) LogDiskUse() ([]LogDayUse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	partitions, err := s.logPartitions(ctx, []DayFamily{LogDays})
	if err != nil {
		return nil, err
	}
	days, _ := localLogDays(partitions)
	oldDays, _ := s.old.localDays()
	byDay := map[projectDay]int64{}
	for _, day := range days {
		byDay[projectDay{day.ProjectID, day.Day}] += day.Bytes
	}
	for _, day := range oldDays {
		byDay[day.projectDay] += day.bytes
	}
	var use []LogDayUse
	for day, bytes := range byDay {
		use = append(use, LogDayUse{ProjectID: day.projectID, Day: day.day, Bytes: bytes})
	}
	slices.SortFunc(use, func(a, b LogDayUse) int {
		return cmp.Or(a.Day.Compare(b.Day), cmp.Compare(a.ProjectID, b.ProjectID))
	})
	return use, nil
}

// EnforceDiskLimit keeps the families' days on local disk within limit,
// with the local copies of the old Log files from before the switch.
//
// First it deletes the local copies of the oldest old files that the bucket
// has, which reads then read there. With a bucket, it then moves the oldest
// days of any Project there, as they would move once local_cache_hours old.
// If the days on local disk are still past the limit, the bucket must be
// failing: it deletes the oldest days, and logs each as an error. Without a
// bucket, it deletes the oldest days. Either way, the oldest days of old
// files go before the oldest partitions. A Project's day goes from every
// family at once, each main table's partition with what is counted from it.
func (s *ClickHouseLogs) EnforceDiskLimit(limit LogDiskLimit, families []DayFamily) error {
	if limit.Bytes <= 0 {
		return fmt.Errorf("no Log disk limit: %d bytes", limit.Bytes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	measure := func() ([]logDay, []oldLocalDay, int64, error) {
		partitions, err := s.logPartitions(ctx, families)
		if err != nil {
			return nil, nil, 0, err
		}
		days, total := localLogDays(partitions)
		oldDays, oldTotal := s.old.localDays()
		return days, oldDays, total + oldTotal, nil
	}
	days, oldDays, total, err := measure()
	if err != nil || total <= limit.Bytes {
		return err
	}
	if total = s.old.freeLocalCopies(oldDays, total, limit.Bytes); total <= limit.Bytes {
		return nil
	}
	if s.bucket {
		s.moveForTheDiskLimit(ctx, days, total, limit.Bytes)
	}
	// Measured again: a move or a deletion may have failed, or the TTL
	// moved parts meanwhile.
	if days, oldDays, total, err = measure(); err != nil {
		return err
	}
	for _, day := range oldDays {
		if total <= limit.Bytes {
			break
		}
		args := []any{"project_id", day.projectID, "day", day.day.Format(time.DateOnly), "bytes", day.bytes, "limit", limit.Bytes}
		if s.bucket {
			logger.L.Error("Deleting the oldest day of old Log files past the disk limit, as their local copies didn't go", args...)
		} else {
			logger.L.Info("Deleting the oldest day of old Log files past the disk limit", args...)
		}
		if err := s.deleteOldDays(day.projectID, day.day.Equal); err != nil {
			return err
		}
		total -= day.bytes
	}
	for _, day := range days {
		if total <= limit.Bytes {
			break
		}
		args := []any{"project_id", day.ProjectID, "day", day.Day.Format(time.DateOnly), "bytes", day.Bytes, "limit", limit.Bytes}
		if s.bucket {
			logger.L.Error("Deleting the oldest day past the disk limit, as it didn't move to the bucket", args...)
		} else {
			logger.L.Info("Deleting the oldest day past the disk limit", args...)
		}
		if err := s.dropPartitions(ctx, day.partitions); err != nil {
			return err
		}
		total -= day.Bytes
	}
	return nil
}

// moveForTheDiskLimit moves the main tables' partitions of the oldest days on
// local disk to the bucket until those left there take no more than
// maxBytes. A move that fails is logged, and the next day is tried.
func (s *ClickHouseLogs) moveForTheDiskLimit(ctx context.Context, days []logDay, total, maxBytes int64) {
	moved, failed := 0, 0
	for _, day := range days {
		if total <= maxBytes {
			break
		}
		for _, p := range day.partitions {
			if !p.main || p.localBytes == 0 {
				continue
			}
			err := s.conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s MOVE PARTITION ID '%s' TO VOLUME '%s'", p.table, p.id, ClickHouseBucketVolume))
			if err != nil {
				failed++
				logger.L.Error("Moving a day to the bucket for the disk limit failed",
					"table", p.table, "project_id", day.ProjectID, "day", day.Day.Format(time.DateOnly), "error", err)
				continue
			}
			moved++
		}
		total -= day.Bytes
	}
	if moved > 0 {
		logger.L.Info("Moved the oldest days past the disk limit to the bucket", "partitions", moved, "failed", failed, "limit", maxBytes)
	}
}
