package handler

import (
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// ScopeRunEventsWrite lets a token, the Dispatcher's, send run events to
// its Organization's Agent Project. It starts no Run: the Dispatcher's API
// needs the factory scope.
const ScopeRunEventsWrite = "run_events:write"

// RunEventsIngest stores the Dispatcher's batches of run events as Logs
// (*ingest.Service).
type RunEventsIngest interface {
	IngestRunEvents(projectID uint32, body ingest.Body) (ingest.LogCounts, error)
}

// SendRunEvents answers POST /api/agent/v1/run-events: a batch of the
// Dispatcher's Logs, a JSON array, one object or NDJSON, as the JSON Log
// route takes them. Each run event has the Attributes run.id and run.seq;
// a Log with no run.id is one of the Dispatcher's warnings. They are stored
// in the token's Organization's Agent Project. The answer is that of the
// JSON Log route: a 200 means that the Logs are stored, and the counts say
// which were dropped.
func (h *AgentHandler) SendRunEvents(c *fiber.Ctx) error {
	token := agentToken(c)
	projectID, on, err := h.settings.AgentProject(token.Organization.ID)
	if err != nil {
		logger.L.Error("Failed to find the Agent Project", "organization_id", token.Organization.ID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Agent Project could not be read. Try again later."})
	}
	if !on {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": fmt.Sprintf("Agent sessions are off in %s, so it has no Agent Project for run events. "+
				"An Admin turns them on in Console on Settings → Agent sessions.", token.Organization.Name)})
	}
	body, err := requestBody(c, logBodyLimit)
	if err != nil {
		return renderBodyError(c, err)
	}
	counts, err := h.runEvents.IngestRunEvents(projectID, body)
	switch {
	case err == nil:
		return c.JSON(counts)
	case isBodyError(err):
		return renderBodyError(c, err)
	case errors.Is(err, ingest.ErrOverloaded), errors.Is(err, storage.ErrShuttingDown):
		c.Set(fiber.HeaderRetryAfter, retryAfterSeconds)
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": err.Error()})
	default:
		logger.L.Error("Run events ingest failed", "project_id", projectID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal error"})
	}
}
