package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

// Log files in a bucket (ADR 0008). With an ObjectStore, each published Log
// file is uploaded at once, under its key, and its local copy stays until
// localCacheHours after its hour ends; then it is evicted. A manifest row's
// local and remote say where the file's bytes are, and a read takes each
// file from its local path while it has one, else from the bucket.
//
// Restarts settle what a crash leaves:
//   - between an upload and marking it remote, the next start uploads the
//     file again, to the same key with the same bytes;
//   - between evicting a file and marking it not local, the next start
//     finds the row local with no file, and marks it not local
//     (settleEvictions).

const (
	// uploadTimeout bounds one file's upload, so that a stalled request
	// doesn't hold up the files after it for good.
	uploadTimeout = 15 * time.Minute
	// deleteObjectTimeout bounds the deletion of one object.
	deleteObjectTimeout = time.Minute
	// evictBatch is how many local copies one commit evicts at most, so
	// that no read waits long to begin.
	evictBatch = 500
)

// UseObjectStore keeps the Log files in a bucket as well as on local disk,
// each file's local copy until localCacheHours after its hour ends. Call it
// once, right after NewLogStore and before any read, upload or Cleanup.
// Without it, the files stay on local disk only.
//
// It settles evictions a crash cut short.
func (s *LogStore) UseObjectStore(bucket ObjectStore, localCacheHours int) error {
	s.bucket, s.localCacheHours = bucket, localCacheHours
	return s.settleEvictions()
}

// LogFilesPublished receives a signal after an export publishes files, which
// UploadLogFiles uploads.
func (s *LogStore) LogFilesPublished() <-chan struct{} {
	return s.published
}

// notifyPublished signals LogFilesPublished, without waiting: one signal
// pending is enough, as an upload takes every file not yet uploaded.
func (s *LogStore) notifyPublished() {
	select {
	case s.published <- struct{}{}:
	default:
	}
}

// location is where a read reads a listed file: its local path while it has
// one, else its URL in the bucket.
func (s *LogStore) location(file logFile) string {
	if file.local || s.bucket == nil {
		return s.files.path(file)
	}
	return s.bucket.URL(file.key())
}

// UploadLogFiles uploads every published Log file not yet in the bucket,
// oldest first, and marks each one remote. A file that fails to upload is
// logged the first time it fails in a row, and the error returned names how
// many failed, so that the caller tries them again later. It does nothing
// without a bucket.
//
// Uploads run outside writeMu and publishMu; only marking a file remote takes
// writeMu.
func (s *LogStore) UploadLogFiles(ctx context.Context) error {
	if s.bucket == nil {
		return nil
	}
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()

	rows, err := s.db.QueryContext(ctx, "SELECT "+manifestColumnsOfAKey+" FROM logs.log_files WHERE local AND NOT remote")
	if err != nil {
		return fmt.Errorf("list log files to upload: %w", err)
	}
	files, err := scanLogFiles(rows)
	if err != nil {
		return err
	}
	sortLogFiles(files)

	failing := map[string]bool{}
	var first error
	for _, file := range files {
		err := s.uploadLogFile(ctx, file)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		key := file.key()
		if err != nil {
			if !s.uploadFailing[key] {
				logger.L.Error("Uploading a Log file to the bucket failed, retrying every minute", "file", key, "error", err)
			}
			failing[key] = true
			if first == nil {
				first = err
			}
			continue
		}
		if s.uploadFailing[key] {
			logger.L.Info("Uploaded a Log file to the bucket after failing", "file", key)
		}
	}
	s.uploadFailing = failing
	if first != nil {
		return fmt.Errorf("%d of %d log files failed to upload, the first: %w", len(failing), len(files), first)
	}
	return nil
}

