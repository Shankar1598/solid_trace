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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pipeline"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/pkg/msgpacker"
	"github.com/solidtrace/event_store/processing"
	"github.com/solidtrace/event_store/storage"
)

type TestEnv struct {
	App          *fiber.App
	QueryApp     *fiber.App
	PebbleWriter *storage.PebbleWriter
	EventWriter  *recordingEventWriter
	DuckDBWriter *storage.DuckDBWriter
	SQLiteWriter *storage.SQLiteWriter
	Issues       *flakyIssueRepository
	Processor    *processing.Processor
	BatchLock    *sync.Mutex
	MQWriter     *storage.MessageQueueWriter
	MQReader     *storage.MessageQueueReader
	MQPath       string
	SQLitePath   string
	Cleanup      func()
}

// Process runs Event processing until every stored Event is handled.
func (env *TestEnv) Process(t *testing.T) {
	t.Helper()
	if err := env.Processor.ProcessUntilCaughtUp(); err != nil {
		t.Fatalf("Event processing failed: %v", err)
	}
}

// Restart starts a new Event processing on the same stores, as after an
// EventStore restart or crash.
func (env *TestEnv) Restart(t *testing.T) {
	t.Helper()
	env.Processor.Stop()
	env.Processor = processing.New(env.PebbleWriter, env.DuckDBWriter, env.MQWriter, env.Issues, env.BatchLock)
	if err := env.Processor.Prepare(); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
}

// flakyIssueRepository is the real SQLite IssueRepository, except that its
// next failFinds lookups fail, as when SQLite is busy.
type flakyIssueRepository struct {
	*storage.SQLiteWriter
	failFinds atomic.Int32
	finds     atomic.Int32
}

func (r *flakyIssueRepository) FindIssueByFingerprint(projectID uint32, fingerprint string) (int64, int64, int, bool, error) {
	r.finds.Add(1)
	if r.failFinds.Add(-1) >= 0 {
		return 0, 0, 0, false, errors.New("database is locked")
	}
	return r.SQLiteWriter.FindIssueByFingerprint(projectID, fingerprint)
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
	ingest.EventWriter
	mu    sync.Mutex
	uuids []string
}

