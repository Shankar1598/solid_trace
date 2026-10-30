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
// the operator sets, the budget of a Query that matches text, and the slug
// a Project's Logs are named by in Queries.
type ConsoleSettings interface {
	Instance() (storage.InstanceSettings, error)
	TextMatchBudgetBytes() (int64, error)
	ProjectSlug(projectID uint32) (string, error)
}

// sqlRequest is the body of a SQL run.
type sqlRequest struct {
	SQL            string    `json:"sql"`
	From           time.Time `json:"from"`
	To             time.Time `json:"to"`
	TimeoutSeconds *float64  `json:"timeout_seconds"`
}

// sqlTimeout is the timeout a SQL run gets for the timeout_seconds it asked
// for: the SQL users' time limit at most, and when it asked for none. One
// startRun refuses, not positive or longer than a day, is passed on for it
// to refuse.
func sqlTimeout(limit time.Duration, asked *float64) *float64 {
	timeout := limit.Seconds()
	if asked != nil && (*asked < timeout || *asked > maxTimeoutSeconds) {
		timeout = *asked
	}
	return &timeout
}

// simplerSQL is the advice for SQL stopped at its timeout.
const simplerSQL = "try a shorter time range or a simpler query"

// POST /api/:project_id/logs/sql
//
// Runs ClickHouse SQL, {"sql", "from", "to"} (RFC 3339), as written, as the
// Project's SQL user, whose logs and events show the Project's rows of
// [from, to) and nothing else (ADR 0003), with "timeout_seconds" optional.
// Only while the instance's SQL queries setting is on; otherwise 403. The run
// stops after timeout_seconds, and at the SQL users' time limit whatever it
// asks for. Answers {"table": {"columns": [{"name", "type"}], "rows": [[…]],
// "truncated"}, "elapsed_ms"}, at most maxTableRows rows. SQL that fails
// answers 400 with {"error"}, ClickHouse's message, which gives a syntax
// error's position: so does SQL that reads what its user may not, or
// changes a setting. A run past its timeout answers 504 with {"errors":
// [{"message"}]}.
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
	run, problem := startRun(c, "sql", sqlTimeout(h.sql.Limits().Time, req.TimeoutSeconds))
	if problem != nil {
		return c.Status(400).JSON(problem)
	}
	defer run.end()
	if problem := rangeProblem(req.From, req.To); problem != nil {
		return c.Status(400).JSON(problem)
	}
	result, err := h.sql.RunSQL(run.ctx, projectID, req.From, req.To, req.SQL, maxTableRows)
	// Stopped, the SQL fails as the context ends or ClickHouse stops it at
	// its time limit.
	var sqlErr *storage.SQLError
	isSQLErr := errors.As(err, &sqlErr)
	if err != nil && (run.timedOut() || isSQLErr && sqlErr.Stopped) {
		return run.stopped(c, simplerSQL)
	}
	if isSQLErr {
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

// columnType names a ClickHouse type as a table's column types are named:
// string, number, bool, datetime, or dynamic for the rest (maps, arrays,
// tuples, JSON, Dynamic, intervals).
func columnType(clickHouseType string) string {
	t := clickHouseType
	for _, wrapper := range []string{"LowCardinality(", "Nullable("} {
		if strings.HasPrefix(t, wrapper) && strings.HasSuffix(t, ")") {
			t = strings.TrimSuffix(strings.TrimPrefix(t, wrapper), ")")
		}
	}
	name, _, _ := strings.Cut(t, "(")
	switch name {
	case "String", "FixedString", "UUID", "Enum8", "Enum16", "IPv4", "IPv6":
		return "string"
	case "Bool":
		return "bool"
	case "Int8", "Int16", "Int32", "Int64", "Int128", "Int256",
		"UInt8", "UInt16", "UInt32", "UInt64", "UInt128", "UInt256",
		"Float32", "Float64", "BFloat16", "Decimal", "Decimal32", "Decimal64", "Decimal128", "Decimal256":
		return "number"
	case "Date", "Date32", "DateTime", "DateTime64":
		return "datetime"
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
	query, err := h.compile(projectID, req.Query, logquery.Bounds{From: req.From, To: req.To, Now: h.clock.Now()}, nil)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}
	return c.JSON(fiber.Map{"sql": query.ReadableSQL()})
}
