package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/storage"
)

// Pinger is a store /health checks.
type Pinger interface {
	Ping() error
}

// HealthHandler handles health check endpoints
type HealthHandler struct {
	pebble     *storage.PebbleWriter
	clickhouse Pinger
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(pebble *storage.PebbleWriter, clickhouse Pinger) *HealthHandler {
	return &HealthHandler{
		pebble:     pebble,
		clickhouse: clickhouse,
	}
}

// Health returns the health status of the event store service
// GET /health
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	status := "healthy"
	httpStatus := 200

	checks := fiber.Map{
		"pebble":     "ok",
		"clickhouse": "ok",
	}

	// Check Pebble
	if err := h.pebble.Ping(); err != nil {
		checks["pebble"] = err.Error()
		status = "unhealthy"
		httpStatus = 503
	}

	if err := h.clickhouse.Ping(); err != nil {
		checks["clickhouse"] = err.Error()
		status = "unhealthy"
		httpStatus = 503
	}

	return c.Status(httpStatus).JSON(fiber.Map{
		"status": status,
		"checks": checks,
	})
}
