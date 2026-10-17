package main_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/storage"
)

// flakyBucket is the Log files' bucket in a local folder, whose URLs DuckDB
// reads as local paths, and which a test can break.
type flakyBucket struct {
	storage.ObjectStore
	dir string
	// failPuts fails every upload. putThenFail stores the object, then
	// fails, as when EventStore stops before it marks the file uploaded.
	// failDeletes fails every deletion, and failLists every listing.
	failPuts, putThenFail, failDeletes, failLists atomic.Bool
	// puts and deletes count the uploads and deletions asked of the bucket.
	puts, deletes atomic.Int64
	// stall, if set before an upload starts, holds each upload after it
	// stores the object, as a request whose answer doesn't come, until
	// stall is closed; stalled then gets a signal.
	stall, stalled chan struct{}
}

func (b *flakyBucket) Put(ctx context.Context, key, localPath string) error {
	b.puts.Add(1)
	if b.failPuts.Load() {
		return errors.New("the bucket is unavailable")
	}
	if err := b.ObjectStore.Put(ctx, key, localPath); err != nil {
		return err
	}
	if b.putThenFail.Load() {
		return errors.New("stopped after the upload")
	}
	if b.stall != nil {
		select {
		case b.stalled <- struct{}{}:
		default:
		}
		select {
		case <-b.stall:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (b *flakyBucket) List(ctx context.Context, prefix string) ([]storage.StoredObject, error) {
	if b.failLists.Load() {
		return nil, errors.New("the bucket is unavailable")
	}
	return b.ObjectStore.List(ctx, prefix)
}

func (b *flakyBucket) Delete(ctx context.Context, key string) error {
	b.deletes.Add(1)
	if b.failDeletes.Load() {
		return errors.New("the bucket is unavailable")
	}
	return b.ObjectStore.Delete(ctx, key)
}

// object is where the bucket keeps a Project's Log file of the hour of at.
func (b *flakyBucket) object(projectID uint32, at time.Time, name string) string {
	return filepath.Join(b.dir, filepath.FromSlash(logFileKey(projectID, at, name)))
}

// setupBucketTestEnv is setupTestEnv with the Log files kept in a bucket too,
// each local copy for localCacheHours after its hour ends.
func setupBucketTestEnv(t *testing.T, localCacheHours int) (*TestEnv, *flakyBucket) {
	t.Helper()
	var bucket *flakyBucket
	env := newTestEnv(t, func(env *TestEnv) {
		dir := filepath.Join(env.dir, "bucket")
		bucket = &flakyBucket{ObjectStore: storage.NewDirStore(dir), dir: dir}
		env.Bucket, env.LocalCacheHours = bucket, localCacheHours
	})
	return env, bucket
}

// logFileKey is a Log file's key: its path under the files folder, and in the
// bucket.
func logFileKey(projectID uint32, at time.Time, name string) string {
	return fmt.Sprintf("project=%d/day=%s/hour=%02d/%s", projectID, at.UTC().Format(time.DateOnly), at.UTC().Hour(), name)
}

// localFile is where a Project's Log file of the hour of at is on local disk.
func (env *TestEnv) localFile(projectID uint32, at time.Time, name string) string {
	return filepath.Join(env.dir, "logs_parquet", filepath.FromSlash(logFileKey(projectID, at, name)))
}

// UploadLogFiles uploads every published Log file not yet in the bucket, as
// the uploads worker does after each export.
func (env *TestEnv) UploadLogFiles(t *testing.T) {
	t.Helper()
	if err := env.LogStore.UploadLogFiles(context.Background()); err != nil {
		t.Fatalf("Uploading Log files failed: %v", err)
	}
}

// EvictLogFiles evicts the local copies of uploaded Log files kept long
// enough by the clock, as Cleanup does each pass.
func (env *TestEnv) EvictLogFiles(t *testing.T) {
	t.Helper()
	if err := env.LogStore.EvictLogFiles(env.Clock.Now()); err != nil {
		t.Fatalf("Evicting Log files failed: %v", err)
	}
}

func assertPresent(t *testing.T, path, when string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Expected %s present %s, got %v", path, when, err)
	}
}

// bucketAnswers is what every kind of read of Project 123 answers: its
// latest Logs, a histogram, a table, SQL mode's logs view, and a Monitor's
// run of the Logs ingested after the cursor it was armed with.
type bucketAnswers struct {
	logs       []listedLog
	histogram  histogramAnswer
	table, sql []string
	afterBatch []string
}

func bucketAnswersOf(t *testing.T, env *TestEnv, monitorCursor string) bucketAnswers {
	t.Helper()
	_, table := runTable(t, env, "summarize count() by bin(time, 1h)")
	slices.Sort(table)
	_, sql := sqlTable(t, env, sqlRequest{SQL: "SELECT message, time FROM logs ORDER BY time", From: allTime.from, To: allTime.to})
	return bucketAnswers{
		logs:       latestLogs(t, env, 123, 100),
		histogram:  histogram(t, env, histogramRequest{From: testNow.AddDate(0, -2, 0), To: testNow.Add(time.Hour), BucketSeconds: 3600}),
		table:      table,
		sql:        sql,
		afterBatch: messages(runAfterBatch(t, env, "", monitorCursor, nil).Logs),
	}
}

// assertBucketAnswers checks every kind of read of Project 123 still answers
// want, and that each Log is fetched by its id and time.
func assertBucketAnswers(t *testing.T, env *TestEnv, monitorCursor string, want bucketAnswers, when string) {
	t.Helper()
	got := bucketAnswersOf(t, env, monitorCursor)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expected the same answers %s:\n want %+v\n got  %+v", when, want, got)
	}
	for _, log := range want.logs {
		if status, fetched := fetchLog(t, env, 123, log.ID, &log.Time); status != 200 || !reflect.DeepEqual(fetched, log) {
			t.Errorf("Expected %q fetched by its id and time %s, got %d %+v", log.Message, when, status, fetched)
		}
	}
}

// assertFetchedWithoutTime checks whether a Log is fetched by its id alone.
func assertFetchedWithoutTime(t *testing.T, env *TestEnv, log listedLog, found bool, when string) {
	t.Helper()
	want := 404
	if found {
		want = 200
	}
	if status, _ := fetchLog(t, env, 123, log.ID, nil); status != want {
		t.Errorf("Expected %q fetched by its id alone to answer %d %s, got %d", log.Message, want, when, status)
	}
}

// logByMessage is the Log with a message.
func logByMessage(t *testing.T, logs []listedLog, message string) listedLog {
	t.Helper()
	for _, log := range logs {
		if log.Message == message {
			return log
		}
	}
	t.Fatalf("Expected a Log %q among %v", message, messages(logs))
	return listedLog{}
}

func TestAnUploadedHourIsReadFromTheBucketOnceItsLocalCopyIsEvicted(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	cursor := armMonitor(t, env, "").cursor
	seven, nine := testNow.Add(-270*time.Minute), testNow.Add(-165*time.Minute) // 07:30 and 09:15
	postAndProcess(t, env,
		logJSON(t, "seven", seven, map[string]any{"user": "a"}),
		logJSON(t, "nine", nine, map[string]any{"user": "b"}),
		logJSON(t, "hot", testNow.Add(-10*time.Minute), map[string]any{"user": "c"}))
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	for _, at := range []time.Time{seven, nine} {
		assertPresent(t, bucket.object(123, at, "data.parquet"), "after the upload")
		assertPresent(t, env.localFile(123, at, "data.parquet"), "after the upload")
	}
	want := bucketAnswersOf(t, env, cursor)
	if got := messages(want.logs); !slices.Equal(got, []string{"hot", "nine", "seven"}) {
		t.Fatalf("Expected every Log read, got %v", got)
	}

	// Within local_cache_hours of the end of its hour, a copy stays.
	env.EvictLogFiles(t)
	assertPresent(t, env.localFile(123, seven, "data.parquet"), "within local_cache_hours")

	// At 09:00 the next day, 07:00's hour ended 25 hours ago, and 09:00's
	// 23: only the first is read from the bucket.
	env.Clock.Set(testNow.Add(21 * time.Hour))
	env.EvictLogFiles(t)
	assertGone(t, env.localFile(123, seven, "data.parquet"), "past local_cache_hours")
	assertPresent(t, env.localFile(123, nine, "data.parquet"), "within local_cache_hours")
	assertPresent(t, bucket.object(123, seven, "data.parquet"), "after the eviction")
	assertBucketAnswers(t, env, cursor, want, "with one hour local and one in the bucket")
	assertFetchedWithoutTime(t, env, logByMessage(t, want.logs, "seven"), false, "once its file is only in the bucket")
	assertFetchedWithoutTime(t, env, logByMessage(t, want.logs, "nine"), true, "while its file is local")

	env.Clock.Set(testNow.Add(46 * time.Hour))
	env.EvictLogFiles(t)
	assertGone(t, env.localFile(123, nine, "data.parquet"), "past local_cache_hours")
	assertBucketAnswers(t, env, cursor, want, "with every exported hour in the bucket")
	assertFetchedWithoutTime(t, env, logByMessage(t, want.logs, "hot"), true, "while it is hot")
}

func TestLateFilesOfAnEvictedHourAreUploadedAndReadWithItsDataFile(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 0)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	cursor := armMonitor(t, env, "").cursor
	hour := testNow.Add(-270 * time.Minute) // 07:30
	postAndProcess(t, env, logJSON(t, "on time", hour, nil))
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	env.EvictLogFiles(t)
	assertGone(t, env.localFile(123, hour, "data.parquet"), "with local_cache_hours 0")

	postAndProcess(t, env, logJSON(t, "late", hour.Add(15*time.Minute), nil))
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	assertPresent(t, bucket.object(123, hour, "data.parquet"), "after the late file's upload")
	assertPresent(t, bucket.object(123, hour, "late-1.parquet"), "after its upload")
	want := bucketAnswersOf(t, env, cursor)
	if got := messages(want.logs); !slices.Equal(got, []string{"late", "on time"}) {
		t.Fatalf("Expected both Logs of the hour, got %v", got)
	}
	if want.histogram.Count != 2 || !slices.Equal(want.table, []string{`["2026-10-02T07:00:00Z",2]`}) {
		t.Errorf("Expected the hour counted with both files, got %d and %v", want.histogram.Count, want.table)
	}

	env.EvictLogFiles(t)
	assertGone(t, env.localFile(123, hour, "late-1.parquet"), "with local_cache_hours 0")
	assertBucketAnswers(t, env, cursor, want, "with both files only in the bucket")
}

