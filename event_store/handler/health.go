package handler

import (
	"errors"

	"github.com/gofiber/fiber/v2"
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
	pebble     Pinger
	clickhouse Pinger
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(pebble, clickhouse Pinger) *HealthHandler {
	return &HealthHandler{
		pebble:     pebble,
		clickhouse: clickhouse,
	}
}

// Health is /health on the query port. It answers 200 only when Pebble and
// ClickHouse are both up. Console reads its "status".
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	return h.health(c, false)
}

// IngestHealth is /health on the ingest port. It answers 200 while ingest is
// up, also while ClickHouse starts. A load balancer and deploy/install check
// it.
func (h *HealthHandler) IngestHealth(c *fiber.Ctx) error {
	return h.health(c, true)
}

// health writes the same body on both ports. "ingest" is "up" while Pebble
// answers, because ingest writes only there. "clickhouse" is "starting",
// "ready" or "down", and "checks" give the error. "status" is "healthy" only
// when both are up. With ingestOnly, the status code follows ingest alone.
func (h *HealthHandler) health(c *fiber.Ctx, ingestOnly bool) error {
	checks := fiber.Map{
		"pebble":     "ok",
		"clickhouse": "ok",
	}
	ingest, clickhouse := "up", "ready"
	if err := h.pebble.Ping(); err != nil {
		checks["pebble"] = err.Error()
		ingest = "down"
	}
	if err := h.clickhouse.Ping(); err != nil {
		checks["clickhouse"] = err.Error()
		clickhouse = "down"
		if errors.Is(err, ErrClickHouseStarting) {
			clickhouse = "starting"
		}
	}

	status, httpStatus := "healthy", fiber.StatusOK
	if ingest != "up" || clickhouse != "ready" {
		status = "unhealthy"
	}
	if ingest != "up" || (!ingestOnly && clickhouse != "ready") {
		httpStatus = fiber.StatusServiceUnavailable
	}
	return c.Status(httpStatus).JSON(fiber.Map{
		"status":     status,
		"ingest":     ingest,
		"clickhouse": clickhouse,
		"checks":     checks,
	})
}
