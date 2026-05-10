package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

// TestPebbleTieringLocal verifies the tiering logic by using two local directories.
// It simulates a "Hot" tier (SSD) and a "Cold" tier (HDD) and ensures that
// Pebble moves SSTables to the cold tier upon compaction to Level 1.
func TestPebbleTieringLocal(t *testing.T) {
	ctx := context.Background()
	logger.Init()

	// Setup Directories
	tmpDir := t.TempDir()
	hotDir := filepath.Join(tmpDir, "hot")
	coldDir := filepath.Join(tmpDir, "cold")
	if err := os.MkdirAll(coldDir, 0755); err != nil {
		t.Fatalf("failed to create cold dir: %v", err)
	}

	// 1. Setup Pebble with Tiering (SSD -> HDD emulation)
	// We map Level 1 to the cold tier so data moves immediately after the first compaction.
	cfg := &config.Config{
		PebblePath: hotDir,
		StorageTiers: []config.StorageTier{
			{
				Kind:    "local",
				Locator: "local-cold",
				Bucket:  coldDir,
				Level:   1, 
			},
		},
	}

	writer, err := NewPebbleWriter(cfg)
	if err != nil {
		t.Fatalf("failed to create PebbleWriter: %v", err)
	}
	defer writer.Close()

	// 2. Write an event
	eventID := "00000000-0000-0000-0000-000000000001"
	event := models.Event{
		EventUUID: eventID,
		RawJSON:   []byte(`{"message": "hello local tiering"}`),
	}

	err = writer.WriteBatch([]models.Event{event})
	if err != nil {
		t.Fatalf("failed to write batch: %v", err)
	}

	// 3. Force Flush to L0
	if err := writer.db.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	// 4. Force Compaction L0 -> L1 (which is mapped to the Cold Tier)
	if err := writer.db.Compact(ctx, []byte{0x00}, []byte{0xff}, false); err != nil {
		t.Fatalf("failed to compact: %v", err)
	}

	// 5. Verify Public Interface: Data must be transparently readable
	data, err := writer.GetEvent(KeyForEvent(eventID))
	if err != nil {
		t.Fatalf("failed to get event: %v", err)
	}
	if string(data) != string(event.RawJSON) {
		t.Errorf("expected %s, got %s", string(event.RawJSON), string(data))
	}

	// 6. Verify Implementation: Check if files actually exist in coldDir
	files, err := os.ReadDir(coldDir)
	if err != nil {
		t.Fatalf("failed to read cold dir: %v", err)
	}
	
	foundSST := false
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".sst" {
			foundSST = true
			break
		}
	}
	if !foundSST {
		t.Error("expected SSTable files in cold (HDD) directory, but found none")
	}
}
