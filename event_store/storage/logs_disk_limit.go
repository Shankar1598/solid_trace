package storage

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"syscall"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

// defaultDiskShare is the share of the disk holding the Log files that they
// may take when no limit is configured. Logs are the largest thing on the
// disk, but Pebble, the Event databases and their archive, the hot tables and
// the system share it; half leaves them room to grow, and doesn't move when
// they do.
const defaultDiskShare = 50 // percent

// LogDiskLimit caps how much disk all Projects' Log files take together.
type LogDiskLimit struct {
	// Bytes is the cap, more than 0.
	Bytes int64
}

// NewLogDiskLimit is the disk limit for the Log files in dir: configured
// bytes if more than 0, else defaultDiskShare of the size of the disk that
// holds dir, read once now.
func NewLogDiskLimit(configured int64, dir string) (LogDiskLimit, error) {
	if configured > 0 {
		return LogDiskLimit{Bytes: configured}, nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return LogDiskLimit{}, err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		return LogDiskLimit{}, fmt.Errorf("read the size of the disk holding %s: %w", dir, err)
	}
	size := int64(fs.Blocks) * int64(fs.Bsize)
	return LogDiskLimit{Bytes: size * defaultDiskShare / 100}, nil
}

// EnforceDiskLimit keeps the Log files on local disk, as the manifest lists
// them, within limit.
//
// With a bucket, it first evicts the local copies of uploaded files, oldest
// first, whatever their age; a file a read has pinned keeps its copy. Only
// when no uploaded copy is left to evict does it delete Logs, and then only
// of files not yet uploaded, which it logs as an error.
//
// It deletes the oldest hours of files, of any Project. Logs not yet exported
// are never deleted. If a read is reading the oldest hour, it stops there
// with ErrLogFilesInUse, and the next pass goes on. Each pass first deletes
// the bytes of files deleted earlier, by any deleter, whose reads have ended
// since, and returns ErrLogFilesInUse while a read holds any of them, so that
// the next pass comes soon.
//
// With a bucket, each pass then deletes the objects of files deleted
// earlier, by any deleter (deleteTombstonedObjects). An object that fails to
// delete is logged, and it and the rest wait for the next pass; it fails
// nothing.
func (s *LogStore) EnforceDiskLimit(limit LogDiskLimit) error {
	maxBytes := limit.Bytes
	if maxBytes <= 0 {
		return fmt.Errorf("no Log disk limit: %d bytes", maxBytes)
	}
	err := s.enforceDiskLimit(maxBytes)
	if objectsErr := s.deleteTombstonedObjects(); objectsErr != nil && (err == nil || errors.Is(err, ErrLogFilesInUse)) {
		return objectsErr
	}
	return err
}

func (s *LogStore) enforceDiskLimit(maxBytes int64) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	pinned, err := s.deleteTombstonedFiles()
	if err != nil {
		return err
	}
	earlier := filesInUse(pinned)
	hours, total, err := s.localLogHours("true")
	if err != nil {
		return err
	}
	if total <= maxBytes {
		return earlier
	}

	if s.bucket != nil {
		if total, err = s.evictForTheDiskLimit(total, maxBytes); err != nil {
			return err
		}
		if total <= maxBytes {
			return earlier
		}
		// Every uploaded copy is evicted, so what is left on disk is not
		// in the bucket yet.
		if hours, _, err = s.localLogHours("NOT remote"); err != nil {
			return err
		}
	}

	var stopped error
	err = s.commitDeletion(func(ctx context.Context, conn *sql.Conn) error {
		for _, hour := range hours {
			if total <= maxBytes {
				return nil
			}
			// A read that begins after this check pins the files, and their
			// bytes wait for it.
			for _, key := range hour.keys {
				if s.pinned(key) {
					stopped = fmt.Errorf("delete %s: %w", key, ErrLogFilesInUse)
					return nil
				}
			}
			where := "project_id = ? AND day = CAST(? AS DATE) AND hour = ? AND local"
			if s.bucket == nil {
				logger.L.Info("Deleting the oldest Log files past the disk limit",
					"project_id", hour.projectID, "hour", hour.hour, "bytes", hour.bytes, "limit", maxBytes)
			} else {
				where += " AND NOT remote"
				logger.L.Error("Deleting the oldest Log files not yet uploaded to the bucket, as nothing uploaded is left on disk to evict and the files are past the disk limit",
					"project_id", hour.projectID, "hour", hour.hour, "bytes", hour.bytes, "limit", maxBytes)
			}
			if err := tombstoneLogFiles(ctx, conn, deletedForTheDiskLimit, where,
				hour.projectID, dayString(LogDay(hour.hour)), hour.hour.Hour()); err != nil {
				return err
			}
			total -= hour.bytes
		}
		return nil
	})
	if err != nil {
		return err
	}
	if pinned, err = s.deleteTombstonedFiles(); err != nil {
		return err
	}
	if stopped != nil {
		return stopped
	}
	return filesInUse(pinned)
}

