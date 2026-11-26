package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// sweepNow is the fixed clock of the Evidence Retention tests.
var sweepNow = time.Date(2026, 12, 1, 6, 0, 0, 0, time.UTC)

var sweepSeed int

// addEvidenceAt stores a new Evidence file of organizationID, uploaded age
// before sweepNow.
func addEvidenceAt(t *testing.T, e *EvidenceFiles, organizationID int64, age time.Duration) EvidenceFile {
	t.Helper()
	sweepSeed++
	upload := evidenceUpload(evidencePNG(100 + sweepSeed))
	upload.OrganizationID, upload.At = organizationID, sweepNow.Add(-age)
	file, created, err := e.Add(context.Background(), upload)
	if err != nil || !created {
		t.Fatalf("Add an Evidence file: %v, %v", created, err)
	}
	return file
}

func sweep(t *testing.T, e *EvidenceFiles, retention map[int64]int) EvidenceSweep {
	t.Helper()
	result, err := e.DeletePastRetention(context.Background(), sweepNow, retention)
	if err != nil {
		t.Fatalf("Evidence Retention: %v", err)
	}
	return result
}

// deletedAt is the deleted_at of file's row, or nil.
func deletedAt(t *testing.T, e *EvidenceFiles, file EvidenceFile) *time.Time {
	t.Helper()
	row, found, err := e.EvidenceFile(context.Background(), file.ID)
	if err != nil || !found {
		t.Fatalf("Read Evidence file %s: %v, %v", file.ID, found, err)
	}
	return row.DeletedAt
}

