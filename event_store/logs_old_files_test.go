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
	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected it to start again, got %v", err)
	}
}

func TestDuckDBDataThatAppearsAfterAStartWithoutAnyStopsTheNextStart(t *testing.T) {
	logger.Init()
	clickhousetest.Empty(t)
	dir := t.TempDir()
	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected a new store to start, got %v", err)
	}

	// The DuckDB release ran on the store since, as after a rollback.
	if err := os.WriteFile(filepath.Join(dir, "logs.duckdb"), []byte("the DuckDB release's Logs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := startOn(t, dir); err == nil || !strings.Contains(err.Error(), "prepare-clickhouse") {
		t.Fatalf("Expected the start to stop and name prepare-clickhouse, got %v", err)
	}
}

// writeMarker writes a prepared folder's marker, as prepare-clickhouse
// does at the end of a run.
func writeMarker(t *testing.T, dir string, preparedAt time.Time) {
	t.Helper()
	marker := fmt.Sprintf(`{"release":"v0.9.1","prepared_at":%q}`, preparedAt.Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, "marker.json"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDuckDBDataStartsOncePreparedAndStopsTheStartAgainAfterARollback(t *testing.T) {
	logger.Init()
	clickhousetest.Empty(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs.duckdb"), []byte("the DuckDB release's Logs"), 0o644); err != nil {
		t.Fatal(err)
	}
	copyDir(t, filepath.Join(preparedFixture, "prepared"), filepath.Join(dir, "prepared"))
	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected a prepared store to start, got %v", err)
	}
	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected it to start again, got %v", err)
	}

	// The DuckDB release starts again on it, which removes the marker.
	if err := os.Remove(filepath.Join(dir, "prepared", "marker.json")); err != nil {
		t.Fatal(err)
	}
	if err := startOn(t, dir); err == nil || !strings.Contains(err.Error(), "prepare-clickhouse") {
		t.Fatalf("Expected the start after a rollback to stop and name prepare-clickhouse, got %v", err)
	}
	// Prepared again, it starts.
	writeMarker(t, filepath.Join(dir, "prepared"), switchTime.Add(time.Hour))
	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected the store prepared again to start, got %v", err)
	}
}

func TestEachMarkerIsLoadedOnceReplacingTheLastOnesFiles(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
	})
	defer env.Cleanup()
	first, second := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	firstDay := []attributeCount{{"user", "string", 4}, {"duration_ms", "number", 2}, {"plan", "string", 2}, {"cached", "bool", 1}}
	secondDay := []attributeCount{{"user", "string", 2}, {"cached", "bool", 1}, {"duration_ms", "number", 1}, {"plan", "string", 1}}

	// Retention deletes the 1st's files and their rows.
	env.Clock.Set(time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC))
	env.DeleteLogsPastRetention(t)
	assertAttributes(t, getFields(t, env, 3, first, second, ""), nil, "the 1st, past Retention")

	// The marker loaded isn't loaded again.
	env.Reopen(t)
	assertAttributes(t, getFields(t, env, 3, first, second, ""), nil, "the 1st, after a restart")

	// A marker of another run, after a rollback, is loaded in place of the
	// last one's files: what it prepared is read, once.
	writeMarker(t, env.OldLogFiles.PreparedDir, switchTime.Add(time.Hour))
	env.Reopen(t)
	assertAttributes(t, getFields(t, env, 3, first, second, ""), firstDay, "the 1st, prepared again")
	assertAttributes(t, getFields(t, env, 3, second, second.Add(time.Hour), ""), secondDay, "the 2nd, prepared again")
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

