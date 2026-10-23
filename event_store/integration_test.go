package main_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/gofiber/fiber/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/logtransform"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/processing"
	"github.com/solidtrace/event_store/storage"
)

type TestEnv struct {
	App          *fiber.App
	QueryApp     *fiber.App
	PebbleWriter *storage.PebbleWriter
	EventWriter  *recordingEventWriter
	// ClickHouse is EventStore's database on the dev ClickHouse: the test
	// package's own (clickhousetest), emptied for each test. Reopen keeps it.
	ClickHouse       *storage.ClickHouse
	ClickHouseConfig config.ClickHouse
	SQLiteWriter     *storage.SQLiteWriter
	Issues           *flakyIssueRepository
	Processor        *processing.Processor
	EventCleanup     *processing.Cleanup
	BatchLock        *sync.Mutex
	// ClickHouseLogs is the Logs in ClickHouse, which Log processing
	// inserts into and Log rows, tables and histograms are read from, with
	// its bin cache.
	ClickHouseLogs *storage.ClickHouseLogs
	// FlakyLogs is ClickHouseLogs as Log processing sees it, which a test
	// can break.
	FlakyLogs    *flakyLogStore
	LogProcessor *logprocessing.Processor
	LogCleanup   *logprocessing.Cleanup
	LogsHandler  *handler.LogsHandler
	// SQL runs SQL queries, each as its Project's SQL user, within
	// SQLLimits, storage's defaults unless a test changes them.
	SQL        *storage.ClickHouseSQL
	SQLLimits  storage.SQLLimits
	MQWriter   *storage.MessageQueueWriter
	MQPath     string
	SQLitePath string

	// LogTransforms runs each Project's Log transform in Log processing.
	// Restart keeps it; Reopen makes a new one.
	LogTransforms *logtransform.Transforms

	// Clock is what Log ingest and Log processing take as now. It starts at
	// testNow and moves only when a test moves it.
	Clock *clock.Manual

	// BucketConfig, if set, is the object_store: the logs table's parts
	// move to its bucket (logs_bucket_test.go).
	BucketConfig *config.ObjectStore
	// BinCacheBytes is the bin cache's size, 64 MiB unless a test changes
	// it; 0 turns it off.
	BinCacheBytes int64
	// OldLogFiles is where the Logs from before the switch are: by default
	// logs.duckdb, prepared/ and logs_parquet/ in the environment's
	// directory, which hold none (usePreparedFixture sets them).
	OldLogFiles storage.OldLogFiles

	dir   string // holds Pebble, SQLite and the Logs from before the switch
	close func()
}

// Process runs Event processing until every stored Event is handled.
func (env *TestEnv) Process(t *testing.T) {
	t.Helper()
	if err := env.Processor.ProcessUntilCaughtUp(); err != nil {
		t.Fatalf("Event processing failed: %v", err)
	}
}

// ProcessLogs runs Log processing until every stored Log batch is handled.
func (env *TestEnv) ProcessLogs(t *testing.T) {
	t.Helper()
	if err := env.LogProcessor.ProcessUntilCaughtUp(); err != nil {
		t.Fatalf("Log processing failed: %v", err)
	}
}

// DeleteLogsPastRetention runs the daily pass at the clock's time: each Project's
// Logs past its Retention, and every Log of a deleted Project, are deleted.
func (env *TestEnv) DeleteLogsPastRetention(t *testing.T) {
	t.Helper()
	if err := env.LogCleanup.DeleteLogsPastRetention(env.Clock.Now()); err != nil {
		t.Fatalf("Deleting Logs past their Retention failed: %v", err)
	}
}

// Restart starts a new Event processing and Log processing on the same
// stores, as after an EventStore restart or crash.
func (env *TestEnv) Restart(t *testing.T) {
	t.Helper()
	env.Processor.Stop()
	env.Processor = processing.New(env.PebbleWriter, env.ClickHouse, env.MQWriter, env.Issues, env.BatchLock)
	if err := env.Processor.Prepare(); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	env.LogProcessor.Stop()
	env.LogProcessor = logprocessing.New(env.PebbleWriter, env.FlakyLogs, env.LogTransforms, env.Clock)
	if err := env.LogProcessor.Prepare(); err != nil {
		t.Fatalf("Log processing Prepare failed: %v", err)
	}
}