func TestTheUploadsWorkerUploadsAtStartAndRightAfterEachExport(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 0)
	defer env.Cleanup()

	first, second := testNow.Add(-270*time.Minute), testNow.Add(-210*time.Minute)
	postAndProcess(t, env, logJSON(t, "first", first, nil))
	env.ExportLogs(t)

	// Started after the export, as after a restart, it uploads what's left.
	uploads := logprocessing.NewLogUploads(env.LogStore, env.Clock)
	uploads.Start()
	defer uploads.Stop()
	uploaded := func(at time.Time) func() bool {
		return func() bool {
			_, inBucket := os.Stat(bucket.object(123, at, "data.parquet"))
			_, onDisk := os.Stat(env.localFile(123, at, "data.parquet"))
			return inBucket == nil && os.IsNotExist(onDisk)
		}
	}
	waitFor(t, 10*time.Second, "the file exported before the start uploaded and evicted", uploaded(first))

	postAndProcess(t, env, logJSON(t, "second", second, nil))
	env.ExportLogs(t)
	waitFor(t, 10*time.Second, "the next export's file uploaded and evicted", uploaded(second))
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"second", "first"}) {
		t.Errorf("Expected both Logs read from the bucket, got %v", got)
	}
}

func TestRetentionAndProjectDeletionDeleteTheirFilesFromTheBucket(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()

	// Project 456 has no row in Console, so it is deleted.
	seedProject(t, env.SQLitePath, 123, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	pastRetention := time.Date(2026, 9, 1, 23, 30, 0, 0, time.UTC)
	within := time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)
	postAt(t, env, 123, "test_public_key", "past retention", pastRetention)
	postAt(t, env, 123, "test_public_key", "within retention", within)
	postAt(t, env, 456, "other_public_key", "deleted project", within)
	env.Clock.Set(testNow)
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	// The oldest hour is read from the bucket only.
	env.EvictLogFiles(t)
	assertGone(t, env.localFile(123, pastRetention, "data.parquet"), "past local_cache_hours")

	// With the bucket failing, the Logs go, and the objects wait: Retention
	// doesn't wait on the bucket, so a failing one fails no Project's pass.
	bucket.failDeletes.Store(true)
	env.DeleteLogsPastRetention(t)
	if got := bucket.deletes.Load(); got != 0 {
		t.Errorf("Expected Retention to ask nothing of the bucket, got %d deletions", got)
	}
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"within retention"}) {
		t.Errorf("Expected only the Log within Retention kept, got %v", got)
	}
	if got := latestLogs(t, env, 456, 10); len(got) != 0 {
		t.Errorf("Expected the deleted Project's Logs gone, got %v", messages(got))
	}
	assertGone(t, env.localFile(456, within, "data.parquet"), "with the deleted Project")
	assertPresent(t, bucket.object(123, pastRetention, "data.parquet"), "while the bucket fails")
	assertPresent(t, bucket.object(456, within, "data.parquet"), "while the bucket fails")

	// Each Cleanup pass stops at the first object that fails to delete, and
	// the failure is logged once until the objects delete again; the disk
	// limit isn't failed by it.
	logs := captureLogs(t)
	for pass := int64(1); pass <= 2; pass++ {
		enforceLogDiskLimit(t, env, 1<<40)
		if got := bucket.deletes.Load(); got != pass {
			t.Errorf("Expected pass %d to stop at the first failed deletion, got %d deletions in all", pass, got)
		}
	}
	if got := strings.Count(logs.String(), "level=error"); got != 1 {
		t.Errorf("Expected one error logged for the failing bucket, got %d:\n%s", got, logs)
	}
	assertPresent(t, bucket.object(123, pastRetention, "data.parquet"), "while the bucket fails")
	assertPresent(t, bucket.object(456, within, "data.parquet"), "while the bucket fails")

	// The next Cleanup pass deletes them.
	bucket.failDeletes.Store(false)
	enforceLogDiskLimit(t, env, 1<<40)
	assertGone(t, bucket.object(123, pastRetention, "data.parquet"), "by the next pass")
	assertGone(t, bucket.object(456, within, "data.parquet"), "by the next pass")
	assertPresent(t, bucket.object(123, within, "data.parquet"), "within Retention")
	assertPresent(t, env.localFile(123, within, "data.parquet"), "within Retention")
}

