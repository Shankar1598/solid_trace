package logprocessing

import (
	"errors"
	"fmt"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// CleanupStore deletes the days of families of day tables, Logs' the first.
// *storage.ClickHouseLogs is the production adapter.
type CleanupStore interface {
	// DayProjects lists the Projects that have any days of a family.
	DayProjects(f storage.DayFamily) ([]uint32, error)
	// DeleteDaysBefore deletes a Project's days of a family before the UTC
	// day of day.
	DeleteDaysBefore(f storage.DayFamily, projectID uint32, day time.Time) error
	// DeleteProjectDays deletes every day of a family of a Project.
	DeleteProjectDays(f storage.DayFamily, projectID uint32) error
	// EnforceDiskLimit keeps the families' days on local disk within limit:
	// it moves the oldest days to the bucket, if there is one, and deletes
	// them if that fails or there is none.
	EnforceDiskLimit(limit storage.LogDiskLimit, families []storage.DayFamily) error
}

// SQLUsers holds the Projects' SQL users: *storage.ClickHouseSQL.
type SQLUsers interface {
	// DropProjectUser drops a Project's SQL user, if it has one.
	DropProjectUser(projectID uint32) error
}

// A Family is a family of day tables with its Retention.
type Family struct {
	Days storage.DayFamily
	// Retention returns the Retention in whole days of every Project that
	// exists, or 0 if its Retention is not valid, as
	// (*storage.ConsoleSettings).ProjectLogRetention reads Log Retention
	// from Console's projects table.
	Retention func() (map[uint32]int, error)
}

const (
	// cleanupAfterHour is how long after each hour start the cleanup runs.
	cleanupAfterHour = 5 * time.Minute
	// retryCleanupAfter is how soon a failed cleanup runs again.
	retryCleanupAfter = time.Minute
)

// Cleanup is the timer inside EventStore that keeps each family's days, as
// Logs, from outliving their Retention or their Project, and from passing
// the disk limit. Once a UTC day by its clock, and when it starts, it
// deletes the days past their family's Retention and every day of a deleted
// Project; a few minutes past each hour start, it enforces the disk limit.
// A failed run is tried again a minute later.
type Cleanup struct {
	store     CleanupStore
	sqlUsers  SQLUsers
	families  []Family
	diskLimit storage.LogDiskLimit
	clock     clock.Clock

	*worker
}

// NewCleanup constructs the cleanup of families, which drops a deleted
// Project's SQL user with its days. It reads the time from clock.
func NewCleanup(store CleanupStore, sqlUsers SQLUsers, diskLimit storage.LogDiskLimit, clock clock.Clock, families ...Family) *Cleanup {
	return &Cleanup{
		store: store, sqlUsers: sqlUsers, families: families, diskLimit: diskLimit, clock: clock,
		worker: newWorker(),
	}
}

// Start runs the timer in its own goroutine.
func (c *Cleanup) Start() {
	c.start(c.run)
}

// Stop stops the timer, waiting for a run in progress to end.
func (c *Cleanup) Stop() {
	c.halt()
}

func (c *Cleanup) run() {
	var lastPass time.Time // the UTC day of the last whole daily pass
	for {
		now := c.clock.Now()
		failed := false
		if day := storage.LogDay(now); !day.Equal(lastPass) {
			if err := c.DeleteDaysPastRetention(now); err != nil {
				logRetry("Deleting days past their Retention failed, retrying", err)
				failed = true
			} else {
				lastPass = day
			}
		}
		if err := c.store.EnforceDiskLimit(c.diskLimit, c.days()); err != nil {
			logRetry("Enforcing the disk limit failed, retrying", err)
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

// logRetry logs a cleanup that runs again soon.
func logRetry(message string, err error) {
	logger.L.Error(message, "error", err, "retry_in", retryCleanupAfter)
}

// days are the families' day tables.
func (c *Cleanup) days() []storage.DayFamily {
	days := make([]storage.DayFamily, len(c.families))
	for i, f := range c.families {
		days[i] = f.Days
	}
	return days
}

// DeleteDaysPastRetention is the daily pass at now. For each family, it
// deletes each Project's UTC days that lie wholly past the family's
// Retention, and every day of a Project whose row is gone from Console's
// projects table: that is how a deleted Project is told apart, not by its
// keys, which Console deletes and adds freely.
//
// A day is kept while any of its time is within Retention, as ingest still
// accepts rows of that time.
//
// A projects table with no rows deletes nothing: it is far likelier to be a
// reset or restored Console database than every Project deleted.
func (c *Cleanup) DeleteDaysPastRetention(now time.Time) error {
	var failed []error
	for _, f := range c.families {
		if err := c.deletePastRetention(f, now); err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", f.Days.Name, err))
		}
	}
	return errors.Join(failed...)
}

// deletePastRetention is the daily pass over one family.
func (c *Cleanup) deletePastRetention(f Family, now time.Time) error {
	name := f.Days.Name
	retention, err := f.Retention()
	if err != nil {
		return fmt.Errorf("read projects: %w", err)
	}
	if len(retention) == 0 {
		logger.L.Warn("Console's projects table has no rows, so no Project's days are deleted this pass", "family", name)
		return nil
	}
	projects, err := c.store.DayProjects(f.Days)
	if err != nil {
		return err
	}
	var failed []error
	for _, projectID := range projects {
		days, exists := retention[projectID]
		switch {
		case !exists:
			logger.L.Info("Deleting the days of a deleted Project", "family", name, "project_id", projectID)
			err = errors.Join(c.store.DeleteProjectDays(f.Days, projectID), c.sqlUsers.DropProjectUser(projectID))
		case days < 1:
			logger.L.Error("Keeping the days of a Project with no valid Retention", "family", name, "project_id", projectID)
			continue
		default:
			err = c.store.DeleteDaysBefore(f.Days, projectID, storage.FirstLogDayKept(now, days))
		}
		if err != nil {
			// The next pass tries again.
			logger.L.Error("Deleting days past their Retention failed", "family", name, "project_id", projectID, "error", err)
			failed = append(failed, fmt.Errorf("project %d: %w", projectID, err))
		}
	}
	return errors.Join(failed...)
}