// evictForTheDiskLimit evicts the local copies of uploaded files, oldest
// first, until the files on local disk take no more than maxBytes, and
// returns what they take then. It skips copies a read has pinned; if the
// files still take more than maxBytes after that, it returns
// ErrLogFilesInUse, so that no Logs are deleted while an uploaded copy is
// left to evict once the read ends. The caller holds writeMu.
func (s *LogStore) evictForTheDiskLimit(total, maxBytes int64) (int64, error) {
	rows, err := s.db.Query("SELECT " + manifestColumnsOfAKey + ", bytes FROM logs.log_files WHERE local AND remote")
	if err != nil {
		return total, fmt.Errorf("list uploaded log files: %w", err)
	}
	defer rows.Close()
	type sizedFile struct {
		file  logFile
		bytes int64
	}
	var uploaded []sizedFile
	for rows.Next() {
		var (
			f    sizedFile
			day  time.Time
			hour int
		)
		if err := rows.Scan(&f.file.projectID, &day, &hour, &f.file.name, &f.bytes); err != nil {
			return total, err
		}
		f.file.hour = day.UTC().Add(time.Duration(hour) * time.Hour)
		uploaded = append(uploaded, f)
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	slices.SortFunc(uploaded, func(a, b sizedFile) int {
		return cmp.Or(a.file.hour.Compare(b.file.hour), cmp.Compare(a.file.projectID, b.file.projectID),
			cmp.Compare(lateFileNumber(a.file.name), lateFileNumber(b.file.name)), cmp.Compare(a.file.name, b.file.name))
	})

	// Copies are chosen until enough would be freed, past those pinned
	// now; evict skips any a read pins meanwhile.
	bytes := map[string]int64{}
	var chosen, pinned []logFile
	left := total
	for _, f := range uploaded {
		if left <= maxBytes {
			break
		}
		if s.pinned(f.file.key()) {
			pinned = append(pinned, f.file)
			continue
		}
		bytes[f.file.key()] = f.bytes
		chosen = append(chosen, f.file)
		left -= f.bytes
	}
	evicted, pinnedMeanwhile, err := s.evict(chosen)
	pinned = append(pinned, pinnedMeanwhile...)
	for _, file := range evicted {
		total -= bytes[file.key()]
	}
	if err != nil {
		return total, err
	}
	if total > maxBytes && len(pinned) > 0 {
		logger.L.Info("Evicting the oldest uploaded Log files past the disk limit waits for a read", "file", pinned[0].key())
		return total, filesInUse(pinned)
	}
	if len(evicted) > 0 {
		logger.L.Info("Evicted the oldest uploaded Log files past the disk limit", "files", len(evicted), "limit", maxBytes)
	}
	return total, nil
}

// logHour is one Project's hour of Log files on local disk.
type logHour struct {
	projectID uint32
	hour      time.Time
	// bytes is the size of the hour's files, and keys their keys.
	bytes int64
	keys  []string
}

// localLogHours returns every Project's hours of Log files on local disk that
// meet where, oldest first, and the size of all those files.
func (s *LogStore) localLogHours(where string) ([]logHour, int64, error) {
	rows, err := s.db.Query("SELECT " + manifestColumnsOfAKey + ", bytes FROM logs.log_files WHERE local AND " + where)
	if err != nil {
		return nil, 0, fmt.Errorf("list log files: %w", err)
	}
	defer rows.Close()
	byHour := make(map[string]*logHour)
	var total int64
	for rows.Next() {
		var (
			file  logFile
			day   time.Time
			hour  int
			bytes int64
		)
		if err := rows.Scan(&file.projectID, &day, &hour, &file.name, &bytes); err != nil {
			return nil, 0, err
		}
		file.hour = day.UTC().Add(time.Duration(hour) * time.Hour)
		dir := path.Dir(file.key())
		h, ok := byHour[dir]
		if !ok {
			h = &logHour{projectID: file.projectID, hour: file.hour}
			byHour[dir] = h
		}
		h.bytes += bytes
		h.keys = append(h.keys, file.key())
		total += bytes
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	hours := make([]logHour, 0, len(byHour))
	for _, h := range byHour {
		hours = append(hours, *h)
	}
	slices.SortFunc(hours, func(a, b logHour) int {
		return cmp.Or(a.hour.Compare(b.hour), cmp.Compare(a.projectID, b.projectID))
	})
	return hours, total, nil
}
