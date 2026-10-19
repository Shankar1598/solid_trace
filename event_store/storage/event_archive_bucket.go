package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/pkg/sqltext"
)

// The Event archive in a bucket (ADR 0008). With an ObjectStore, each archive
// file is uploaded after the run that published it, under its path below the
// archive folder with eventArchivePrefix before it, and its local copy is
// deleted once it is uploaded: the archive keeps no local cache. The events
// view lists every archive file by name: those in the bucket, listed once
// after startup and added to after each upload, and the local files still
// waiting for theirs.
//
// Startup never waits on the bucket: the archive consumer lists it in the
// background. Until it does, and after any rebuild of the view fails, there
// is no events view, so Event reads fail (ErrEventsUnavailable) rather than
// miss archived Events; the consumer tries again every minute.
//
// Restarts settle what a crash leaves: a local file whose key the bucket
// lists with the same size was uploaded, so the next upload pass deletes its
// copy without uploading it again, and the view meanwhile reads it once, from
// the bucket. Any other local file is uploaded, again if need be.

// eventArchivePrefix begins the key of every Event archive file, keeping them
// apart from the Log files' keys, which begin with project=.
const eventArchivePrefix = "events/"

// archiveListTimeout bounds a listing of the bucket's archive files, so that
// a stalled request holds up neither startup nor the uploads for long.
const archiveListTimeout = time.Minute

// archivePattern matches a published archive file's path below the archive
// folder, as the local events view's glob does.
const archivePattern = "*/*.parquet"

// UseEventArchiveBucket keeps the Event archive in a bucket instead of on
// local disk. Call it once, right after NewDuckDBWriter and UseBucket, and
// before any read or archive run. It doesn't reach the bucket, so that a
// bucket that is slow or down never holds up startup: it drops the events
// view, and Event reads fail with ErrEventsUnavailable until EnsureEventsView
// lists the bucket's archive files and builds the view.
func (w *DuckDBWriter) UseEventArchiveBucket(bucket ObjectStore) error {
	w.archiveMu.Lock()
	w.archiveBucket = bucket
	w.archiveMu.Unlock()
	return w.recreateEventsView()
}

// UseLocalEventArchiveOnly is UseEventArchiveBucket's counterpart, for when
// no bucket is configured: call it instead, at the same point. It warns when
// archive files whose local copies were deleted once uploaded are missing
// from local disk, as after an object_store block is removed from the
// config, since Event reads then miss their Events.
func (w *DuckDBWriter) UseLocalEventArchiveOnly() error {
	rows, err := w.db.Query("SELECT key FROM event_archive_in_bucket")
	if err != nil {
		return fmt.Errorf("list the Event archive files uploaded to a bucket: %w", err)
	}
	defer rows.Close()
	onlyInABucket := 0
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		file := filepath.Join(w.parquetPath, filepath.FromSlash(strings.TrimPrefix(key, eventArchivePrefix)))
		if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
			onlyInABucket++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if onlyInABucket > 0 {
		logger.L.Warn("Some Event archive files are only in a bucket, but no object_store is configured, so Event reads miss their Events: add the object_store block back to read them", "files", onlyInABucket)
	}
	return nil
}

// EnsureEventsView builds the events view if there is none that reads every
// archive file: with a bucket, it first lists the bucket's archive files if
// they haven't been listed, and it rebuilds the view if the last rebuild
// failed. Event reads fail with ErrEventsUnavailable until it succeeds. The
// archive consumer calls it through UploadEventArchive, when it starts and
// a minute after each failure.
func (w *DuckDBWriter) EnsureEventsView(ctx context.Context) error {
	w.archiveUploadMu.Lock()
	defer w.archiveUploadMu.Unlock()
	return w.ensureEventsView(ctx)
}

// ensureEventsView is EnsureEventsView. The caller holds archiveUploadMu.
func (w *DuckDBWriter) ensureEventsView(ctx context.Context) error {
	if w.archiveBucket != nil && !w.archiveListed() {
		if err := w.listEventArchive(ctx); err != nil {
			return err
		}
	} else if w.eventsUnavailable() == nil {
		return nil
	}
	return w.recreateEventsView()
}

// archiveListed reports whether the bucket's archive files have been listed.
func (w *DuckDBWriter) archiveListed() bool {
	w.archiveMu.Lock()
	defer w.archiveMu.Unlock()
	return w.remoteArchive != nil
}

