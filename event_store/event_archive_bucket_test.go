package main_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pipeline"
	"github.com/solidtrace/event_store/pkg/msgpacker"
	"github.com/solidtrace/event_store/storage"
)

// Days of the Events these tests archive, before testNow's 2026-10-02.
var (
	september30 = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	october1    = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	october2    = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
)

// postEventAt stores an Event for Project 123 with its time, and returns its
// UUID.
func postEventAt(t *testing.T, env *TestEnv, message string, at time.Time) string {
	t.Helper()
	return postEvent(t, env, fmt.Sprintf(`{"message":%q,"timestamp":%q}`, message, at.Format(time.RFC3339)))
}

// postArchivedDays stores Project 123's Events on 30 September, two on
// 1 October and one on 2 October, and processes them. It returns the UUID of
// the first on 1 October.
func postArchivedDays(t *testing.T, env *TestEnv) string {
	t.Helper()
	postEventAt(t, env, "september", september30.Add(8*time.Hour))
	first := postEventAt(t, env, "october first", october1.Add(9*time.Hour))
	postEventAt(t, env, "october second", october1.Add(15*time.Hour))
	postEventAt(t, env, "hot", october2.Add(11*time.Hour))
	env.Process(t)
	return first
}

// ArchiveEvents archives every Event dated on or before cutoff, as the
// archive consumer does.
func (env *TestEnv) ArchiveEvents(t *testing.T, cutoff time.Time) {
	t.Helper()
	env.BatchLock.Lock()
	defer env.BatchLock.Unlock()
	if err := env.DuckDBWriter.ArchiveEventsUpTo(cutoff); err != nil {
		t.Fatalf("Archiving Events failed: %v", err)
	}
}

// UploadEventArchive uploads the archive files on local disk, as the archive
// consumer does after each run.
func (env *TestEnv) UploadEventArchive(t *testing.T) {
	t.Helper()
	if err := env.DuckDBWriter.UploadEventArchive(context.Background()); err != nil {
		t.Fatalf("Uploading the Event archive failed: %v", err)
	}
}

// EnsureEventsView lists the Event archive in the bucket and builds the
// events view, as the archive consumer does first when EventStore starts.
func (env *TestEnv) EnsureEventsView(t *testing.T) {
	t.Helper()
	if err := env.DuckDBWriter.EnsureEventsView(context.Background()); err != nil {
		t.Fatalf("Building the events view failed: %v", err)
	}
}

