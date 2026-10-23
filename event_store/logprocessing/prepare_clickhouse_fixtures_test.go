package logprocessing

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
)

// The fixture set: an install prepared by prepare-clickhouse, committed for
// the ClickHouse release's tests (the ClickHouse effort's ticket 09), which
// read old Log files in place from it. Regenerate it with
//
//	go test ./logprocessing -run TestTheCommittedFixtureSetIsWhatPrepareWrites -update-fixtures
//
// It holds, under event_store/testdata/prepared_clickhouse:
//   - prepared/: the prepared files and the marker;
//   - logs_parquet/: the Log files with a local copy, as under
//     logs_parquet_path;
//   - bucket/: the fake bucket's objects, every Log file under logs/;
//   - logs.ndjson: every Log the install was given, one fixtureLog a line.
//
// Project 3 is busy on 2026-10-01: its 10:00 quarter-hour has its own file,
// data-00.parquet, a Log of it that came later is in late-1.parquet, and plan
// joined that day's promoted set partway through. Project 2 is quiet: an hour
// file each, its 07:00 and 09:00 files only in the bucket. On 2026-10-02 both
// have an hour of Logs that were hot or waiting in Pebble when the install
// was prepared, at 00:30.
var updateFixtures = flag.Bool("update-fixtures", false, "rewrite the committed prepare-clickhouse fixture set")

// fixtureDir is where the fixture set is committed.
const fixtureDir = "../testdata/prepared_clickhouse"

// fixtureRelease is the release the fixture set's marker names.
const fixtureRelease = "fixture"

// fixtureLog is a Log of the fixture set as logs.ndjson has it.
type fixtureLog struct {
	ProjectID   uint32    `json:"project_id"`
	ID          uuid.UUID `json:"id"`
	Time        time.Time `json:"time"`
	Level       string    `json:"level,omitempty"`
	Message     string    `json:"message"`
	Service     string    `json:"service,omitempty"`
	Environment string    `json:"environment,omitempty"`
	TraceID     string    `json:"trace_id,omitempty"`
	SpanID      string    `json:"span_id,omitempty"`
	Attributes  struct {
		String map[string]string  `json:"string,omitempty"`
		Number map[string]float64 `json:"number,omitempty"`
		Bool   map[string]bool    `json:"bool,omitempty"`
		JSON   map[string]string  `json:"json,omitempty"`
	} `json:"attributes"`
}

// fixtureInstall is an install that records every Log it is given.
type fixtureInstall struct {
	*install
	logs []fixtureLog
}

// fixtureID is the id of the nth Log of the fixture set: a UUIDv7 of its
// time, as Log ingest makes one, whose random bits are n.
func fixtureID(at time.Time, n int) uuid.UUID {
	var id uuid.UUID
	binary.BigEndian.PutUint64(id[0:8], uint64(at.UnixMilli())<<16|0x7000)
	binary.BigEndian.PutUint64(id[8:16], uint64(n))
	id[8] = id[8]&0x3f | 0x80
	return id
}

// give posts a Log batch of a Project's Logs, each from fields, at receivedAt.
func (in *fixtureInstall) give(projectID uint32, receivedAt time.Time, logs ...models.Log) {
	in.t.Helper()
	for i := range logs {
		logs[i].ID = fixtureID(logs[i].Time, len(in.logs)+1)
		log := fixtureLog{ProjectID: projectID, ID: logs[i].ID, Time: logs[i].Time, Level: logs[i].Level, Message: logs[i].Message,
			Service: logs[i].Service, Environment: logs[i].Environment, TraceID: logs[i].TraceID, SpanID: logs[i].SpanID}
		log.Attributes.String, log.Attributes.Number = logs[i].Attributes.String, logs[i].Attributes.Number
		log.Attributes.Bool, log.Attributes.JSON = logs[i].Attributes.Bool, logs[i].Attributes.JSON
		in.logs = append(in.logs, log)
	}
	in.post(projectID, receivedAt, logs...)
}

// fixtureLogAt is a Log of a service at a time with a message and Attributes.
func fixtureLogAt(at time.Time, service, message string, attributes models.Attributes) models.Log {
	return models.Log{Time: at, Level: "info", Message: message, Service: service, Environment: "production", Attributes: attributes}
}