func TestTheDiskLimitEvictsUploadedCopiesBeforeItDeletesAnything(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()

	uploaded, notUploaded := testNow.Add(-270*time.Minute), testNow.Add(-210*time.Minute)
	postAndProcess(t, env, logJSON(t, "uploaded", uploaded, nil))
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	bucket.failPuts.Store(true)
	postAndProcess(t, env, logJSON(t, "not uploaded", notUploaded, nil))
	env.ExportLogs(t)
	if err := env.LogStore.UploadLogFiles(context.Background()); err == nil {
		t.Fatalf("Expected the upload to fail while the bucket fails")
	}
	info, err := os.Stat(env.localFile(123, notUploaded, "data.parquet"))
	if err != nil {
		t.Fatal(err)
	}

	// Room for the file not uploaded: the uploaded one's copy is evicted,
	// and every Log is still read.
	enforceLogDiskLimit(t, env, info.Size())
	assertGone(t, env.localFile(123, uploaded, "data.parquet"), "evicted by the disk limit")
	assertPresent(t, env.localFile(123, notUploaded, "data.parquet"), "within the disk limit")
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"not uploaded", "uploaded"}) {
		t.Errorf("Expected every Log kept while the disk limit evicts, got %v", got)
	}

	// With nothing uploaded left to evict, it deletes the file not uploaded,
	// and says so.
	logs := captureLogs(t)
	enforceLogDiskLimit(t, env, 1)
	assertGone(t, env.localFile(123, notUploaded, "data.parquet"), "deleted by the disk limit")
	assertPresent(t, bucket.object(123, uploaded, "data.parquet"), "after the disk limit")
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"uploaded"}) {
		t.Errorf("Expected only the uploaded Log kept, got %v", got)
	}
	if !strings.Contains(logs.String(), "not yet uploaded") || !strings.Contains(logs.String(), "level=error") {
		t.Errorf("Expected an error logged for deleting a file not yet uploaded, got %s", logs)
	}
}

