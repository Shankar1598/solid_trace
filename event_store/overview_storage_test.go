package main_test

import (
	"encoding/json"
	"math"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/storage"
)

// The Overview's Storage: the whole install's disk against the disk limit,
// the bucket and the old Log files, answered only when asked for, as Console
// asks for Instance admins.

type overviewStorage struct {
	DiskLimitBytes    int64    `json:"disk_limit_bytes"`
	LocalBytes        int64    `json:"local_bytes"`
	DiskBytes         int64    `json:"disk_bytes"`
	FreeBytes         int64    `json:"free_bytes"`
	Bucket            bool     `json:"bucket"`
	BucketBytes       int64    `json:"bucket_bytes"`
	GrowthBytesPerDay float64  `json:"growth_bytes_per_day"`
	DaysUntilLimit    *float64 `json:"days_until_limit"`
	OldFiles          *struct {
		Files             int     `json:"files"`
		LocalBytes        int64   `json:"local_bytes"`
		NewestDay         string  `json:"newest_day"`
		PassesRetentionOn *string `json:"passes_retention_on"`
	} `json:"old_files"`
}

// overviewStorageOf asks the query app for the Overview of projects over
// the UTC days from to to, with Storage, and returns its Storage.
func overviewStorageOf(t *testing.T, env *TestEnv, from, to time.Time, projects ...uint32) overviewStorage {
	t.Helper()
	status, raw := postQueryApp(t, env, "/overview", overviewRequest{
		ProjectIDs: projects, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), IncludeStorage: true,
	})
	if status != 200 {
		t.Fatalf("Expected the Overview, got %d: %s", status, raw)
	}
	var answer struct {
		Storage *overviewStorage `json:"storage"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode the Overview %s: %v", raw, err)
	}
	if answer.Storage == nil {
		t.Fatalf("Expected Storage, got %s", raw)
	}
	return *answer.Storage
}

func TestStorageAnswersTheInstallsLocalDiskAgainstTheDiskLimitOnlyWhenAsked(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 456, 30)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	day := func(month, day int) time.Time { return time.Date(2026, time.Month(month), day, 9, 0, 0, 0, time.UTC) }
	// Today is 2 October: the last 7 days are 25 September to 1 October.
	postAt(t, env, 123, "test_public_key", "too old to count as growth", day(9, 20))
	postAt(t, env, 123, "test_public_key", "the last 7 days", day(9, 28))
	postAt(t, env, 456, "other_public_key", "another Organization's, the same install", day(9, 30))
	postAt(t, env, 123, "test_public_key", "yesterday", day(10, 1))
	postAt(t, env, 123, "test_public_key", "today, not a whole day yet", testNow)
	env.Clock.Set(testNow)

	if got := overview(t, env, testNow, testNow, 123); got.Storage != nil {
		t.Errorf("Expected no Storage unless asked, got %s", got.Storage)
	}

	_, local := localDiskUse(t, env)
	lastWeek := overview(t, env, day(9, 25), day(10, 1), 123, 456).Band.Stored.Bytes
	got := overviewStorageOf(t, env, testNow, testNow, 123)
	if got.LocalBytes != local || local <= 0 {
		t.Errorf("Expected the install's %d bytes on local disk, the disk limit's measure, got %d", local, got.LocalBytes)
	}
	if want := float64(lastWeek) / 7; math.Abs(got.GrowthBytesPerDay-want) > 0.01 || want <= 0 {
		t.Errorf("Expected the last 7 days' %d bytes a day's growth, %.2f, got %.2f", lastWeek, want, got.GrowthBytesPerDay)
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(clickhousetest.DataDir(t), &fs); err != nil {
		t.Fatalf("statfs: %v", err)
	}
	if disk := int64(fs.Blocks) * int64(fs.Bsize); got.DiskBytes != disk || got.FreeBytes <= 0 || got.FreeBytes > disk {
		t.Errorf("Expected ClickHouse's %d-byte disk with some free, got %d with %d free", disk, got.DiskBytes, got.FreeBytes)
	}
	if got.Bucket || got.BucketBytes != 0 || got.OldFiles != nil {
		t.Errorf("Expected no bucket and no old Log files, got %+v", got)
	}
	if got.DaysUntilLimit != nil {
		t.Errorf("Expected no days until the limit with none set in the test environment, got %v", *got.DaysUntilLimit)
	}

	// The limit main works out at start: 10 days' growth past the disk use
	// is 10 days off.
	env.LogDiskLimit = storage.LogDiskLimit{Bytes: local + int64(10*got.GrowthBytesPerDay)}
	env.Reopen(t)
	got = overviewStorageOf(t, env, testNow, testNow, 123)
	if got.DiskLimitBytes != env.LogDiskLimit.Bytes {
		t.Errorf("Expected the disk limit in effect, %d, got %d", env.LogDiskLimit.Bytes, got.DiskLimitBytes)
	}
	if got.DaysUntilLimit == nil || math.Abs(*got.DaysUntilLimit-10) > 0.01 {
		t.Errorf("Expected 10 days until the limit, got %v", got.DaysUntilLimit)
	}

	// Past the limit, it's now.
	env.LogDiskLimit = storage.LogDiskLimit{Bytes: local - 1}
	env.Reopen(t)
	if got := overviewStorageOf(t, env, testNow, testNow, 123); got.DaysUntilLimit == nil || *got.DaysUntilLimit != 0 {
		t.Errorf("Expected 0 days until a limit already passed, got %v", got.DaysUntilLimit)
	}
}
