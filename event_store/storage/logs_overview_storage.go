package storage

import (
	"context"
	"fmt"
	"time"
)

// The Overview's Storage reads, the whole install's: ClickHouse's local disk
// and the day tables' parts on it and in the bucket, and the old Log files.

// InstallStorage is how much the install's days take, every Project's of
// every family of day tables.
type InstallStorage struct {
	// LocalBytes is what the disk limit counts: the families' main tables'
	// parts on local disk and the old Log files' local copies.
	LocalBytes int64
	// BucketBytes is the main tables' parts in the bucket.
	BucketBytes int64
	// RecentBytes is the main tables' parts, local and in the bucket, of
	// the days from the day of from up to the day of to, not included.
	RecentBytes int64
	// RecentBytesByProject is RecentBytes by family's Name and by Project,
	// of those with any.
	RecentBytesByProject map[string]map[uint32]int64
	// DiskBytes and FreeBytes are the size of ClickHouse's local disk for
	// the logs table, and how much of it is free, read now.
	DiskBytes, FreeBytes int64
	// Bucket is whether the logs table's parts move to a bucket.
	Bucket bool
	// OldFiles is the old Log files, or nil with none.
	OldFiles *OldLogFilesStorage
}

// OldLogFilesStorage is the old Log files kept: how many, local or only in
// the bucket, how much disk their local copies take, read now, and each
// Project's newest day of them. Their bucket bytes aren't recorded.
type OldLogFilesStorage struct {
	Files      int
	LocalBytes int64
	NewestDays map[uint32]time.Time
}

// logsLocalDisks selects the names of the local disks of the logs table's
// storage policy.
const logsLocalDisks = `
	SELECT name FROM system.disks
	WHERE NOT is_remote AND name IN (
		SELECT arrayJoin(disks) FROM system.storage_policies
		WHERE policy_name = (SELECT storage_policy FROM system.tables WHERE database = currentDatabase() AND name = 'logs'))`

// InstallStorage reads how much the install's days take, with the days from
// the day of from up to the day of to as RecentBytes.
func (s *ClickHouseLogs) InstallStorage(ctx context.Context, from, to time.Time) (InstallStorage, error) {
	got := InstallStorage{Bucket: s.bucket, RecentBytesByProject: map[string]map[uint32]int64{}}
	family := map[string]string{}
	var tables []string
	for _, f := range dayFamilies {
		family[f.Tables[0]] = f.Name
		tables = append(tables, f.Tables[0])
	}
	partitions, err := s.conn.Query(ctx, `
		SELECT table, partition_id, `+localAndBucketBytes+`
		FROM system.parts
		WHERE database = currentDatabase() AND table IN ? AND active
		GROUP BY table, partition_id`, tables)
	if err != nil {
		return got, fmt.Errorf("list the parts of the install's days: %w", err)
	}
	defer partitions.Close()
	from, to = LogDay(from), LogDay(to)
	for partitions.Next() {
		var (
			table, id     string
			local, bucket int64
		)
		if err := partitions.Scan(&table, &id, &local, &bucket); err != nil {
			return got, err
		}
		projectID, day, err := parseLogPartitionID(id)
		if err != nil {
			return got, fmt.Errorf("partition %s of %s: %w", id, table, err)
		}
		got.LocalBytes += local
		got.BucketBytes += bucket
		if !day.Before(from) && day.Before(to) {
			got.RecentBytes += local + bucket
			byProject := got.RecentBytesByProject[family[table]]
			if byProject == nil {
				byProject = map[uint32]int64{}
				got.RecentBytesByProject[family[table]] = byProject
			}
			byProject[projectID] += local + bucket
		}
	}
	if err := partitions.Err(); err != nil {
		return got, err
	}

	if err := s.conn.QueryRow(ctx, `
		SELECT toInt64(sum(total_space)), toInt64(sum(free_space)) FROM system.disks
		WHERE name IN (`+logsLocalDisks+`)`).Scan(&got.DiskBytes, &got.FreeBytes); err != nil {
		return got, fmt.Errorf("read ClickHouse's local disk: %w", err)
	}

	got.OldFiles = s.old.storage()
	if got.OldFiles != nil {
		got.LocalBytes += got.OldFiles.LocalBytes
	}
	return got, nil
}
