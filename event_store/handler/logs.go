package handler

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/logquery"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	defaultQueryLogs = 200
	maxQueryLogs     = 1000
)

// LogsHandler serves stored Logs on the query app: it runs and validates
// Queries and fetches single Logs.
type LogsHandler struct {
	logs *storage.LogStore
}

func NewLogsHandler(logs *storage.LogStore) *LogsHandler {
	return &LogsHandler{logs: logs}
}

// queryRequest is the body of a run.
type queryRequest struct {
	Query  string    `json:"query"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Limit  int       `json:"limit"`
	Cursor string    `json:"cursor"`
}

// POST /api/:project_id/logs/query
//
// Runs a Query over a Project's Logs of [from, to): {"query", "from", "to"}
// (RFC 3339), with "limit" (default 200, at most 1,000) and "cursor" (a
// previous page's next_cursor) optional. Answers {"logs": […], "next_cursor"},
// newest first; next_cursor is null once there are no older Logs. A Query
// with mistakes answers 400 with {"errors": […]}, as validate lists them.
//
// The scan goes on if the client goes away: it stops at limit Logs or the
// end of the range.
func (h *LogsHandler) Query(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	var req queryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with query, from and to"})
	}
	if req.From.IsZero() || req.To.IsZero() || req.To.Before(req.From) {
		return c.Status(400).JSON(fiber.Map{"error": "from and to must be RFC 3339 times, from no later than to"})
	}
	limit := defaultQueryLogs
	if req.Limit < 0 {
		return c.Status(400).JSON(fiber.Map{"error": "limit must be a positive whole number"})
	} else if req.Limit > 0 {
		limit = min(req.Limit, maxQueryLogs)
	}
	var after *storage.LogPosition
	if req.Cursor != "" {
		if after, err = decodeCursor(req.Cursor); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid cursor"})
		}
	}
	query, err := logquery.Compile(req.Query)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}

	logs, err := h.logs.ScanLogs(c.UserContext(), storage.LogScan{
		ProjectID: projectID,
		From:      req.From,
		To:        req.To,
		After:     after,
		Where:     query.Where,
		Limit:     limit,
	})
	if err != nil {
		logger.L.Error("Log query failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	var next *string
	if len(logs) == limit {
		cursor := encodeCursor(logs[len(logs)-1])
		next = &cursor
	}
	return c.JSON(fiber.Map{"logs": logs, "next_cursor": next})
}

// POST /api/:project_id/logs/validate
//
// Checks a Query, {"query"}, without running it. Answers {"errors": […],
// "chips"}: each error with its message and the line and column (from 1, in
// characters) where it starts and ends; chips when the Builder can show the
// Query, else null.
func (h *LogsHandler) Validate(c *fiber.Ctx) error {
	var req struct {
		Query string `json:"query"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with query"})
	}
	query, err := logquery.Compile(req.Query)
	if err != nil {
		return c.JSON(fiber.Map{"errors": queryErrors(req.Query, err), "chips": nil})
	}
	return c.JSON(fiber.Map{"errors": []logquery.Problem{}, "chips": query.Chips()})
}

// GET /api/:project_id/logs/:log_id?time=<RFC 3339>
//
// Returns one of a Project's Logs by id. With time, the Log's own time, only
// that hour's files are searched; without it, every file.
func (h *LogsHandler) Get(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	id, err := uuid.Parse(c.Params("log_id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid Log id"})
	}
	var at *time.Time
	if value := c.Query("time"); value != "" {
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "time must be an RFC 3339 time"})
		}
		at = &t
	}
	log, found, err := h.logs.LogByID(c.UserContext(), projectID, id, at)
	if err != nil {
		logger.L.Error("Log fetch failed", "project_id", projectID, "log_id", id, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	if !found {
		return c.Status(404).JSON(fiber.Map{"error": "Log not found"})
	}
	return c.JSON(log)
}

func queryErrors(source string, err error) []logquery.Problem {
	var queryErr *logquery.Error
	if errors.As(err, &queryErr) {
		return []logquery.Problem{logquery.ProblemOf(source, queryErr)}
	}
	return []logquery.Problem{{Message: err.Error(), Line: 1, Column: 1, EndLine: 1, EndColumn: 1}}
}

// A cursor is a Log's position, opaque to clients.
func encodeCursor(log models.Log) string {
	return base64.RawURLEncoding.EncodeToString(
		fmt.Appendf(nil, "%s|%s", log.Time.UTC().Format(time.RFC3339Nano), log.ID))
}

func decodeCursor(cursor string) (*storage.LogPosition, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}
	t, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, errors.New("invalid cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, t)
	if err != nil {
		return nil, err
	}
	logID, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	return &storage.LogPosition{Time: at, ID: logID}, nil
}
