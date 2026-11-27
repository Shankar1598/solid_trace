package storage

import (
	"errors"
	"path/filepath"
	"sync"
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

	rawJSON := []byte(`{"event":"data"}`)
	event := models.Event{ProjectID: 1, RawJSON: rawJSON}

	before := time.Now()
	if err := writer.WriteEvent(&event); err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	u, err := uuid.Parse(event.EventUUID)
	if err != nil {
		t.Fatalf("WriteEvent set an invalid EventUUID %q: %v", event.EventUUID, err)
	}
	if u.Version() != 7 {
		t.Errorf("Expected a UUIDv7, got version %d", u.Version())
	}

	key := KeyForEvent(event.ProjectID, event.EventUUID)
	data, err := writer.GetEvent(key)
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}
	if string(data) != string(rawJSON) {
		t.Errorf("Expected raw JSON %s, got %s", string(rawJSON), string(data))
	}

	receivedAt := ReceiveTimeForEventKey(key)
	if receivedAt.Before(before.Truncate(time.Millisecond)) || receivedAt.After(time.Now()) {
		t.Errorf("Expected receive time between %v and now, got %v", before, receivedAt)
	}
}

func TestPebbleWriterRefusesWritesOnceClosed(t *testing.T) {
	logger.Init()
	open := func(t *testing.T) *PebbleWriter {
		writer, err := NewPebbleWriter(&config.Config{PebblePath: filepath.Join(t.TempDir(), "pebble")})
		if err != nil {
			t.Fatalf("Failed to create PebbleWriter: %v", err)
		}
		return writer
	}
	assertRefused := func(t *testing.T, writer *PebbleWriter) {
		event := models.Event{ProjectID: 1, RawJSON: []byte(`{}`)}
		if err := writer.WriteEvent(&event); !errors.Is(err, ErrShuttingDown) {
			t.Fatalf("Expected ErrShuttingDown, got %v", err)
		}
	}

	t.Run("after StopWrites", func(t *testing.T) {
		writer := open(t)
		defer writer.Close()
		writer.StopWrites()
		assertRefused(t, writer)
	})

	t.Run("after Close", func(t *testing.T) {
		writer := open(t)
		writer.Close()
		assertRefused(t, writer)
	})
}

