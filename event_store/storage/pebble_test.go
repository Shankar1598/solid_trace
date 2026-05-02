package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"

	"github.com/solidtrace/event_store/models"
)

func TestPebbleWriter(t *testing.T) {
	tmpDir := t.TempDir()
	logger.Init()
	dbPath := filepath.Join(tmpDir, "test_pebble")

	writer, err := NewPebbleWriter(dbPath)
	if err != nil {
		t.Fatalf("Failed to create PebbleWriter: %v", err)
	}
	defer writer.Close()

	timestamp := time.Now().UTC()
	uuidHex := "8e06f9c623114e978329e37700b5f261"
	rawJSON := []byte(`{"event":"data"}`)

	event := models.Event{
		ProjectID:          1,
		IssueFingerprintID: 100,
		EventUUID:          uuidHex,
		Timestamp:          timestamp,
		RawJSON:            rawJSON,
	}

	// Test WriteBatch
	if err := writer.WriteBatch([]models.Event{event}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}

	// Test GetEvent
	key := KeyForEvent(event.EventUUID)
	data, err := writer.GetEvent(key)
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}
	if string(data) != string(rawJSON) {
		t.Errorf("Expected raw JSON %s, got %s", string(rawJSON), string(data))
	}

}

func TestKeyForEvent(t *testing.T) {
	// Generate a v7 UUID
	u, _ := uuid.NewV7()
	uuidStr := u.String()

	key := KeyForEvent(uuidStr)

	// Format: UUID (16 bytes)
	// Total: 16 bytes

	if len(key) != 16 {
		t.Errorf("Expected key length 16, got %d", len(key))
	}

	// Verify exact match
	if string(key) != string(u[:]) {
		t.Errorf("UUID mismatch in key")
	}

	// Test with hex string (no dashes) - should also work if Parse handles it
	// uuid.Parse supports 32-char hex.
	hexStr := "8e06f9c623114e978329e37700b5f261"
	keyHex := KeyForEvent(hexStr)
	if len(keyHex) != 16 {
		t.Errorf("Expected key length 16 for hex string, got %d", len(keyHex))
	}
}
