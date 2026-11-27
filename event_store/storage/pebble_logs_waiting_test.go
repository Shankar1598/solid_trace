package storage

import (
	"path/filepath"
	"slices"
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
	// The Overview's Health reads the same oldest waiting batch.
	others, err := w.LogBatchesAfter(8, uuid.Nil, 100)
	if err != nil || len(others) != 1 {
		t.Fatalf("Expected 1 batch of Project 8, got %d (%v)", len(others), err)
	}
	health, err := (&ClickHouseLogs{pebble: w}).WaitingLogBatches([]uint32{7, 8, 9}, now)
	var got []OldestLogBatch
	for _, batch := range health {
		got = append(got, OldestLogBatch{batch.ProjectID, batch.BatchID})
	}
	want := []OldestLogBatch{{7, batches[2].ID}, {8, others[0].ID}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Health: expected %v, got %v (%v)", want, got, err)
	}

	// As Log processing does, the batches up to the cursor are deleted, the
	// cursor's batch too.
	if err := w.DeleteLogBatchesUpTo(7, batches[1].ID); err != nil {
		t.Fatalf("Delete the stored batches: %v", err)
	}
	assertWaiting("with the batches up to the cursor deleted", batches[2].ID)

	if err := w.SetLogProcessingCursor(7, batches[2].ID); err != nil {
		t.Fatalf("Move the cursor: %v", err)
	}
	assertWaiting("with the cursor at the last batch", uuid.Nil)
}

// A clock 10 min ahead writes a batch, and comes back. The batch id floor
// keeps the next ids at that time, also after a restart. So the oldest
// waiting batch's id is after now. Log processing that does not reach it
// is behind once the batch has waited LogProcessingLag since this process
// first read it, and a batch that it reaches starts a new wait.
func TestABatchWhoseIdIsAheadWaitsFromWhenItWasFirstRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pebble")
	w, err := NewPebbleWriter(&config.Config{PebblePath: path})
	if err != nil {
		t.Fatalf("Open Pebble: %v", err)
	}
	now := time.Now()
	ahead := now.Add(10 * time.Minute)
	if err := w.WriteLogBatch(7, []models.Log{{Message: "clock ahead", Time: ahead}}, ahead); err != nil {
		t.Fatalf("Write a Log batch: %v", err)
	}
	w.StopWrites()
	w.Close()

	w, err = NewPebbleWriter(&config.Config{PebblePath: path})
	if err != nil {
		t.Fatalf("Open Pebble again: %v", err)
	}
	defer w.Close()
	stored, _, err := w.OldestWaitingLogBatch(7)
	if err != nil {
		t.Fatalf("Read the oldest waiting batch: %v", err)
	}
	if err := w.SetLogProcessingCursor(7, stored); err != nil {
		t.Fatalf("Move the cursor: %v", err)
	}
	for i := range 2 {
		if err := w.WriteLogBatch(7, []models.Log{{Message: "waits", Time: now}}, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("Write a Log batch: %v", err)
		}
	}
	waits := func(at time.Time) WaitingLogBatch {
		t.Helper()
		batch, waiting, err := w.WaitingLogBatch(7, at)
		if err != nil || !waiting {
			t.Fatalf("Expected a waiting batch, got %v (%v)", waiting, err)
		}
		return batch
	}
	first := waits(now)
	if !LogBatchWrittenAt(first.BatchID).After(now) {
		t.Fatalf("Expected the floor to put the batch id after now, got %v", LogBatchWrittenAt(first.BatchID))
	}
	if first.Waited > 0 {
		t.Fatalf("Expected no wait at the first read, got %v", first.Waited)
	}
	later := now.Add(LogProcessingLag + time.Second)
	if waited := waits(later).Waited; waited != LogProcessingLag+time.Second {
		t.Fatalf("Expected the batch to have waited %v since the first read, got %v", LogProcessingLag+time.Second, waited)
	}

	if err := w.SetLogProcessingCursor(7, first.BatchID); err != nil {
		t.Fatalf("Move the cursor: %v", err)
	}
	if next := waits(later); next.BatchID == first.BatchID || next.Waited != 0 {
		t.Fatalf("Expected the next batch to start a new wait, got %+v", next)
	}
}
