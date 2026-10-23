package main_test

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clickhousetest"
)

// The bucket tier: with an object_store, a part of the logs table moves to
// the bucket once its newest Log is local_cache_hours old, and the disk
// limit moves the oldest days there before it deletes any. These tests run
// against the S3 stand-in (clickhousetest.Bucket), and skip without one.
// ClickHouse moves parts by its own clock, so their Logs are of now.

// setupBucketTestEnv is setupTestEnv with a bucket, Logs staying on local
// disk for localCacheHours, and the clock at now.
func setupBucketTestEnv(t *testing.T, localCacheHours int) (*TestEnv, time.Time) {
	t.Helper()
	bucket := clickhousetest.Bucket(t)
	bucket.LocalCacheHours = localCacheHours
	env := newTestEnv(t, func(env *TestEnv) {
		env.ClickHouseConfig.StoragePolicy = clickhousetest.BucketPolicy
		env.BucketConfig = bucket
	})
	now := time.Now().UTC().Truncate(time.Second)
	env.Clock.Set(now)
	seedProject(t, env.SQLitePath, 123, 0)
	return env, now
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
