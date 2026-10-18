package storage

import (
	"fmt"
	"os"
	"path/filepath"
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
// the export that wrote it has committed. The manifest (logs_manifest.go)
// lists the published files; nothing lists the folders but the recovery of
// staged files.
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

// lateFileNumber is n for late-<n>.parquet, and 0 for any other name.
func lateFileNumber(name string) int {
	name, ok := strings.CutPrefix(name, lateFilePrefix)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSuffix(name, parquetSuffix))
	if err != nil {
		return 0
	}
	return n
}

// stagedFiles returns every staged Log file by its path, with the file it
// becomes once published.
func (f LogFiles) stagedFiles() (map[string]logFile, error) {
	paths, err := filepath.Glob(filepath.Join(f.Dir, "project=*", "day=*", "hour=*", "*"+parquetSuffix+stagedSuffix))
	if err != nil {
		return nil, err
	}
	staged := make(map[string]logFile, len(paths))
	for _, path := range paths {
		file, ok := f.parsePath(strings.TrimSuffix(path, stagedSuffix))
		if !ok {
			continue // not a file the export writes
		}
		staged[path] = file
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