// localArchive lists the archive files on local disk, by their path below
// the archive folder.
func (env *TestEnv) localArchive(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(env.dir, "parquet")
	files, err := filepath.Glob(filepath.Join(dir, "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range files {
		rel, _ := filepath.Rel(dir, file)
		files[i] = filepath.ToSlash(rel)
	}
	return files
}

// archiveObjects lists the Event archive's objects in the bucket, by key.
func archiveObjects(t *testing.T, bucket storage.ObjectStore) []string {
	t.Helper()
	objects, err := bucket.List(context.Background(), "events/")
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	return keys
}

// assertArchiveDays checks that keys are one archive file for each day,
// under prefix.
func assertArchiveDays(t *testing.T, keys []string, prefix string, days []time.Time, where string) {
	t.Helper()
	if len(keys) != len(days) {
		t.Fatalf("Expected %d archive files %s, got %v", len(days), where, keys)
	}
	for i, day := range days {
		want := prefix + "event_date=" + day.Format(time.DateOnly) + "/data-"
		if !strings.HasPrefix(keys[i], want) || !strings.HasSuffix(keys[i], ".parquet") {
			t.Errorf("Expected an archive file %s*.parquet %s, got %s", want, where, keys[i])
		}
	}
}

// getQuery sends a GET to the query app and returns the status and the raw
// answer.
func getQuery(t *testing.T, env *TestEnv, path string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", path, nil)
	resp, err := env.QueryApp.Test(req, 5000)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// eventAnswers is what every kind of read of Project 123's Events answers:
// the list of them, their count, an Event with its neighbours, and SQL
// mode's events view.
type eventAnswers struct {
	list, sql []string
	count     string
	context   string
}

func eventAnswersOf(t *testing.T, env *TestEnv, contextUUID string) eventAnswers {
	t.Helper()
	var answers eventAnswers
	for _, read := range []struct {
		path string
		into *string
	}{
		{"/api/123/events?limit=100&sort=desc", nil},
		{"/api/123/events/count", &answers.count},
		{"/api/123/events/context?uuid=" + contextUUID, &answers.context},
	} {
		status, raw := getQuery(t, env, read.path)
		if status != 200 {
			t.Fatalf("GET %s expected 200, got %d: %s", read.path, status, raw)
		}
		if read.into != nil {
			*read.into = string(raw)
			continue
		}
		var events []struct {
			UUID      string    `json:"uuid"`
			Timestamp time.Time `json:"timestamp"`
		}
		if err := json.Unmarshal(raw, &events); err != nil {
			t.Fatalf("Decode %s: %v", raw, err)
		}
		for _, event := range events {
			answers.list = append(answers.list, event.UUID+" "+event.Timestamp.UTC().Format(time.RFC3339))
		}
	}
	_, answers.sql = sqlTable(t, env, sqlRequest{
		SQL:  "SELECT uuid, timestamp, level, tags FROM events WHERE project_id = 123 ORDER BY uuid",
		From: allTime.from, To: allTime.to,
	})
	return answers
}

// assertEventAnswers checks every kind of read of Project 123's Events still
// answers want.
func assertEventAnswers(t *testing.T, env *TestEnv, contextUUID string, want eventAnswers, when string) {
	t.Helper()
	got := eventAnswersOf(t, env, contextUUID)
	if !slices.Equal(got.list, want.list) {
		t.Errorf("Expected the Events listed %s as before, got\n%v\nwant\n%v", when, got.list, want.list)
	}
	if got.count != want.count {
		t.Errorf("Expected the Events counted %s as before, got %s, want %s", when, got.count, want.count)
	}
	if got.context != want.context {
		t.Errorf("Expected the Event's neighbours %s as before, got %s, want %s", when, got.context, want.context)
	}
	if !slices.Equal(got.sql, want.sql) {
		t.Errorf("Expected SQL mode's events view %s as before, got\n%v\nwant\n%v", when, got.sql, want.sql)
	}
}

func TestArchivedEventsAreUploadedAndReadFromTheBucket(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	contextUUID := postArchivedDays(t, env)
	want := eventAnswersOf(t, env, contextUUID)
	if len(want.list) != 4 || want.count != `{"count":4}` {
		t.Fatalf("Expected 4 Events, got %v and %s", want.list, want.count)
	}

	env.ArchiveEvents(t, october1)
	assertArchiveDays(t, env.localArchive(t), "", []time.Time{september30, october1}, "on local disk before the upload")
	assertEventAnswers(t, env, contextUUID, want, "before the upload")

	logs := captureLogs(t)
	env.UploadEventArchive(t)
	objects := archiveObjects(t, bucket)
	assertArchiveDays(t, objects, "events/", []time.Time{september30, october1}, "in the bucket")
	if local := env.localArchive(t); len(local) != 0 {
		t.Errorf("Expected no local copy or date folder kept after the upload, got %v", local)
	}
	// Each file's upload and the deletion of its local copy are logged.
	for _, message := range []string{"Uploaded an Event archive file to the bucket", "Deleted the local copy of an uploaded Event archive file"} {
		if got := strings.Count(logs.String(), message); got != len(objects) {
			t.Errorf("Expected %q logged for each of %d files, got %d in %s", message, len(objects), got, logs.String())
		}
	}
	for _, key := range objects {
		if !strings.Contains(logs.String(), key) {
			t.Errorf("Expected %s named in the log, got %s", key, logs.String())
		}
	}
	assertEventAnswers(t, env, contextUUID, want, "read from the bucket")

	env.Reopen(t)
	env.EnsureEventsView(t)
	assertEventAnswers(t, env, contextUUID, want, "after a restart lists the bucket")

	// A later run's file joins them.
	env.ArchiveEvents(t, october2)
	assertEventAnswers(t, env, contextUUID, want, "with one day local and two in the bucket")
	env.UploadEventArchive(t)
	assertArchiveDays(t, archiveObjects(t, bucket), "events/", []time.Time{september30, october1, october2}, "after the next run")
	assertEventAnswers(t, env, contextUUID, want, "with every day in the bucket")
	if puts := bucket.puts.Load(); puts != 3 {
		t.Errorf("Expected each file uploaded once, got %d uploads", puts)
	}
}

func TestAFailingBucketKeepsTheArchiveLocalUntilAnUploadAfterARestart(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	logs := captureLogs(t)

	contextUUID := postArchivedDays(t, env)
	want := eventAnswersOf(t, env, contextUUID)
	env.ArchiveEvents(t, october1)
	bucket.failPuts.Store(true)
	for range 2 {
		if err := env.DuckDBWriter.UploadEventArchive(context.Background()); err == nil {
			t.Fatalf("Expected the upload to fail while the bucket fails")
		}
	}
	if n := strings.Count(logs.String(), "Uploading an Event archive file to the bucket failed"); n != 2 {
		t.Errorf("Expected each failing file logged once, got %d lines:\n%s", n, logs)
	}
	assertArchiveDays(t, env.localArchive(t), "", []time.Time{september30, october1}, "while the bucket fails")
	assertEventAnswers(t, env, contextUUID, want, "while the bucket fails")

	// EventStore stops before any upload gets through.
	bucket.failPuts.Store(false)
	env.Reopen(t)
	env.EnsureEventsView(t)
	assertEventAnswers(t, env, contextUUID, want, "after the restart, from local disk")
	env.UploadEventArchive(t)
	assertArchiveDays(t, archiveObjects(t, bucket), "events/", []time.Time{september30, october1}, "once the bucket is back")
	if local := env.localArchive(t); len(local) != 0 {
		t.Errorf("Expected the local copies deleted once uploaded, got %v", local)
	}
	assertEventAnswers(t, env, contextUUID, want, "from the bucket")
}

func TestARestartBetweenAnUploadAndDeletingTheLocalCopyReadsEachEventOnce(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	contextUUID := postArchivedDays(t, env)
	want := eventAnswersOf(t, env, contextUUID)
	env.ArchiveEvents(t, october1)
	// Both objects are stored, and EventStore stops before it sees.
	bucket.putThenFail.Store(true)
	if err := env.DuckDBWriter.UploadEventArchive(context.Background()); err == nil {
		t.Fatalf("Expected the upload to fail")
	}
	bucket.putThenFail.Store(false)
	objects := archiveObjects(t, bucket)
	assertArchiveDays(t, objects, "events/", []time.Time{september30, october1}, "after the upload")
	assertArchiveDays(t, env.localArchive(t), "", []time.Time{september30, october1}, "after the upload")
	assertEventAnswers(t, env, contextUUID, want, "with each file in both places")

	// One object was cut short, as by a store that isn't the bucket's own.
	cutShort := filepath.Join(bucket.dir, filepath.FromSlash(objects[1]))
	if err := os.WriteFile(cutShort, []byte("PAR1"), 0o644); err != nil {
		t.Fatal(err)
	}

	env.Reopen(t)
	env.EnsureEventsView(t)
	assertEventAnswers(t, env, contextUUID, want, "after the restart, with each file in both places")
	puts := bucket.puts.Load()
	env.UploadEventArchive(t)
	if got := bucket.puts.Load() - puts; got != 1 {
		t.Errorf("Expected only the file cut short uploaded again, got %d uploads", got)
	}
	if local := env.localArchive(t); len(local) != 0 {
		t.Errorf("Expected the local copies deleted, got %v", local)
	}
	assertEventAnswers(t, env, contextUUID, want, "from the bucket")
}

func TestEventReadsFailUntilTheArchiveInTheBucketIsListed(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	contextUUID := postArchivedDays(t, env)
	want := eventAnswersOf(t, env, contextUUID)
	env.ArchiveEvents(t, october1)
	env.UploadEventArchive(t)

	logs := captureLogs(t)
	bucket.failLists.Store(true)
	env.Reopen(t)
	assertEventReadsUnavailable(t, env, contextUUID, "before the archive is listed")
	for range 2 {
		if err := env.DuckDBWriter.UploadEventArchive(context.Background()); err == nil {
			t.Errorf("Expected the upload pass to fail while the bucket can't be listed")
		}
	}
	if n := strings.Count(logs.String(), "Listing the Event archive in the bucket failed"); n != 1 {
		t.Errorf("Expected the failing listing logged once, got %d lines:\n%s", n, logs)
	}
	assertEventReadsUnavailable(t, env, contextUUID, "while the archive can't be listed")
	// Ingest and Event processing carry on.
	later := postEventAt(t, env, "while the bucket can't be listed", october2.Add(12*time.Hour))
	env.Process(t)

	bucket.failLists.Store(false)
	env.UploadEventArchive(t)
	got := eventAnswersOf(t, env, contextUUID)
	want.list = append([]string{later + " " + october2.Add(12*time.Hour).Format(time.RFC3339)}, want.list...)
	if !slices.Equal(got.list, want.list) || got.count != `{"count":5}` || len(got.sql) != 5 {
		t.Errorf("Expected the 5 Events read once the bucket is listed, got %v, %s and %v", got.list, got.count, got.sql)
	}
}

// assertEventReadsUnavailable checks that every kind of read of Project
// 123's Events fails and says why, rather than answer without some of them.
func assertEventReadsUnavailable(t *testing.T, env *TestEnv, contextUUID, when string) {
	t.Helper()
	const why = "Events can't be read now"
	for _, path := range []string{
		"/api/123/events?limit=100&sort=desc",
		"/api/123/events/count",
		"/api/123/events/context?uuid=" + contextUUID,
	} {
		if status, raw := getQuery(t, env, path); status != 503 || !strings.Contains(string(raw), why) {
			t.Errorf("Expected GET %s to answer 503 %s, got %d: %s", path, when, status, raw)
		}
	}
	status, raw := runSQL(t, env, 123, sqlRequest{SQL: "SELECT count(*) FROM events", From: allTime.from, To: allTime.to})
	if status == 200 || !strings.Contains(string(raw), why) {
		t.Errorf("Expected SQL mode's events view to fail %s, got %d: %s", when, status, raw)
	}
}

func TestEventReadsFailAfterARebuildOfTheEventsViewFailsUntilARetrySucceeds(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	logs := captureLogs(t)

	contextUUID := postArchivedDays(t, env)
	want := eventAnswersOf(t, env, contextUUID)
	env.ArchiveEvents(t, september30)
	env.UploadEventArchive(t)

	// 30 September's object can't be read for a while.
	object := filepath.Join(bucket.dir, filepath.FromSlash(archiveObjects(t, bucket)[0]))
	saved, err := os.ReadFile(object)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(object, []byte("unreadable"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The next run moves 1 October's Events out of the hot table, but the
	// events view can't be rebuilt to read their file.
	env.ArchiveEvents(t, october1)
	assertEventReadsUnavailable(t, env, contextUUID, "after the rebuild fails")
	if err := env.DuckDBWriter.UploadEventArchive(context.Background()); err == nil {
		t.Errorf("Expected the retry to fail while an object can't be read")
	}

	// The object can be read again, but the view would still miss
	// 1 October's Events until a retry rebuilds it.
	if err := os.WriteFile(object, saved, 0o644); err != nil {
		t.Fatal(err)
	}
	assertEventReadsUnavailable(t, env, contextUUID, "until a retry rebuilds the view")
	if n := strings.Count(logs.String(), "Rebuilding the events view failed"); n != 1 {
		t.Errorf("Expected the failing rebuilds logged once, got %d lines:\n%s", n, logs)
	}

	env.UploadEventArchive(t)
	assertEventAnswers(t, env, contextUUID, want, "once a retry rebuilds the view")
	assertArchiveDays(t, archiveObjects(t, bucket), "events/", []time.Time{september30, october1}, "after the retry")
	if local := env.localArchive(t); len(local) != 0 {
		t.Errorf("Expected the local copy deleted once the view reads it from the bucket, got %v", local)
	}
	if !strings.Contains(logs.String(), "Rebuilt the events view after failing") {
		t.Errorf("Expected the rebuild logged once it succeeds, got:\n%s", logs)
	}
}

func TestIngestWorksWhileTheBucketDoesntAnswerAtStartup(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	contextUUID := postArchivedDays(t, env)
	env.ArchiveEvents(t, october1)
	env.UploadEventArchive(t)

	// EventStore starts again while the bucket doesn't answer, and the
	// archive consumer starts to list it.
	bucket.hangLists = make(chan struct{})
	started := time.Now()
	env.Reopen(t)
	if took := time.Since(started); took > 10*time.Second {
		t.Errorf("Expected EventStore to start without waiting for the bucket, took %v", took)
	}
	consumer := pipeline.NewArchiveConsumer(env.MQReader, env.DuckDBWriter, env.BatchLock)
	go consumer.Run()
	defer consumer.Stop()

	// Ingest and Event processing carry on, and Event reads say why they
	// can't answer yet.
	postEventAt(t, env, "while the bucket doesn't answer", october2.Add(12*time.Hour))
	env.Process(t)
	assertEventReadsUnavailable(t, env, contextUUID, "while the bucket doesn't answer")

	close(bucket.hangLists)
	waitFor(t, 10*time.Second, "Event reads to answer once the bucket is listed", func() bool {
		status, _ := getQuery(t, env, "/api/123/events/count")
		return status == 200
	})
	if events := listEvents(t, env); len(events) != 5 {
		t.Errorf("Expected 5 Events listed once the bucket is listed, got %d", len(events))
	}
}

func TestRemovingTheBucketWarnsThatTheArchivedEventsOnlyInItAreUnread(t *testing.T) {
	env, _ := setupBucketTestEnv(t, 24)
	defer env.Cleanup()

	postArchivedDays(t, env)
	env.ArchiveEvents(t, september30)
	env.UploadEventArchive(t)
	env.ArchiveEvents(t, october1)

	// The object_store block is removed, with 1 October's file still on
	// local disk.
	logs := captureLogs(t)
	env.Bucket = nil
	env.Reopen(t)
	if !strings.Contains(logs.String(), "Some Event archive files are only in a bucket") || !strings.Contains(logs.String(), "files=1") {
		t.Errorf("Expected a warning that 1 Event archive file is only in the bucket, got:\n%s", logs)
	}
	if events := listEvents(t, env); len(events) != 3 {
		t.Errorf("Expected 3 Events listed, without those only in the bucket, got %d", len(events))
	}
}

func TestAnUploadDeletesALocalCopyOnlyOnceTheReadsThatMayOpenItEnd(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	postArchivedDays(t, env)
	env.ArchiveEvents(t, october1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	longRead := make(chan error, 1)
	go func() {
		_, err := env.LogStore.RunSQL(ctx, 123, allTime.from, allTime.to,
			"SELECT count(*) FROM events, range(1000000000000)", 10)
		longRead <- err
	}()
	time.Sleep(300 * time.Millisecond)

	uploaded := make(chan error, 1)
	go func() { uploaded <- env.DuckDBWriter.UploadEventArchive(context.Background()) }()
	waitFor(t, 10*time.Second, "the files uploaded", func() bool { return len(archiveObjects(t, bucket)) == 2 })
	time.Sleep(300 * time.Millisecond)
	assertArchiveDays(t, env.localArchive(t), "", []time.Time{september30, october1}, "while a read that may open them runs")
	if events := listEvents(t, env); len(events) != 4 {
		t.Errorf("Expected 4 Events listed meanwhile, got %d", len(events))
	}

	cancel()
	<-longRead
	if err := <-uploaded; err != nil {
		t.Fatalf("Uploading the Event archive failed: %v", err)
	}
	if local := env.localArchive(t); len(local) != 0 {
		t.Errorf("Expected the local copies deleted once the read ended, got %v", local)
	}
	if events := listEvents(t, env); len(events) != 4 {
		t.Errorf("Expected 4 Events listed from the bucket, got %d", len(events))
	}
}

// enqueueArchive asks EventStore to archive every Event dated on or before
// cutoff, as Console's archive job does.
func enqueueArchive(t *testing.T, env *TestEnv, cutoff time.Time) {
	t.Helper()
	payload, err := msgpacker.New(msgpacker.ModeRaw).Pack(map[string]string{"date": cutoff.Format(time.DateOnly)})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", env.MQPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().Format("2006-01-02 15:04:05.000000")
	if _, err := db.Exec(`
		INSERT INTO console_messages (message_type, payload, status, attempts, created_at, updated_at)
		VALUES ('archive_events', ?, ?, 0, ?, ?)`, payload, storage.StatusPending, now, now); err != nil {
		t.Fatal(err)
	}
}

func TestTheArchiveConsumerUploadsAtStartAndRightAfterEachRun(t *testing.T) {
	env, bucket := setupBucketTestEnv(t, 24)
	defer env.Cleanup()

	postArchivedDays(t, env)
	bucket.failPuts.Store(true)
	env.ArchiveEvents(t, september30)
	if err := env.DuckDBWriter.UploadEventArchive(context.Background()); err == nil {
		t.Fatalf("Expected the upload to fail")
	}
	bucket.failPuts.Store(false)

	// Started with a file waiting, as after a restart, it uploads it.
	consumer := pipeline.NewArchiveConsumer(env.MQReader, env.DuckDBWriter, env.BatchLock)
	go consumer.Run()
	defer consumer.Stop()
	uploaded := func(days ...time.Time) func() bool {
		return func() bool {
			return len(archiveObjects(t, bucket)) == len(days) && len(env.localArchive(t)) == 0
		}
	}
	waitFor(t, 10*time.Second, "the file archived before the start uploaded", uploaded(september30))

	enqueueArchive(t, env, october1)
	waitFor(t, 15*time.Second, "the next run's file uploaded", uploaded(september30, october1))
	assertArchiveDays(t, archiveObjects(t, bucket), "events/", []time.Time{september30, october1}, "after the run")
	if events := listEvents(t, env); len(events) != 4 {
		t.Errorf("Expected 4 Events listed, got %d", len(events))
	}
}

// TestTheEventArchiveInAnS3BucketIsReadThroughHttpfs runs against the S3 API
// at SOLIDTRACE_TEST_S3_ENDPOINT, such as MinIO, and skips without it.
func TestTheEventArchiveInAnS3BucketIsReadThroughHttpfs(t *testing.T) {
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
	env.EnsureEventsView(t)
	setSQLQueries(t, env, true)

	contextUUID := postArchivedDays(t, env)
	want := eventAnswersOf(t, env, contextUUID)
	env.ArchiveEvents(t, october1)
	env.UploadEventArchive(t)
	assertArchiveDays(t, archiveObjects(t, bucket), "events/", []time.Time{september30, october1}, "in the bucket")
	if local := env.localArchive(t); len(local) != 0 {
		t.Errorf("Expected no local copy kept after the upload, got %v", local)
	}
	assertEventAnswers(t, env, contextUUID, want, "read from the bucket through httpfs")

	env.Reopen(t)
	env.EnsureEventsView(t)
	assertEventAnswers(t, env, contextUUID, want, "after a restart lists the bucket")
}
