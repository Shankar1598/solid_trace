package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/models"
)

// The oldest waiting Log batch is the first after the Processing cursor: a
// batch that Log processing stored but has not deleted yet does not wait.
func TestTheOldestWaitingLogBatchIsTheFirstAfterTheCursor(t *testing.T) {
	w, err := NewPebbleWriter(&config.Config{PebblePath: filepath.Join(t.TempDir(), "pebble")})
	if err != nil {
		t.Fatalf("Open Pebble: %v", err)
	}
	defer w.Close()
	assertWaiting := func(when string, want uuid.UUID) {
		t.Helper()
		got, waiting, err := w.OldestWaitingLogBatch(7)
		if err != nil || waiting != (want != uuid.Nil) || got != want {
			t.Fatalf("%s: expected %v, got %v (waiting %v, %v)", when, want, got, waiting, err)
		}
	}
	assertWaiting("with no batch", uuid.Nil)

	now := time.Now()
	for i := range 3 {
		if err := w.WriteLogBatch(7, []models.Log{{Message: "m", Time: now}}, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("Write a Log batch: %v", err)
		}
	}
	if err := w.WriteLogBatch(8, []models.Log{{Message: "other Project", Time: now}}, now); err != nil {
		t.Fatalf("Write a Log batch: %v", err)
	}
	batches, err := w.LogBatchesAfter(7, uuid.Nil, 100)
	if err != nil || len(batches) != 3 {
		t.Fatalf("Expected 3 batches, got %d (%v)", len(batches), err)
	}
	assertWaiting("with no cursor", batches[0].ID)

	if err := w.SetLogProcessingCursor(7, batches[1].ID); err != nil {
		t.Fatalf("Move the cursor: %v", err)
	}
	assertWaiting("with the cursor at the second batch, none deleted", batches[2].ID)

	if err := w.SetLogProcessingCursor(7, batches[2].ID); err != nil {
		t.Fatalf("Move the cursor: %v", err)
	}
	assertWaiting("with the cursor at the last batch", uuid.Nil)
}
