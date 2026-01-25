package main_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pipeline"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type TestEnv struct {
	App           *fiber.App
	RocksDBWriter *storage.RocksDBWriter
	DuckDBWriter  *storage.DuckDBWriter
	SQLiteWriter  *storage.SQLiteWriter
	MQWriter      *storage.MessageQueueWriter
	Cleanup       func()
}

func setupTestEnv(t *testing.T) *TestEnv {
	logger.Init()
	tmpDir := t.TempDir()

	rocksDBPath := filepath.Join(tmpDir, "rocksdb")
	duckDBPath := filepath.Join(tmpDir, "duckdb.db")
	sqlitePath := filepath.Join(tmpDir, "sqlite.db")
	mqPath := filepath.Join(tmpDir, "mq")

	// Seed SQLite Auth DB
	db, err := sql.Open("sqlite3", sqlitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite for seeding: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE project_keys (
			public_key TEXT PRIMARY KEY,
			project_id INTEGER
		);
		INSERT INTO project_keys (public_key, project_id) VALUES ('test_public_key', 123);

		CREATE TABLE IF NOT EXISTS project_issue_counters (
			project_id INTEGER PRIMARY KEY,
			value INTEGER
		);

		CREATE TABLE IF NOT EXISTS issues (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			number INTEGER,
			title TEXT,
			culprit TEXT,
			kind INTEGER,
			status INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		);

		CREATE TABLE IF NOT EXISTS issue_fingerprints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id INTEGER,
			project_id INTEGER,
			fingerprint TEXT,
			created_at DATETIME,
			updated_at DATETIME
		);
	`)
	if err != nil {
		t.Fatalf("Failed to seed sqlite: %v", err)
	}
	db.Close()

	// Initialize Storage
	rocksdbWriter, err := storage.NewRocksDBWriter(rocksDBPath)
	if err != nil {
		t.Fatalf("Failed to create RocksDB: %v", err)
	}

	duckdbWriter, err := storage.NewDuckDBWriter(duckDBPath)
	if err != nil {
		t.Fatalf("Failed to create DuckDB: %v", err)
	}

	sqliteWriter, err := storage.NewSQLiteWriter(sqlitePath)
	if err != nil {
		t.Fatalf("Failed to create SQLite writer: %v", err)
	}

	mqWriter, err := storage.NewMessageQueueWriter(mqPath)
	if err != nil {
		t.Fatalf("Failed to create MQ: %v", err)
	}

	// Seed MQ Table
	mqDB, err := sql.Open("sqlite3", mqPath)
	if err != nil {
		t.Fatalf("Failed to open MQ DB for seeding: %v", err)
	}
	_, err = mqDB.Exec(`
		CREATE TABLE IF NOT EXISTS event_store_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_type TEXT,
			payload BLOB,
			status INTEGER,
			attempts INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		);
	`)
	if err != nil {
		t.Fatalf("Failed to seed MQ DB: %v", err)
	}
	mqDB.Close()

	// Initialize Auth
	projectAuth, err := auth.NewProjectAuth(sqlitePath)
	if err != nil {
		t.Fatalf("Failed to create auth: %v", err)
	}

	// Create channels
	rocksdbChan := make(chan models.Event, 100)
	duckdbChan := make(chan models.Event, 100)

	// Start ingesters with short flush timeout
	rocksdbIngester := pipeline.NewRocksDBIngester(
		rocksdbChan, duckdbChan, rocksdbWriter,
		10, 100*time.Millisecond,
	)
	go rocksdbIngester.Run()

	duckdbIngester := pipeline.NewDuckDBIngester(duckdbChan, duckdbWriter, mqWriter, 100*time.Millisecond)
	go duckdbIngester.Run()

	// Setup Handler
	ingestHandler := handler.NewIngestHandler(projectAuth, sqliteWriter, rocksdbChan)
	eventsHandler := handler.NewEventsHandler(rocksdbWriter, duckdbWriter)

	app := fiber.New()
	app.Post("/api/:project_id/store", ingestHandler.Store)
	app.Post("/api/:project_id/envelope", ingestHandler.Envelope)
	app.Get("/api/events/:event_uuid", eventsHandler.GetEvent)
	app.Get("/api/:project_id/events", eventsHandler.List)
	app.Get("/api/:project_id/events/count", eventsHandler.Count)

	cleanup := func() {
		close(rocksdbChan)
		close(duckdbChan)
		// Give some time for ingesters to flush and exit
		time.Sleep(50 * time.Millisecond)

		rocksdbWriter.Close()
		duckdbWriter.Close()
		sqliteWriter.Close()
		mqWriter.Close()
		projectAuth.Close()
	}

	return &TestEnv{
		App:           app,
		RocksDBWriter: rocksdbWriter,
		DuckDBWriter:  duckdbWriter,
		SQLiteWriter:  sqliteWriter,
		MQWriter:      mqWriter,
		Cleanup:       cleanup,
	}
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
	// Wait for pipelines to flush
	time.Sleep(500 * time.Millisecond)

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

	// Verify RocksDB
	key := storage.KeyForEvent(persistedEvent.EventUUID)
	rawJSON, err := env.RocksDBWriter.GetEvent(key)
	if err != nil {
		t.Fatalf("RocksDB GetEvent failed: %v", err)
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
	itemHeader := `{"type": "event", "length": 50}` // Length is ignored by our simple parser currently or must be valid?
	// The code: lines := strings.SplitN(string(body), "\n", 3)
	// rawEventJSON := lines[2]
	// It doesn't strictly validate length header vs content length.
	eventPayload := `{"event_id": "9b7d475493954601ab7f3b599b2578cb", "message": "Envelope Test", "environment": "production"}`

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
	time.Sleep(500 * time.Millisecond)

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
	time.Sleep(500 * time.Millisecond)

	// Get the generated event UUID from DuckDB
	events, _ := env.DuckDBWriter.QueryEvents(storage.QueryParams{ProjectID: 123})
	if len(events) == 0 {
		t.Fatal("Setup failed: no events seeded")
	}
	eventUUID := events[0].EventUUID

	// 2. Test GET /api/:project_id/events
	listReq, _ := http.NewRequest("GET", "/api/123/events?limit=10", nil)
	listResp, err := env.App.Test(listReq, 2000)
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
	countResp, err := env.App.Test(countReq, 2000)
	if err != nil {
		t.Fatalf("Count request failed: %v", err)
	}
	var countRes map[string]int64
	json.NewDecoder(countResp.Body).Decode(&countRes)
	if countRes["count"] != 1 {
		t.Errorf("Count expected 1, got %d", countRes["count"])
	}

	// 4. Test GET /api/events/:event_uuid
	getReq, _ := http.NewRequest("GET", "/api/events/"+eventUUID, nil)
	getResp, err := env.App.Test(getReq, 2000)
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