func TestAnUploadThatStallsHoldsUpNoDeletionByTheDiskLimit(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()

	older, newer := testNow.Add(-270*time.Minute), testNow.Add(-210*time.Minute)
	postAndProcess(t, env, logJSON(t, "older", older, nil))
	postAndProcess(t, env, logJSON(t, "newer", newer, nil))
	env.ExportLogs(t)

	// The bucket stores the oldest file, and its answer doesn't come.
	stall := make(chan struct{})
	bucket.stall, bucket.stalled = stall, make(chan struct{}, 1)
	release := sync.OnceFunc(func() { close(stall) })
	defer release()
	uploaded := make(chan error, 1)
	go func() { uploaded <- env.LogStore.UploadLogFiles(context.Background()) }()
	<-bucket.stalled

	// The disk limit doesn't wait for the upload: it deletes both hours.
	enforceLogDiskLimit(t, env, 1)
	assertGone(t, env.localFile(123, older, "data.parquet"), "deleted by the disk limit while it uploads")
	assertGone(t, env.localFile(123, newer, "data.parquet"), "deleted by the disk limit")
	if got := latestLogs(t, env, 123, 10); len(got) != 0 {
		t.Errorf("Expected the disk limit to delete every Log, got %v", messages(got))
	}

	// The upload then ends, and the next pass deletes the object it stored.
	release()
	if err := <-uploaded; err != nil {
		t.Errorf("Expected the upload of a file deleted meanwhile to end without an error, got %v", err)
	}
	assertPresent(t, bucket.object(123, older, "data.parquet"), "stored before the deletion")
	enforceLogDiskLimit(t, env, 1)
	assertGone(t, bucket.object(123, older, "data.parquet"), "by the pass after the upload")
	assertGone(t, bucket.object(123, newer, "data.parquet"), "as it was deleted before its upload")
	if got := latestLogs(t, env, 123, 10); len(got) != 0 {
		t.Errorf("Expected no Log back after the upload, got %v", messages(got))
	}
}

