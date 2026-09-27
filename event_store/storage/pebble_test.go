package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/models"
)

func TestPebbleWriter(t *testing.T) {
	tmpDir := t.TempDir()
	logger.Init()
	dbPath := filepath.Join(tmpDir, "test_pebble")

	writer, err := NewPebbleWriter(&config.Config{PebblePath: dbPath})
	if err != nil {
		t.Fatalf("Failed to create PebbleWriter: %v", err)
	}
	defer writer.Close()

	if got := writer.db.FormatMajorVersion(); got != pebble.FormatValueSeparation {
		t.Errorf("Expected format major version %s, got %s", pebble.FormatValueSeparation, got)
	}

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
	key := KeyForEvent(event.ProjectID, event.EventUUID)
	data, err := writer.GetEvent(key)
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}
	if string(data) != string(rawJSON) {
		t.Errorf("Expected raw JSON %s, got %s", string(rawJSON), string(data))
	}

}

func TestKeyForEvent(t *testing.T) {
	u, _ := uuid.NewV7()

	key := KeyForEvent(7, u.String())

	// Format: project ID (4 bytes, big-endian) + UUID (16 bytes)
	if len(key) != 20 {
		t.Fatalf("Expected key length 20, got %d", len(key))
	}
	if string(key[:4]) != "\x00\x00\x00\x07" {
		t.Errorf("Expected project prefix 00000007, got %x", key[:4])
	}
	if string(key[4:]) != string(u[:]) {
		t.Errorf("UUID mismatch in key")
	}

	// uuid.Parse also accepts 32-char hex without dashes.
	keyHex := KeyForEvent(7, "8e06f9c623114e978329e37700b5f261")
	if len(keyHex) != 20 {
		t.Errorf("Expected key length 20 for hex string, got %d", len(keyHex))
	}

	if string(KeyForEvent(8, u.String())) == string(key) {
		t.Errorf("Expected different projects to produce different keys")
	}
}
