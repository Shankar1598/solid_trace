package logprocessing

import (
	"errors"
	"fmt"
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
}

// Projects tells which Projects exist, and the Retention of each.
// *storage.ConsoleSettings, which reads Console's projects table, is the
// production adapter.
type Projects interface {
	// ProjectLogRetention returns the Retention in whole days of every
	// Project that exists, or 0 if its Retention is not valid.
	ProjectLogRetention() (map[uint32]int, error)
}

// Cleanup keeps Logs from outliving their Retention or their Project.
type Cleanup struct {
	store    CleanupStore
	projects Projects
	clock    clock.Clock
}

// NewCleanup constructs the Log cleanup. It reads the time from clock.
func NewCleanup(store CleanupStore, projects Projects, clock clock.Clock) *Cleanup {
	return &Cleanup{store: store, projects: projects, clock: clock}
}

// DeleteExpiredLogs is the daily pass at now. It deletes each Project's Logs
// of the UTC days that lie wholly past its Retention, and every Log of a
// Project whose row is gone from Console's projects table: that is how a
// deleted Project is told apart, not by its keys, which Console deletes and
// adds freely.
//
// A day is kept while any of its time is within Retention, as Log ingest
// still accepts Logs of that time.
func (c *Cleanup) DeleteExpiredLogs(now time.Time) error {
	retention, err := c.projects.ProjectLogRetention()
	if err != nil {
		return fmt.Errorf("read projects: %w", err)
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
			logger.L.Error("Deleting expired Logs failed", "project_id", projectID, "error", err)
			failed = append(failed, fmt.Errorf("project %d: %w", projectID, err))
		}
	}
	return errors.Join(failed...)
}
