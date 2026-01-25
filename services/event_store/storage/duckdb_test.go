package storage

import (
	"path/filepath"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

func TestDuckDBWriter(t *testing.T) {
	tmpDir := t.TempDir()
	logger.Init()
	dbPath := filepath.Join(tmpDir, "test_events.duckdb")

	writer, err := NewDuckDBWriter(dbPath)
	if err != nil {
		t.Fatalf("Failed to create DuckDBWriter: %v", err)
	}
	defer writer.Close()

	timestamp := time.Now().UTC().Truncate(time.Second)
	eventsToInsert := []models.Event{
		{
			EventUUID:          "test-uuid-1",
			ProjectID:          123,
			IssueFingerprintID: 456,
			Timestamp:          timestamp,
			Environment:        "production",
			Tags:               map[string]string{"browser": "chrome"},
		},
		{
			EventUUID:          "test-uuid-2",
			ProjectID:          123,
			IssueFingerprintID: 789,
			Timestamp:          timestamp.Add(time.Minute),
			Environment:        "staging",
			Tags:               map[string]string{"os": "linux"},
		},
	}

	if err := writer.WriteBatch(eventsToInsert); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}

	params := QueryParams{
		ProjectID: 123,
		SortDesc:  false, // Sort by timestamp ASC
	}
	events, err := writer.QueryEvents(params)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}

	// Verify first event
	if events[0].EventUUID != "test-uuid-1" {
		t.Errorf("Expected UUID test-uuid-1, got %s", events[0].EventUUID)
	}
	if events[0].Tags["browser"] != "chrome" {
		t.Errorf("Expected tag browser=chrome, got %v", events[0].Tags["browser"])
	}

	// Verify second event
	if events[1].EventUUID != "test-uuid-2" {
		t.Errorf("Expected UUID test-uuid-2, got %s", events[1].EventUUID)
	}
	if events[1].Environment != "staging" {
		t.Errorf("Expected Environment staging, got %s", events[1].Environment)
	}
	if events[1].Tags["os"] != "linux" {
		t.Errorf("Expected tag os=linux, got %v", events[1].Tags["os"])
	}

	// Test querying by tags
	tagParams := QueryParams{
		ProjectID: 123,
		Tags:      map[string]string{"browser": "chrome"},
	}
	tagEvents, err := writer.QueryEvents(tagParams)
	if err != nil {
		t.Fatalf("QueryEvents (tags) failed: %v", err)
	}
	if len(tagEvents) != 1 {
		t.Fatalf("Expected 1 event for tag browser=chrome, got %d", len(tagEvents))
	}
	if tagEvents[0].EventUUID != "test-uuid-1" {
		t.Errorf("Expected UUID test-uuid-1, got %s", tagEvents[0].EventUUID)
	}

	// Test querying by another tag
	osParams := QueryParams{
		ProjectID: 123,
		Tags:      map[string]string{"os": "linux"},
	}
	osEvents, err := writer.QueryEvents(osParams)
	if err != nil {
		t.Fatalf("QueryEvents (tags) failed: %v", err)
	}
	if len(osEvents) != 1 {
		t.Fatalf("Expected 1 event for tag os=linux, got %d", len(osEvents))
	}
	if osEvents[0].EventUUID != "test-uuid-2" {
		t.Errorf("Expected UUID test-uuid-2, got %s", osEvents[0].EventUUID)
	}

	// Test querying by non-existent tag
	noneParams := QueryParams{
		ProjectID: 123,
		Tags:      map[string]string{"browser": "firefox"},
	}
	noneEvents, err := writer.QueryEvents(noneParams)
	if err != nil {
		t.Fatalf("QueryEvents (tags) failed: %v", err)
	}
	if len(noneEvents) != 0 {
		t.Fatalf("Expected 0 events for tag browser=firefox, got %d", len(noneEvents))
	}

	// Test CountEvents with tags
	count, err := writer.CountEvents(tagParams)
	if err != nil {
		t.Fatalf("CountEvents (tags) failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1 for tag browser=chrome, got %d", count)
	}

	noneCount, err := writer.CountEvents(noneParams)
	if err != nil {
		t.Fatalf("CountEvents (tags) failed: %v", err)
	}
	if noneCount != 0 {
		t.Errorf("Expected count 0 for tag browser=firefox, got %d", noneCount)
	}
}
