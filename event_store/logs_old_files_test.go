package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// startOn starts EventStore's stores as main does, on a storage directory
// that may hold the DuckDB release's logs.duckdb and prepare-clickhouse's
// prepared/, and returns why the start stopped, if it did.
func startOn(t *testing.T, dir string) error {
	t.Helper()
	pebble, err := storage.NewPebbleWriter(&config.Config{PebblePath: filepath.Join(dir, "pebble")})
	if err != nil {
		t.Fatalf("Open Pebble: %v", err)
	}
	defer pebble.Close()
	clickHouse, err := storage.OpenClickHouse(clickhousetest.Config(), nil)
	if err != nil {
		t.Fatalf("Open ClickHouse: %v", err)
	}
	defer clickHouse.Close()
	return storage.NewClickHouseLogs(clickHouse, pebble).UseOldLogFiles(storage.OldLogFiles{
		LogsDuckDB:  filepath.Join(dir, "logs.duckdb"),
		PreparedDir: filepath.Join(dir, "prepared"),
		LocalDir:    filepath.Join(dir, "logs_parquet"),
	})
}

func TestUnpreparedDuckDBDataStopsTheStart(t *testing.T) {
	logger.Init()
	clickhousetest.Empty(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs.duckdb"), []byte("the DuckDB release's Logs"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := startOn(t, dir)
	if err == nil || !strings.Contains(err.Error(), "prepare-clickhouse") {
		t.Fatalf("Expected the start to stop and name prepare-clickhouse, got %v", err)
	}
	// It stops each time, until the Logs are prepared.
	if err := startOn(t, dir); err == nil {
		t.Fatal("Expected the second start to stop too")
	}
}

func TestAStoreWithoutDuckDBDataStartsAndStartsAgain(t *testing.T) {
	logger.Init()
	clickhousetest.Empty(t)
	dir := t.TempDir()

	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected a new store to start, got %v", err)
	}
	// This release still makes an empty logs.duckdb at start (SQL mode's).
	if err := os.WriteFile(filepath.Join(dir, "logs.duckdb"), []byte("made by this release"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected it to start again, got %v", err)
	}
}

// preparedFixture is an install prepare-clickhouse prepared, written by the
// last DuckDB release's tests: its prepared/, its Log file store and its
// bucket's objects, and logs.ndjson, the Logs they hold.
const preparedFixture = "testdata/prepared_clickhouse"

// switchTime is when the fixture was prepared, its marker's prepared_at.
var switchTime = time.Date(2026, 10, 2, 0, 30, 0, 0, time.UTC)

// fixtureProjects are the fixture's Projects: 2, quiet, whose hours 07 and
// 09 of 2026-10-01 are only in the bucket, and 3, busy, whose hour 10 of
// 2026-10-01 is in quarter-hour files and a late file.
var fixtureProjects = []uint32{2, 3}

// fixtureKey is a fixture Project's key.
func fixtureKey(projectID uint32) string { return fmt.Sprintf("fixture-key-%d", projectID) }

// seedFixtureProjects adds the fixture's Projects to Console's database,
// with a Retention of 30 days and a key each.
func seedFixtureProjects(t *testing.T, env *TestEnv) {
	t.Helper()
	for _, projectID := range fixtureProjects {
		seedProject(t, env.SQLitePath, projectID, 30)
		seedProjectKey(t, env.SQLitePath, projectID, fixtureKey(projectID))
	}
}

// fixtureLog is one of logs.ndjson's Logs.
type fixtureLog struct {
	ProjectID   uint32    `json:"project_id"`
	ID          uuid.UUID `json:"id"`
	Time        time.Time `json:"time"`
	Level       string    `json:"level"`
	Message     string    `json:"message"`
	Service     string    `json:"service"`
	Environment string    `json:"environment"`
	Attributes  struct {
		String map[string]string  `json:"string"`
		Number map[string]float64 `json:"number"`
		Bool   map[string]bool    `json:"bool"`
		JSON   map[string]string  `json:"json"`
	} `json:"attributes"`
}

// fixtureLogs reads the Logs the fixture's Log files hold.
func fixtureLogs(t *testing.T) []fixtureLog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(preparedFixture, "logs.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	var logs []fixtureLog
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var log fixtureLog
		if err := json.Unmarshal([]byte(line), &log); err != nil {
			t.Fatalf("Decode %s: %v", line, err)
		}
		logs = append(logs, log)
	}
	return logs
}

