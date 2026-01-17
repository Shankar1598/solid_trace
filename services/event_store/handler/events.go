package handler

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
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

// GET /api/events/:project_id/:event_uuid/:timestamp_micro
func (h *EventsHandler) GetEvent(c *fiber.Ctx) error {
	projectID, _ := c.ParamsInt("project_id")
	eventUUID := c.Params("event_uuid")
	timestampMicro, _ := c.ParamsInt("timestamp_micro")

	timestamp := time.UnixMicro(int64(timestampMicro))
	key := storage.KeyForEvent(uint32(projectID), eventUUID, timestamp)

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
		log.Printf("Query error: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}

	return c.JSON(events)
}

// GET /api/:project_id/events/count
func (h *EventsHandler) Count(c *fiber.Ctx) error {
	params, err := h.parseParams(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	count, err := h.duckdb.CountEvents(params)
	if err != nil {
		log.Printf("Count error: %v", err)
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