func TestAnOldLogIsFoundByItsIDAsClickHouseWouldFindIt(t *testing.T) {
	logs := fixtureLogs(t)
	got, want := oldAndNewAnswers(t, func(env *TestEnv) map[string]string {
		answers := map[string]string{}
		get := func(name, path string) {
			status, raw := getQuery(t, env, path)
			answers[name] = fmt.Sprintf("%d %s", status, raw)
		}
		for _, log := range logs {
			path := fmt.Sprintf("/api/%d/logs/%s", log.ProjectID, log.ID)
			name := fmt.Sprintf("%q (%s)", log.Message, log.Time.Format(time.RFC3339))
			get(name+" by id", path)
			get(name+" by id and time", path+"?time="+log.Time.Format(time.RFC3339Nano))
			get(name+" by id and an hour before", path+"?time="+log.Time.Add(-time.Hour).Format(time.RFC3339Nano))
			get(name+" by id in another Project", fmt.Sprintf("/api/%d/logs/%s", 5-log.ProjectID, log.ID))
		}
		return answers
	})
	assertSameAnswers(t, got, want)
	name := fmt.Sprintf("%q (%s) by id", logs[0].Message, logs[0].Time.Format(time.RFC3339))
	if found := want[name]; !strings.HasPrefix(found, "200 ") || !strings.Contains(found, logs[0].ID.String()) {
		t.Errorf("Expected the Log found by its id, got %s", found)
	}
}

func TestFieldsOfOldDaysCountWhatTheDuckDBReleaseCounted(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
	})
	defer env.Cleanup()
	first, second := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)

	// The DuckDB release never counted a late Log, such as the 10:05 one.
	assertAttributes(t, getFields(t, env, 3, first, second, ""), []attributeCount{
		{"user", "string", 4}, {"duration_ms", "number", 2}, {"plan", "string", 2}, {"cached", "bool", 1},
	}, "Project 3's first day")
	assertAttributes(t, getFields(t, env, 2, first, second.Add(time.Hour), ""), []attributeCount{
		{"path", "string", 4},
	}, "Project 2's two days")

	// Logs inserted since the switch count with them, late ones too.
	postAfterTheSwitch(t, env)
	assertAttributes(t, getFields(t, env, 3, first, second, ""), []attributeCount{
		{"user", "string", 5}, {"duration_ms", "number", 3}, {"plan", "string", 3}, {"cached", "bool", 1},
	}, "Project 3's first day, with a late Log")
	assertAttributes(t, getFields(t, env, 3, second, second.Add(24*time.Hour), ""), []attributeCount{
		{"user", "string", 5}, {"cached", "bool", 3}, {"duration_ms", "number", 2}, {"plan", "string", 2},
	}, "Project 3's second day")
}

// oldFilesLeft lists the old Log files left in env's Log file store and in
// its bucket, as "local <key>" and "bucket <key>".
func oldFilesLeft(t *testing.T, env *TestEnv) []string {
	t.Helper()
	var left []string
	root := env.OldLogFiles.LocalDir
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		left = append(left, "local "+filepath.ToSlash(rel))
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if env.OldLogFiles.Bucket == nil {
		slices.Sort(left)
		return left
	}
	objects, err :=storage.NewObjectStore(env.OldLogFiles.Bucket).List(context.Background(), "logs/")
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		left = append(left, "bucket "+strings.TrimPrefix(object.Key, "logs/"))
	}
	slices.Sort(left)
	return left
}