// insertFixtureLogs stores the fixture's Logs in ClickHouse with their ids,
// as Log processing stores Logs that come after the switch: the answers the
// old files must give.
func insertFixtureLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	for _, log := range fixtureLogs(t) {
		stored := models.Log{
			ID: log.ID, Time: log.Time, Level: log.Level, Message: log.Message,
			Service: log.Service, Environment: log.Environment,
			Attributes: models.Attributes{String: log.Attributes.String, Number: log.Attributes.Number, Bool: log.Attributes.Bool, JSON: log.Attributes.JSON},
		}
		batch := uuid.Must(uuid.NewV7())
		insert := storage.LogInsert{After: uuid.Nil, UpTo: batch, Batches: []storage.LogBatch{{ID: batch, Logs: []models.Log{stored}}}}
		if err := env.ClickHouseLogs.InsertLogBatches(log.ProjectID, insert); err != nil {
			t.Fatalf("Insert %s: %v", log.Message, err)
		}
	}
}

// usePreparedFixture makes env an install prepare-clickhouse prepared, as
// configure: prepared/ beside logs.duckdb, the Log file store where the dev
// ClickHouse's file() reads it, and the bucket's objects in the S3
// stand-in, under a prefix of the test's own. It skips the test without
// them.
func usePreparedFixture(t *testing.T, env *TestEnv) {
	t.Helper()
	name := strings.Trim(notPlainName.ReplaceAllString(strings.ToLower(t.Name()), "_"), "_")
	inStore := "clickhouse-tests/" + clickhousetest.Config().Database + "/" + name
	local := filepath.Join(clickhousetest.UserFiles(t), filepath.FromSlash(inStore))
	bucket := *clickhousetest.S3StandIn(t)
	bucket.Prefix = inStore + "/"

	if err := os.RemoveAll(local); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(local) })
	copyDir(t, filepath.Join(preparedFixture, "logs_parquet"), local)
	copyDir(t, filepath.Join(preparedFixture, "prepared"), filepath.Join(env.dir, "prepared"))

	objects := storage.NewObjectStore(&bucket)
	ctx := context.Background()
	t.Cleanup(func() {
		stored, _ := objects.List(ctx, "")
		for _, object := range stored {
			objects.Delete(ctx, object.Key)
		}
	})
	root := filepath.Join(preparedFixture, "bucket")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		return objects.Put(ctx, filepath.ToSlash(rel), path)
	})
	if err != nil {
		t.Fatalf("Upload the fixture's bucket to the S3 stand-in: %v", err)
	}

	env.OldLogFiles = storage.OldLogFiles{
		LogsDuckDB:    filepath.Join(env.dir, "logs.duckdb"),
		PreparedDir:   filepath.Join(env.dir, "prepared"),
		LocalDir:      local,
		ClickHouseDir: inStore,
		Bucket:        &bucket,
	}
}

var notPlainName = regexp.MustCompile(`[^a-z0-9_]+`)

// copyDir copies the files under from to the same paths under to.
func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		t.Fatalf("Copy %s to %s: %v", from, to, err)
	}
}

// postAfterTheSwitch posts the Logs the fixture's Projects send after the
// switch, and processes them: late Logs of old hours, one of the switch's
// hour before the switch, and Logs after it.
func postAfterTheSwitch(t *testing.T, env *TestEnv) {
	t.Helper()
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC) }
	postAndProcessTo(t, env, 3, fixtureKey(3),
		logJSON(t, "order refunded", at(1, 10, 30), map[string]any{"service": "checkout", "level": "warn", "user": "ada", "plan": "pro", "duration_ms": 40}),
		logJSON(t, "order placed", at(2, 0, 29), map[string]any{"service": "checkout", "level": "info", "user": "gus", "cached": true}),
		logJSON(t, "order placed", at(2, 0, 45), map[string]any{"service": "checkout", "level": "info", "user": "hal", "plan": "pro", "duration_ms": 9}),
		logJSON(t, "order paid", at(2, 1, 10), map[string]any{"service": "checkout", "level": "error", "user": "hal", "cached": false}),
	)
	postAndProcessTo(t, env, 2, fixtureKey(2),
		logJSON(t, "GET /health", at(1, 9, 55), map[string]any{"service": "api", "path": "/health"}),
		logJSON(t, "GET /", at(2, 0, 40), map[string]any{"service": "api", "path": "/"}),
	)
}