// testNow is where the test environment's clock starts.
var testNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// flakyIssueRepository is the real SQLite IssueRepository, except that its
// next failFinds lookups fail, as when SQLite is busy, and that recording seen
// counts fails while failSeen is set.
type flakyIssueRepository struct {
	*storage.SQLiteWriter
	failFinds atomic.Int32
	finds     atomic.Int32

	failSeen     atomic.Bool
	seenFailures atomic.Int32
}

func (r *flakyIssueRepository) RecordSeenEvents(projectID uint32, events []models.Event, throughUUID string) error {
	if r.failSeen.Load() {
		r.seenFailures.Add(1)
		return errors.New("database is locked")
	}
	return r.SQLiteWriter.RecordSeenEvents(projectID, events, throughUUID)
}

func (r *flakyIssueRepository) FindIssueByFingerprint(projectID uint32, fingerprint string) (int64, int64, int, bool, error) {
	r.finds.Add(1)
	if r.failFinds.Add(-1) >= 0 {
		return 0, 0, 0, false, errors.New("database is locked")
	}
	return r.SQLiteWriter.FindIssueByFingerprint(projectID, fingerprint)
}

// flakyLogStore is the real ClickHouse logs table as Log processing uses it,
// except while a test breaks it: then inserting Logs fails with the test's
// error, for every Project or only some, and while it is down, every call
// fails. It can also let inserts through and then fail them, as when
// ClickHouse takes an insert but its answer is lost, or EventStore crashes
// before it hears it.
type flakyLogStore struct {
	*storage.ClickHouseLogs
	// attempts counts inserts tried, failed or not: each try at a
	// Project's Logs makes one.
	attempts atomic.Int32

	mu       sync.Mutex
	storeErr error
	projects []uint32 // the Projects whose Logs fail; all if empty
	down     bool
	// afterInsert fails the next inserts, once they have stored their Logs,
	// with its error, and then calls its then, if set.
	afterInsert []insertFailure
}

// insertFailure fails an insert that has stored its Logs with err, and then
// calls then, if set.
type insertFailure struct {
	err  error
	then func()
}

// fail makes inserting the Logs of projects, or of every Project, fail with
// err. Trying Logs without storing them still works, as on a full disk.
func (s *flakyLogStore) fail(err error, projects ...uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeErr, s.projects = err, projects
}

// failAfterInsert makes the next insert fail with err once it has stored its
// Logs, and then calls then, if set.
func (s *flakyLogStore) failAfterInsert(err error, then func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterInsert = append(s.afterInsert, insertFailure{err, then})
}

// goDown makes every call fail, as when ClickHouse doesn't answer at all.
func (s *flakyLogStore) goDown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeErr, s.projects, s.down = connectionLost, nil, true
}

// connectionLost is how an insert fails when ClickHouse is gone.
var connectionLost = &clickhouse.Exception{Code: 210, Name: "DB::NetException", Message: "Connection reset by peer"}

// recover ends every failure.
func (s *flakyLogStore) recover() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeErr, s.projects, s.down = nil, nil, false
}

func (s *flakyLogStore) failure(projectID uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.projects) == 0 || slices.Contains(s.projects, projectID) {
		return s.storeErr
	}
	return nil
}

func (s *flakyLogStore) isDown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.down
}

// nextAfterInsert takes the failure the next insert gets once it has stored
// its Logs, if any.
func (s *flakyLogStore) nextAfterInsert() (insertFailure, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.afterInsert) == 0 {
		return insertFailure{}, false
	}
	next := s.afterInsert[0]
	s.afterInsert = s.afterInsert[1:]
	return next, true
}

func (s *flakyLogStore) InsertLogBatches(projectID uint32, insert storage.LogInsert) error {
	s.attempts.Add(1)
	if err := s.failure(projectID); err != nil {
		return err
	}
	if err := s.ClickHouseLogs.InsertLogBatches(projectID, insert); err != nil {
		return err
	}
	if failure, ok := s.nextAfterInsert(); ok {
		if failure.then != nil {
			failure.then()
		}
		return failure.err
	}
	return nil
}

