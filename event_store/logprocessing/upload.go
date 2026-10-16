package logprocessing

import (
	"context"
	"errors"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// retryUploadAfter is how soon failed uploads are tried again.
const retryUploadAfter = time.Minute

// Uploader keeps published Log files in a bucket. *storage.LogStore is the
// production adapter.
type Uploader interface {
	// UploadLogFiles uploads every published Log file not yet in the
	// bucket, and fails if any of them failed.
	UploadLogFiles(ctx context.Context) error
	// EvictLogFiles removes the local copies of uploaded Log files kept
	// long enough.
	EvictLogFiles(now time.Time) error
	// LogFilesPublished receives a signal after an export publishes files.
	LogFilesPublished() <-chan struct{}
}

// LogUploads is the worker inside EventStore that uploads each Log file to
// the bucket right after the export publishes it, and once when it starts,
// to catch up on files a restart left. After each round it evicts the local
// copies kept long enough, so that with local_cache_hours 0 a file's copy
// goes right after its upload. Failed uploads are tried again a minute
// later.
type LogUploads struct {
	store Uploader
	clock clock.Clock

	*worker
}

// NewLogUploads constructs the uploads worker. It reads the time from clock.
func NewLogUploads(store Uploader, clock clock.Clock) *LogUploads {
	return &LogUploads{store: store, clock: clock, worker: newWorker()}
}

// Start runs the worker in its own goroutine.
func (u *LogUploads) Start() {
	u.start(u.run)
}

// Stop stops the worker, cancelling an upload in progress.
func (u *LogUploads) Stop() {
	u.halt()
}

func (u *LogUploads) run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-u.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		// Each file that fails is logged by the store, once in a row.
		retry := u.store.UploadLogFiles(ctx) != nil
		if ctx.Err() != nil {
			return
		}
		if err := u.store.EvictLogFiles(u.clock.Now()); err != nil {
			if !errors.Is(err, storage.ErrLogFilesInUse) {
				logger.L.Error("Evicting the local copies of uploaded Log files failed, retrying", "error", err, "retry_in", retryUploadAfter)
			}
			retry = true
		}

		var again <-chan time.Time
		timer := time.NewTimer(retryUploadAfter)
		if retry {
			again = timer.C
		}
		select {
		case <-u.stop:
			timer.Stop()
			return
		case <-u.store.LogFilesPublished():
		case <-again:
		}
		timer.Stop()
	}
}
