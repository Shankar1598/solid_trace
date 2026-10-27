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
	ProjectedBytes    int64    `json:"projected_local_bytes"`
	StaysUnderLimit   bool     `json:"stays_under_limit"`
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
	seedProject(t, env.SQLitePath, 123, 60)

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
	// At steady state each Project keeps its Retention's days of its last
	// 7 days' average: 60 days of 123's and 30 of 456's.
	lastWeekOf := func(projectID uint32) float64 {
		return float64(overview(t, env, day(9, 25), day(10, 1), projectID).Band.Stored.Bytes) / 7
	}
	projected := int64(lastWeekOf(123)*60 + lastWeekOf(456)*30)
	if math.Abs(float64(got.ProjectedBytes-projected)) > 1 {
		t.Errorf("Expected %d bytes on local disk at steady state, got %d", projected, got.ProjectedBytes)
	}

	// The limit main works out at start. Under steady state's bytes, 10
	// days' growth past the disk use is 10 days off.
	env.LogDiskLimit = storage.LogDiskLimit{Bytes: local + int64(10*got.GrowthBytesPerDay)}
	if env.LogDiskLimit.Bytes >= projected {
		t.Fatalf("Expected the limit, %d bytes, under steady state's %d", env.LogDiskLimit.Bytes, projected)
	}
	env.Reopen(t)
	got = overviewStorageOf(t, env, testNow, testNow, 123)
	if got.DiskLimitBytes != env.LogDiskLimit.Bytes {
		t.Errorf("Expected the disk limit in effect, %d, got %d", env.LogDiskLimit.Bytes, got.DiskLimitBytes)
	}
	if got.StaysUnderLimit || got.DaysUntilLimit == nil || math.Abs(*got.DaysUntilLimit-10) > 0.01 {
		t.Errorf("Expected 10 days until the limit, got %v (stays under: %v)", got.DaysUntilLimit, got.StaysUnderLimit)
	}

	// Over steady state's bytes, Retention deletes as much as arrives
	// before the limit is reached: no day count.
	env.LogDiskLimit = storage.LogDiskLimit{Bytes: projected + 1}
	env.Reopen(t)
	got = overviewStorageOf(t, env, testNow, testNow, 123)
	if !got.StaysUnderLimit || got.DaysUntilLimit != nil || math.Abs(float64(got.ProjectedBytes-projected)) > 1 {
		t.Errorf("Expected it to stay under the limit at %d bytes, with no day count, got %+v", projected, got)
	}

	// Past the limit, it's now.
	env.LogDiskLimit = storage.LogDiskLimit{Bytes: local - 1}
	env.Reopen(t)
	if got := overviewStorageOf(t, env, testNow, testNow, 123); got.DaysUntilLimit == nil || *got.DaysUntilLimit != 0 {
		t.Errorf("Expected 0 days until a limit already passed, got %v", got.DaysUntilLimit)
	}
}

func TestStorageCountsSpansAsTheDiskLimitDoes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	// Project 123 keeps its Logs 60 days and its Spans 10.
	seedProject(t, env.SQLitePath, 123, 60)
	consoleExec(t, env, "UPDATE projects SET trace_retention_days = 10 WHERE id = 123")

	day := func(month, day int) time.Time { return time.Date(2026, time.Month(month), day, 9, 0, 0, 0, time.UTC) }
	postAt(t, env, 123, "test_public_key", "the last 7 days", day(9, 30))
	sent := sentTransaction{fixture: "sentry-ruby-6.3-rails", traceID: traceIDOf(1), at: day(9, 30)}
	postEnvelopeWithKey(t, env, "test_public_key", sent.envelope(t))
	env.ProcessSpans(t)
	env.Clock.Set(testNow)

	_, logsLocal := localDiskUse(t, env)
	logs := overview(t, env, day(9, 25), day(10, 1), 123).Band.Stored.Bytes
	spans, _ := overviewSpansOf(t, env, day(9, 25), day(10, 1), 123)
	if logsLocal <= 0 || spans.LocalBytes <= 0 {
		t.Fatalf("Expected Logs and Spans on local disk, got %d and %+v", logsLocal, spans)
	}

	got := overviewStorageOf(t, env, testNow, testNow, 123)
	if want := logsLocal + spans.LocalBytes; got.LocalBytes != want {
		t.Errorf("Expected the Logs' %d and the Spans' %d bytes on local disk, %d, got %d", logsLocal, spans.LocalBytes, want, got.LocalBytes)
	}
	if want := float64(logs+spans.StoredBytes) / 7; math.Abs(got.GrowthBytesPerDay-want) > 0.01 {
		t.Errorf("Expected the last 7 days' Logs and Spans a day's growth, %.2f, got %.2f", want, got.GrowthBytesPerDay)
	}
	// Each at steady state keeps its own Retention's days.
	if want := int64(float64(logs)/7*60 + float64(spans.StoredBytes)/7*10); math.Abs(float64(got.ProjectedBytes-want)) > 1 {
		t.Errorf("Expected %d bytes on local disk at steady state, got %d", want, got.ProjectedBytes)
	}
}

func TestStorageAnswersTheOldLogFilesKeptAndWhenRetentionDeletesTheLast(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixtureLocally(t, env)
	})
	defer env.Cleanup()
	// Project 2 keeps its Logs 40 days, Project 3 the fixture's 30.
	consoleExec(t, env, "UPDATE projects SET log_retention_days = 40 WHERE id = 2")

	got := overviewStorageOf(t, env, testNow, testNow, 3)
	old := got.OldFiles
	if old == nil {
		t.Fatalf("Expected the old Log files, got none")
	}
	// The fixture's 8 files of 1 and 2 October, 6 with local copies.
	if old.Files != 8 || old.LocalBytes != fixtureLocalBytes(t) || old.NewestDay != "2026-10-02" {
		t.Errorf("Expected 8 files of %d local bytes, the newest of 2 October, got %+v", fixtureLocalBytes(t), *old)
	}
	// 2 October kept 40 days is deleted on 12 November, whichever
	// Organization asks.
	if old.PassesRetentionOn == nil || *old.PassesRetentionOn != "2026-11-12" {
		t.Errorf("Expected the last to pass Retention on 2026-11-12, got %v", old.PassesRetentionOn)
	}
	if _, local := localDiskUse(t, env); got.LocalBytes != local || local < old.LocalBytes {
		t.Errorf("Expected the disk limit's %d local bytes, the old files' among them, got %d", local, got.LocalBytes)
	}

	// Once Retention has deleted them all, there are none.
	env.Clock.Set(time.Date(2026, 11, 12, 0, 30, 0, 0, time.UTC))
	env.DeleteLogsPastRetention(t)
	if got := overviewStorageOf(t, env, testNow, testNow, 3); got.OldFiles != nil {
		t.Errorf("Expected no old Log files past Retention, got %+v", *got.OldFiles)
	}
}
