package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/storage"
)

// HealthHandler handles health check endpoints
type HealthHandler struct {
	rocksdb *storage.RocksDBWriter
	duckdb  *storage.DuckDBWriter
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(rocksdb *storage.RocksDBWriter, duckdb *storage.DuckDBWriter) *HealthHandler {
	return &HealthHandler{
		rocksdb: rocksdb,
		duckdb:  duckdb,
	}
}

// Health returns the health status of the event store service
// GET /health
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	status := "healthy"
	httpStatus := 200

	checks := fiber.Map{
		"rocksdb": "ok",
		"duckdb":  "ok",
	}

	// Check RocksDB
	if err := h.rocksdb.Ping(); err != nil {
		checks["rocksdb"] = err.Error()
		status = "unhealthy"
		httpStatus = 503
	}

	// Check DuckDB
	if err := h.duckdb.Ping(); err != nil {
		checks["duckdb"] = err.Error()
		status = "unhealthy"
		httpStatus = 503
	}

	return c.Status(httpStatus).JSON(fiber.Map{
		"status": status,
		"checks": checks,
	})
}
