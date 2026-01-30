package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type EventsHandler struct {
	rocksdb *storage.RocksDBWriter
	duckdb  *storage.DuckDBWriter
}

func NewEventsHandler(rocksdb *storage.RocksDBWriter, duckdb *storage.DuckDBWriter) *EventsHandler {
	return &EventsHandler{
		rocksdb: rocksdb,
		duckdb:  duckdb,
	}
}

// GET /api/events/:event_uuid
func (h *EventsHandler) GetEvent(c *fiber.Ctx) error {
	eventUUID := c.Params("event_uuid")
	key := storage.KeyForEvent(eventUUID)

	data, err := h.rocksdb.GetEvent(key)
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

func (h *EventsHandler) parseParams(c *fiber.Ctx) (storage.QueryParams, error) {
	projectIDStr := c.Params("project_id")
	projectID, err := strconv.ParseUint(projectIDStr, 10, 32)
	if err != nil {
		return storage.QueryParams{}, err
	}

	params := storage.QueryParams{
		ProjectID: uint32(projectID),
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

	if newerThan := c.Query("newer_than"); newerThan != "" {
		if t, err := time.Parse(time.RFC3339, newerThan); err == nil {
			params.NewerThan = t
		}
	}

	if olderThan := c.Query("older_than"); olderThan != "" {
		if t, err := time.Parse(time.RFC3339, olderThan); err == nil {
			params.OlderThan = t
		}
	}

	return params, nil
}