// oldAndNewAnswers runs answers on an install whose Logs from before the
// switch are the fixture's old files, and on one where the same Logs were
// stored in ClickHouse, each with the same Logs posted after the switch,
// and returns both: the second is what the first must answer.
func oldAndNewAnswers(t *testing.T, answers func(env *TestEnv) map[string]string) (got, want map[string]string) {
	t.Helper()
	inClickHouse := newTestEnv(t, func(env *TestEnv) { seedFixtureProjects(t, env) })
	insertFixtureLogs(t, inClickHouse)
	postAfterTheSwitch(t, inClickHouse)
	want = answers(inClickHouse)
	inClickHouse.Cleanup()

	prepared := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
	})
	defer prepared.Cleanup()
	postAfterTheSwitch(t, prepared)
	return answers(prepared), want
}

// assertSameAnswers checks each answer against the one wanted.
func assertSameAnswers(t *testing.T, got, want map[string]string) {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(want)) {
		if got[name] != want[name] {
			t.Errorf("%s\n want %s\n got  %s", name, want[name], got[name])
		}
	}
}

// pagesOf runs a Query for a Project over [from, to) in pages of size Logs,
// following each next cursor, and returns every Log listed, without ids:
// those of the Logs posted after the switch differ between installs.
func pagesOf(t *testing.T, env *TestEnv, projectID uint32, query string, from, to time.Time, size int) []listedLog {
	t.Helper()
	req := queryRequest{Query: query, From: from, To: to, Limit: size}
	var logs []listedLog
	for range 100 {
		page := runQuery(t, env, projectID, req)
		for _, log := range page.Logs {
			log.ID = ""
			logs = append(logs, log)
		}
		if page.NextCursor == nil {
			return logs
		}
		req.Cursor = *page.NextCursor
	}
	t.Fatalf("Query %q: more than 100 pages", query)
	return nil
}

// pageQueries are Queries of Log rows over the fixture's Projects.
var pageQueries = []string{
	``,
	`where user == "ada"`,
	`where plan == "pro"`,
	`where plan != "pro"`,
	`where duration_ms > 10`,
	`where cached == true`,
	`where cached == false`,
	`where tags contains "a"`,
	`where tostring(duration_ms) == "12"`,
	`search "paid"`,
	`search "ev"`,
	`where message contains "order" and level == "info"`,
	`where path == "/health"`,
	`where service == "api"`,
	`where isnotnull(plan)`,
}

// fixtureRanges are time ranges over the old hours, the switch and after.
var fixtureRanges = []struct {
	name     string
	from, to time.Time
}{
	{"all time", allTime.from, allTime.to},
	{"2026-10-01 10:00 to 10:15", time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 10, 15, 0, 0, time.UTC)},
	{"2026-10-01 10:05 to 10:20", time.Date(2026, 10, 1, 10, 5, 0, 0, time.UTC), time.Date(2026, 10, 1, 10, 20, 0, 0, time.UTC)},
	{"the switch's hour", time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)},
	{"from the switch", switchTime, allTime.to},
	{"up to the switch", allTime.from, switchTime},
}

// tableOf runs a Query that returns a table for a Project over [from, to),
// and returns its columns and rows as JSON.
func tableOf(t *testing.T, env *TestEnv, projectID uint32, query string, from, to time.Time) string {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/logs/query", projectID), queryRequest{Query: query, From: from, To: to})
	var answer tableAnswer
	if status != 200 || json.Unmarshal(raw, &answer) != nil || answer.Table == nil {
		t.Fatalf("Query %q expected 200 with a table, got %d: %s", query, status, raw)
	}
	table, _ := json.Marshal(answer.Table)
	return string(table)
}

// tableQueries are Queries of tables over the fixture's Projects.
var tableQueries = []string{
	`summarize count()`,
	`summarize count() by user`,
	`summarize count() by bin(time, 1h)`,
	`summarize count() by bin(time, 15m), level`,
	`summarize avg(duration_ms), max(duration_ms), count() by plan`,
	`where plan == "pro" | summarize count() by user`,
	`summarize dcount(user), dcount(plan)`,
	`summarize count() by cached`,
	`search "order" | summarize count() by service`,
	`where isnotnull(tags) | summarize count() by tostring(tags)`,
	`project message, user, plan, duration_ms, cached, tags`,
	`extend who = toupper(user), n = strlen(plan) | summarize count() by who, n`,
	`summarize count() by path`,
	`where path != "/" | top 2 by time`,
}