func TestAFailingBucketIsRetriedAndTheUploadsCatchUp(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 0)
	defer env.Cleanup()
	logs := captureLogs(t)

	hour := testNow.Add(-270 * time.Minute)
	postAndProcess(t, env, logJSON(t, "waiting", hour, nil))
	env.ExportLogs(t)
	bucket.failPuts.Store(true)
	for range 2 {
		if err := env.LogStore.UploadLogFiles(context.Background()); err == nil {
			t.Fatalf("Expected the upload to fail while the bucket fails")
		}
	}
	// A file not uploaded keeps its local copy, whatever its age.
	env.EvictLogFiles(t)
	assertPresent(t, env.localFile(123, hour, "data.parquet"), "while it isn't uploaded")
	assertGone(t, bucket.object(123, hour, "data.parquet"), "while the bucket fails")
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"waiting"}) {
		t.Errorf("Expected the Log read from local disk, got %v", got)
	}
	if n := strings.Count(logs.String(), "Uploading a Log file to the bucket failed"); n != 1 {
		t.Errorf("Expected the failing file logged once, got %d times:\n%s", n, logs)
	}

	bucket.failPuts.Store(false)
	env.UploadLogFiles(t)
	env.EvictLogFiles(t)
	assertPresent(t, bucket.object(123, hour, "data.parquet"), "once the bucket is back")
	assertGone(t, env.localFile(123, hour, "data.parquet"), "once uploaded")
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"waiting"}) {
		t.Errorf("Expected the Log read from the bucket, got %v", got)
	}
}

func TestAFileAReadHasPinnedKeepsItsLocalCopyUntilTheReadEnds(t *testing.T) {
	env, _ := setupBucketTestEnv(t, 0)
	defer env.Cleanup()

	hour := testNow.Add(-270 * time.Minute)
	postAndProcess(t, env, logJSON(t, "pinned", hour, nil))
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	cancel, longRead := startLongRead(t, env)
	defer cancel()

	if err := env.LogStore.EvictLogFiles(env.Clock.Now()); !errors.Is(err, storage.ErrLogFilesInUse) {
		t.Errorf("Expected eviction to wait for the read, got %v", err)
	}
	if err := env.LogStore.EnforceDiskLimit(storage.LogDiskLimit{Bytes: 1}); !errors.Is(err, storage.ErrLogFilesInUse) {
		t.Errorf("Expected the disk limit to wait for the read, got %v", err)
	}
	assertPresent(t, env.localFile(123, hour, "data.parquet"), "while a read has it pinned")
	if got, err := listLatest(env); err != nil || !slices.Equal(got, []string{"pinned"}) {
		t.Errorf("Expected the Log kept during the read, got %v (%v)", got, err)
	}

	cancel()
	<-longRead
	env.EvictLogFiles(t)
	assertGone(t, env.localFile(123, hour, "data.parquet"), "after the read")
	if got, err := listLatest(env); err != nil || !slices.Equal(got, []string{"pinned"}) {
		t.Errorf("Expected the Log read from the bucket after the read, got %v (%v)", got, err)
	}
}

