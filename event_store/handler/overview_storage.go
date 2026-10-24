package handler

import (
	"context"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// The Overview's Storage, the whole install's, which Console asks for
// Instance admins only: ClickHouse's local disk against the disk limit in
// effect, the bucket, about how many days until the limit at the last 7
// days' growth, and the old Log files from before the switch to ClickHouse.

// growthDays is how many whole days before today the growth is measured
// over.
const growthDays = 7

// RetentionReader gives each Project's Retention in days, as Console holds
// it; a Project missing from it has been deleted.
type RetentionReader interface {
	ProjectLogRetention() (map[uint32]int, error)
}

// overviewStorageData is what Storage's parts read.
type overviewStorageData struct {
	install   storage.InstallStorage
	retention map[uint32]int
}

// storageParts are Storage's reads, each filling its own field of d, or
// none when it isn't asked for.
func (h *OverviewHandler) storageParts(q overviewQuery, d *overviewStorageData) []func(context.Context) error {
	if !q.storage {
		return nil
	}
	today := storage.LogDay(q.now)
	return []func(context.Context) error{
		func(ctx context.Context) (err error) {
			d.install, err = h.logs.InstallStorage(ctx, today.AddDate(0, 0, -growthDays), today)
			return err
		},
		func(context.Context) (err error) {
			if h.retention != nil {
				d.retention, err = h.retention.ProjectLogRetention()
			}
			return err
		},
	}
}

// Storage's answer.
type (
	storageAnswer struct {
		DiskLimitBytes int64 `json:"disk_limit_bytes"`
		LocalBytes     int64 `json:"local_bytes"`
		DiskBytes      int64 `json:"disk_bytes"`
		FreeBytes      int64 `json:"free_bytes"`
		Bucket         bool  `json:"bucket"`
		BucketBytes    int64 `json:"bucket_bytes"`
		// GrowthBytesPerDay is the bytes stored a day of the last 7 whole
		// days' Logs, local and in the bucket.
		GrowthBytesPerDay float64 `json:"growth_bytes_per_day"`
		// DaysUntilLimit is about how many days until the local disk use
		// reaches the limit at that growth, 0 once it has; null with a
		// bucket, which the Logs move to after local_cache_hours, with no
		// growth, or with no limit.
		DaysUntilLimit *float64         `json:"days_until_limit"`
		OldFiles       *oldFilesStorage `json:"old_files"`
	}
	// oldFilesStorage is the old Log files kept: how many, the disk their
	// local copies take, their newest day, and the day Retention deletes
	// the last of them (null when no Project of them has a Retention:
	// Console deleted them, or it's invalid).
	oldFilesStorage struct {
		Files             int     `json:"files"`
		LocalBytes        int64   `json:"local_bytes"`
		NewestDay         string  `json:"newest_day"`
		PassesRetentionOn *string `json:"passes_retention_on"`
	}
)

// assembleStorage makes Storage's answer, or nil when it isn't asked for.
func assembleStorage(q overviewQuery, limit storage.LogDiskLimit, d overviewStorageData) *storageAnswer {
	if !q.storage {
		return nil
	}
	in := d.install
	a := &storageAnswer{
		DiskLimitBytes: limit.Bytes, LocalBytes: in.LocalBytes, DiskBytes: in.DiskBytes, FreeBytes: in.FreeBytes,
		Bucket: in.Bucket, BucketBytes: in.BucketBytes,
		GrowthBytesPerDay: float64(in.RecentBytes) / growthDays,
	}
	if !in.Bucket && limit.Bytes > 0 && a.GrowthBytesPerDay > 0 {
		days := max(float64(limit.Bytes-in.LocalBytes), 0) / a.GrowthBytesPerDay
		a.DaysUntilLimit = &days
	}
	if old := in.OldFiles; old != nil {
		a.OldFiles = &oldFilesStorage{Files: old.Files, LocalBytes: old.LocalBytes}
		var newest, last time.Time
		for projectID, day := range old.NewestDays {
			if day.After(newest) {
				newest = day
			}
			// Retention keeps a day for its Retention's days after it, and
			// deletes it the day after.
			if days := d.retention[projectID]; days > 0 && day.AddDate(0, 0, days+1).After(last) {
				last = day.AddDate(0, 0, days+1)
			}
		}
		a.OldFiles.NewestDay = newest.Format(time.DateOnly)
		if !last.IsZero() {
			on := last.Format(time.DateOnly)
			a.OldFiles.PassesRetentionOn = &on
		}
	}
	return a
}
