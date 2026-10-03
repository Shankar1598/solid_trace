package storage

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

// freeDiskShare is the share of the disk holding the Log files that the
// default disk limit keeps free.
const freeDiskShare = 10 // percent

// LogDiskLimit caps how much disk all Projects' Log files take together.
type LogDiskLimit struct {
	// Bytes is the cap. If it is 0 or less, the Log files may grow until the
	// disk that holds them has 10% of its space left, whatever else fills it.
	Bytes int64
}

// EnforceDiskLimit deletes the oldest hours of Log files, of any Project,
// until the files take no more than limit. Logs not yet exported are never
// deleted. If a read is reading the oldest hour, it stops there with
// ErrLogFilesInUse, and the next pass goes on.
func (s *LogStore) EnforceDiskLimit(limit LogDiskLimit) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	hours, total, err := s.files.hourDirs()
	if err != nil {
		return fmt.Errorf("list log files: %w", err)
	}
	maxBytes := limit.Bytes
	if maxBytes <= 0 {
		if maxBytes, err = defaultLogDiskLimit(s.files.Dir, total); err != nil {
			return err
		}
	}
	if total <= maxBytes {
		return nil
	}

	// No read begins meanwhile, and none sees a file vanish under it.
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	slices.SortFunc(hours, func(a, b logHourDir) int {
		return cmp.Or(a.hour.Compare(b.hour), cmp.Compare(a.projectID, b.projectID))
	})
	for _, hour := range hours {
		if total <= maxBytes {
			break
		}
		logger.L.Info("Deleting the oldest Log files past the disk limit",
			"project_id", hour.projectID, "hour", hour.hour, "bytes", hour.bytes, "limit", maxBytes)
		if err := s.removeUnread(hour.dir); err != nil {
			return err
		}
		total -= hour.bytes
		// Gone only once its last hour is.
		_ = os.Remove(filepath.Dir(hour.dir))
	}
	return nil
}

// defaultLogDiskLimit is how large the Log files in dir, now logBytes, may
// grow before the disk holding them has freeDiskShare of its space left.
func defaultLogDiskLimit(dir string, logBytes int64) (int64, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		return 0, fmt.Errorf("read free space of %s: %w", dir, err)
	}
	size := int64(fs.Blocks) * int64(fs.Bsize)
	free := int64(fs.Bavail) * int64(fs.Bsize)
	return max(logBytes+free-size*freeDiskShare/100, 0), nil
}

// logHourDir is one Project's hour folder of Log files.
type logHourDir struct {
	projectID uint32
	hour      time.Time
	dir       string
	bytes     int64
}

// hourDirs returns every Project's hour folders that hold Log files, with
// the size of their files, and the size of all of them.
func (f LogFiles) hourDirs() ([]logHourDir, int64, error) {
	files, err := filepath.Glob(filepath.Join(f.Dir, "project=*", "day=*", "hour=*", "*"+parquetSuffix))
	if err != nil {
		return nil, 0, err
	}
	byDir := make(map[string]*logHourDir)
	var total int64
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return nil, 0, err
		}
		dir := filepath.Dir(file)
		hour, ok := byDir[dir]
		if !ok {
			hour, ok = parseHourDir(dir)
			if !ok {
				continue // not a folder the export writes
			}
			byDir[dir] = hour
		}
		hour.bytes += info.Size()
		total += info.Size()
	}
	hours := make([]logHourDir, 0, len(byDir))
	for _, hour := range byDir {
		hours = append(hours, *hour)
	}
	return hours, total, nil
}

// parseHourDir reads the Project and hour of a folder
// <Dir>/project=<id>/day=<YYYY-MM-DD>/hour=<HH>.
func parseHourDir(dir string) (*logHourDir, bool) {
	dayDir := filepath.Dir(dir)
	id, err := strconv.ParseUint(strings.TrimPrefix(filepath.Base(filepath.Dir(dayDir)), "project="), 10, 32)
	if err != nil {
		return nil, false
	}
	day, err := time.Parse(time.DateOnly, strings.TrimPrefix(filepath.Base(dayDir), "day="))
	if err != nil {
		return nil, false
	}
	h, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(dir), "hour="))
	if err != nil || h < 0 || h > 23 {
		return nil, false
	}
	return &logHourDir{projectID: uint32(id), hour: day.Add(time.Duration(h) * time.Hour), dir: dir}, true
}