func hasBytes(t *testing.T, e *EvidenceFiles, file EvidenceFile) bool {
	t.Helper()
	_, err := os.Stat(e.localPath(file.StorageKey))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

func TestEvidenceRetentionDeletesTheBytesOfOldFilesAndMarksTheirRows(t *testing.T) {
	e := newTestEvidenceFiles(t, nil, t.TempDir())
	day := 24 * time.Hour
	young := addEvidenceAt(t, e, 1, 2*day-time.Minute)
	old := addEvidenceAt(t, e, 1, 2*day+time.Minute)
	// Organization 2 has no Agent Project, so it has the default of 90 days.
	keptByDefault := addEvidenceAt(t, e, 2, 89*day)
	pastDefault := addEvidenceAt(t, e, 2, 91*day)
	// Organization 3's Evidence Retention is not valid, so its files stay.
	notValid := addEvidenceAt(t, e, 3, 1000*day)

	if got := sweep(t, e, map[int64]int{1: 2, 3: 0}); got != (EvidenceSweep{Deleted: 2}) {
		t.Errorf("Expected 2 files deleted, got %+v", got)
	}
	for _, file := range []EvidenceFile{old, pastDefault} {
		if at := deletedAt(t, e, file); at == nil || !at.Equal(sweepNow) {
			t.Errorf("Expected %s deleted at %v, got %v", file.ID, sweepNow, at)
		}
		if hasBytes(t, e, file) {
			t.Errorf("Expected the bytes of %s to be gone", file.ID)
		}
	}
	for _, file := range []EvidenceFile{young, keptByDefault, notValid} {
		if at := deletedAt(t, e, file); at != nil || !hasBytes(t, e, file) {
			t.Errorf("Expected %s kept with its bytes, got deleted at %v", file.ID, at)
		}
	}

	if got := sweep(t, e, map[int64]int{1: 2, 3: 0}); got != (EvidenceSweep{}) {
		t.Errorf("Expected a second run to delete nothing, got %+v", got)
	}
	if at := deletedAt(t, e, old); at == nil || !at.Equal(sweepNow) {
		t.Errorf("Expected a second run to keep the first deleted_at, got %v", at)
	}
}

func TestAShorterEvidenceRetentionAppliesToTheFilesAlreadyThere(t *testing.T) {
	e := newTestEvidenceFiles(t, nil, t.TempDir())
	file := addEvidenceAt(t, e, 1, 10*24*time.Hour)
	if got := sweep(t, e, map[int64]int{1: 90}); got.Deleted != 0 {
		t.Fatalf("Expected no file deleted at 90 days, got %+v", got)
	}
	if got := sweep(t, e, map[int64]int{1: 7}); got.Deleted != 1 || deletedAt(t, e, file) == nil {
		t.Errorf("Expected the 10-day-old file deleted at 7 days, got %+v", got)
	}
}

func TestEvidenceRetentionThatStopsBeforeTheRowUpdateEndsTheSameOnTheNextRun(t *testing.T) {
	e := newTestEvidenceFiles(t, nil, t.TempDir())
	file := addEvidenceAt(t, e, 1, 100*24*time.Hour)
	if _, err := e.db.Exec(`CREATE TRIGGER refuse_deleted_at BEFORE UPDATE OF deleted_at ON evidence_files
		BEGIN SELECT RAISE(ABORT, 'stopped by the test'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DeletePastRetention(context.Background(), sweepNow, nil); err == nil || !strings.Contains(err.Error(), "stopped by the test") {
		t.Fatalf("Expected the row update's error, got %v", err)
	}
	if hasBytes(t, e, file) || deletedAt(t, e, file) != nil {
		t.Fatalf("Expected the bytes gone and the row as it was, got bytes %v, deleted at %v", hasBytes(t, e, file), deletedAt(t, e, file))
	}

	if _, err := e.db.Exec("DROP TRIGGER refuse_deleted_at"); err != nil {
		t.Fatal(err)
	}
	if got := sweep(t, e, nil); got.Deleted != 1 || deletedAt(t, e, file) == nil {
		t.Errorf("Expected the next run to mark the row, got %+v", got)
	}
}

func TestAFailedDeleteOfTheBytesKeepsItsRowForTheNextRun(t *testing.T) {
	store := newMemoryStore()
	e := newTestEvidenceFiles(t, store, t.TempDir())
	stuck := addEvidenceAt(t, e, 1, 100*24*time.Hour)
	other := addEvidenceAt(t, e, 1, 100*24*time.Hour)
	store.failDelete = func(key string) error {
		if key == stuck.StorageKey {
			return errors.New("the bucket refused")
		}
		return nil
	}
	got, err := e.DeletePastRetention(context.Background(), sweepNow, nil)
	if err == nil || !strings.Contains(err.Error(), "1 of 2 deletes failed") || got.Deleted != 1 {
		t.Fatalf("Expected 1 delete and the bucket's error, got %+v, %v", got, err)
	}
	if deletedAt(t, e, stuck) != nil || deletedAt(t, e, other) == nil {
		t.Errorf("Expected only the deleted bytes' row marked")
	}

	store.failDelete = nil
	if got := sweep(t, e, nil); got.Deleted != 1 || deletedAt(t, e, stuck) == nil {
		t.Errorf("Expected the next run to delete the kept file, got %+v", got)
	}
}

func TestEvidenceRetentionDeletesMoreFilesThanOneBatch(t *testing.T) {
	e := newTestEvidenceFiles(t, nil, t.TempDir())
	count := evidenceSweepBatch*2 + 1
	for range count {
		addEvidenceAt(t, e, 1, 100*24*time.Hour)
	}
	if got := sweep(t, e, nil); got.Deleted != count {
		t.Errorf("Expected %d files deleted, got %+v", count, got)
	}
	left, err := filepath.Glob(filepath.Join(e.dir, "evidence", "org=1", "*"))
	if err != nil || len(left) != 0 {
		t.Errorf("Expected no bytes left, got %d (%v)", len(left), err)
	}
}

// putObject stores bytes under key with no row, modified age before now.
func putObject(t *testing.T, e *EvidenceFiles, key string, age time.Duration) {
	t.Helper()
	if err := e.put(context.Background(), key, []byte("bytes with no row")); err != nil {
		t.Fatal(err)
	}
	if e.bucket == nil {
		at := time.Now().Add(-age)
		if err := os.Chtimes(e.localPath(key), at, at); err != nil {
			t.Fatal(err)
		}
	} else {
		e.bucket.(*memoryStore).modified[key] = time.Now().Add(-age)
	}
}

func TestEvidenceRetentionDeletesObjectsWithNoRowThatAreOlderThanADay(t *testing.T) {
	for _, where := range []string{"on the data disk", "in a bucket"} {
		t.Run(where, func(t *testing.T) {
			var bucket ObjectStore
			if where == "in a bucket" {
				bucket = newMemoryStore()
			}
			e := newTestEvidenceFiles(t, bucket, t.TempDir())
			kept := addEvidenceAt(t, e, 1, time.Hour)
			if e.bucket == nil {
				old := time.Now().Add(-48 * time.Hour)
				if err := os.Chtimes(e.localPath(kept.StorageKey), old, old); err != nil {
					t.Fatal(err)
				}
			} else {
				e.bucket.(*memoryStore).modified[kept.StorageKey] = time.Now().Add(-48 * time.Hour)
			}
			stopped := "evidence/org=1/" + uuid.NewString() + ".png"
			putObject(t, e, stopped, 25*time.Hour)
			inProgress := "evidence/org=1/" + uuid.NewString() + ".png"
			putObject(t, e, inProgress, 23*time.Hour)

			result, err := e.DeletePastRetention(context.Background(), time.Now(), nil)
			if err != nil || result != (EvidenceSweep{Unreferenced: 1}) {
				t.Fatalf("Expected 1 object with no row deleted, got %+v (%v)", result, err)
			}
			objects, err := e.listObjects(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var keys []string
			for _, object := range objects {
				keys = append(keys, object.Key)
			}
			if len(keys) != 2 || !slices.Contains(keys, kept.StorageKey) || !slices.Contains(keys, inProgress) {
				t.Errorf("Expected the file with a row and the upload in progress, got %v", keys)
			}
		})
	}
}