// uploadLogFile uploads a file and marks it remote. It pins the file while it
// uploads, and only then checks the file is still listed: a deletion that
// takes its row meanwhile then leaves its bytes, here and in the bucket, for
// a pass after the upload, and one that took it before isn't uploaded.
func (s *LogStore) uploadLogFile(ctx context.Context, file logFile) error {
	key := file.key()
	s.pin([]string{key})
	defer s.unpin([]string{key})
	listed, err := s.logFileListed(file)
	if err != nil || !listed {
		return err
	}
	putCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	if err := s.bucket.Put(putCtx, key, s.files.path(file)); err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.db.ExecContext(ctx, "UPDATE logs.log_files SET remote = true WHERE "+manifestConditionOfAKey, file.keyArgs()...); err != nil {
		return fmt.Errorf("mark %s uploaded: %w", key, err)
	}
	return nil
}

// EvictLogFiles removes the local copies of uploaded Log files whose hour
// ended localCacheHours or more before now; reads take them from the bucket
// from then on. A file a read has pinned keeps its copy, and
// ErrLogFilesInUse says a pass should come again soon. It does nothing
// without a bucket.
func (s *LogStore) EvictLogFiles(now time.Time) error {
	if s.bucket == nil {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.db.Query(`
		SELECT `+manifestColumnsOfAKey+` FROM logs.log_files
		WHERE local AND remote AND day + to_hours(CAST(hour AS BIGINT) + 1 + ?) <= CAST(? AS TIMESTAMP)`,
		s.localCacheHours, timestampString(now))
	if err != nil {
		return fmt.Errorf("list log files to evict: %w", err)
	}
	files, err := scanLogFiles(rows)
	if err != nil {
		return err
	}
	sortLogFiles(files)
	_, pinned, err := s.evict(files)
	if err != nil {
		return err
	}
	return filesInUse(pinned)
}

// evict removes the local copies of uploaded files and marks them not local,
// with no read beginning meanwhile: a read that began before has pinned the
// files it reads, and one that begins after reads them from the bucket. A
// pinned file keeps its copy and is returned with pinned; evicted are the
// others. The caller holds writeMu.
//
// A copy removed whose row a failed commit leaves local is removed again,
// harmlessly, by the next eviction that lists it, which then marks it.
func (s *LogStore) evict(files []logFile) (evicted, pinned []logFile, err error) {
	for len(files) > 0 {
		batch := files[:min(len(files), evictBatch)]
		files = files[len(batch):]
		done, held, err := s.evictBatch(batch)
		evicted, pinned = append(evicted, done...), append(pinned, held...)
		if err != nil {
			return evicted, pinned, err
		}
	}
	return evicted, pinned, nil
}

func (s *LogStore) evictBatch(files []logFile) (evicted, pinned []logFile, err error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return nil, nil, err
	}
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	var removeErr error
	for _, file := range files {
		if s.pinned(file.key()) {
			pinned = append(pinned, file)
			continue
		}
		path := s.files.path(file)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			removeErr = fmt.Errorf("evict %s: %w", path, err)
			break
		}
		s.removeEmptyFolders(path)
		if _, err := conn.ExecContext(ctx, "UPDATE logs.log_files SET local = false WHERE "+manifestConditionOfAKey, file.keyArgs()...); err != nil {
			if _, rollbackErr := conn.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
				discardConn(conn)
			}
			return nil, pinned, fmt.Errorf("mark %s evicted: %w", file.key(), err)
		}
		evicted = append(evicted, file)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		discardConn(conn)
		return nil, pinned, err
	}
	return evicted, pinned, removeErr
}

// removeEmptyFolders removes each folder above a removed file's path once it
// is empty, up to the files folder.
func (s *LogStore) removeEmptyFolders(path string) {
	for dir := filepath.Dir(path); dir != filepath.Clean(s.files.Dir); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
}

