package main_test

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/storage"
)

// The bucket tier: with an object_store, a part of the logs table moves to
// the bucket once its newest Log is local_cache_hours old, and the disk
// limit moves the oldest days there before it deletes any. These tests run
// against the S3 stand-in (clickhousetest.Bucket), and skip without one.
// ClickHouse moves parts by its own clock, so their Logs are of now.

// setupBucketTestEnv is setupTestEnv with a bucket, Logs staying on local
// disk for localCacheHours, as set on Settings → Instance before the start,
// and the clock at now.
func setupBucketTestEnv(t *testing.T, localCacheHours int) (*TestEnv, time.Time) {
	t.Helper()
	return setupBucketTestEnvWith(t, localCacheHours, nil)
}

// setupBucketTestEnvWith is setupBucketTestEnv, with configure, if set,
// changing the environment before its stores open.
func setupBucketTestEnvWith(t *testing.T, localCacheHours int, configure func(*TestEnv)) (*TestEnv, time.Time) {
	t.Helper()
	bucket := clickhousetest.Bucket(t)
	env := newTestEnv(t, func(env *TestEnv) {
		env.ClickHouseConfig.StoragePolicy = clickhousetest.BucketPolicy
		env.BucketConfig = bucket
		setLocalCacheHours(t, env, localCacheHours)
		if configure != nil {
			configure(env)
		}
	})
	now := time.Now().UTC().Truncate(time.Second)
	env.Clock.Set(now)
	seedProject(t, env.SQLitePath, 123, 0)
	return env, now
}