func (s *flakyLogStore) TryLogBatches(projectID uint32, batches []storage.LogBatch) error {
	if s.isDown() {
		return connectionLost
	}
	return s.ClickHouseLogs.TryLogBatches(projectID, batches)
}

func (s *flakyLogStore) Ping() error {
	if s.isDown() {
		return connectionLost
	}
	return s.ClickHouseLogs.Ping()
}

func resetTestDB(t *testing.T) {
	cmd := exec.Command("bin/rails", "db:reset")
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("Failed to locate test file path for Rails db reset")
	}
	consoleDir := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "console"))
	cmd.Dir = consoleDir
	cmd.Env = append(os.Environ(), "RAILS_ENV=test")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to reset test DB: %v\nOutput: %s", err, string(output))
	}
}

// recordingEventWriter records the UUID of each Event written, because an
// ingest response doesn't carry it.
type recordingEventWriter struct {
	mu     sync.Mutex
	writer ingest.EventWriter
	uuids  []string
}

// setWriter makes w write through writer, keeping the UUIDs recorded so far.
func (w *recordingEventWriter) setWriter(writer ingest.EventWriter) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writer = writer
}

func (w *recordingEventWriter) WriteEvent(event *models.Event) error {
	w.mu.Lock()
	writer := w.writer
	w.mu.Unlock()
	if err := writer.WriteEvent(event); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.uuids = append(w.uuids, event.EventUUID)
	return nil
}

func (w *recordingEventWriter) UUIDs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.uuids)
}

// seedProjectKey adds a Project's public key to the Console database.
func seedProjectKey(t *testing.T, sqlitePath string, projectID uint32, publicKey string) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqlitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite for seeding: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("INSERT INTO project_keys (public_key, project_id, created_at, updated_at) VALUES (?, ?, ?, ?)", publicKey, projectID, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to seed sqlite: %v", err)
	}
}

func setupTestEnv(t *testing.T) *TestEnv {
	return newTestEnv(t, nil)
}

// newTestEnv is setupTestEnv, with configure, if set, changing the
// environment before its stores open.
func newTestEnv(t *testing.T, configure func(*TestEnv)) *TestEnv {
	logger.Init()
	resetTestDB(t)
	clickhousetest.Empty(t)

	env := &TestEnv{
		dir:              t.TempDir(),
		SQLitePath:       "../storage/sqlite/test/solid_trace.sqlite3",
		MQPath:           "../storage/sqlite/test/message_queue.sqlite3",
		BatchLock:        &sync.Mutex{},
		EventWriter:      &recordingEventWriter{},
		Clock:            clock.NewManual(testNow),
		ClickHouseConfig: clickhousetest.Config(),

		BinCacheBytes: config.DefaultLogsBinCacheMB << 20,
		SQLLimits:     storage.DefaultSQLLimits(),
	}
	if configure != nil {
		configure(env)
	}
	seedProjectKey(t, env.SQLitePath, 123, "test_public_key")
	env.open(t)
	return env
}