func TestARestartBetweenAnUploadAndMarkingItUploadsTheFileAgain(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 0)
	defer env.Cleanup()

	hour := testNow.Add(-270 * time.Minute)
	postAndProcess(t, env, logJSON(t, "uploaded twice", hour, nil))
	env.ExportLogs(t)
	bucket.putThenFail.Store(true)
	if err := env.LogStore.UploadLogFiles(context.Background()); err == nil {
		t.Fatalf("Expected the upload to fail")
	}
	assertPresent(t, bucket.object(123, hour, "data.parquet"), "after the upload")
	// Not marked uploaded, its copy stays.
	env.EvictLogFiles(t)
	assertPresent(t, env.localFile(123, hour, "data.parquet"), "before it is marked uploaded")

	bucket.putThenFail.Store(false)
	env.Reopen(t)
	env.UploadLogFiles(t)
	env.EvictLogFiles(t)
	assertGone(t, env.localFile(123, hour, "data.parquet"), "once uploaded again")
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"uploaded twice"}) {
		t.Errorf("Expected the Log read from the bucket, got %v", got)
	}
}

func TestARestartBetweenEvictingAFileAndMarkingItReadsTheFileFromTheBucket(t *testing.T) {
	env, _ := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	cursor := armMonitor(t, env, "").cursor
	hour := testNow.Add(-270 * time.Minute)
	postAndProcess(t, env, logJSON(t, "evicted", hour, nil), logJSON(t, "hot", testNow, nil))
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	want := bucketAnswersOf(t, env, cursor)

	// The eviction removed the copy, and EventStore stopped before it
	// marked the file.
	if err := os.Remove(env.localFile(123, hour, "data.parquet")); err != nil {
		t.Fatal(err)
	}
	env.Reopen(t)
	assertBucketAnswers(t, env, cursor, want, "after the restart")
	assertFetchedWithoutTime(t, env, logByMessage(t, want.logs, "evicted"), false, "once its file is only in the bucket")
}

func TestARestartWithAFileNeitherUploadedNorOnDiskSaysItIsLost(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()

	lost, kept := testNow.Add(-270*time.Minute), testNow.Add(-210*time.Minute)
	postAndProcess(t, env, logJSON(t, "lost", lost, nil), logJSON(t, "kept", kept, nil))
	env.ExportLogs(t)
	bucket.failPuts.Store(true)
	if err := env.LogStore.UploadLogFiles(context.Background()); err == nil {
		t.Fatalf("Expected the upload to fail")
	}
	if err := os.Remove(env.localFile(123, lost, "data.parquet")); err != nil {
		t.Fatal(err)
	}

	logs := captureLogs(t)
	env.Reopen(t)
	if !strings.Contains(logs.String(), "missing from local disk") || !strings.Contains(logs.String(), logFileKey(123, lost, "data.parquet")) {
		t.Errorf("Expected the lost file logged at the restart, got %s", logs)
	}
	bucket.failPuts.Store(false)
	if err := env.LogStore.UploadLogFiles(context.Background()); err == nil {
		t.Errorf("Expected the lost file to fail to upload")
	}
	assertPresent(t, bucket.object(123, kept, "data.parquet"), "after the restart")
}

// testExtensionDirectory holds httpfs for the tests that read a bucket:
// SOLIDTRACE_TEST_DUCKDB_EXTENSION_DIRECTORY, or the dev box's
// ~/.cache/solid-trace/duckdb_extensions.
func testExtensionDirectory(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("SOLIDTRACE_TEST_DUCKDB_EXTENSION_DIRECTORY"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".cache", "solid-trace", "duckdb_extensions")
}

