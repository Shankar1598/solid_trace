package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/storage"
)

type EventsHandler struct {
	rocksdb *storage.RocksDBWriter
}

func NewEventsHandler(rocksdb *storage.RocksDBWriter) *EventsHandler {
	return &EventsHandler{rocksdb: rocksdb}
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