// open opens every store on the environment's files, and builds Event
// processing, Log processing and both apps on them.
func (env *TestEnv) open(t *testing.T) {
	t.Helper()

	pebbleWriter, err := storage.NewPebbleWriter(&config.Config{PebblePath: filepath.Join(env.dir, "pebble")})
	if err != nil {
		t.Fatalf("Failed to create Pebble: %v", err)
	}

	clickHouse, err := storage.OpenClickHouse(env.ClickHouseConfig, env.BucketConfig)
	if err != nil {
		t.Fatalf("Failed to open ClickHouse: %v", err)
	}
	clickHouseLogs := storage.NewClickHouseLogs(clickHouse, pebbleWriter)
	clickHouseLogs.UseBinCache(env.BinCacheBytes)
	// Before Log processing, as in main.
	old := env.OldLogFiles
	if old.LogsDuckDB == "" {
		old = storage.OldLogFiles{
			LogsDuckDB: filepath.Join(env.dir, "logs.duckdb"),
			LocalDir:   filepath.Join(env.dir, "logs_parquet"),
		}
	}
	if err := clickHouseLogs.UseOldLogFiles(old); err != nil {
		t.Fatalf("Failed to ready the Logs from before the switch: %v", err)
	}

	sqliteWriter, err := storage.NewSQLiteWriter(env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to create SQLite writer: %v", err)
	}

	mqWriter, err := storage.NewMessageQueueWriter(env.MQPath)
	if err != nil {
		t.Fatalf("Failed to create MQ Writer: %v", err)
	}

	// Initialize Auth
	projectAuth, err := auth.NewProjectAuth(env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to create auth: %v", err)
	}

	consoleSettings, err := storage.NewConsoleSettings(env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open Console settings: %v", err)
	}

	env.PebbleWriter = pebbleWriter
	env.ClickHouse = clickHouse
	env.ClickHouseLogs = clickHouseLogs
	env.FlakyLogs = &flakyLogStore{ClickHouseLogs: env.ClickHouseLogs}
	env.SQLiteWriter = sqliteWriter
	env.Issues = &flakyIssueRepository{SQLiteWriter: sqliteWriter}
	env.MQWriter = mqWriter

	env.Processor = processing.New(pebbleWriter, clickHouse, mqWriter, env.Issues, env.BatchLock)
	if err := env.Processor.Prepare(); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	env.EventCleanup = processing.NewCleanup(pebbleWriter, clickHouse, sqliteWriter, consoleSettings, env.BatchLock, env.Clock)
	env.LogTransforms = logtransform.New(consoleSettings, env.Clock)
	env.LogProcessor = logprocessing.New(pebbleWriter, env.FlakyLogs, env.LogTransforms, env.Clock)
	if err := env.LogProcessor.Prepare(); err != nil {
		t.Fatalf("Log processing Prepare failed: %v", err)
	}
	env.SQL = storage.NewClickHouseSQL(clickHouse, env.SQLLimits)
	env.LogCleanup = logprocessing.NewCleanup(env.ClickHouseLogs, env.SQL, consoleSettings, storage.LogDiskLimit{}, env.Clock)

	// Setup Handler
	env.EventWriter.setWriter(pebbleWriter)
	ingestService := ingest.NewService(ingest.Config{
		Events:       env.EventWriter,
		NotifyEvents: func(projectID uint32) { env.Processor.Notify(projectID) },
		Logs:         pebbleWriter,
		NotifyLogs:   func(projectID uint32) { env.LogProcessor.Notify(projectID) },
		Clock:        env.Clock,
		Retention:    consoleSettings,
		MaxWaiting:   100,
	})
	ingestHandler := handler.NewIngestHandler(projectAuth, ingestService)
	eventsHandler := handler.NewEventsHandler(pebbleWriter, clickHouse)
	logsHandler := handler.NewLogsHandler(env.ClickHouseLogs, env.SQL, consoleSettings, env.Clock)
	env.LogsHandler = logsHandler
	healthHandler := handler.NewHealthHandler(pebbleWriter, clickHouse)

	env.App = handler.NewIngestApp(ingestHandler, healthHandler)
	env.QueryApp = fiber.New()
	handler.RegisterQueryRoutes(env.QueryApp, eventsHandler, logsHandler, handler.NewLogTransformHandler(env.LogTransforms), healthHandler, handler.NewConfigHandler(&config.Config{}, 0))

	env.close = func() {
		pebbleWriter.StopWrites()
		env.Processor.Stop()
		env.LogProcessor.Stop()

		pebbleWriter.Close()
		clickHouse.Close()
		sqliteWriter.Close()
		mqWriter.Close()
		projectAuth.Close()
		env.LogTransforms.Close()
		consoleSettings.Close()
	}
}

// Cleanup closes every store.
func (env *TestEnv) Cleanup() {
	env.close()
}

// Reopen closes every store and opens them again on the same files, with new
// processing and apps, as when EventStore exits and starts again. Unlike
// Restart, nothing held in memory survives.
func (env *TestEnv) Reopen(t *testing.T) {
	t.Helper()
	env.close()
	env.open(t)
}