// hourCounts is a Chart's table of a Project's Logs by hour over the
// fixture's days.
func hourCounts(t *testing.T, env *TestEnv, projectID uint32) string {
	t.Helper()
	return tableOf(t, env, projectID, `summarize count() by bin(time, 1h)`,
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
}

func TestRetentionDeletesOldLogFilesPastIt(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
	})
	defer env.Cleanup()
	postAfterTheSwitch(t, env)

	// The Chart's bins of old hours are kept, so the second run reads
	// none.
	env.Clock.Set(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	before := hourCounts(t, env, 3)
	hits, _ := env.ClickHouseLogs.BinCacheStats()
	if again := hourCounts(t, env, 3); again != before {
		t.Fatalf("Expected the same table again, got %s, then %s", before, again)
	}
	if hitsAgain, _ := env.ClickHouseLogs.BinCacheStats(); hitsAgain-hits != 48 {
		t.Fatalf("Expected the second run to take all 48 hours from the bin cache, got %d", hitsAgain-hits)
	}
	// Retention passing with nothing past it deletes nothing.
	env.DeleteLogsPastRetention(t)
	allFiles := oldFilesLeft(t, env)
	if len(allFiles) != 14 {
		t.Fatalf("Expected the fixture's 6 local copies and 8 objects, got %v", allFiles)
	}

	// Half an hour past midnight on 1 November, 30 days back is 2 October:
	// the 1st goes.
	env.Clock.Set(time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC))
	env.DeleteLogsPastRetention(t)

	assertDeleted := func(when string) {
		t.Helper()
		assertProjectQueries(t, env, 3, map[string][]string{
			``: {"order paid", "order placed", "order placed", "order paid", "order placed"},
		}, when)
		assertProjectQueries(t, env, 2, map[string][]string{
			``: {"GET /", "GET /", "GET /health"},
		}, when)
		first := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		assertAttributes(t, getFields(t, env, 3, first, first.Add(24*time.Hour), ""), nil, when+": Project 3's Fields of the 1st")
		if after := hourCounts(t, env, 3); after == before || strings.Contains(after, "2026-10-01") {
			t.Errorf("%s: expected the Chart without the 1st's hours, got %s", when, after)
		}
		want := []string{
			"bucket project=2/day=2026-10-02/hour=00/data.parquet",
			"bucket project=3/day=2026-10-02/hour=00/data.parquet",
			"local project=2/day=2026-10-02/hour=00/data.parquet",
			"local project=3/day=2026-10-02/hour=00/data.parquet",
		}
		if left := oldFilesLeft(t, env); !slices.Equal(left, want) {
			t.Errorf("%s: expected the 2nd's files left, got %v", when, left)
		}
	}
	assertDeleted("after Retention")
	env.Reopen(t)
	assertDeleted("after a restart")
	env.DeleteLogsPastRetention(t)
	assertDeleted("after another pass")
}

func TestADeletedProjectsOldLogFilesAreDeleted(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
	})
	defer env.Cleanup()
	project3 := runQueryPages(t, env, 3, queryRequest{From: allTime.from, To: allTime.to, Limit: 100})

	consoleExec(t, env, "DELETE FROM projects WHERE id = 2")
	env.DeleteLogsPastRetention(t)

	assertDeleted := func(when string) {
		t.Helper()
		if got := latestLogs(t, env, 2, 10); len(got) != 0 {
			t.Errorf("%s: expected the deleted Project to have no Logs, got %v", when, messages(got))
		}
		day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		assertAttributes(t, getFields(t, env, 2, day, day.Add(48*time.Hour), ""), nil, when+": the deleted Project's Fields")
		for _, file := range oldFilesLeft(t, env) {
			if strings.Contains(file, "project=2/") {
				t.Errorf("%s: expected none of the deleted Project's files left, got %s", when, file)
			}
		}
		assertListsUnchanged(t, env, 3, project3, when)
	}
	assertDeleted("after the pass")
	env.Reopen(t)
	assertDeleted("after a restart")
}

// ingestedAfter runs a Query of a Project's Logs ingested after a Log
// batch, as a Match monitor does.
func ingestedAfter(t *testing.T, env *TestEnv, projectID uint32, query, afterBatch string, limit int) ingestedPage {
	t.Helper()
	answer := answerOf(t, env, fmt.Sprintf("/api/%d/logs/query", projectID),
		map[string]any{"query": query, "after_batch": afterBatch, "limit": limit}, 200)
	var page ingestedPage
	if err := json.Unmarshal(mustMarshal(t, answer), &page); err != nil {
		t.Fatalf("Decode %s: %v", mustMarshal(t, answer), err)
	}
	return page
}

