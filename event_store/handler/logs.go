package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	defaultLatestLogs = 100
	maxLatestLogs     = 10000
)

// LogsHandler serves stored Logs on the query app.
type LogsHandler struct {
	logs *storage.LogStore
}

func NewLogsHandler(logs *storage.LogStore) *LogsHandler {
	return &LogsHandler{logs: logs}
}

// GET /api/:project_id/logs/latest?limit=N
// Returns the Project's latest N Logs by time, newest first, with every
// built-in field and Attribute. N defaults to 100 and is capped at 10,000.
func (h *LogsHandler) Latest(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	limit := defaultLatestLogs
	if value := c.Query("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "limit must be a positive whole number"})
		}
		limit = min(limit, maxLatestLogs)
	}

	logs, err := h.logs.LatestLogs(projectID, limit)
	if err != nil {
		logger.L.Error("Latest logs query failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	return c.JSON(fiber.Map{"logs": logs})
}