// listEventArchive lists the bucket's archive files for the events view,
// logging a failure the first time in a row. A local file listed with a
// different size counts as not uploaded, so that it is read locally and
// uploaded again. The caller holds archiveUploadMu, and rebuilds the view.
func (w *DuckDBWriter) listEventArchive(ctx context.Context) error {
	listCtx, cancel := context.WithTimeout(ctx, archiveListTimeout)
	defer cancel()
	objects, err := w.archiveBucket.List(listCtx, eventArchivePrefix)
	if err != nil {
		if !w.archiveListFailing {
			logger.L.Error("Listing the Event archive in the bucket failed, retrying every minute; Event reads fail until it succeeds", "error", err)
		}
		w.archiveListFailing = true
		return fmt.Errorf("list the Event archive in the bucket: %w", err)
	}
	if w.archiveListFailing {
		logger.L.Info("Listed the Event archive in the bucket after failing")
		w.archiveListFailing = false
	}
	remote := make(map[string]int64, len(objects))
	for _, object := range objects {
		if ok, _ := path.Match(eventArchivePrefix+archivePattern, object.Key); ok {
			remote[object.Key] = object.Size
		}
	}
	local, err := w.localArchiveFiles()
	if err != nil {
		return err
	}
	for _, file := range local {
		key := w.archiveKey(file)
		size, listed := remote[key]
		if !listed {
			continue
		}
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		if info.Size() != size {
			logger.L.Warn("An Event archive file in the bucket differs from its local copy, uploading it again", "file", key, "bucket_bytes", size, "local_bytes", info.Size())
			delete(remote, key)
		}
	}
	w.archiveMu.Lock()
	w.remoteArchive = remote
	w.archiveMu.Unlock()
	return nil
}

// UploadEventArchive uploads every published archive file still on local
// disk, oldest first, and deletes each local copy once it is uploaded and no
// read may still open it. First it builds the events view if there is none
// (EnsureEventsView), listing the bucket if it hasn't been listed. A file
// that fails to upload is logged the first time it fails in a row and stays
// on local disk, where the events view reads it; the error returned says how
// many failed, so that the caller tries them again later. So does an error
// building the view, which keeps the local copies too. Without a bucket it
// only builds the view if there is none.
//
// Call it outside batchLock, so that Event processing never waits on the
// bucket.
func (w *DuckDBWriter) UploadEventArchive(ctx context.Context) error {
	w.archiveUploadMu.Lock()
	defer w.archiveUploadMu.Unlock()

	// A view that fails to build doesn't stop the uploads, as the bucket
	// may still take files; a listing that fails does.
	viewErr := w.ensureEventsView(ctx)
	if w.archiveBucket == nil || (viewErr != nil && !w.archiveListed()) {
		return viewErr
	}

	files, err := w.localArchiveFiles()
	if err != nil {
		return err
	}
	var uploaded []string
	failing := map[string]bool{}
	var first error
	for _, file := range files {
		key := w.archiveKey(file)
		if !w.isArchiveRemote(key) {
			err := w.uploadArchiveFile(ctx, key, file)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				if !w.archiveUploadFailing[key] {
					logger.L.Error("Uploading an Event archive file to the bucket failed, retrying every minute", "file", key, "error", err)
				}
				failing[key] = true
				if first == nil {
					first = err
				}
				continue
			}
			if w.archiveUploadFailing[key] {
				logger.L.Info("Uploaded an Event archive file to the bucket after failing", "file", key)
			}
		}
		uploaded = append(uploaded, file)
	}
	w.archiveUploadFailing = failing

	if len(uploaded) > 0 {
		viewErr = w.deleteUploadedArchiveFiles(uploaded)
	}
	if viewErr != nil {
		return viewErr
	}
	if first != nil {
		return fmt.Errorf("%d of %d Event archive files failed to upload, the first: %w", len(failing), len(files), first)
	}
	return nil
}

// uploadArchiveFile uploads a local archive file as key, and records it as in
// the bucket.
func (w *DuckDBWriter) uploadArchiveFile(ctx context.Context, key, file string) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	putCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	if err := w.archiveBucket.Put(putCtx, key, file); err != nil {
		return err
	}
	w.archiveMu.Lock()
	w.remoteArchive[key] = info.Size()
	w.archiveMu.Unlock()
	logger.L.Info("Uploaded an Event archive file to the bucket", "file", key)
	return nil
}