// prepareFixtureSet makes the fixture set's install and prepares it.
func prepareFixtureSet(t *testing.T) *fixtureInstall {
	in := &fixtureInstall{install: newInstall(t).withBucket(1)}
	const quiet, busy = 2, 3
	day1 := func(hour, minute int) time.Time { return october(1, hour, minute) }
	day2 := func(hour, minute int) time.Time { return october(2, hour, minute) }

	in.run(day1(10, 8), func(r *running) {
		r.store.UseBusyProjectLogs(2)
		in.give(busy, day1(10, 4),
			fixtureLogAt(day1(10, 1), "checkout", "order placed", models.Attributes{
				String: map[string]string{"user": "ada"}, Number: map[string]float64{"duration_ms": 12},
				Bool: map[string]bool{"cached": true}, JSON: map[string]string{"tags": `["a","b"]`}}),
			fixtureLogAt(day1(10, 3), "checkout", "order paid", models.Attributes{
				String: map[string]string{"user": "bo"}, Number: map[string]float64{"duration_ms": 30}}))
		in.give(quiet, day1(10, 5),
			fixtureLogAt(day1(7, 30), "api", "nightly job ran late", models.Attributes{String: map[string]string{"path": "/jobs"}}),
			fixtureLogAt(day1(9, 50), "api", "GET /", models.Attributes{String: map[string]string{"path": "/"}}),
			fixtureLogAt(day1(10, 4), "api", "GET /health", models.Attributes{String: map[string]string{"path": "/health"}}))
		r.process()

		// plan joins the day's set partway through the day.
		r.clock.Set(day1(10, 12))
		in.give(busy, day1(10, 11), fixtureLogAt(day1(10, 11), "checkout", "plan chosen", models.Attributes{
			String: map[string]string{"user": "cy", "plan": "pro"}}))
		r.process()

		// The busy Project's 10:00 quarter-hour gets its own file, and the
		// quiet Project's late 07:30 Log its hour's file, which is uploaded
		// and evicted.
		r.clock.Set(day1(10, 30))
		r.export()
		r.uploadAndEvict()

		// A Log of the busy Project's 10:00 quarter-hour comes after its
		// file, so it goes to a late file.
		r.clock.Set(day1(10, 31))
		in.give(busy, day1(10, 31),
			fixtureLogAt(day1(10, 5), "checkout", "order shipped", models.Attributes{String: map[string]string{"user": "dee"}}),
			fixtureLogAt(day1(10, 20), "checkout", "plan changed", models.Attributes{String: map[string]string{"user": "eve", "plan": "free"}}))
		r.process()

		r.clock.Set(day1(11, 16))
		r.export()
		r.uploadAndEvict()

		// A new day's set starts from the day before's.
		r.clock.Set(day2(0, 20))
		in.give(busy, day2(0, 11), fixtureLogAt(day2(0, 10), "checkout", "order placed", models.Attributes{
			String: map[string]string{"user": "fay", "plan": "pro"}, Number: map[string]float64{"duration_ms": 5}}))
		in.give(quiet, day2(0, 16), fixtureLogAt(day2(0, 15), "api", "GET /health", models.Attributes{String: map[string]string{"path": "/health"}}))
		r.process()
	})
	// Waiting in Pebble when the stack stops.
	in.give(quiet, day2(0, 25), fixtureLogAt(day2(0, 25), "api", "GET /", models.Attributes{String: map[string]string{"path": "/"}}))
	in.give(busy, day2(0, 26), fixtureLogAt(day2(0, 26), "checkout", "order paid", models.Attributes{
		String: map[string]string{"user": "fay"}, Bool: map[string]bool{"cached": false}}))

	in.mustPrepare(day2(0, 30), fixtureRelease)
	in.assertEveryLogFiled()
	return in
}

