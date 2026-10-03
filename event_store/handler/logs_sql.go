package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/logquery"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// InstanceSettings reads the instance settings the operator sets in Console.
type InstanceSettings interface {
	Instance() (storage.InstanceSettings, error)
}

// sqlRequest is the body of a SQL run.
type sqlRequest struct {
	SQL  string    `json:"sql"`
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// POST /api/:project_id/logs/sql
//
// Runs raw DuckDB SQL, {"sql", "from", "to"} (RFC 3339), exactly as written,
// with the logs view holding the Project's Logs of [from, to) (ADR 0003). Only
// while the instance's SQL queries setting is on; otherwise 403. Answers as a
// run's table does, {"table": {"columns": [{"name", "type"}], "rows": [[…]],
// "truncated"}}, at most maxTableRows rows. SQL that fails answers 400 with
// {"error"}, DuckDB's message.
func (h *LogsHandler) SQL(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	settings, err := h.settings.Instance()
	if err != nil {
		logger.L.Error("Reading the instance settings failed", "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	if !settings.SQLQueriesEnabled {
		return c.Status(403).JSON(fiber.Map{"error": "SQL queries are turned off on this instance"})
	}
	var req sqlRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with sql, from and to"})
	}
	if req.From.IsZero() || req.To.IsZero() || req.To.Before(req.From) {
		return c.Status(400).JSON(fiber.Map{"error": "from and to must be RFC 3339 times, from no later than to"})
	}
	result, err := h.logs.RunSQL(c.UserContext(), projectID, req.From, req.To, req.SQL, maxTableRows)
	var sqlErr *storage.SQLError
	if errors.As(err, &sqlErr) {
		return c.Status(400).JSON(fiber.Map{"error": sqlErr.Error()})
	}
	if err != nil {
		logger.L.Error("SQL query failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	columns := make([]logquery.Column, len(result.Columns))
	for i, col := range result.Columns {
		columns[i] = logquery.Column{Name: col.Name, Type: columnType(col.Type)}
	}
	return c.JSON(fiber.Map{"table": fiber.Map{"columns": columns, "rows": result.Rows, "truncated": result.Truncated}})
}

// columnType names a DuckDB type as a table's column types are named: string,
// number, bool, datetime, timespan, or dynamic for the rest (maps, lists,
// structs, JSON).
func columnType(duckdbType string) string {
	switch duckdbType {
	case "VARCHAR", "UUID", "ENUM", "TIME", "TIMETZ", "BIT":
		return "string"
	case "BOOLEAN":
		return "bool"
	case "TINYINT", "SMALLINT", "INTEGER", "BIGINT", "HUGEINT",
		"UTINYINT", "USMALLINT", "UINTEGER", "UBIGINT", "UHUGEINT", "FLOAT", "DOUBLE":
		return "number"
	case "DATE", "TIMESTAMP", "TIMESTAMP_S", "TIMESTAMP_MS", "TIMESTAMP_NS", "TIMESTAMPTZ":
		return "datetime"
	case "INTERVAL":
		return "timespan"
	}
	if strings.HasPrefix(duckdbType, "DECIMAL") {
		return "number"
	}
	return "dynamic"
}

// POST /api/:project_id/logs/readable-sql
//
// Writes a Query, {"query", "from", "to"} as for a run, as readable SQL
// against the logs view, to seed SQL mode: {"sql"}. Run through
// /logs/sql with the same range, it returns the Query's rows. A Query with
// mistakes answers 400 with {"errors": […]}, as validate lists them. It runs
// nothing, so it answers whatever the SQL queries setting.
func (h *LogsHandler) ReadableSQL(c *fiber.Ctx) error {
	var req queryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with query, from and to"})
	}
	if req.From.IsZero() || req.To.IsZero() || req.To.Before(req.From) {
		return c.Status(400).JSON(fiber.Map{"error": "from and to must be RFC 3339 times, from no later than to"})
	}
	query, err := logquery.Compile(req.Query, logquery.Bounds{From: req.From, To: req.To, Now: h.clock.Now()})
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}
	return c.JSON(fiber.Map{"sql": query.ReadableSQL()})
}
