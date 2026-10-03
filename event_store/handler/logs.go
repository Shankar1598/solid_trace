package handler

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/logquery"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	defaultQueryLogs = 200
	maxQueryLogs     = 1000
	// maxTableRows is how many rows of a table a run returns at most.
	maxTableRows = 10000
)

// LogsHandler serves stored Logs on the query app: it runs and validates
// Queries and fetches single Logs.
type LogsHandler struct {
	logs  *storage.LogStore
	clock clock.Clock
}

// NewLogsHandler serves logs; clock is the time ago() and now() read.
func NewLogsHandler(logs *storage.LogStore, clock clock.Clock) *LogsHandler {
	return &LogsHandler{logs: logs, clock: clock}
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
// previous page's next_cursor) optional. A Query of Log rows answers {"logs":
// […], "next_cursor"}, newest first; next_cursor is null once there are no
// older Logs, or take's number of Logs has been given. A Query that returns a
// table, one with project, extend, summarize, a sort other than newest first,
// top, count or distinct, answers {"table": {"columns": [{"name", "type"}],
// "rows": [[…]], "truncated"}}, and ignores limit and cursor. A Query with
// mistakes answers 400 with {"errors": […]}, as validate lists them.
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
	left := -1 // Logs a take has left to give, or -1 for no take
	if req.Cursor != "" {
		if after, left, err = decodeCursor(req.Cursor); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid cursor"})
		}
	}
	query, err := logquery.Compile(req.Query, logquery.Bounds{From: req.From, To: req.To, Now: h.clock.Now()})
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}

	from, to := query.Range()
	if query.Table() {
		return h.table(c, projectID, query, from, to)
	}
	if take, ok := query.Take(); ok && (req.Cursor == "" || left < 0) {
		left = take
	}
	if left >= 0 {
		limit = min(limit, left)
	}
	logs, err := h.logs.ScanLogs(c.UserContext(), storage.LogScan{
		ProjectID: projectID,
		From:      from,
		To:        to,
		After:     after,
		Where:     query.Where,
		Limit:     limit,
	})
	if err != nil {
		logger.L.Error("Log query failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	var next *string
	if len(logs) == limit && limit > 0 && left != limit {
		if left >= 0 {
			left -= limit
		}
		cursor := encodeCursor(logs[len(logs)-1], left)
		next = &cursor
	}
	return c.JSON(fiber.Map{"logs": logs, "next_cursor": next})
}

// table answers a run of a Query that returns a table: {"table": {"columns":
// [{"name", "type"}], "rows": [[…]], "truncated"}}, at most maxTableRows
// rows, truncated when there were more.
func (h *LogsHandler) table(c *fiber.Ctx, projectID uint32, query *logquery.Query, from, to time.Time) error {
	raw, err := h.logs.ScanLogTable(c.UserContext(), storage.LogTableScan{
		ProjectID: projectID,
		From:      from,
		To:        to,
		Where:     query.Where,
		Select:    query.Select,
		Query:     func(logs string) string { return query.TableSQL(logs, maxTableRows+1) },
	})
	if err != nil {
		logger.L.Error("Log table query failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	truncated := len(raw) > maxTableRows
	raw = raw[:min(len(raw), maxTableRows)]
	rows := make([][]any, len(raw))
	for i, r := range raw {
		rows[i] = query.Row(r)
	}
	return c.JSON(fiber.Map{"table": fiber.Map{"columns": query.Columns(), "rows": rows, "truncated": truncated}})
}

// POST /api/:project_id/logs/validate
//
// Checks a Query, {"query"}, without running it. Answers {"errors": […],
// "chips", "stages"}: each error with its message and the line and column
// (from 1, in characters) where it starts and ends; chips when the Builder
// can show the Query, else null; and, when it has no errors, where each stage
// is and whether it is a leading filter the Builder can show
// (logquery.Stage).
func (h *LogsHandler) Validate(c *fiber.Ctx) error {
	var req struct {
		Query string `json:"query"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with query"})
	}
	query, err := logquery.Compile(req.Query, logquery.Bounds{Now: h.clock.Now()})
	if err != nil {
		return c.JSON(fiber.Map{"errors": queryErrors(req.Query, err), "chips": nil, "stages": []logquery.Stage{}})
	}
	return c.JSON(fiber.Map{"errors": []logquery.Problem{}, "chips": query.Chips(), "stages": query.Stages()})
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

// A cursor is a Log's position, and how many Logs the Query's take has left
// to give (-1 for none), opaque to clients.
func encodeCursor(log models.Log, left int) string {
	text := fmt.Sprintf("%s|%s", log.Time.UTC().Format(time.RFC3339Nano), log.ID)
	if left >= 0 {
		text += "|" + strconv.Itoa(left)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(text))
}

func decodeCursor(cursor string) (*storage.LogPosition, int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, 0, err
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, 0, errors.New("invalid cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, 0, err
	}
	logID, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, 0, err
	}
	left := -1
	if len(parts) == 3 {
		if left, err = strconv.Atoi(parts[2]); err != nil || left < 0 {
			return nil, 0, errors.New("invalid cursor")
		}
	}
	return &storage.LogPosition{Time: at, ID: logID}, left, nil
}