func (w *recordingEventWriter) WriteEvent(event *models.Event) error {
	if err := w.EventWriter.WriteEvent(event); err != nil {
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
	logger.Init()
	resetTestDB(t)

	tmpDir := t.TempDir()

	pebblePath := filepath.Join(tmpDir, "pebble")
	duckDBPath := filepath.Join(tmpDir, "duckdb.db")
	sqlitePath := "../storage/sqlite/test/solid_trace.sqlite3"
	mqPath := "../storage/sqlite/test/message_queue.sqlite3"

	seedProjectKey(t, sqlitePath, 123, "test_public_key")

	// Initialize Storage
	pebbleWriter, err := storage.NewPebbleWriter(&config.Config{PebblePath: pebblePath})
	if err != nil {
		t.Fatalf("Failed to create Pebble: %v", err)
	}

	duckdbWriter, err := storage.NewDuckDBWriter(duckDBPath, filepath.Join(tmpDir, "parquet"), "", "")
	if err != nil {
		t.Fatalf("Failed to create DuckDB: %v", err)
	}

	sqliteWriter, err := storage.NewSQLiteWriter(sqlitePath)
	if err != nil {
		t.Fatalf("Failed to create SQLite writer: %v", err)
	}

	mqWriter, err := storage.NewMessageQueueWriter(mqPath)
	if err != nil {
		t.Fatalf("Failed to create MQ Writer: %v", err)
	}

	mqReader, err := storage.NewMessageQueueReader(mqPath)
	if err != nil {
		t.Fatalf("Failed to create MQ Reader: %v", err)
	}

	// Initialize Auth
	projectAuth, err := auth.NewProjectAuth(sqlitePath)
	if err != nil {
		t.Fatalf("Failed to create auth: %v", err)
	}

	env := &TestEnv{
		PebbleWriter: pebbleWriter,
		DuckDBWriter: duckdbWriter,
		SQLiteWriter: sqliteWriter,
		Issues:       &flakyIssueRepository{SQLiteWriter: sqliteWriter},
		BatchLock:    &sync.Mutex{},
		MQWriter:     mqWriter,
		MQReader:     mqReader,
		MQPath:       mqPath,
		SQLitePath:   sqlitePath,
	}
	env.Processor = processing.New(pebbleWriter, duckdbWriter, mqWriter, env.Issues, env.BatchLock)
	if err := env.Processor.Prepare(); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Setup Handler
	env.EventWriter = &recordingEventWriter{EventWriter: pebbleWriter}
	ingestService := ingest.NewService(env.EventWriter, func(projectID uint32) { env.Processor.Notify(projectID) }, 100)
	ingestHandler := handler.NewIngestHandler(projectAuth, ingestService)
	eventsHandler := handler.NewEventsHandler(pebbleWriter, duckdbWriter)
	healthHandler := handler.NewHealthHandler(pebbleWriter, duckdbWriter)

	env.App = fiber.New()
	handler.RegisterIngestRoutes(env.App, ingestHandler, healthHandler)
	env.QueryApp = fiber.New()
	handler.RegisterQueryRoutes(env.QueryApp, eventsHandler, healthHandler)

	env.Cleanup = func() {
		pebbleWriter.StopWrites()
		env.Processor.Stop()

		pebbleWriter.Close()
		duckdbWriter.Close()
		sqliteWriter.Close()
		mqWriter.Close()
		mqReader.Close()
		projectAuth.Close()
	}

	return env
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
	events, err := env.DuckDBWriter.QueryEvents(params)
	if err != nil {
		t.Fatalf("DuckDB Query failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event in DuckDB, got %d", len(events))
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
	events, err := env.DuckDBWriter.QueryEvents(params)
	if err != nil {
		t.Fatalf("DuckDB Query failed: %v", err)
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

	// Get the generated event UUID from DuckDB
	events, _ := env.DuckDBWriter.QueryEvents(storage.QueryParams{ProjectID: 123})
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

	if resp := postStore(t, env); resp.StatusCode != 503 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 503, got %d: %s", resp.StatusCode, string(body))
	}
	if uuids := env.EventWriter.UUIDs(); len(uuids) != 0 {
		t.Errorf("Expected no Event written after the writer stopped, got %d", len(uuids))
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

func TestArchiveEvents(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// 1. Seed events for "yesterday"
	yesterday := time.Now().Add(-24 * time.Hour).Truncate(24 * time.Hour)
	event := models.Event{
		EventUUID:          "8a6c475493954601ab7f3b599b2578ca",
		ProjectID:          123,
		IssueFingerprintID: 1,
		Timestamp:          yesterday.Add(1 * time.Hour), // Yesterday + 1h
		Environment:        "production",
		ServerName:         "web-1",
		Release:            "v123",
		Level:              "error",
		Tags:               map[string]string{"foo": "bar"},
	}

	if err := env.DuckDBWriter.WriteBatch([]models.Event{event}); err != nil {
		t.Fatalf("Failed to seed event: %v", err)
	}

	// 2. Insert Archive Message (mimic Rails)
	// We need to pack the payload roughly how Rails would (msgpack)
	// Using our packer to simulate it
	archivePayload := map[string]string{
		"date": yesterday.Format("2006-01-02"),
	}
	packer := msgpacker.New(msgpacker.ModeRaw)
	packedPayload, err := packer.Pack(archivePayload)
	if err != nil {
		t.Fatalf("Failed to pack payload: %v", err)
	}

	// Direct sqlite insert
	mqDB, err := sql.Open("sqlite3", env.MQPath)
	if err != nil {
		t.Fatalf("Failed to open MQ DB: %v", err)
	}
	defer mqDB.Close()

	now := time.Now().Format("2006-01-02 15:04:05.000000")
	_, err = mqDB.Exec(`
		INSERT INTO console_messages (message_type, payload, status, attempts, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "archive_events", packedPayload, storage.StatusPending, 0, now, now)
	if err != nil {
		t.Fatalf("Failed to insert console message: %v", err)
	}

	// 3. Start Consumer
	consumer := pipeline.NewArchiveConsumer(env.MQReader, env.DuckDBWriter, env.BatchLock)
	go consumer.Run()
	defer consumer.Stop()

	// 4. Wait for processing (poll status)
	deadline := time.Now().Add(10 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		err := mqDB.QueryRow("SELECT status FROM console_messages WHERE message_type = ?", "archive_events").Scan(&status)
		if err != nil {
			t.Fatalf("Failed to query status: %v", err)
		}

		if status == storage.StatusProcessed {
			break
		}
		if status == storage.StatusFailed {
			var errMsg string
			mqDB.QueryRow("SELECT error_message FROM console_messages WHERE message_type = ?", "archive_events").Scan(&errMsg)
			t.Fatalf("Message processing failed: %s", errMsg)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if status != storage.StatusProcessed {
		t.Fatalf("Timed out waiting for message processing, status: %d", status)
	}

	// 5. Verify Archival
	// Events should be moved from hot table to parquet.
	// Since we can't easily check parquet file content here without reading it back (which QueryEvents does),
	// let's verify QueryEvents returns 1 event (it reads primarily from parquet if archived).
	// Also check that event is NOT in hot table?
	// But first, QueryEvents.

	// Need to force view recreation or ensure it picks up changes?
	// ArchiveEventsUpTo calls recreateEventsView.

	params := storage.QueryParams{
		ProjectID: 123,
	}
	events, err := env.DuckDBWriter.QueryEvents(params)
	if err != nil {
		t.Fatalf("DuckDB Query failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event after archive, got %d", len(events))
	}
}
