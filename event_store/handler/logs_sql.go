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

// ConsoleSettings reads what Logs need from Console: the instance settings
// the operator sets, and the slug a Project's Logs are named by in Queries.
type ConsoleSettings interface {
	Instance() (storage.InstanceSettings, error)
	ProjectSlug(projectID uint32) (string, error)
}

// sqlRequest is the body of a SQL run.
type sqlRequest struct {
	SQL            string    `json:"sql"`
	From           time.Time `json:"from"`
	To             time.Time `json:"to"`
	TimeoutSeconds *float64  `json:"timeout_seconds"`
}

// POST /api/:project_id/logs/sql
//
// Runs raw DuckDB SQL, {"sql", "from", "to"} (RFC 3339), exactly as written,
// with the logs view holding the Project's Logs of [from, to) (ADR 0003), and
// "timeout_seconds" optional. Only while the instance's SQL queries setting is
// on; otherwise 403. Answers {"table": {"columns": [{"name", "type"}],
// "rows": [[…]], "truncated"}, "elapsed_ms"}, at most maxTableRows rows. SQL
// that fails answers 400 with {"error"}, DuckDB's message, and a run past its
// timeout 504 with {"errors": [{"message"}]}.
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
	run, problem := startRun(c, req.TimeoutSeconds)
	if problem != nil {
		return c.Status(400).JSON(problem)
	}
	defer run.end()
	if problem := rangeProblem(req.From, req.To); problem != nil {
		return c.Status(400).JSON(problem)
	}
	result, err := h.logs.RunSQL(run.ctx, projectID, req.From, req.To, req.SQL, maxTableRows)
	// Stopped, the SQL fails as DuckDB interrupts it.
	if err != nil && run.timedOut() {
		return run.stopped(c)
	}
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
	return run.answer(c, fiber.Map{"table": fiber.Map{"columns": columns, "rows": result.Rows, "truncated": result.Truncated}})
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
// nothing, so EventStore answers whatever the SQL queries setting; Console,
// which only seeds SQL mode with it, refuses it while the setting is off.
func (h *LogsHandler) ReadableSQL(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	var req queryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with query, from and to"})
	}
	if problem := rangeProblem(req.From, req.To); problem != nil {
		return c.Status(400).JSON(problem)
	}
	query, err := h.compile(projectID, req.Query, logquery.Bounds{From: req.From, To: req.To, Now: h.clock.Now()})
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}
	return c.JSON(fiber.Map{"sql": query.ReadableSQL()})
}
