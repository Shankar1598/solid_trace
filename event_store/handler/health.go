package handler

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/storage"
)

// Pinger is a store /health checks.
type Pinger interface {
	Ping() error
}

// ErrClickHouseStarting is what a ClickHouse Pinger returns while
// EventStore's start of ClickHouse has not passed. Ingest takes Logs then,
// and the query port answers each read with 503.
var ErrClickHouseStarting = errors.New("ClickHouse is starting: EventStore takes Logs and Events, and answers reads when ClickHouse is ready")

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
//
// "ingest" is "up" while Pebble answers, as ingest writes only there.
// "clickhouse" is "starting", "ready" or "down". The status is "healthy",
// with 200, only when both are up: deploy/install waits for that, and
// Console's /health reads it.
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	status := "healthy"
	httpStatus := 200

	checks := fiber.Map{
		"pebble":     "ok",
		"clickhouse": "ok",
	}
	ingest, clickhouse := "up", "ready"

	// Check Pebble
	if err := h.pebble.Ping(); err != nil {
		checks["pebble"] = err.Error()
		ingest = "down"
		status = "unhealthy"
		httpStatus = 503
	}

	if err := h.clickhouse.Ping(); err != nil {
		checks["clickhouse"] = err.Error()
		clickhouse = "down"
		if errors.Is(err, ErrClickHouseStarting) {
			checks["clickhouse"] = "starting"
			clickhouse = "starting"
		}
		status = "unhealthy"
		httpStatus = 503
	}

	return c.Status(httpStatus).JSON(fiber.Map{
		"status":     status,
		"ingest":     ingest,
		"clickhouse": clickhouse,
		"checks":     checks,
	})
}
