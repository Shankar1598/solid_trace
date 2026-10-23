package logprocessing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// These tests run prepare-clickhouse on an install of the last DuckDB
// release, as the command does with the stack stopped, and check what the
// ClickHouse release reads: the prepared files, the marker and the Log files
// they list.

// keepEveryLog is a Log transform that stores every Log as it came.
type keepEveryLog struct{}

func (keepEveryLog) Apply(_ uint32, batches []storage.LogBatch) ([]storage.LogBatch, error) {
	return batches, nil
}

// install is an install of the DuckDB release in a folder of its own. Its
// Pebble stays open throughout, as one process can open it once; logs.duckdb
// is opened by the running release (run) and by the command (prepare) in
// turn.
type install struct {
	t       *testing.T
	dir     string
	batches *storage.PebbleWriter
	// bucket is the fake bucket, or nil when the files stay on local disk.
	bucket storage.ObjectStore
	// localCacheHours is how long an uploaded file's local copy stays.
	localCacheHours int
	// posted are the ids of every Log posted, by Project.
	posted map[uint32][]string
}

func newInstall(t *testing.T) *install {
	t.Helper()
	logger.Init()
	dir := t.TempDir()
	batches, err := storage.NewPebbleWriter(&config.Config{PebblePath: filepath.Join(dir, "pebble")})
	if err != nil {
		t.Fatalf("NewPebbleWriter: %v", err)
	}
	t.Cleanup(batches.Close)
	return &install{t: t, dir: dir, batches: batches, posted: map[uint32][]string{}}
}

// withBucket keeps the install's Log files in a fake bucket too, each local
// copy until localCacheHours after its hour ends.
func (in *install) withBucket(localCacheHours int) *install {
	in.bucket = storage.NewDirStore(filepath.Join(in.dir, "bucket"))
	in.localCacheHours = localCacheHours
	return in
}

func (in *install) logsDuckDB() string   { return filepath.Join(in.dir, "logs.duckdb") }
func (in *install) logFilesDir() string  { return filepath.Join(in.dir, "logs_parquet") }
func (in *install) preparedDir() string  { return storage.PreparedDir(in.logsDuckDB()) }
func (in *install) eventsDuckDB() string { return filepath.Join(in.dir, "events.duckdb") }

// post writes a Log batch of a Project's Logs to Pebble, as Log ingest does.
func (in *install) post(projectID uint32, receivedAt time.Time, logs ...models.Log) {
	in.t.Helper()
	if err := in.batches.WriteLogBatch(projectID, logs, receivedAt); err != nil {
		in.t.Fatalf("WriteLogBatch: %v", err)
	}
	for _, log := range logs {
		in.posted[projectID] = append(in.posted[projectID], log.ID.String())
	}
}

// useFiles points a LogStore at the install's Log files, and its bucket if
// it has one.
func (in *install) useFiles(store *storage.LogStore) {
	in.t.Helper()
	var err error
	if in.bucket != nil {
		err = store.UseObjectStore(in.bucket, in.localCacheHours)
	} else {
		err = store.UseLocalDiskOnly()
	}
	if err != nil {
		in.t.Fatalf("use the Log files' store: %v", err)
	}
}

// running is the DuckDB release running on the install.
type running struct {
	in     *install
	store  *storage.LogStore
	clock  *clock.Manual
	events *storage.DuckDBWriter
}

// run starts the DuckDB release's Log stores on the install, at now, and
// stops them at the end of do.
func (in *install) run(now time.Time, do func(r *running)) {
	in.t.Helper()
	events, err := storage.NewDuckDBWriter(in.eventsDuckDB(), filepath.Join(in.dir, "events_parquet"), "", "")
	if err != nil {
		in.t.Fatalf("NewDuckDBWriter: %v", err)
	}
	defer events.Close()
	store, err := storage.NewLogStore(events, in.logsDuckDB(), in.logFilesDir())
	if err != nil {
		in.t.Fatalf("NewLogStore: %v", err)
	}
	in.useFiles(store)
	do(&running{in: in, store: store, clock: clock.NewManual(now), events: events})
}