// setLocalCacheHours sets how long Logs stay on local disk, as an Instance
// admin does on Settings → Instance.
func setLocalCacheHours(t *testing.T, env *TestEnv, hours int) {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		INSERT INTO instance_settings (id, local_cache_hours, created_at, updated_at) VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET local_cache_hours = excluded.local_cache_hours`,
		hours, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to set the hours on local disk: %v", err)
	}
}

// dayKey is a Project's day as localDiskUse names it.
func dayKey(projectID uint32, at time.Time) string {
	return fmt.Sprintf("%d %s", projectID, at.UTC().Format(time.DateOnly))
}

// bucketAnswers is what reads of Project 123 answer: its Logs newest first,
// a table, and each Log fetched by its id with and without its time.
type bucketAnswers struct {
	logs    []listedLog
	table   []string
	fetched map[string]listedLog
}

func bucketAnswersOf(t *testing.T, env *TestEnv) bucketAnswers {
	t.Helper()
	_, table := runTable(t, env, "summarize count(), max(n) by user")
	slices.Sort(table)
	answers := bucketAnswers{logs: latestLogs(t, env, 123, 100), table: table, fetched: map[string]listedLog{}}
	for _, log := range answers.logs {
		for _, hint := range []*time.Time{nil, &log.Time} {
			if status, fetched := fetchLog(t, env, 123, log.ID, hint); status == 200 {
				answers.fetched[log.Message+" "+map[bool]string{true: "with", false: "without"}[hint != nil]+" its time"] = fetched
			}
		}
	}
	return answers
}

func TestLogsPastLocalCacheHoursAreReadFromTheBucket(t *testing.T) {
	env, now := setupBucketTestEnv(t, 24)
	defer env.Cleanup()

	recent, old, older := now.Add(-time.Hour), now.AddDate(0, 0, -3), now.AddDate(0, 0, -5)
	postAndProcess(t, env,
		logJSON(t, "recent", recent, map[string]any{"user": "ada", "n": 1}),
		logJSON(t, "old", old, map[string]any{"user": "bob", "n": 2}),
		logJSON(t, "older", older, map[string]any{"user": "cy", "n": 3}))

	// Only the recent Log's day is on local disk.
	if use, _ := localDiskUse(t, env); !slices.Equal(slices.Collect(maps.Keys(use)), []string{dayKey(123, recent)}) {
		t.Errorf("Expected only %s on local disk, got %v", dayKey(123, recent), use)
	}
	// Every read answers from the bucket as from local disk.
	got := bucketAnswersOf(t, env)
	if messages := messages(got.logs); !slices.Equal(messages, []string{"recent", "old", "older"}) {
		t.Fatalf("Expected every Log listed, got %v", messages)
	}
	if want := []string{`["ada",1,1]`, `["bob",1,2]`, `["cy",1,3]`}; !slices.Equal(got.table, want) {
		t.Errorf("Expected the table %v, got %v", want, got.table)
	}
	for _, log := range got.logs {
		for _, how := range []string{"with", "without"} {
			if fetched := got.fetched[log.Message+" "+how+" its time"]; !reflect.DeepEqual(fetched, log) {
				t.Errorf("Expected %q fetched by its id %s its time, got %+v", log.Message, how, fetched)
			}
		}
	}
	assertProjectQueries(t, env, 123, map[string][]string{
		`where user == "bob"`: {"old"},
		`where n > 1`:         {"old", "older"},
	}, "from the bucket")
	week := struct{ from, to time.Time }{now.AddDate(0, 0, -7).Truncate(24 * time.Hour), now.AddDate(0, 0, 1).Truncate(24 * time.Hour)}
	assertCountedIn(t, env, 123, week.from, week.to, 3, "from the bucket")
	env.Reopen(t)
	if again := bucketAnswersOf(t, env); !reflect.DeepEqual(again, got) {
		t.Errorf("Expected the same answers after a restart:\n want %+v\n got  %+v", got, again)
	}

	// Retention drops the older day from the bucket.
	consoleExec(t, env, "UPDATE projects SET log_retention_days = 4 WHERE id = 123")
	env.DeleteLogsPastRetention(t)
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"recent", "old"}) {
		t.Errorf("Expected Retention to delete the older Log from the bucket, got %v", got)
	}
	assertCountedIn(t, env, 123, week.from, week.to, 2, "after Retention")
}

func TestWithABucketTheDiskLimitMovesTheOldestDaysThereAndDeletesNone(t *testing.T) {
	// Nothing is old enough to move by itself.
	env, now := setupBucketTestEnv(t, 24*30)
	defer env.Cleanup()

	today, yesterday, before := now, now.AddDate(0, 0, -1), now.AddDate(0, 0, -2)
	postAndProcess(t, env,
		logJSON(t, "today", today, map[string]any{"user": "ada", "n": 1}),
		logJSON(t, "yesterday", yesterday, map[string]any{"user": "bob", "n": 2}),
		logJSON(t, "the day before", before, map[string]any{"user": "cy", "n": 3}))
	use, _ := localDiskUse(t, env)
	if len(use) != 3 {
		t.Fatalf("Expected three days on local disk, got %v", use)
	}
	want := bucketAnswersOf(t, env)

	enforceLogDiskLimit(t, env, use[dayKey(123, today)])
	if after, _ := localDiskUse(t, env); !maps.Equal(after, map[string]int64{dayKey(123, today): use[dayKey(123, today)]}) {
		t.Errorf("Expected only today on local disk, got %v", after)
	}
	if got := bucketAnswersOf(t, env); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected the same answers with the older days in the bucket:\n want %+v\n got  %+v", want, got)
	}
	week := struct{ from, to time.Time }{now.AddDate(0, 0, -7).Truncate(24 * time.Hour), now.AddDate(0, 0, 1).Truncate(24 * time.Hour)}
	assertCountedIn(t, env, 123, week.from, week.to, 3, "with the older days in the bucket")
}

// dayTablesTTL is the TTL of each family's main table, as system.tables
// writes it, by table.
func dayTablesTTL(t *testing.T, env *TestEnv) map[string]string {
	t.Helper()
	ttls := map[string]string{}
	for _, f := range []storage.DayFamily{storage.LogDays, storage.SpanDays} {
		var engine string
		if err := env.ClickHouse.Conn().QueryRow(context.Background(),
			"SELECT engine_full FROM system.tables WHERE database = currentDatabase() AND name = ?", f.Tables[0]).Scan(&engine); err != nil {
			t.Fatalf("Read the %s table's TTL: %v", f.Tables[0], err)
		}
		_, ttl, _ := strings.Cut(engine, " TTL ")
		ttl, _, _ = strings.Cut(ttl, " SETTINGS ")
		ttls[f.Tables[0]] = ttl
	}
	return ttls
}

// Every day table moves its parts by the hours on local disk Console has,
// set at start, and again within the poll when an Instance admin changes
// them, with no restart.
func TestAChangeOfTheHoursOnLocalDiskMovesOlderPartsToTheBucketWithoutARestart(t *testing.T) {
	env, now := setupBucketTestEnvWith(t, 24*30, func(env *TestEnv) { env.LocalCacheInterval = 100 * time.Millisecond })
	defer env.Cleanup()

	hoursIn := func(ttls map[string]string, hours int) bool {
		for _, ttl := range ttls {
			if !strings.Contains(ttl, fmt.Sprintf("toIntervalHour(%d) TO VOLUME 'bucket'", hours)) {
				return false
			}
		}
		return true
	}
	if ttls := dayTablesTTL(t, env); !hoursIn(ttls, 720) || env.ClickHouse.LocalCacheHours() != 720 {
		t.Fatalf("Expected 720 hours on local disk from the start, got %v and %d", ttls, env.ClickHouse.LocalCacheHours())
	}
	recent, old := now.Add(-time.Hour), now.AddDate(0, 0, -3)
	postAndProcess(t, env, logJSON(t, "recent", recent, nil), logJSON(t, "old", old, nil))
	if use, _ := localDiskUse(t, env); len(use) != 2 {
		t.Fatalf("Expected both days on local disk under 720 hours, got %v", use)
	}

	setLocalCacheHours(t, env, 24)
	waitFor(t, 10*time.Second, "the day tables' TTL at 24 hours", func() bool { return hoursIn(dayTablesTTL(t, env), 24) })
	if got := env.ClickHouse.LocalCacheHours(); got != 24 {
		t.Errorf("Expected the Overview's hours on local disk at 24, got %d", got)
	}
	// ClickHouse moves the old day's part in the background.
	waitFor(t, 30*time.Second, "the old day moved to the bucket", func() bool {
		use, _ := localDiskUse(t, env)
		return maps.Equal(use, map[string]int64{dayKey(123, recent): use[dayKey(123, recent)]})
	})
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"recent", "old"}) {
		t.Errorf("Expected both Logs read, the old one from the bucket, got %v", got)
	}

	// Hours out of range, which only Console's validations prevent, leave
	// the TTL as it was.
	setLocalCacheHours(t, env, 721)
	time.Sleep(500 * time.Millisecond)
	if ttls := dayTablesTTL(t, env); !hoursIn(ttls, 24) || env.ClickHouse.LocalCacheHours() != 24 {
		t.Errorf("Expected 24 hours kept past hours out of range, got %v and %d", ttls, env.ClickHouse.LocalCacheHours())
	}
}
