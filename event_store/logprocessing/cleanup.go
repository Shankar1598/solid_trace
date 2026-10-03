package logprocessing

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// CleanupStore deletes stored Logs. *storage.LogStore is the production
// adapter.
type CleanupStore interface {
	// LogProjects lists the Projects that have any Logs stored.
	LogProjects() ([]uint32, error)
	// DeleteLogDaysBefore deletes a Project's Logs of the UTC days before
	// that of day, with those days' promoted sets and counts.
	DeleteLogDaysBefore(projectID uint32, day time.Time) error
	// DeleteProjectLogs deletes everything of a Project's Logs.
	DeleteProjectLogs(projectID uint32) error
	// EnforceDiskLimit deletes the oldest hours of Log files, of any
	// Project, until the files fit limit.
	EnforceDiskLimit(limit storage.LogDiskLimit) error
}

// Projects tells which Projects exist, and the Retention of each.
// *storage.ConsoleSettings, which reads Console's projects table, is the
// production adapter.
type Projects interface {
	// ProjectLogRetention returns the Retention in whole days of every
	// Project that exists, or 0 if its Retention is not valid.
	ProjectLogRetention() (map[uint32]int, error)
}

const (
	// cleanupAfterHour is how long after each hour start the cleanup runs,
	// so that it follows the hourly export.
	cleanupAfterHour = 5 * time.Minute
	// retryCleanupAfter is how soon a failed cleanup runs again.
	retryCleanupAfter = time.Minute
)

// Cleanup is the timer inside EventStore that keeps Logs from outliving
// their Retention or their Project, and Log files from passing the disk
// limit. Once a UTC day by its clock, and when it starts, it deletes Logs past
// their Retention; a few minutes past each hour start, it enforces the disk limit. A
// failed run is tried again a minute later.
type Cleanup struct {
	store     CleanupStore
	projects  Projects
	diskLimit storage.LogDiskLimit
	clock     clock.Clock

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
	started  bool
}

// NewCleanup constructs the Log cleanup. It reads the time from clock.
func NewCleanup(store CleanupStore, projects Projects, diskLimit storage.LogDiskLimit, clock clock.Clock) *Cleanup {
	return &Cleanup{
		store: store, projects: projects, diskLimit: diskLimit, clock: clock,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

// Start runs the timer in its own goroutine.
func (c *Cleanup) Start() {
	c.started = true
	go c.run()
}

// Stop stops the timer, waiting for a run in progress to end.
func (c *Cleanup) Stop() {
	c.stopOnce.Do(func() { close(c.stop) })
	if c.started {
		<-c.done
	}
}

func (c *Cleanup) run() {
	defer close(c.done)
	var lastPass time.Time // the UTC day of the last whole daily pass
	for {
		now := c.clock.Now()
		failed := false
		if day := storage.LogDay(now); !day.Equal(lastPass) {
			if err := c.DeleteLogsPastRetention(now); err != nil {
				logRetry("Deleting Logs past their Retention failed, retrying", err)
				failed = true
			} else {
				lastPass = day
			}
		}
		if err := c.store.EnforceDiskLimit(c.diskLimit); err != nil {
			logRetry("Enforcing the Log disk limit failed, retrying", err)
			failed = true
		}

		wait := retryCleanupAfter
		if !failed {
			now = c.clock.Now()
			next := now.Truncate(time.Hour).Add(cleanupAfterHour)
			if !next.After(now) {
				next = next.Add(time.Hour)
			}
			wait = next.Sub(now)
		}
		timer := time.NewTimer(wait)
		select {
		case <-c.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// logRetry logs a cleanup that runs again soon: as an error, unless it only
// waits for a read of the files it would delete.
func logRetry(message string, err error) {
	if errors.Is(err, storage.ErrLogFilesInUse) {
		logger.L.Info(message, "error", err, "retry_in", retryCleanupAfter)
		return
	}
	logger.L.Error(message, "error", err, "retry_in", retryCleanupAfter)
}

// DeleteLogsPastRetention is the daily pass at now. It deletes each Project's Logs
// of the UTC days that lie wholly past its Retention, and every Log of a
// Project whose row is gone from Console's projects table: that is how a
// deleted Project is told apart, not by its keys, which Console deletes and
// adds freely.
//
// A day is kept while any of its time is within Retention, as Log ingest
// still accepts Logs of that time.
//
// A projects table with no rows deletes nothing: it is far likelier to be a
// reset or restored Console database than every Project deleted.
func (c *Cleanup) DeleteLogsPastRetention(now time.Time) error {
	retention, err := c.projects.ProjectLogRetention()
	if err != nil {
		return fmt.Errorf("read projects: %w", err)
	}
	if len(retention) == 0 {
		logger.L.Warn("Console's projects table has no rows, so no Project's Logs are deleted this pass")
		return nil
	}
	projects, err := c.store.LogProjects()
	if err != nil {
		return err
	}
	var failed []error
	for _, projectID := range projects {
		days, exists := retention[projectID]
		switch {
		case !exists:
			logger.L.Info("Deleting the Logs of a deleted Project", "project_id", projectID)
			err = c.store.DeleteProjectLogs(projectID)
		case days < 1:
			logger.L.Error("Keeping the Logs of a Project with no valid Retention", "project_id", projectID)
			continue
		default:
			err = c.store.DeleteLogDaysBefore(projectID, storage.LogDay(now.AddDate(0, 0, -days)))
		}
		if err != nil {
			// The next pass tries again.
			logger.L.Error("Deleting Logs past their Retention failed", "project_id", projectID, "error", err)
			failed = append(failed, fmt.Errorf("project %d: %w", projectID, err))
		}
	}
	return errors.Join(failed...)
}