// process runs Log processing over every Log batch waiting, at the clock's
// time.
func (r *running) process() {
	r.in.t.Helper()
	processor := New(r.in.batches, r.store, keepEveryLog{}, r.clock)
	if err := processor.Prepare(); err != nil {
		r.in.t.Fatalf("Prepare: %v", err)
	}
	if err := processor.ProcessUntilCaughtUp(); err != nil {
		r.in.t.Fatalf("ProcessUntilCaughtUp: %v", err)
	}
}

// export runs the Log export at the clock's time.
func (r *running) export() {
	r.in.t.Helper()
	if err := r.store.ExportLogs(r.clock.Now()); err != nil {
		r.in.t.Fatalf("ExportLogs: %v", err)
	}
}

// prepare runs prepare-clickhouse on the install at now, as the release
// named release, and returns what it returned.
func (in *install) prepare(now time.Time, release string) (alreadyPrepared bool, err error) {
	in.t.Helper()
	duck, err := storage.NewLogsDuckDB("", "")
	if err != nil {
		in.t.Fatalf("NewLogsDuckDB: %v", err)
	}
	defer duck.Close()
	store, err := storage.NewLogStore(duck, in.logsDuckDB(), in.logFilesDir())
	if err != nil {
		in.t.Fatalf("NewLogStore: %v", err)
	}
	in.useFiles(store)
	processor := New(in.batches, store, keepEveryLog{}, clock.NewManual(now))
	return PrepareClickHouse(context.Background(), processor, store, in.preparedDir(), release)
}

// mustPrepare runs prepare-clickhouse and fails the test if it fails.
func (in *install) mustPrepare(now time.Time, release string) {
	in.t.Helper()
	if _, err := in.prepare(now, release); err != nil {
		in.t.Fatalf("PrepareClickHouse: %v", err)
	}
}