// TestLogFilesInAnS3BucketAreReadThroughHttpfs runs against the S3 API at
// SOLIDTRACE_TEST_S3_ENDPOINT, such as MinIO
// (~/.cache/solid-trace/logs-v2-notes/setup.md), and skips without it.
func TestLogFilesInAnS3BucketAreReadThroughHttpfs(t *testing.T) {
	endpoint := os.Getenv("SOLIDTRACE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SOLIDTRACE_TEST_S3_ENDPOINT, and the bucket's key in SOLIDTRACE_OBJECT_STORE_KEY_ID and SOLIDTRACE_OBJECT_STORE_SECRET, to run against an S3 API")
	}
	name := os.Getenv("SOLIDTRACE_TEST_S3_BUCKET")
	if name == "" {
		name = "solidtrace-test"
	}
	cfg := &config.ObjectStore{
		Kind: config.ObjectStoreS3, Bucket: name, Prefix: "test-" + uuid.NewString() + "/",
		Region: "us-east-1", Endpoint: endpoint,
		KeyID: os.Getenv("SOLIDTRACE_OBJECT_STORE_KEY_ID"), Secret: os.Getenv("SOLIDTRACE_OBJECT_STORE_SECRET"),
	}
	bucket := storage.NewObjectStore(cfg)
	ctx := context.Background()
	t.Cleanup(func() {
		objects, _ := bucket.List(ctx, "")
		for _, object := range objects {
			_ = bucket.Delete(ctx, object.Key)
		}
	})
	env := newTestEnv(t, func(env *TestEnv) {
		env.Bucket, env.BucketConfig, env.LocalCacheHours = bucket, cfg, 0
	})
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedProject(t, env.SQLitePath, 123, 0)

	cursor := armMonitor(t, env, "").cursor
	pastRetention := time.Date(2026, 9, 1, 23, 30, 0, 0, time.UTC)
	postAt(t, env, 123, "test_public_key", "past retention", pastRetention)
	env.Clock.Set(testNow)
	seven, nine := testNow.Add(-270*time.Minute), testNow.Add(-165*time.Minute)
	postAndProcess(t, env, logJSON(t, "seven", seven, nil), logJSON(t, "nine", nine, nil), logJSON(t, "hot", testNow, nil))
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	want := bucketAnswersOf(t, env, cursor)
	if got := messages(want.logs); !slices.Equal(got, []string{"hot", "nine", "seven", "past retention"}) {
		t.Fatalf("Expected every Log read, got %v", got)
	}

	env.EvictLogFiles(t)
	assertGone(t, env.localFile(123, seven, "data.parquet"), "with local_cache_hours 0")
	assertBucketAnswers(t, env, cursor, want, "read from the bucket through httpfs")

	// Retention deletes the Logs, and the rest of the Cleanup pass, the disk
	// limit, deletes their object.
	env.DeleteLogsPastRetention(t)
	enforceLogDiskLimit(t, env, 1<<40)
	objects, err := bucket.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	if wantKeys := []string{logFileKey(123, seven, "data.parquet"), logFileKey(123, nine, "data.parquet")}; !slices.Equal(keys, wantKeys) {
		t.Errorf("Expected Retention to delete its file from the bucket, leaving %v, got %v", wantKeys, keys)
	}
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"hot", "nine", "seven"}) {
		t.Errorf("Expected the Logs within Retention kept, got %v", got)
	}
}

func TestARestartWithoutTheBucketWarnsThatEvictedHoursCantBeRead(t *testing.T) {
	env, _ := setupBucketTestEnv(t, 0)
	defer env.Cleanup()

	postAndProcess(t, env, logJSON(t, "evicted", testNow.Add(-270*time.Minute), nil), logJSON(t, "hot", testNow, nil))
	env.ExportLogs(t)
	env.UploadLogFiles(t)

	// Before the eviction, every file is still on local disk.
	logs := captureLogs(t)
	env.Bucket = nil
	env.Reopen(t)
	if strings.Contains(logs.String(), "only in a bucket") {
		t.Errorf("Expected no warning while every file is local, got %s", logs)
	}

	env.Bucket = storage.NewDirStore(filepath.Join(env.dir, "bucket"))
	env.Reopen(t)
	env.EvictLogFiles(t)
	logs.Reset()
	env.Bucket = nil
	env.Reopen(t)
	if !strings.Contains(logs.String(), "only in a bucket") || !strings.Contains(logs.String(), "files=1") {
		t.Errorf("Expected a warning that one file is only in a bucket, got %s", logs)
	}
}
