package storage

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/solidtrace/event_store/models"
)

// scanMessages returns the messages of every Log of the Project, newest
// first.
func scanMessages(t *testing.T, store *LogStore) []string {
	t.Helper()
	page, err := store.ScanLogs(context.Background(), LogScan{ProjectID: promotionProject, From: october(1, 0, 0), To: october(2, 0, 0), Limit: 100})
	if err != nil {
		t.Fatalf("ScanLogs: %v", err)
	}
	var messages []string
	for _, log := range page.Logs {
		messages = append(messages, log.Message)
	}
	return messages
}

// messageLog is a Log at a time with a message.
func messageLog(at time.Time, message string) models.Log {
	log := stringLog(at, nil)
	log.Message = message
	return log
}

func assertHourFileCount(t *testing.T, store *LogStore, hour time.Time, want int) {
	t.Helper()
	files, err := store.HourFiles(promotionProject, hour)
	if err != nil {
		t.Fatalf("HourFiles: %v", err)
	}
	if len(files) != want {
		t.Errorf("Expected %d files for %v, got %v", want, hour, files)
	}
}

func TestLogsStoredWhileAnExportWritesItsFilesStayForTheNextExport(t *testing.T) {
	store := newTestLogStore(t)
	appendLogs(t, store, october(1, 10, 29),
		messageLog(october(1, 10, 10), "hot"),
		messageLog(october(1, 8, 10), "late"))
	want := []string{"hot", "late"}
	if got := scanMessages(t, store); !slices.Equal(got, want) {
		t.Fatalf("Expected %v before the export, got %v", want, got)
	}

	// Log processing stores a late Log of an exported hour and a hot Log
	// while the export at 12:05 writes the files of 08:00 and 10:00.
	store.staged = func(uint32) {
		store.staged = nil
		appendLogs(t, store, october(1, 12, 5),
			messageLog(october(1, 12, 1), "hot while exporting"),
			messageLog(october(1, 10, 20), "late while exporting"))
	}
	if err := store.ExportLogs(october(1, 12, 5)); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	want = []string{"hot while exporting", "late while exporting", "hot", "late"}
	if got := scanMessages(t, store); !slices.Equal(got, want) {
		t.Errorf("Expected %v after the export, got %v", want, got)
	}
	assertHourFileCount(t, store, october(1, 8, 0), 1)
	assertHourFileCount(t, store, october(1, 10, 0), 1)

	// The next export moves the late Log to its hour's late file.
	if err := store.ExportLogs(october(1, 13, 5)); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	if got := scanMessages(t, store); !slices.Equal(got, want) {
		t.Errorf("Expected %v after the next export, got %v", want, got)
	}
	assertHourFileCount(t, store, october(1, 10, 0), 2)
}

func TestAProjectDeletedWhileAnExportWritesItsFilesGetsNone(t *testing.T) {
	store := newTestLogStore(t)
	appendLogs(t, store, october(1, 10, 29),
		messageLog(october(1, 10, 10), "hot"),
		messageLog(october(1, 8, 10), "late"))

	store.staged = func(uint32) {
		store.staged = nil
		if err := store.DeleteProjectLogs(promotionProject); err != nil {
			t.Errorf("DeleteProjectLogs: %v", err)
		}
	}
	// The export finds the Logs gone, and exports again with Log processing
	// held up, which finds none.
	if err := store.ExportLogs(october(1, 12, 5)); err != nil {
		t.Errorf("ExportLogs: %v", err)
	}
	if got := scanMessages(t, store); len(got) != 0 {
		t.Errorf("Expected no Logs, got %v", got)
	}
	assertHourFileCount(t, store, october(1, 8, 0), 0)
	assertHourFileCount(t, store, october(1, 10, 0), 0)
	err := filepath.WalkDir(store.files.Dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			t.Errorf("Expected no Log file on disk, found %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestALogStoredOutOfBatchOrderIsStillExported(t *testing.T) {
	store := newTestLogStore(t)
	appendLogs(t, store, october(1, 10, 29),
		messageLog(october(1, 10, 10), "in order"),
		messageLog(october(1, 10, 20), "out of order"))
	// As if stored after a batch with a later id than the cursor's.
	if _, err := store.db.Exec(fmt.Sprintf(
		"UPDATE logs.logs_hot_%d SET batch = 'ffffffff-ffff-7fff-bfff-ffffffffffff' WHERE message = 'out of order'", promotionProject)); err != nil {
		t.Fatal(err)
	}

	if err := store.ExportLogs(october(1, 12, 5)); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	if got, want := scanMessages(t, store), []string{"out of order", "in order"}; !slices.Equal(got, want) {
		t.Errorf("Expected %v after the export, got %v", want, got)
	}
	assertHourFileCount(t, store, october(1, 10, 0), 1)
	var left int
	if err := store.db.QueryRow(fmt.Sprintf("SELECT count(*) FROM logs.logs_hot_%d", promotionProject)).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("Expected the hot table empty after the export, got %d Logs", left)
	}
}