// uploadAndEvict uploads the Log files, then evicts the local copies kept
// long enough, as the uploads worker does.
func (r *running) uploadAndEvict() {
	r.in.t.Helper()
	if err := r.store.UploadLogFiles(context.Background()); err != nil {
		r.in.t.Fatalf("UploadLogFiles: %v", err)
	}
	if err := r.store.EvictLogFiles(r.clock.Now()); err != nil {
		r.in.t.Fatalf("EvictLogFiles: %v", err)
	}
}

// writeFixtureSet replaces the committed fixture set with the install's.
func (in *fixtureInstall) writeFixtureSet() {
	in.t.Helper()
	if err := os.RemoveAll(fixtureDir); err != nil {
		in.t.Fatal(err)
	}
	for _, folder := range []struct{ from, to string }{
		{in.preparedDir(), "prepared"},
		{in.logFilesDir(), "logs_parquet"},
		{filepath.Join(in.dir, "bucket"), "bucket"},
	} {
		if err := os.CopyFS(filepath.Join(fixtureDir, folder.to), os.DirFS(folder.from)); err != nil {
			in.t.Fatalf("copy %s: %v", folder.from, err)
		}
	}
	file, err := os.Create(filepath.Join(fixtureDir, "logs.ndjson"))
	if err != nil {
		in.t.Fatal(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, log := range in.logs {
		if err := encoder.Encode(log); err != nil {
			in.t.Fatal(err)
		}
	}
}

// contract is what of a prepared folder the ClickHouse release relies on and
// a run of prepare-clickhouse decides: each prepared file's columns and
// types, the rows that don't hold Log batch ids or sizes, and the marker.
func contract(t *testing.T, dir string) map[string][]string {
	t.Helper()
	described := func(name string) []string {
		return query(t, "SELECT column_name, column_type FROM (DESCRIBE SELECT * FROM read_parquet(?))", filepath.Join(dir, name))
	}
	rows := func(name, columns string) []string {
		return query(t, "SELECT "+columns+" FROM read_parquet(?)", filepath.Join(dir, name))
	}
	marker, err := storage.ReadClickHouseMarker(dir)
	if err != nil || marker == nil {
		t.Fatalf("Expected a marker in %s, got %v, %v", dir, marker, err)
	}
	return map[string][]string{
		"log_files columns":              described("log_files.parquet"),
		"log_promoted_sets columns":      described("log_promoted_sets.parquet"),
		"log_key_counts columns":         described("log_key_counts.parquet"),
		"log_processing_cursors columns": described("log_processing_cursors.parquet"),
		"log_files": rows("log_files.parquet",
			"project_id, day, hour, name, key, min_time, max_time, log_count, batch_max IS NULL, local, remote"),
		"log_promoted_sets":      rows("log_promoted_sets.parquet", "*"),
		"log_key_counts":         rows("log_key_counts.parquet", "*"),
		"log_processing_cursors": rows("log_processing_cursors.parquet", "project_id"),
		"marker":                 {marker.Release, marker.PreparedAt.String()},
	}
}

// The committed fixture set is the one prepare-clickhouse writes now, so the
// ClickHouse release's tests read the prepared files as the last DuckDB
// release writes them.
func TestTheCommittedFixtureSetIsWhatPrepareWrites(t *testing.T) {
	in := prepareFixtureSet(t)
	if *updateFixtures {
		in.writeFixtureSet()
	}

	want := contract(t, in.preparedDir())
	got := contract(t, filepath.Join(fixtureDir, "prepared"))
	for what := range want {
		assertRows(t, "the committed fixture set's "+what+" (regenerate it with -update-fixtures)", got[what], want[what])
	}
	// Every Log file the committed manifest lists is committed too.
	for _, row := range query(t, "SELECT key, local, remote FROM read_parquet(?)", filepath.Join(fixtureDir, "prepared", "log_files.parquet")) {
		parts := strings.Split(row, "|")
		key, local, remote := parts[0], parts[1], parts[2]
		if local == "true" && !exists(t, filepath.Join(fixtureDir, "logs_parquet", filepath.FromSlash(key))) {
			t.Errorf("Expected the local copy of %s committed", key)
		}
		if remote == "true" && !exists(t, filepath.Join(fixtureDir, "bucket", "logs", filepath.FromSlash(key))) {
			t.Errorf("Expected the object of %s committed", key)
		}
	}
}