func TestStoreIngestion(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// 2. Prepare Request
	sampleEventBytes := getSampleEventJSON()

	req, _ := http.NewRequest("POST", "/api/123/store?sentry_key=test_public_key", bytes.NewReader(sampleEventBytes))
	req.Header.Set("Content-Type", "application/json")

	// 3. Execute
	resp, err := env.App.Test(req, 2000)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	// 4. Verification
	env.Process(t)

	params := storage.QueryParams{
		ProjectID: 123,
	}
	events, err := env.ClickHouse.QueryEvents(params)
	if err != nil {
		t.Fatalf("Query Events failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 Event stored, got %d", len(events))
	}

	persistedEvent := events[0]
	if persistedEvent.ProjectID != 123 {
		t.Errorf("Expected ProjectID 123, got %d", persistedEvent.ProjectID)
	}

	// Verify custom tag
	if val, ok := persistedEvent.Tags["custom_tag"]; !ok || val != "custom_value" {
		t.Errorf("Expected custom_tag=custom_value, got %v", val)
	}

	if persistedEvent.Release != "7573d29d72861e4ef4d37f53c687b242e5b15953" {
		t.Errorf("Unexpected release: %s", persistedEvent.Release)
	}

	// Verify Pebble
	key := storage.KeyForEvent(persistedEvent.ProjectID, persistedEvent.EventUUID)
	rawJSON, err := env.PebbleWriter.GetEvent(key)
	if err != nil {
		t.Fatalf("Pebble GetEvent failed: %v", err)
	}

	var savedPayload map[string]interface{}
	if err := json.Unmarshal(rawJSON, &savedPayload); err != nil {
		t.Fatalf("Failed to unmarshal saved JSON: %v", err)
	}

	if savedPayload["event_id"] != "8a6c475493954601ab7f3b599b2578ca" {
		t.Errorf("Saved JSON content mismatch (event_id)")
	}
}

func TestEnvelopeIngestion(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Construct an envelope body
	// Line 1: Envelope Header
	// Line 2: Item Header
	// Line 3: Payload
	envelopeHeader := `{"event_id": "9b7d475493954601ab7f3b599b2578cb", "sent_at": "2026-01-24T15:00:00Z"}`
	eventPayload := `{"event_id": "9b7d475493954601ab7f3b599b2578cb", "message": "Envelope Test", "environment": "production"}`
	itemHeader := fmt.Sprintf(`{"type": "event", "length": %d}`, len(eventPayload))

	body := envelopeHeader + "\n" + itemHeader + "\n" + eventPayload

	req, _ := http.NewRequest("POST", "/api/123/envelope?sentry_key=test_public_key", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/x-sentry-envelope")

	resp, err := env.App.Test(req, 2000)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Verification
	env.Process(t)

	params := storage.QueryParams{
		ProjectID: 123,
	}
	events, err := env.ClickHouse.QueryEvents(params)
	if err != nil {
		t.Fatalf("Query Events failed: %v", err)
	}

	// We might have events from other tests if running in parallel?
	// t.TempDir ensures isolation per test function run.
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	if events[0].Environment != "production" {
		t.Errorf("Expected environment production, got %s", events[0].Environment)
	}
}

func TestQueryEndpoints(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// 1. Seed an event via Store endpoint
	sampleEventBytes := getSampleEventJSON()
	req, _ := http.NewRequest("POST", "/api/123/store?sentry_key=test_public_key", bytes.NewReader(sampleEventBytes))
	req.Header.Set("Content-Type", "application/json")
	env.App.Test(req, 2000)
	env.Process(t)

	// Get the generated event UUID from ClickHouse
	events, _ := env.ClickHouse.QueryEvents(storage.QueryParams{ProjectID: 123})
	if len(events) == 0 {
		t.Fatal("Setup failed: no events seeded")
	}
	eventUUID := events[0].EventUUID

	// 2. Test GET /api/:project_id/events
	listReq, _ := http.NewRequest("GET", "/api/123/events?limit=10", nil)
	listResp, err := env.QueryApp.Test(listReq, 2000)
	if err != nil {
		t.Fatalf("List request failed: %v", err)
	}
	if listResp.StatusCode != 200 {
		t.Errorf("List expected 200, got %d", listResp.StatusCode)
	}
	var listEvents []models.Event
	json.NewDecoder(listResp.Body).Decode(&listEvents)
	if len(listEvents) != 1 {
		t.Errorf("List expected 1 event, got %d", len(listEvents))
	}

	// 3. Test GET /api/:project_id/events/count
	countReq, _ := http.NewRequest("GET", "/api/123/events/count", nil)
	countResp, err := env.QueryApp.Test(countReq, 2000)
	if err != nil {
		t.Fatalf("Count request failed: %v", err)
	}
	var countRes map[string]int64
	json.NewDecoder(countResp.Body).Decode(&countRes)
	if countRes["count"] != 1 {
		t.Errorf("Count expected 1, got %d", countRes["count"])
	}

	// 4. Test GET /api/:project_id/events/:event_uuid
	getReq, _ := http.NewRequest("GET", "/api/123/events/"+eventUUID, nil)
	getResp, err := env.QueryApp.Test(getReq, 2000)
	if err != nil {
		t.Fatalf("GetEvent request failed: %v", err)
	}
	if getResp.StatusCode != 200 {
		t.Errorf("GetEvent expected 200, got %d", getResp.StatusCode)
	}

	// Verify content matches sample payload
	var payload map[string]interface{}
	json.NewDecoder(getResp.Body).Decode(&payload)
	if payload["event_id"] != "8a6c475493954601ab7f3b599b2578ca" {
		t.Errorf("GetEvent payload mismatch")
	}
}

func postStore(t *testing.T, env *TestEnv) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", "/api/123/store?sentry_key=test_public_key", bytes.NewReader(getSampleEventJSON()))
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.App.Test(req, 2000)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return resp
}