func TestAMatchMonitorReadsTheLogsStoredBeforeTheSwitchSinceItsLastRun(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
	})
	defer env.Cleanup()
	// The last Log batch the DuckDB release stored for Project 3.
	const preparedCursor = "01a0fa00-edc0-705e-aa45-8bd4afaf37fd"

	// The Monitor last ran before the switch, after the batch of the 1st's
	// first two Logs: it reads the Logs of the batches stored since.
	page := ingestedAfter(t, env, 3, ``, "01a0f6eb-be80-779f-a5aa-16f24fd08419", 100)
	want := []string{"order paid", "order placed", "plan changed", "plan chosen", "order shipped"}
	if got := messages(page.Logs); !slices.Equal(got, want) || page.Total != 5 || page.IngestCursor != preparedCursor {
		t.Errorf("Expected %v of 5 up to %s, got %v of %d up to %s", want, preparedCursor, got, page.Total, page.IngestCursor)
	}
	page = ingestedAfter(t, env, 3, `where plan == "free" or user == "fay"`, "01a0f6eb-be80-779f-a5aa-16f24fd08419", 2)
	if got := messages(page.Logs); !slices.Equal(got, []string{"order paid", "order placed"}) || page.Total != 3 {
		t.Errorf("Expected the newest 2 of 3 matching Logs, got %v of %d", got, page.Total)
	}

	// Its next run reads the Logs stored after the switch only.
	postAfterTheSwitch(t, env)
	page = ingestedAfter(t, env, 3, ``, preparedCursor, 100)
	if got := messages(page.Logs); !slices.Equal(got, []string{"order paid", "order placed", "order placed", "order refunded"}) {
		t.Errorf("Expected the 4 Logs stored after the switch, got %v", got)
	}
}

func TestAKeyPromotedAfterAFileWasWrittenReadsAsMissingInIt(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
	})
	defer env.Cleanup()
	// The fixture's files all have their day's columns. As if region had
	// been promoted on the 1st after every file was written, the day's set
	// gets a column no file has.
	if err := env.ClickHouse.Conn().Exec(context.Background(),
		"INSERT INTO old_log_promoted_sets (project_id, day, type, key, column_name, partway) VALUES (3, '2026-10-01', 'string', 'region', 'attrs_string.region', false)"); err != nil {
		t.Fatal(err)
	}
	env.Reopen(t)

	assertProjectQueries(t, env, 3, map[string][]string{
		`where region == "eu"`:    {},
		`where isnotnull(region)`: {},
		`where isnull(region)`:    {"order paid", "order placed", "plan changed", "plan chosen", "order shipped", "order paid", "order placed"},
	}, "with region promoted")
	if table := tableOf(t, env, 3, `summarize count() by region`, allTime.from, allTime.to); !strings.Contains(table, `"rows":[[null,7]]`) {
		t.Errorf("Expected all 7 Logs counted without a region, got %s", table)
	}
}

func TestAnOldFileWhoseLocalCopyIsGoneIsReadFromTheBucket(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
		// As an operator might have deleted it, the local copy of Project
		// 3's first hour of the 2nd is gone; its object is in the bucket.
		if err := os.Remove(filepath.Join(env.OldLogFiles.LocalDir, "project=3", "day=2026-10-02", "hour=00", "data.parquet")); err != nil {
			t.Fatal(err)
		}
	})
	defer env.Cleanup()

	assertProjectQueries(t, env, 3, map[string][]string{
		``:                    {"order paid", "order placed", "plan changed", "plan chosen", "order shipped", "order paid", "order placed"},
		`where user == "fay"`: {"order paid", "order placed"},
	}, "with a local copy gone")
	if table := tableOf(t, env, 3, `summarize count()`, allTime.from, allTime.to); !strings.Contains(table, `"rows":[[7]]`) {
		t.Errorf("Expected all 7 old Logs counted, got %s", table)
	}
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

// timedLogs lists a Project's Logs, newest first, each as its time and
// message.
func timedLogs(t *testing.T, env *TestEnv, projectID uint32) []string {
	t.Helper()
	var listed []string
	for _, log := range latestLogs(t, env, projectID, 100) {
		listed = append(listed, log.Time.Format("2006-01-02 15:04 ")+log.Message)
	}
	return listed
}

