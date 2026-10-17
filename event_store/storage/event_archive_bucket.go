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
// view lists every archive file by name: those in the bucket, listed once at
// startup and added to after each upload, and the local files still waiting
// for theirs.
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
// before any read or archive run. It lists the bucket's archive files for the
// events view. If the listing fails, it logs why and the view is left out, so
// Event reads fail rather than miss archived Events, until an upload pass
// (UploadEventArchive) lists the bucket.
func (w *DuckDBWriter) UseEventArchiveBucket(ctx context.Context, bucket ObjectStore) error {
	w.archiveMu.Lock()
	w.archiveBucket = bucket
	w.archiveMu.Unlock()
	w.archiveUploadMu.Lock()
	defer w.archiveUploadMu.Unlock()
	_ = w.listEventArchive(ctx)
	return w.recreateEventsView()
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
// read may still open it. If the bucket hasn't been listed yet, it lists it
// first. A file that fails to upload is logged the first time it fails in a
// row and stays on local disk, where the events view reads it; the error
// returned says how many failed, so that the caller tries them again later.
// It does nothing without a bucket.
//
// Call it outside batchLock, so that Event processing never waits on the
// bucket.
func (w *DuckDBWriter) UploadEventArchive(ctx context.Context) error {
	w.archiveMu.Lock()
	bucket, listed := w.archiveBucket, w.remoteArchive != nil
	w.archiveMu.Unlock()
	if bucket == nil {
		return nil
	}
	w.archiveUploadMu.Lock()
	defer w.archiveUploadMu.Unlock()

	if !listed {
		if err := w.listEventArchive(ctx); err != nil {
			return err
		}
		if err := w.recreateEventsView(); err != nil {
			return fmt.Errorf("recreate the events view: %w", err)
		}
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

	if err := w.deleteUploadedArchiveFiles(uploaded); err != nil {
		return err
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
	return nil
}

// deleteUploadedArchiveFiles makes the events view read the uploaded files
// from the bucket, waits for the reads that began before, which may still
// open their local copies, and then deletes those copies, with any date
// folder left empty.
func (w *DuckDBWriter) deleteUploadedArchiveFiles(files []string) error {
	if len(files) == 0 {
		return nil
	}
	if err := w.recreateEventsView(); err != nil {
		return fmt.Errorf("recreate the events view: %w", err)
	}
	w.archiveReads.waitForEarlierReads()

	var first error
	for _, file := range files {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			if first == nil {
				first = fmt.Errorf("delete the local copy of an uploaded Event archive file: %w", err)
			}
			continue
		}
		// Fails, harmlessly, while the folder holds other files.
		_ = os.Remove(filepath.Dir(file))
	}
	return first
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