// deleteUploadedArchiveFiles makes the events view read the uploaded files
// from the bucket, waits for the reads that began before, which may still
// open their local copies, and then deletes those copies, with any date
// folder left empty. It records each file as only in the bucket first.
func (w *DuckDBWriter) deleteUploadedArchiveFiles(files []string) error {
	if err := w.recreateEventsView(); err != nil {
		return fmt.Errorf("recreate the events view: %w", err)
	}
	w.archiveReads.waitForEarlierReads()
	if err := w.recordArchiveInBucket(files); err != nil {
		return fmt.Errorf("record the Event archive files only in the bucket: %w", err)
	}

	var first error
	for _, file := range files {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			if first == nil {
				first = fmt.Errorf("delete the local copy of an uploaded Event archive file: %w", err)
			}
			continue
		}
		logger.L.Info("Deleted the local copy of an uploaded Event archive file", "file", w.archiveKey(file))
		// Fails, harmlessly, while the folder holds other files.
		_ = os.Remove(filepath.Dir(file))
	}
	return first
}

// recordArchiveInBucket records the keys of local archive files whose
// copies are about to be deleted, for UseLocalEventArchiveOnly.
func (w *DuckDBWriter) recordArchiveInBucket(files []string) error {
	values := make([]string, len(files))
	for i, file := range files {
		values[i] = "(" + sqltext.String(w.archiveKey(file)) + ")"
	}
	_, err := w.db.Exec("INSERT OR IGNORE INTO event_archive_in_bucket VALUES " + strings.Join(values, ", "))
	return err
}

// readingArchive holds off the deletion of uploaded archive files' local
// copies until the returned func is called. Every read of the events view
// calls it before it begins and the func once it ends.
func (w *DuckDBWriter) readingArchive() func() {
	return w.archiveReads.begin()
}

// readGenerations lets an upload wait for the reads that began before it
// rebuilt the events view, without holding up the reads that begin after,
// as a write lock would while a long SQL query runs. Its zero value is ready.
type readGenerations struct {
	mu sync.Mutex
	// current counts the reads begun since the last wait.
	current *sync.WaitGroup
}

// begin records a read, and returns the func that records its end.
func (g *readGenerations) begin() func() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current == nil {
		g.current = &sync.WaitGroup{}
	}
	g.current.Add(1)
	return g.current.Done
}

// waitForEarlierReads waits until every read begun before it was called has
// ended. Calls must not overlap: archiveUploadMu serialises them, so the
// reads of earlier generations have ended already.
func (g *readGenerations) waitForEarlierReads() {
	g.mu.Lock()
	earlier := g.current
	g.current = &sync.WaitGroup{}
	g.mu.Unlock()
	if earlier != nil {
		earlier.Wait()
	}
}

// isArchiveRemote reports whether the bucket has the archive file of key.
func (w *DuckDBWriter) isArchiveRemote(key string) bool {
	w.archiveMu.Lock()
	defer w.archiveMu.Unlock()
	_, ok := w.remoteArchive[key]
	return ok
}

// archiveFiles is what the events view reads with a bucket: each archive
// file once, from the bucket if it is there, else from local disk. The caller
// holds archiveMu.
func (w *DuckDBWriter) archiveFiles() ([]string, error) {
	local, err := w.localArchiveFiles()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(w.remoteArchive))
	for key := range w.remoteArchive {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	files := make([]string, 0, len(keys)+len(local))
	for _, key := range keys {
		files = append(files, w.archiveBucket.URL(key))
	}
	for _, file := range local {
		if _, ok := w.remoteArchive[w.archiveKey(file)]; !ok {
			files = append(files, file)
		}
	}
	return files, nil
}

// localArchiveFiles lists the published archive files on local disk, oldest
// date first.
func (w *DuckDBWriter) localArchiveFiles() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(w.parquetPath, filepath.FromSlash(archivePattern)))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	return files, nil
}

// archiveKey is the key in the bucket of a local archive file, which is at
// archivePattern below the archive folder.
func (w *DuckDBWriter) archiveKey(file string) string {
	return eventArchivePrefix + filepath.Base(filepath.Dir(file)) + "/" + filepath.Base(file)
}

// sqlList is a DuckDB list literal of strings.
func sqlList(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = sqltext.String(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