// settleEvictions settles, at startup, rows that say a file is local when its
// local copy is gone: a crash between evicting a file and marking it.
// Uploaded, it is marked not local, and reads take it from the bucket. Not
// uploaded, its bytes are lost, which is logged as an error; its row stays,
// so reads of its hour fail on it, as they did before the bucket.
func (s *LogStore) settleEvictions() error {
	notUploaded, err := s.listLocalLogFiles("NOT remote")
	if err != nil {
		return err
	}
	for _, file := range notUploaded {
		if path := s.files.path(file); isMissing(path) {
			logger.L.Error("A Log file not yet uploaded is missing from local disk, so reads of its hour fail", "file", path)
		}
	}
	uploaded, err := s.listLocalLogFiles("remote")
	if err != nil {
		return err
	}
	var gone []logFile
	for _, file := range uploaded {
		if isMissing(s.files.path(file)) {
			gone = append(gone, file)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	for _, file := range gone {
		if _, err := conn.ExecContext(ctx, "UPDATE logs.log_files SET local = false WHERE "+manifestConditionOfAKey, file.keyArgs()...); err != nil {
			if _, rollbackErr := conn.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
				discardConn(conn)
			}
			return fmt.Errorf("mark %s evicted: %w", file.key(), err)
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		discardConn(conn)
		return err
	}
	logger.L.Info("Marked uploaded Log files whose eviction a restart cut short as evicted", "files", len(gone))
	return nil
}

// listLocalLogFiles lists the files the manifest has on local disk that meet
// where, oldest first.
func (s *LogStore) listLocalLogFiles(where string) ([]logFile, error) {
	rows, err := s.db.Query("SELECT " + manifestColumnsOfAKey + " FROM logs.log_files WHERE local AND " + where)
	if err != nil {
		return nil, fmt.Errorf("list local log files: %w", err)
	}
	files, err := scanLogFiles(rows)
	sortLogFiles(files)
	return files, err
}

// isMissing reports whether nothing is at path.
func isMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// deleteTombstonedObjects deletes from the bucket, one at a time, oldest
// first, the object of every tombstoned file no read or upload has pinned
// whose local bytes are gone, then its tombstone: a tombstone goes only once
// both copies are gone. deleteTombstonedFiles deletes the local bytes first.
//
// It stops at the first object that fails to delete, as the bucket is then
// most likely down, and leaves the rest with their tombstones for the next
// pass: a pass waits on a failing bucket for one deleteObjectTimeout at
// most. The failure is logged the first time a pass stops, until a pass
// gets through; it returns only the manifest's errors.
//
// It must run with neither writeMu nor publishMu held, as it waits on the
// bucket; it takes writeMu only to drop the tombstones.
func (s *LogStore) deleteTombstonedObjects() error {
	if s.bucket == nil {
		return nil
	}
	s.objectsMu.Lock()
	defer s.objectsMu.Unlock()
	rows, err := s.db.Query("SELECT DISTINCT " + manifestColumnsOfAKey + " FROM logs.log_file_deletions")
	if err != nil {
		return fmt.Errorf("list tombstoned log files: %w", err)
	}
	files, err := scanLogFiles(rows)
	if err != nil {
		return err
	}
	sortLogFiles(files)
	var (
		deleted []logFile
		failed  error
	)
	for _, file := range files {
		if s.pinned(file.key()) {
			continue
		}
		if !isMissing(s.files.path(file)) {
			continue // deleteTombstonedFiles deletes these bytes first
		}
		ctx, cancel := context.WithTimeout(context.Background(), deleteObjectTimeout)
		err := s.bucket.Delete(ctx, file.key())
		cancel()
		if err != nil {
			failed = fmt.Errorf("delete %s from the bucket: %w", file.key(), err)
			break
		}
		deleted = append(deleted, file)
	}
	switch {
	case failed != nil && !s.objectsFailing:
		logger.L.Error("Deleting deleted Log files from the bucket failed, retrying on each Cleanup pass", "error", failed)
	case failed == nil && s.objectsFailing:
		logger.L.Info("Deleted deleted Log files from the bucket after failing", "files", len(deleted))
	}
	s.objectsFailing = failed != nil
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.dropTombstones(deleted)
}