func TestTablesOverOldHoursAndTheSwitchAnswerAsClickHouseWould(t *testing.T) {
	got, want := oldAndNewAnswers(t, func(env *TestEnv) map[string]string {
		answers := map[string]string{}
		for _, projectID := range fixtureProjects {
			for _, r := range fixtureRanges {
				for _, query := range tableQueries {
					// Twice: the second run takes the bins of ended hours
					// from the bin cache.
					for _, run := range []string{"first", "again"} {
						answers[fmt.Sprintf("Project %d, %s, %s run: %s", projectID, r.name, run, query)] = tableOf(t, env, projectID, query, r.from, r.to)
					}
				}
			}
		}
		return answers
	})
	assertSameAnswers(t, got, want)
	if counted := want["Project 3, all time, first run: summarize count()"]; !strings.Contains(counted, "[11]") {
		t.Errorf("Expected Project 3's 7 old Logs and 4 new ones counted, got %s", counted)
	}
}

// histogramOf is a Project's histogram, as JSON with only its buckets that
// count any Logs.
func histogramOf(t *testing.T, env *TestEnv, projectID uint32, req histogramRequest) string {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/logs/histogram", projectID), req)
	var answer histogramAnswer
	if status != 200 || json.Unmarshal(raw, &answer) != nil {
		t.Fatalf("Histogram of %q expected 200, got %d: %s", req.Query, status, raw)
	}
	counted, _ := json.Marshal(map[string]any{"buckets": bucketsOf(answer), "count": answer.Count, "levels": answer.Levels, "bucket_seconds": answer.BucketSeconds})
	return string(counted)
}

func TestHistogramsOverOldHoursAndTheSwitchCountWhatClickHouseWould(t *testing.T) {
	day := func(d, hour, minute int) time.Time { return time.Date(2026, 10, d, hour, minute, 0, 0, time.UTC) }
	requests := []histogramRequest{
		// Every Log, by minute or more: counted by minute after the old
		// files, and from them and logs before.
		{From: day(1, 9, 0), To: day(2, 2, 0), BucketSeconds: 60},
		{From: day(1, 0, 0), To: day(3, 0, 0), BucketSeconds: 3600},
		{From: day(1, 10, 0), To: day(2, 1, 30), BucketSeconds: 900},
		{From: day(1, 10, 2), To: day(2, 0, 50)},
		{From: switchTime, To: day(2, 3, 0), BucketSeconds: 300},
		// Some Logs, or buckets of seconds: from the Logs themselves.
		{Query: `where user == "hal" or plan == "pro"`, From: day(1, 0, 0), To: day(3, 0, 0), BucketSeconds: 3600},
		{Query: `search "order"`, From: day(1, 10, 0), To: day(2, 1, 30), BucketSeconds: 600},
		{From: day(1, 10, 0), To: day(1, 10, 30), BucketSeconds: 30},
	}
	got, want := oldAndNewAnswers(t, func(env *TestEnv) map[string]string {
		answers := map[string]string{}
		for _, projectID := range fixtureProjects {
			for _, req := range requests {
				for _, run := range []string{"first", "again"} {
					name := fmt.Sprintf("Project %d, %s run: %q from %s to %s by %ds", projectID, run, req.Query, req.From.Format(time.RFC3339), req.To.Format(time.RFC3339), req.BucketSeconds)
					answers[name] = histogramOf(t, env, projectID, req)
				}
			}
		}
		return answers
	})
	assertSameAnswers(t, got, want)
}

func TestPagesOverOldHoursAndTheSwitchListTheLogsClickHouseWould(t *testing.T) {
	got, want := oldAndNewAnswers(t, func(env *TestEnv) map[string]string {
		answers := map[string]string{}
		for _, projectID := range fixtureProjects {
			for _, r := range fixtureRanges {
				for _, query := range pageQueries {
					for _, size := range []int{2, 1000} {
						logs := pagesOf(t, env, projectID, query, r.from, r.to, size)
						raw, _ := json.Marshal(logs)
						answers[fmt.Sprintf("Project %d, %s, pages of %d: %s", projectID, r.name, size, query)] = string(raw)
					}
				}
			}
		}
		return answers
	})
	assertSameAnswers(t, got, want)
	if all := want["Project 3, all time, pages of 1000: "]; !strings.Contains(all, "order refunded") || !strings.Contains(all, "plan chosen") {
		t.Errorf("Expected the fixture's Logs and the late Log of an old hour, got %s", all)
	}
}
