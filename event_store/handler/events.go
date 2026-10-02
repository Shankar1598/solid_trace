package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type EventsHandler struct {
	pebble *storage.PebbleWriter
	duckdb *storage.DuckDBWriter
}

func NewEventsHandler(pebble *storage.PebbleWriter, duckdb *storage.DuckDBWriter) *EventsHandler {
	return &EventsHandler{
		pebble: pebble,
		duckdb: duckdb,
	}
}

// GET /api/:project_id/events/:event_uuid
// Returns the raw payload only when the event belongs to the project in the path.
func (h *EventsHandler) GetEvent(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	eventUUID, err := uuid.Parse(c.Params("event_uuid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid event UUID"})
	}

	// The key carries the project, so an event from another project is not found.
	data, err := h.pebble.GetEvent(storage.KeyForEvent(projectID, eventUUID.String()))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if data == nil {
		return c.Status(404).JSON(fiber.Map{"error": "Event not found"})
	}

	c.Set("Content-Type", "application/json")
	return c.Send(data)
}

// GET /api/:project_id/events
func (h *EventsHandler) List(c *fiber.Ctx) error {
	params, err := h.parseParams(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	events, err := h.duckdb.QueryEvents(params)
	if err != nil {
		logger.L.Error("Query error", "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}

	return c.JSON(events)
}

// GET /api/:project_id/events/context
// Returns event with prev/next UUIDs.
// If uuid query param is provided, returns that specific event with context.
// If uuid is not provided, returns the latest event with context (limit=2 to get prev).
func (h *EventsHandler) GetEventWithContext(c *fiber.Ctx) error {
	params, err := h.parseParams(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	// prev/next run over the Issue's whole Event sequence, so a time window
	// can't apply; refuse it rather than ignore it.
	if !params.NewerThan.IsZero() || !params.OlderThan.IsZero() {
		return c.Status(400).JSON(fiber.Map{"error": "newer_than and older_than are not supported on events/context"})
	}

	// If no UUID provided, we want the latest 2 events to determine prev
	if params.UUID == "" {
		params.Limit = 2
		params.SortDesc = true
	}

	results, err := h.duckdb.QueryEventWithContext(params)
	if err != nil {
		logger.L.Error("QueryEventWithContext error", "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}

	if len(results) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "Event not found"})
	}

	// Return the first result (the target event)
	result := results[0]

	// If we queried for latest (no UUID), the first result is the latest event
	// For "latest" scenario, prev is actually the second result if exists
	// Note: When sorted DESC, the "prev" in time is actually the next row
	if params.UUID == "" && len(results) > 1 {
		result.PrevUUID = results[1].Event.EventUUID
		result.NextUUID = "" // No newer event than the latest
	}

	return c.JSON(fiber.Map{
		"event":     result.Event,
		"prev_uuid": result.PrevUUID,
		"next_uuid": result.NextUUID,
	})
}

// GET /api/:project_id/events/count
func (h *EventsHandler) Count(c *fiber.Ctx) error {
	params, err := h.parseParams(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	count, err := h.duckdb.CountEvents(params)
	if err != nil {
		logger.L.Error("Count error", "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}

	return c.JSON(fiber.Map{"count": count})
}

func parseProjectID(c *fiber.Ctx) (uint32, error) {
	projectID, err := strconv.ParseUint(c.Params("project_id"), 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(projectID), nil
}

func (h *EventsHandler) parseParams(c *fiber.Ctx) (storage.QueryParams, error) {
	projectID, err := parseProjectID(c)
	if err != nil {
		return storage.QueryParams{}, err
	}

	params := storage.QueryParams{
		ProjectID: projectID,
		SortDesc:  c.Query("sort") == "desc",
		Limit:     20,
		Offset:    0,
		UUID:      c.Query("uuid"),
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			params.Limit = l
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			params.Offset = o
		}
	}

	if fps := c.Query("fingerprint_ids"); fps != "" {
		parts := strings.Split(fps, ",")
		for _, p := range parts {
			if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
				params.IssueFingerprintIDs = append(params.IssueFingerprintIDs, id)
			}
		}
	}

	if params.NewerThan, err = parseTimeBound(c, "newer_than"); err != nil {
		return storage.QueryParams{}, err
	}
	if params.OlderThan, err = parseTimeBound(c, "older_than"); err != nil {
		return storage.QueryParams{}, err
	}

	return params, nil
}

// parseTimeBound reads an RFC 3339 time bound, or the zero time when it is
// absent. A malformed bound is an error: ignoring it would widen the window.
func parseTimeBound(c *fiber.Ctx, name string) (time.Time, error) {
	value := c.Query(name)
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s %q: expected an RFC 3339 time", name, value)
	}
	return t, nil
}
