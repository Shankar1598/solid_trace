package storage

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/zerodha/logf"
)

// failingStore is a bucket in a local folder whose deletions a test can
// fail.
type failingStore struct {
	ObjectStore
	failDeletes atomic.Bool
}

func (b *failingStore) Delete(ctx context.Context, key string) error {
	if b.failDeletes.Load() {
		return errors.New("the bucket is unavailable")
	}
	return b.ObjectStore.Delete(ctx, key)
}

// A pass that skips every tombstone, as each is pinned, deleted nothing, so
// it says nothing of a bucket that was failing.
func TestAnObjectPassThatTriesNoDeletionLeavesAFailingBucketFailing(t *testing.T) {
	store := newTestLogStore(t)
	bucket := &failingStore{ObjectStore: NewDirStore(filepath.Join(t.TempDir(), "bucket"))}
	if err := store.UseObjectStore(bucket, 24); err != nil {
		t.Fatal(err)
	}
	hour := october(1, 12, 0)
	appendLogs(t, store, hour, stringLog(hour.Add(30*time.Minute), nil))
	if err := store.ExportLogs(october(1, 15, 0)); err != nil {
		t.Fatal(err)
	}
	if err := store.UploadLogFiles(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProjectLogs(promotionProject); err != nil {
		t.Fatal(err)
	}
	key := logFile{projectID: promotionProject, hour: hour, name: "data.parquet"}.key()

	var logs bytes.Buffer
	logger.L = logf.New(logf.Opts{Writer: &logs})
	t.Cleanup(logger.Init)
	pass := func() {
		t.Helper()
		if err := store.deleteTombstonedObjects(); err != nil {
			t.Fatalf("deleteTombstonedObjects: %v", err)
		}
	}

	bucket.failDeletes.Store(true)
	pass()
	// A read then pins the file, so the next pass tries no deletion.
	store.pin([]string{key})
	pass()
	if strings.Contains(logs.String(), "after failing") {
		t.Errorf("Expected a pass that deleted nothing not to say the bucket recovered, got %s", logs.String())
	}
	store.unpin([]string{key})
	pass()
	if got := strings.Count(logs.String(), "level=error"); got != 1 {
		t.Errorf("Expected the failing bucket logged once, got %d:\n%s", got, logs.String())
	}
	bucket.failDeletes.Store(false)
	pass()
	if got := strings.Count(logs.String(), "after failing"); got != 1 {
		t.Errorf("Expected the recovery logged once, got %d:\n%s", got, logs.String())
	}
}
