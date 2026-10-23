package processing

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
)

// CleanupEvents holds the Events: *storage.PebbleWriter.
type CleanupEvents interface {
	// ProjectsWithEvents lists the Projects that have any Event stored.
	ProjectsWithEvents() ([]uint32, error)
	// DeleteProjectEvents deletes a Project's Events and processing cursor.
	DeleteProjectEvents(projectID uint32) error
}

// CleanupIndex holds the Events' rows that Event reads read:
// *storage.ClickHouse.
type CleanupIndex interface {
	// DeleteProjectEventRows deletes a Project's rows from the events table.
	DeleteProjectEventRows(projectID uint32) error
}

// CleanupIssues holds the Issue rows Event processing writes in Console's
// tables: *storage.SQLiteWriter.
type CleanupIssues interface {
	// DeleteProjectIssues deletes a Project's Issues, fingerprints, Issue
	// counter and seen cursor.
	DeleteProjectIssues(projectID uint32) error
}

// Projects tells which Projects exist: *storage.ConsoleSettings, which reads
// Console's projects table.
type Projects interface {
	// ProjectLogRetention returns every Project that exists, by id, with its
	// Log Retention. Only the ids are read here.
	ProjectLogRetention() (map[uint32]int, error)
}

// retryCleanupAfter is how soon a failed cleanup runs again.
const retryCleanupAfter = time.Minute

// Cleanup is the timer inside EventStore that deletes a deleted Project's
// Events, when it starts and once a UTC day by its clock. A Project is
// deleted when its row is gone from Console's projects table, as for the Log
// cleanup. A failed run is tried again a minute later.
type Cleanup struct {
	events   CleanupEvents
	index    CleanupIndex
	issues   CleanupIssues
	projects Projects
	clock    clock.Clock

	// batchLock is Event processing's: holding it keeps it out while a
	// Project's rows go.
	batchLock sync.Locker

	stop     chan struct{}
	stopOnce sync.Once
	started  bool
	done     chan struct{}
}

// NewCleanup constructs the Event cleanup. It reads the time from clock.
func NewCleanup(events CleanupEvents, index CleanupIndex, issues CleanupIssues, projects Projects, batchLock sync.Locker, clock clock.Clock) *Cleanup {
	return &Cleanup{
		events: events, index: index, issues: issues, projects: projects, batchLock: batchLock, clock: clock,
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
	for {
		wait := retryCleanupAfter
		if err := c.DeleteEventsOfDeletedProjects(); err != nil {
			logger.L.Error("Deleting the Events of deleted Projects failed, retrying", "error", err, "retry_in", wait)
		} else {
			now := c.clock.Now().UTC()
			wait = now.Truncate(24 * time.Hour).Add(24 * time.Hour).Sub(now)
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

// DeleteEventsOfDeletedProjects is one pass. For each Project with Events
// whose row is gone from Console's projects table, it deletes the Project's
// rows in ClickHouse's events table, then any Issue rows Event processing
// wrote for it after Console deleted it, and last its Events, processing
// cursor and recorded range in Pebble. The Events go last because they are
// how the next pass finds the Project, should a step fail.
//
// A projects table with no rows deletes nothing: it is far likelier to be a
// reset or restored Console database than every Project deleted.
func (c *Cleanup) DeleteEventsOfDeletedProjects() error {
	existing, err := c.projects.ProjectLogRetention()
	if err != nil {
		return fmt.Errorf("read projects: %w", err)
	}
	if len(existing) == 0 {
		logger.L.Warn("Console's projects table has no rows, so no Project's Events are deleted this pass")
		return nil
	}
	withEvents, err := c.events.ProjectsWithEvents()
	if err != nil {
		return fmt.Errorf("list projects with events: %w", err)
	}
	var failed []error
	for _, projectID := range withEvents {
		if _, ok := existing[projectID]; ok {
			continue
		}
		logger.L.Info("Deleting the Events of a deleted Project", "project_id", projectID)
		if err := c.deleteProject(projectID); err != nil {
			// The next pass tries again.
			logger.L.Error("Deleting the Events of a deleted Project failed", "project_id", projectID, "error", err)
			failed = append(failed, fmt.Errorf("project %d: %w", projectID, err))
		}
	}
	return errors.Join(failed...)
}

func (c *Cleanup) deleteProject(projectID uint32) error {
	c.batchLock.Lock()
	defer c.batchLock.Unlock()

	if err := c.index.DeleteProjectEventRows(projectID); err != nil {
		return fmt.Errorf("delete event rows: %w", err)
	}
	if err := c.issues.DeleteProjectIssues(projectID); err != nil {
		return fmt.Errorf("delete issues: %w", err)
	}
	if err := c.events.DeleteProjectEvents(projectID); err != nil {
		return fmt.Errorf("delete events: %w", err)
	}
	return nil
}