// Ingest writes before processing prepares, as while ClickHouse starts. A
// batch written then, with the clock behind the Processing cursor, must come
// after the cursor. Else processing never reads it.
func TestBatchWrittenAtOpenComesAfterTheProcessingCursor(t *testing.T) {
	const project = 7
	for _, kind := range []struct {
		name string
		// setCursor sets the Project's Processing cursor of this kind.
		setCursor func(w *PebbleWriter, cursor uuid.UUID) error
		// write writes one batch of this kind, and after counts the
		// batches after the cursor.
		write func(w *PebbleWriter) error
		after func(w *PebbleWriter, cursor uuid.UUID) (int, error)
	}{
		{
			name: "Log",
			setCursor: func(w *PebbleWriter, cursor uuid.UUID) error {
				return w.SetLogProcessingCursor(project, cursor)
			},
			write: func(w *PebbleWriter) error {
				return w.WriteLogBatch(project, []models.Log{{Message: "after a restart"}}, time.Now())
			},
			after: func(w *PebbleWriter, cursor uuid.UUID) (int, error) {
				batches, err := w.LogBatchesAfter(project, cursor, 100)
				return len(batches), err
			},
		},
		{
			name: "Transaction",
			setCursor: func(w *PebbleWriter, cursor uuid.UUID) error {
				return w.SetSpanProcessingCursor(project, cursor)
			},
			write: func(w *PebbleWriter) error {
				return w.WriteTransactionBatch(project, []models.Transaction{{}}, time.Now())
			},
			after: func(w *PebbleWriter, cursor uuid.UUID) (int, error) {
				batches, err := w.TransactionBatchesAfter(project, cursor, 1<<20)
				return len(batches), err
			},
		},
		{
			name: "Agent file",
			setCursor: func(w *PebbleWriter, cursor uuid.UUID) error {
				return w.SetAgentFileCursor(project, cursor)
			},
			write: func(w *PebbleWriter) error {
				session := uuid.New()
				_, _, err := w.WriteAgentFileUnit(project, AgentUpload{Unit: AgentFileUnit{
					SessionID: session, Path: session.String() + ".jsonl", Version: 1,
					Bytes:   []byte("after a restart\n"),
					Session: AgentSession{UserID: 1, Harness: "claude-code", Repository: "github.com/acme/shop"},
				}})
				return err
			},
			after: func(w *PebbleWriter, cursor uuid.UUID) (int, error) {
				batches, err := w.AgentFileBatchesAfter(project, cursor, 1<<20)
				return len(batches), err
			},
		},
	} {
		t.Run(kind.name, func(t *testing.T) {
			logger.Init()
			path := filepath.Join(t.TempDir(), "pebble")
			writer, err := NewPebbleWriter(&config.Config{PebblePath: path})
			if err != nil {
				t.Fatalf("Failed to create PebbleWriter: %v", err)
			}
			cursor, err := newLogBatchID(time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if err := kind.setCursor(writer, cursor); err != nil {
				t.Fatalf("Setting the cursor failed: %v", err)
			}
			writer.Close()

			writer, err = NewPebbleWriter(&config.Config{PebblePath: path})
			if err != nil {
				t.Fatalf("Failed to open PebbleWriter again: %v", err)
			}
			defer writer.Close()
			if err := kind.write(writer); err != nil {
				t.Fatalf("Writing the batch failed: %v", err)
			}
			if n, err := kind.after(writer, cursor); err != nil || n != 1 {
				t.Fatalf("Expected the batch written after the open to come after the cursor, got %d batches after it (%v)", n, err)
			}
		})
	}
}

// The Logs from before the switch to ClickHouse move the Log processing
// cursors after the open. A Log batch written after that, with the clock
// behind a moved cursor, must come after it. Else Log processing deletes it
// unstored.
func TestLogBatchWrittenAfterTheCursorsMoveComesAfterThem(t *testing.T) {
	logger.Init()
	writer, err := NewPebbleWriter(&config.Config{PebblePath: filepath.Join(t.TempDir(), "pebble")})
	if err != nil {
		t.Fatalf("Failed to create PebbleWriter: %v", err)
	}
	defer writer.Close()
	cursor, err := newLogBatchID(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.SetLogProcessingCursors(map[uint32]uuid.UUID{7: cursor}); err != nil {
		t.Fatalf("SetLogProcessingCursors failed: %v", err)
	}
	if err := writer.WriteLogBatch(7, []models.Log{{Message: "while ClickHouse starts"}}, time.Now()); err != nil {
		t.Fatalf("WriteLogBatch failed: %v", err)
	}
	batches, err := writer.LogBatchesAfter(7, cursor, 100)
	if err != nil {
		t.Fatalf("LogBatchesAfter failed: %v", err)
	}
	if len(batches) != 1 {
		t.Fatalf("Expected the batch written after the cursors moved to come after them, got %d batches after them", len(batches))
	}
}

// Event processing reads a Project forward from a processing cursor, so a
// reader must never see an Event key while a lower one is still uncommitted.
func TestConcurrentWritesCommitInUUIDOrder(t *testing.T) {
	logger.Init()
	writer, err := NewPebbleWriter(&config.Config{PebblePath: filepath.Join(t.TempDir(), "pebble")})
	if err != nil {
		t.Fatalf("Failed to create PebbleWriter: %v", err)
	}
	defer writer.Close()

	const writers, perWriter = 8, 200
	projectKeys := func() []string {
		iter, err := writer.db.NewIter(&pebble.IterOptions{
			LowerBound: []byte{0, 0, 0, 1},
			UpperBound: []byte{0, 0, 0, 2},
		})
		if err != nil {
			t.Fatalf("NewIter failed: %v", err)
		}
		defer iter.Close()
		var keys []string
		for iter.First(); iter.Valid(); iter.Next() {
			keys = append(keys, string(iter.Key()))
		}
		return keys
	}

	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWriter {
				event := models.Event{ProjectID: 1, RawJSON: []byte(`{}`)}
				if err := writer.WriteEvent(&event); err != nil {
					t.Errorf("WriteEvent failed: %v", err)
					return
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	var scans [][]string
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
			scans = append(scans, projectKeys())
		}
	}

	final := projectKeys()
	if len(final) != writers*perWriter {
		t.Fatalf("Expected %d keys, got %d", writers*perWriter, len(final))
	}
	for i, scan := range scans {
		for j, key := range scan {
			if key != final[j] {
				t.Fatalf("Scan %d saw key %x at position %d before a lower key was committed", i, key, j)
			}
		}
	}
}

func TestReceiveTimeForEventKey(t *testing.T) {
	// A UUIDv7 whose first 48 bits are the Unix time 2026-09-30T12:34:56.789Z in ms.
	receivedAt := time.Date(2026, 9, 30, 12, 34, 56, 789_000_000, time.UTC)
	var u uuid.UUID
	ms := uint64(receivedAt.UnixMilli())
	for i := range 6 {
		u[i] = byte(ms >> (40 - 8*i))
	}
	u[6] = 0x70 // version 7

	got := ReceiveTimeForEventKey(KeyForEvent(3, u.String()))
	if !got.Equal(receivedAt) {
		t.Errorf("Expected %v, got %v", receivedAt, got)
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