// fixtureLocalBytes is how much disk the fixture's local copies take.
func fixtureLocalBytes(t *testing.T) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(filepath.Join(preparedFixture, "logs_parquet"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err == nil {
			total += info.Size()
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func TestTheDiskLimitDeletesTheLocalCopiesOfOldFilesTheBucketHasFirst(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
	})
	defer env.Cleanup()
	postAfterTheSwitch(t, env)
	listed := map[uint32][]string{}
	for _, projectID := range fixtureProjects {
		listed[projectID] = timedLogs(t, env, projectID)
	}
	assertListed := func(when string) {
		t.Helper()
		for _, projectID := range fixtureProjects {
			if got := timedLogs(t, env, projectID); !slices.Equal(got, listed[projectID]) {
				t.Errorf("%s: expected Project %d's Logs unchanged\n want %v\n got  %v", when, projectID, listed[projectID], got)
			}
		}
	}
	allFiles := oldFilesLeft(t, env)
	_, total := localDiskUse(t, env)

	// The local copy of the oldest day's file goes, Project 2's of the 1st:
	// it is read from the bucket.
	enforceLogDiskLimit(t, env, total-1)
	assertListed("past the limit by a byte")
	want := slices.DeleteFunc(slices.Clone(allFiles), func(file string) bool {
		return file == "local project=2/day=2026-10-01/hour=10/data.parquet"
	})
	if left := oldFilesLeft(t, env); !slices.Equal(left, want) {
		t.Errorf("Expected only Project 2's local copy of the 1st gone\n want %v\n got  %v", want, left)
	}

	// With no room for any, every local copy goes, and no Log.
	enforceLogDiskLimit(t, env, total-fixtureLocalBytes(t))
	assertListed("with no room for the local copies")
	want = slices.DeleteFunc(slices.Clone(allFiles), func(file string) bool { return strings.HasPrefix(file, "local ") })
	if left := oldFilesLeft(t, env); !slices.Equal(left, want) {
		t.Errorf("Expected the bucket's objects alone left\n want %v\n got  %v", want, left)
	}
	env.Reopen(t)
	assertListed("after a restart")
}

func TestWithoutABucketTheDiskLimitDeletesTheOldestOldDaysBeforeTheOldestPartitions(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) {
		seedFixtureProjects(t, env)
		usePreparedFixture(t, env)
		env.OldLogFiles.Bucket = nil
	})
	defer env.Cleanup()
	postAfterTheSwitch(t, env)
	project2, project3 := timedLogs(t, env, 2), timedLogs(t, env, 3)
	_, total := localDiskUse(t, env)

	// The oldest day of old files goes, Project 2's 1st, and not the
	// partition of its Log of the 1st stored since the switch.
	enforceLogDiskLimit(t, env, total-1)
	want := slices.DeleteFunc(slices.Clone(project2), func(log string) bool { return log == "2026-10-01 10:04 GET /health" })
	if got := timedLogs(t, env, 2); len(want) != len(project2)-1 || !slices.Equal(got, want) {
		t.Errorf("Expected Project 2's old Log of the 1st gone\n want %v\n got  %v", want, got)
	}
	if got := timedLogs(t, env, 3); !slices.Equal(got, project3) {
		t.Errorf("Expected Project 3's Logs unchanged\n want %v\n got  %v", project3, got)
	}

	// With no room for old files, they all go, and then the oldest
	// partition.
	enforceLogDiskLimit(t, env, total-fixtureLocalBytes(t)-1)
	assertGone := func(when string) {
		t.Helper()
		if got, want := timedLogs(t, env, 2), []string{"2026-10-02 00:40 GET /"}; !slices.Equal(got, want) {
			t.Errorf("%s: expected Project 2's Log of the 2nd since the switch alone\n want %v\n got  %v", when, want, got)
		}
		want := []string{"2026-10-02 01:10 order paid", "2026-10-02 00:45 order placed", "2026-10-02 00:29 order placed", "2026-10-01 10:30 order refunded"}
		if got := timedLogs(t, env, 3); !slices.Equal(got, want) {
			t.Errorf("%s: expected Project 3's Logs since the switch alone\n want %v\n got  %v", when, want, got)
		}
		for _, file := range oldFilesLeft(t, env) {
			if strings.HasPrefix(file, "local ") {
				t.Errorf("%s: expected no local copy left, got %s", when, file)
			}
		}
	}
	assertGone("with no room for old files")
	env.Reopen(t)
	assertGone("after a restart")
}
