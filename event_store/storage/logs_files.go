package storage

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// LogFiles is where exported Logs live on local disk: one folder per Project,
// UTC day and hour,
//
//	<Dir>/project=<id>/day=<YYYY-MM-DD>/hour=<HH>/
//
// holding the hour's data.parquet, written by the hourly export, and then
// late-1.parquet, late-2.parquet … for Logs of that hour that came late.
// Every Log in an hour's files has a time inside that hour.
//
// A file is written under a staged name, <name>.tmp, and renamed only once
// the export that wrote it has committed. Readers see only *.parquet.
type LogFiles struct {
	Dir string
}

const (
	hourDataFile   = "data.parquet"
	lateFilePrefix = "late-"
	parquetSuffix  = ".parquet"
	// stagedSuffix marks a Log file whose export may not have committed.
	stagedSuffix = ".tmp"
)

// ProjectDir is the folder of every exported Log of a Project.
func (f LogFiles) ProjectDir(projectID uint32) string {
	return filepath.Join(f.Dir, fmt.Sprintf("project=%d", projectID))
}

// DayDir is the folder of a Project's exported Logs of the UTC day of t.
func (f LogFiles) DayDir(projectID uint32, t time.Time) string {
	return filepath.Join(f.ProjectDir(projectID), "day="+dayString(LogDay(t)))
}

// HourDir is the folder of a Project's exported Logs of the UTC hour of t.
func (f LogFiles) HourDir(projectID uint32, t time.Time) string {
	return filepath.Join(f.DayDir(projectID, t), fmt.Sprintf("hour=%02d", t.UTC().Hour()))
}

// Hour returns the files that hold a Project's exported Logs of the UTC hour
// of t: data.parquet, if the hour has been exported, then its late files in
// the order they were written. Staged files are left out.
func (f LogFiles) Hour(projectID uint32, t time.Time) ([]string, error) {
	return publishedFiles(filepath.Join(f.HourDir(projectID, t), "*"+parquetSuffix))
}

// Project returns every file that holds a Project's exported Logs, oldest
// hour first.
func (f LogFiles) Project(projectID uint32) ([]string, error) {
	return publishedFiles(filepath.Join(f.ProjectDir(projectID), "day=*", "hour=*", "*"+parquetSuffix))
}

// publishedFiles returns the files matching a pattern of *.parquet files,
// sorted by folder, then data.parquet, then late files by number.
func publishedFiles(pattern string) ([]string, error) {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(filepath.Dir(a), filepath.Dir(b)),
			cmp.Compare(lateFileNumber(a), lateFileNumber(b)),
			cmp.Compare(a, b),
		)
	})
	return files, nil
}

// lateFileNumber is n for late-<n>.parquet, and 0 for any other file.
func lateFileNumber(path string) int {
	name, ok := strings.CutPrefix(filepath.Base(path), lateFilePrefix)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSuffix(name, parquetSuffix))
	if err != nil {
		return 0
	}
	return n
}

// nextHourFile is the name the next file of an hour's folder gets:
// data.parquet if the hour has none, else the next late file.
func nextHourFile(dir string) (string, error) {
	files, err := publishedFiles(filepath.Join(dir, "*"+parquetSuffix))
	if err != nil {
		return "", err
	}
	if !slices.Contains(files, filepath.Join(dir, hourDataFile)) {
		return hourDataFile, nil
	}
	last := 0
	for _, file := range files {
		last = max(last, lateFileNumber(file))
	}
	return fmt.Sprintf("%s%d%s", lateFilePrefix, last+1, parquetSuffix), nil
}

// stagedFiles returns every staged Log file, with the Project it holds Logs
// of.
func (f LogFiles) stagedFiles() (map[string]uint32, error) {
	files, err := filepath.Glob(filepath.Join(f.Dir, "project=*", "day=*", "hour=*", "*"+parquetSuffix+stagedSuffix))
	if err != nil {
		return nil, err
	}
	staged := make(map[string]uint32, len(files))
	for _, file := range files {
		project := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(file))))
		id, err := strconv.ParseUint(strings.TrimPrefix(project, "project="), 10, 32)
		if err != nil {
			continue // not a folder the export writes
		}
		staged[file] = uint32(id)
	}
	return staged, nil
}

// syncFile flushes a written file to disk.
func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