// query runs a query on a DuckDB of its own, as the ClickHouse release would
// read the prepared files, and returns its rows, each as its columns' text
// joined by |.
func query(t *testing.T, sqlText string, args ...any) []string {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("open DuckDB: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(sqlText, args...)
	if err != nil {
		t.Fatalf("query %s: %v", sqlText, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		texts := make([]string, len(values))
		for i, v := range values {
			texts[i] = v.String
			if !v.Valid {
				texts[i] = "NULL"
			}
		}
		out = append(out, strings.Join(texts, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// preparedFile is the path of one of the prepared files.
func (in *install) preparedFile(name string) string {
	return filepath.Join(in.preparedDir(), name)
}

// fileLocation is where the ClickHouse release reads a prepared Log file:
// its local copy if it has one, else its object in the bucket.
func (in *install) fileLocation(key string, local bool) string {
	if local {
		return filepath.Join(in.logFilesDir(), filepath.FromSlash(key))
	}
	return in.bucket.URL("logs/" + key)
}

// filedLogs returns the ids of every Log in the Log files the prepared
// manifest lists for a Project, sorted.
func (in *install) filedLogs(projectID uint32) []string {
	in.t.Helper()
	rows := query(in.t, "SELECT key, local FROM read_parquet(?) WHERE project_id = ? ORDER BY key",
		in.preparedFile("log_files.parquet"), projectID)
	var ids []string
	for _, row := range rows {
		key, local, _ := strings.Cut(row, "|")
		ids = append(ids, query(in.t, "SELECT CAST(id AS VARCHAR) FROM read_parquet(?)", in.fileLocation(key, local == "true"))...)
	}
	slices.Sort(ids)
	return ids
}

// assertEveryLogFiled checks that the Log files the prepared manifest lists
// hold every Log posted to each Project, once.
func (in *install) assertEveryLogFiled() {
	in.t.Helper()
	for projectID, posted := range in.posted {
		want := slices.Sorted(slices.Values(posted))
		if got := in.filedLogs(projectID); !slices.Equal(got, want) {
			in.t.Errorf("Expected the prepared files of Project %d to hold its %d Logs, got %d:\n got %v\nwant %v",
				projectID, len(want), len(got), got, want)
		}
	}
}

// logAt is a Log at a time, with an id of that time, as Log ingest makes it.
func logAt(at time.Time, message string) models.Log {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return models.Log{ID: id, Time: at, Message: message}
}

// october is a time on a day of October 2026, UTC.
func october(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

// withStrings is a Log with string Attributes.
func withStrings(log models.Log, attributes map[string]string) models.Log {
	log.Attributes.String = attributes
	return log
}

// lastWaitingBatch is the id of the last of a Project's Log batches waiting
// in Pebble.
func (in *install) lastWaitingBatch(projectID uint32) string {
	in.t.Helper()
	batches, err := in.batches.LogBatchesAfter(projectID, uuid.Nil, 1_000_000)
	if err != nil || len(batches) == 0 {
		in.t.Fatalf("Expected Log batches of Project %d waiting, got %d (%v)", projectID, len(batches), err)
	}
	return batches[len(batches)-1].ID.String()
}

// prepared returns the rows of a prepared file, in the order written.
func (in *install) prepared(name, columns string) []string {
	in.t.Helper()
	return query(in.t, "SELECT "+columns+" FROM read_parquet(?)", in.preparedFile(name))
}

func assertRows(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("Expected %s:\n%s\ngot:\n%s", what, strings.Join(want, "\n"), strings.Join(got, "\n"))
	}
}

func TestPrepareFilesTheLogsStillInDuckDBAndThoseWaitingInPebble(t *testing.T) {
	in := newInstall(t)
	in.post(1, october(5, 10, 20), logAt(october(5, 10, 20), "hot"))
	in.run(october(5, 10, 21), func(r *running) { r.process() })
	in.post(1, october(5, 10, 25), logAt(october(5, 10, 25), "waiting"))

	in.mustPrepare(october(5, 10, 30), "v1.0.0")

	in.assertEveryLogFiled()
}

// Hot Logs before and after the export's cutoffs, late Logs, Logs of an hour
// whose file is written, and a busy Project's quarter-hours all end up in
// files.
func TestPrepareFilesEveryLogWhateverTheCutoffs(t *testing.T) {
	in := newInstall(t)
	const quiet, busy = 1, 2
	in.post(quiet, october(5, 10, 3), logAt(october(5, 10, 2), "quiet, hot"))
	in.post(busy, october(5, 10, 6), logAt(october(5, 10, 5), "busy, first quarter"), logAt(october(5, 10, 10), "busy, first quarter"))
	in.run(october(5, 10, 21), func(r *running) {
		r.store.UseBusyProjectLogs(2)
		r.process()
		r.clock.Set(october(5, 10, 30))
		r.export() // the busy Project's 10:00 quarter-hour

		in.post(quiet, october(5, 10, 31), logAt(october(5, 8, 30), "quiet, late"))
		in.post(busy, october(5, 10, 31), logAt(october(5, 10, 12), "busy, late to its quarter-hour's file"), logAt(october(5, 10, 20), "busy, hot"))
		r.clock.Set(october(5, 10, 32))
		r.process()
	})
	in.post(quiet, october(5, 10, 40), logAt(october(5, 10, 40), "quiet, waiting"))
	in.post(busy, october(5, 10, 40), logAt(october(5, 10, 40), "busy, waiting"))

	in.mustPrepare(october(5, 10, 45), "v1.0.0")

	in.assertEveryLogFiled()
}

// snapshot is every file under the install's prepared folder, Log files and
// bucket, each with its size, time and a digest of its bytes.
func (in *install) snapshot() map[string]string {
	in.t.Helper()
	files := map[string]string{}
	for _, root := range []string{in.preparedDir(), in.logFilesDir(), filepath.Join(in.dir, "bucket")} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			files[path] = fmt.Sprintf("%d bytes at %v, %x", info.Size(), info.ModTime(), sha256.Sum256(data))
			return nil
		})
		if err != nil {
			in.t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return files
}

func TestPrepareWritesTheMarkerLastAndASecondRunDoesNothing(t *testing.T) {
	in := newInstall(t)
	in.post(1, october(5, 10, 20), logAt(october(5, 10, 20), "hot"))

	if already, err := in.prepare(october(5, 10, 30), "v1.0.0"); err != nil || already {
		t.Fatalf("Expected the first run to prepare the install, got already prepared %v, %v", already, err)
	}
	marker, err := storage.ReadClickHouseMarker(in.preparedDir())
	if err != nil {
		t.Fatal(err)
	}
	if want := (storage.ClickHouseMarker{Release: "v1.0.0", PreparedAt: october(5, 10, 30)}); marker == nil || *marker != want {
		t.Fatalf("Expected the marker %+v, got %+v", want, marker)
	}

	before := in.snapshot()
	// Only a DuckDB release started again could store this.
	in.post(1, october(5, 10, 40), logAt(october(5, 10, 40), "after"))
	if already, err := in.prepare(october(5, 11, 0), "v1.0.1"); err != nil || !already {
		t.Fatalf("Expected the second run to find the marker, got already prepared %v, %v", already, err)
	}

	if after := in.snapshot(); !maps.Equal(after, before) {
		t.Errorf("Expected the second run to change no file, before:\n%v\nafter:\n%v", before, after)
	}
	in.lastWaitingBatch(1) // still waiting
}

// localFile is where a Log file of a Project's hour is on local disk.
func (in *install) localFile(projectID uint32, hour time.Time, name string) string {
	return filepath.Join(in.logFilesDir(), fmt.Sprintf("project=%d", projectID), "day="+hour.Format(time.DateOnly),
		fmt.Sprintf("hour=%02d", hour.Hour()), name)
}

// object is where a Log file of a Project's hour is in the fake bucket.
func (in *install) object(projectID uint32, hour time.Time, name string) string {
	return in.bucket.URL(fmt.Sprintf("logs/project=%d/day=%s/hour=%02d/%s", projectID, hour.Format(time.DateOnly), hour.Hour(), name))
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// Retention leaves a file's bytes on disk when it can't delete them: they are
// deleted before the switch, which knows nothing of the DuckDB release's
// pending deletions.
func TestPrepareFinishesTheDeletionsOfLocalFiles(t *testing.T) {
	in := newInstall(t)
	const retained, deleted = 1, 3
	hour := october(4, 12, 0)
	in.post(deleted, october(4, 12, 1), logAt(october(4, 12, 0), "past Retention"))
	in.post(retained, october(5, 10, 20), logAt(october(5, 10, 20), "kept"))
	in.run(october(4, 12, 2), func(r *running) {
		r.process()
		r.clock.Set(october(4, 14, 0))
		r.export()

		folder := filepath.Dir(in.localFile(deleted, hour, "data.parquet"))
		if err := os.Chmod(folder, 0555); err != nil {
			t.Fatal(err)
		}
		if err := r.store.DeleteLogDaysBefore(deleted, october(5, 0, 0)); err == nil {
			t.Fatal("Expected Retention to fail to delete the file's bytes")
		}
		if err := os.Chmod(folder, 0755); err != nil {
			t.Fatal(err)
		}
	})
	delete(in.posted, deleted)

	in.mustPrepare(october(5, 10, 30), "v1.0.0")

	if exists(t, in.localFile(deleted, hour, "data.parquet")) {
		t.Error("Expected the file Retention deleted to be gone")
	}
	in.assertEveryLogFiled()
}

// failingPuts is a bucket in a local folder whose uploads fail.
type failingPuts struct{ storage.ObjectStore }

func (failingPuts) Put(context.Context, string, string) error {
	return errors.New("the bucket is unavailable")
}

// With a bucket, every file is uploaded, and the objects of files Retention
// deleted are deleted.
func TestPrepareUploadsEveryFileAndDeletesTheObjectsOfDeletedFiles(t *testing.T) {
	in := newInstall(t).withBucket(1)
	const retained, deleted = 1, 3
	in.post(deleted, october(4, 12, 1), logAt(october(4, 12, 0), "past Retention"))
	in.post(retained, october(4, 12, 1), logAt(october(4, 12, 0), "uploaded and evicted"))
	in.run(october(4, 12, 2), func(r *running) {
		r.process()
		r.clock.Set(october(4, 14, 0))
		r.export()
		if err := r.store.UploadLogFiles(context.Background()); err != nil {
			t.Fatal(err)
		}
		r.clock.Set(october(5, 9, 0))
		if err := r.store.EvictLogFiles(r.clock.Now()); err != nil {
			t.Fatal(err)
		}
		if err := r.store.DeleteLogDaysBefore(deleted, october(5, 0, 0)); err != nil {
			t.Fatal(err)
		}
		in.post(retained, october(5, 9, 0), logAt(october(5, 7, 30), "exported, not uploaded"))
		r.process()
		r.clock.Set(october(5, 9, 15))
		r.export()
		in.post(retained, october(5, 9, 20), logAt(october(5, 9, 20), "hot"))
		r.process()
	})
	delete(in.posted, deleted)
	in.post(retained, october(5, 9, 25), logAt(october(5, 9, 25), "waiting"))
	if !exists(t, in.object(deleted, october(4, 12, 0), "data.parquet")) {
		t.Fatal("Expected the deleted file's object to wait for the disk limit's pass")
	}

	in.mustPrepare(october(5, 9, 30), "v1.0.0")

	if exists(t, in.object(deleted, october(4, 12, 0), "data.parquet")) {
		t.Error("Expected the deleted file's object to be gone")
	}
	rows := in.prepared("log_files.parquet", "key, remote")
	if len(rows) != 3 {
		t.Errorf("Expected three files, got %v", rows)
	}
	for _, row := range rows {
		key, remote, _ := strings.Cut(row, "|")
		if remote != "true" || !exists(t, in.bucket.URL("logs/"+key)) {
			t.Errorf("Expected %s uploaded, got remote %s", key, remote)
		}
	}
	in.assertEveryLogFiled()
}

func TestPrepareFailsAndWritesNoMarkerWhenAFileFailsToUpload(t *testing.T) {
	in := newInstall(t)
	in.bucket, in.localCacheHours = failingPuts{storage.NewDirStore(filepath.Join(in.dir, "bucket"))}, 24
	in.post(1, october(5, 10, 20), logAt(october(5, 10, 20), "hot"))

	_, err := in.prepare(october(5, 10, 30), "v1.0.0")

	if err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Errorf("Expected an error that names the bucket, got %v", err)
	}
	if marker, _ := storage.ReadClickHouseMarker(in.preparedDir()); marker != nil {
		t.Errorf("Expected no marker, got %+v", marker)
	}
}

// The promoted sets, key counts and Log processing cursors the ClickHouse
// release loads are those the stores hold once every Log batch waiting is
// stored.
func TestPrepareWritesThePromotedSetsKeyCountsAndCursors(t *testing.T) {
	in := newInstall(t)
	in.post(1, october(5, 10, 5), withStrings(logAt(october(5, 10, 5), "first"), map[string]string{"user": "ada"}))
	in.run(october(5, 10, 6), func(r *running) {
		r.process()
		// plan joins the day's set partway through the day.
		in.post(1, october(5, 10, 10), withStrings(logAt(october(5, 10, 10), "second"), map[string]string{"user": "bo", "plan": "pro"}))
		r.clock.Set(october(5, 10, 11))
		r.process()
	})
	in.post(1, october(5, 10, 25), withStrings(logAt(october(5, 10, 25), "waiting"), map[string]string{"user": "cy"}))
	in.post(2, october(5, 10, 20), logAt(october(5, 10, 20), "another Project's"))
	cursors := []string{"1|" + in.lastWaitingBatch(1), "2|" + in.lastWaitingBatch(2)}

	in.mustPrepare(october(5, 10, 30), "v1.0.0")

	assertRows(t, "the promoted sets", in.prepared("log_promoted_sets.parquet", "project_id, day, type, key, \"column\", added_at, partway"), []string{
		"1|2026-10-05T00:00:00Z|string|plan|attrs_string.plan|2026-10-05T10:11:00Z|true",
		"1|2026-10-05T00:00:00Z|string|user|attrs_string.user|2026-10-05T10:06:00Z|false",
	})
	assertRows(t, "the key counts", in.prepared("log_key_counts.parquet", "project_id, day, type, key, logs"), []string{
		"1|2026-10-05T00:00:00Z|string|plan|1",
		"1|2026-10-05T00:00:00Z|string|user|3",
	})
	assertRows(t, "the Log processing cursors", in.prepared("log_processing_cursors.parquet", "project_id, batch_id"), cursors)
}
