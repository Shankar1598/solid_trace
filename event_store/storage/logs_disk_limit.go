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

// EnforceDiskLimit deletes the oldest hours of Log files, of any Project,
// until the files on local disk, as the manifest lists them, take no more
// than limit. Logs not yet exported are never deleted. If a read is reading
// the oldest hour, it stops there with ErrLogFilesInUse, and the next pass
// goes on. Each pass first deletes the bytes of files deleted earlier whose
// reads have ended since.
func (s *LogStore) EnforceDiskLimit(limit LogDiskLimit) error {
	maxBytes := limit.Bytes
	if maxBytes <= 0 {
		return fmt.Errorf("no Log disk limit: %d bytes", maxBytes)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	earlier := s.deleteTombstonedFiles()
	if earlier != nil && !errors.Is(earlier, ErrLogFilesInUse) {
		return earlier
	}
	hours, total, err := s.localLogHours()
	if err != nil {
		return err
	}
	if total <= maxBytes {
		return earlier
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
			logger.L.Info("Deleting the oldest Log files past the disk limit",
				"project_id", hour.projectID, "hour", hour.hour, "bytes", hour.bytes, "limit", maxBytes)
			if err := tombstoneLogFiles(ctx, conn, deletedForTheDiskLimit,
				"project_id = ? AND day = CAST(? AS DATE) AND hour = ? AND local",
				hour.projectID, dayString(LogDay(hour.hour)), hour.hour.Hour()); err != nil {
				return err
			}
			total -= hour.bytes
		}
		return nil
	}, func() {})
	if err != nil {
		return err
	}
	if err := s.deleteTombstonedFiles(); err != nil {
		return err
	}
	return stopped
}

// logHour is one Project's hour of Log files on local disk.
type logHour struct {
	projectID uint32
	hour      time.Time
	// bytes is the size of the hour's files, and keys their keys.
	bytes int64
	keys  []string
}

// localLogHours returns every Project's hours of Log files on local disk,
// oldest first, and the size of all their files.
func (s *LogStore) localLogHours() ([]logHour, int64, error) {
	rows, err := s.db.Query("SELECT " + manifestColumnsOfAKey + ", bytes FROM logs.log_files WHERE local")
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