func TestEventIsReadableByUUIDAsSoonAsItIsAcknowledged(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	if resp := postStore(t, env); resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	uuids := env.EventWriter.UUIDs()
	if len(uuids) != 1 {
		t.Fatalf("Expected 1 Event written, got %d", len(uuids))
	}

	getReq, _ := http.NewRequest("GET", "/api/123/events/"+uuids[0], nil)
	getResp, err := env.QueryApp.Test(getReq, 2000)
	if err != nil {
		t.Fatalf("GetEvent request failed: %v", err)
	}
	if getResp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", getResp.StatusCode)
	}
	var payload map[string]interface{}
	json.NewDecoder(getResp.Body).Decode(&payload)
	if payload["event_id"] != "8a6c475493954601ab7f3b599b2578ca" {
		t.Errorf("GetEvent payload mismatch: %v", payload)
	}
}

func TestIngestAfterWriterStopsIsServiceUnavailable(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	env.PebbleWriter.StopWrites()

	resp := postStore(t, env)
	if resp.StatusCode != 503 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 503, got %d: %s", resp.StatusCode, string(body))
	}
	assertRetryAfterAFewSeconds(t, resp)
	if uuids := env.EventWriter.UUIDs(); len(uuids) != 0 {
		t.Errorf("Expected no Event written after the writer stopped, got %d", len(uuids))
	}
}

// assertRetryAfterAFewSeconds checks that a response tells the sender to
// retry in a few seconds.
func assertRetryAfterAFewSeconds(t *testing.T, resp *http.Response) {
	t.Helper()
	header := resp.Header.Get("Retry-After")
	if seconds, err := strconv.Atoi(header); err != nil || seconds < 1 || seconds > 10 {
		t.Errorf("Expected Retry-After of a few seconds, got %q", header)
	}
}

func getSampleEventJSON() []byte {
	event := map[string]interface{}{
		"event_id":    "8a6c475493954601ab7f3b599b2578ca",
		"level":       "error",
		"timestamp":   "2026-01-24T14:29:30Z",
		"release":     "7573d29d72861e4ef4d37f53c687b242e5b15953",
		"environment": "development",
		"tags": map[string]string{
			"custom_tag": "custom_value",
		},
		"message": "Test event message",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "ZeroDivisionError",
					"value": "divided by 0 (ZeroDivisionError)",
				},
			},
		},
	}
	b, _ := json.Marshal(event)
	return b
}
