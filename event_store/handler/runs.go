package handler

import (
	"context"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// RunReader reads the Runs of an Agent Project from their run events.
// *storage.ClickHouseLogs is the production adapter.
type RunReader interface {
	RunList(ctx context.Context, projectID uint32, filter storage.RunFilter) ([]storage.RunSummary, error)
	RunEvents(ctx context.Context, projectID uint32, runID string) ([]models.Log, error)
}

// RunsHandler answers Console's reads of the Runs list and of one Run.
// Console sends the Organization's Agent Project.
type RunsHandler struct {
	runs  RunReader
	clock clock.Clock
}

func NewRunsHandler(runs RunReader, clock clock.Clock) *RunsHandler {
	return &RunsHandler{runs: runs, clock: clock}
}

// RegisterRunsQueryRoutes mounts the Query API's routes for the Runs
// pages, on the loopback query listener.
func RegisterRunsQueryRoutes(app *fiber.App, h *RunsHandler) {
	app.Post("/api/:project_id/runs", h.List)
	app.Get("/api/:project_id/runs/:run_id", h.Run)
}

const (
	runsTimeout = 30 * time.Second
	// runsLimit is the most rows that the list gives, and the rows that it
	// gives when the request sets no limit.
	runsLimit = 500
	// runsDefaultRange is the list's range when the request sets no from:
	// each Run that is not closed has an event in it, because a Run closes
	// after 30 days with no activity.
	runsDefaultRange = 30 * 24 * time.Hour
)

var runIDParam = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type runsRequest struct {
	From    *time.Time `json:"from"`
	To      *time.Time `json:"to"`
	Project string     `json:"project"`
	State   string     `json:"state"`
	Limit   int        `json:"limit"`
}

// List answers POST /api/:project_id/runs: the last event of each Run with
// an event in [from, to] (to defaults to now, from to 30 days before to),
// Active Runs first, then latest event first, at most limit rows. project
// and state filter on the last event.
func (h *RunsHandler) List(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	var req runsRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON, its times RFC 3339 times"})
		}
	}
	to := h.clock.Now()
	if req.To != nil {
		to = *req.To
	}
	from := to.Add(-runsDefaultRange)
	if req.From != nil {
		from = *req.From
	}
	if to.Before(from) {
		return c.Status(400).JSON(fiber.Map{"error": "from must be before to"})
	}
	if req.Limit < 0 || req.Limit > runsLimit {
		return c.Status(400).JSON(fiber.Map{"error": "limit must be from 1 to 500"})
	}
	limit := req.Limit
	if limit == 0 {
		limit = runsLimit
	}

	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadRuns), runsTimeout)
	defer cancel()
	runs, err := h.runs.RunList(ctx, projectID, storage.RunFilter{From: from, To: to, Project: req.Project, State: req.State})
	if err != nil {
		logger.L.Error("Reading the Runs failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	more := len(runs) > limit
	if more {
		runs = runs[:limit]
	}
	return c.JSON(fiber.Map{"runs": runs, "more": more})
}

// Run answers GET /api/:project_id/runs/:run_id: the Run as its last event
// shows it, and all its events in run.seq order, each one time. 404 when
// the Run has no event.
func (h *RunsHandler) Run(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	runID := c.Params("run_id")
	if !runIDParam.MatchString(runID) {
		return c.Status(404).JSON(fiber.Map{"error": "No such Run"})
	}
	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadRuns), runsTimeout)
	defer cancel()
	events, err := h.runs.RunEvents(ctx, projectID, runID)
	if err != nil {
		logger.L.Error("Reading a Run failed", "project_id", projectID, "run_id", runID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	if len(events) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "No such Run"})
	}
	return c.JSON(fiber.Map{"run": storage.RunSummaryOf(events[len(events)-1]), "events": events})
}
