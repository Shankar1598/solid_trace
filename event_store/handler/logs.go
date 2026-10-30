package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/logquery"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// defaultScanBudget is how long a run of Log rows reads hours before it
	// answers with what it has found, so that a search for rare Logs shows
	// them while it goes on.
	defaultScanBudget = 1500 * time.Millisecond
	defaultQueryLogs  = 200
	maxQueryLogs      = 1000
	// maxTableRows is how many rows of a table a run returns at most.
	maxTableRows = 10000
)

// LogsHandler serves stored Logs on the query app: it runs and validates
// Queries, fetches single Logs, and runs SQL while the instance allows it.
type LogsHandler struct {
	// clickHouse holds the Logs: Log rows, tables, the histogram, single
	// Logs, the Logs ingested after a batch and the Fields are read from it.
	// sql runs SQL queries on it, each as its Project's SQL user.
	clickHouse *storage.ClickHouseLogs
	sql        *storage.ClickHouseSQL
	settings   ConsoleSettings
	clock      clock.Clock
	// ScanBudget is how long a run of Log rows reads before it answers with
	// what it has found and a next_cursor.
	ScanBudget time.Duration
	// HeavyReads are the slots heavy reads hold while they run
	// (logs_heavy_reads.go).
	HeavyReads *HeavyReads
}

// NewLogsHandler serves the Logs in clickHouse, and SQL queries through sql;
// settings say whether SQL runs, how much a Query that matches text may
// read, and what each Project's Logs are named in Queries; clock is the time
// ago() and now() read.
func NewLogsHandler(clickHouse *storage.ClickHouseLogs, sql *storage.ClickHouseSQL, settings ConsoleSettings, clock clock.Clock) *LogsHandler {
	return &LogsHandler{
		clickHouse: clickHouse, sql: sql, settings: settings, clock: clock, ScanBudget: defaultScanBudget,
		HeavyReads: NewHeavyReads(config.DefaultLogsHeavyReadSlots(runtime.NumCPU())),
	}
}

// queryRequest is the body of a run.
type queryRequest struct {
	Query          string    `json:"query"`
	From           time.Time `json:"from"`
	To             time.Time `json:"to"`
	Limit          int       `json:"limit"`
	Cursor         string    `json:"cursor"`
	TimeoutSeconds *float64  `json:"timeout_seconds"`
	// AfterBatch is the after_batch as sent, nil when the request has
	// none. It is raw so that null can be told apart from no after_batch.
	AfterBatch json.RawMessage `json:"after_batch"`
	// Totals asks for a time chart's totals with its table (see table).
	Totals bool `json:"totals"`
	// Parameters are the values of the Dashboard filters the Query names,
	// by name. A filter without values is All.
	Parameters map[string][]string `json:"parameters"`
	// check_budget, force and estimate say how the read is sized.
	budgetOptions
}

// The most values a run takes for one Dashboard filter, and the longest
// value, in bytes.
const (
	maxParameterValues      = 100
	maxParameterValueLength = 1000
)

// parametersProblem says what is wrong with a run's Dashboard filter
// values, if anything.
func parametersProblem(parameters map[string][]string) fiber.Map {
	for name, values := range parameters {
		if len(values) > maxParameterValues {
			return fiber.Map{"error": fmt.Sprintf("parameters: %s has more than %d values", name, maxParameterValues)}
		}
		for _, v := range values {
			if len(v) > maxParameterValueLength {
				return fiber.Map{"error": fmt.Sprintf("parameters: a value of %s is longer than %d bytes", name, maxParameterValueLength)}
			}
		}
	}
	return nil
}

// POST /api/:project_id/logs/query
//
// Runs a Query over a Project's Logs of [from, to): {"query", "from", "to"}
// (RFC 3339), with "limit" (default 200, at most 1,000), "cursor" (a
// previous page's next_cursor), "timeout_seconds", "totals" and "parameters"
// optional. "parameters" fills in the Dashboard filters the Query names,
// {"<name>": ["<value>", …]}, with at most 100 values of at most 1,000 bytes
// each; a filter it leaves out is All. A Query of Log rows answers
// {"logs": […], "next_cursor"}, newest first; next_cursor is null once there
// are no older Logs, or take's number of Logs has been given.
// A run that has read for ScanBudget answers with what it has found, fewer
// than limit Logs or none, and a next_cursor to go on from. A Query that
// returns a table, one with project, extend, summarize, a sort other than
// newest first, top, count or distinct, answers {"table": …} (see table), and
// ignores limit and cursor. Either answer carries "elapsed_ms", how long the
// run took. A Query with mistakes answers 400 with {"errors": […]}, as
// validate lists them.
//
// Before it runs, the read is sized from the counts: "check_budget" refuses
// a Query over the budget, answering {"refusal": …} instead; "force" runs
// it as written; "estimate" answers {"estimate", "refusal"} without running
// it (logs_budget.go). A page after the first, with a cursor, is never
// refused: its range is within the first page's.
//
// The scan goes on if the client goes away, as Fiber gives the handler no
// context that ends with the connection: it stops at limit Logs, the end of
// the range, or timeout_seconds, past which the run answers 504 with
// {"errors": [{"message"}]}.
//
// With "after_batch", a Match monitor's run reads the Logs ingested since
// its last run instead (afterBatch).
func (h *LogsHandler) Query(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	var req queryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with query, from and to"})
	}
	if problem := parametersProblem(req.Parameters); problem != nil {
		return c.Status(400).JSON(problem)
	}
	read := "query"
	if req.AfterBatch != nil {
		read = "match"
	}
	run, problem := startRun(c, read, req.TimeoutSeconds)
	if problem != nil {
		return c.Status(400).JSON(problem)
	}
	defer run.end()
	if req.AfterBatch != nil {
		return h.afterBatch(c, run, projectID, req)
	}
	if problem := rangeProblem(req.From, req.To); problem != nil {
		return c.Status(400).JSON(problem)
	}
	limit, ok := limitOf(req.Limit)
	if !ok {
		return c.Status(400).JSON(fiber.Map{"error": "limit must be a positive whole number"})
	}
	var after *storage.LogPosition
	left := -1 // Logs a take has left to give, or -1 for no take
	if req.Cursor != "" {
		if after, left, err = decodeCursor(req.Cursor); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid cursor"})
		}
	}
	query, err := h.compile(projectID, req.Query, logquery.Bounds{From: req.From, To: req.To, Now: h.clock.Now()}, req.Parameters)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}

	from, to := query.Range()
	budget := req.budgetOptions
	budget.CheckBudget = budget.CheckBudget && req.Cursor == ""
	if answered, err := h.sizeUp(c, run, projectID, req.Query, query, from, to, budget); answered {
		return err
	}
	// A page after the first is never refused.
	if req.Cursor == "" {
		if answered, err := h.holdSlot(c, run, projectID, req.Query, budget); answered {
			return err
		}
	}
	if query.Table() {
		run.ctx = storage.TagRead(run.ctx, projectID, storage.ReadTable)
		return h.table(c, run, projectID, query, from, to, req.Totals)
	}
	if take, ok := query.Take(); ok && (req.Cursor == "" || left < 0) {
		left = take
	}
	if left >= 0 {
		limit = min(limit, left)
	}
	page, err := h.clickHouse.ScanLogs(run.ctx, storage.LogScan{
		ProjectID: projectID,
		From:      from,
		To:        to,
		After:     after,
		Where:     func(cols storage.LogColumns) storage.Filter { return query.Filter(cols) },
		Limit:     limit,
		Budget:    h.ScanBudget,
	})
	if err != nil {
		if run.timedOut() {
			return run.stopped(c, shorterRange)
		}
		if run.replaced() {
			return run.answerReplaced(c)
		}
		logger.L.Error("Log query failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	var next *string
	if left >= 0 {
		left -= len(page.Logs)
	}
	if page.Next != nil && left != 0 {
		cursor := encodeCursor(*page.Next, left)
		next = &cursor
	}
	return run.answer(c, fiber.Map{"logs": page.Logs, "next_cursor": next})
}

// afterBatch answers a run with "after_batch", as a Match monitor runs its
// Query: it reads the Project's Logs ingested after a Log batch, those of
// the later batches Log processing has stored, whatever their time. Log ids
// don't follow the order Log processing stores Logs in, but each Project's
// Log batch ids do.
//
//   - "after_batch": "" arms a new Monitor: the Query is checked, nothing is
//     read, and the answer is {"logs": [], "next_cursor": null,
//     "ingest_cursor", "total": 0, "elapsed_ms"}.
//   - "after_batch": W, a Log batch id, the last run's ingest_cursor, reads
//     the Logs of the batches after W up to ingest_cursor, and answers
//     {"logs": […], "next_cursor": null, "ingest_cursor", "total",
//     "elapsed_ms"}: up to limit Logs that pass the Query, newest first, and
//     total, how many passed it in all.
//
// ingest_cursor is the Project's Log processing cursor, read in the same
// transaction as the Logs: the after_batch of the Monitor's next run. It is
// the nil UUID until Log processing has stored a batch for the Project, so a
// Monitor armed then reads every Log stored after.
//
// from, to and cursor are ignored, and from and to may be left out; a where
// on time in the Query still applies. Only a Query that returns Log rows can
// run after a batch: one that returns a table answers 400 with {"errors":
// [{"message"}]}, as does a Query with mistakes. An after_batch that is
// neither "" nor a UUID answers 400, null too: a Monitor with no
// ingest_cursor yet arms with "".
//
// The run reads its whole window, with no ScanBudget, but stops at
// timeout_seconds with a 504. The Monitor then keeps its after_batch, and
// its next window is larger still, so one whose runs keep stopping never
// catches up: Console arms it again with "", which skips the Logs ingested
// in between, and says so.
func (h *LogsHandler) afterBatch(c *fiber.Ctx, run *run, projectID uint32, req queryRequest) error {
	afterBatch, after, ok := parseAfterBatch(req.AfterBatch)
	if !ok {
		return c.Status(400).JSON(fiber.Map{"error": "after_batch must be a Log batch id, or empty to arm a Monitor"})
	}
	limit, ok := limitOf(req.Limit)
	if !ok {
		return c.Status(400).JSON(fiber.Map{"error": "limit must be a positive whole number"})
	}
	query, err := h.compile(projectID, req.Query, logquery.Bounds{Now: h.clock.Now()}, nil)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}
	if query.Table() {
		return c.Status(400).JSON(fiber.Map{"errors": []fiber.Map{{
			"message": "only a Query that returns Logs can read the Logs ingested after a batch, and this one returns a table",
		}}})
	}
	if take, ok := query.Take(); ok {
		limit = min(limit, take)
	}

	var found storage.IngestedLogs
	if afterBatch == "" {
		found.Logs = []models.Log{}
		found.Cursor, err = h.clickHouse.LogProcessingCursor(projectID)
	} else {
		found, err = h.clickHouse.ScanLogsIngestedAfter(run.ctx, storage.IngestedLogScan{
			ProjectID: projectID,
			After:     after,
			Where:     func(cols storage.LogColumns) storage.Filter { return query.Filter(cols) },
			Limit:     limit,
		})
	}
	if err != nil {
		if run.timedOut() {
			return run.stopped(c, "too many Logs were ingested since the last run to read in time")
		}
		logger.L.Error("Log query after a batch failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	return run.answer(c, fiber.Map{
		"logs":          found.Logs,
		"next_cursor":   nil,
		"ingest_cursor": found.Cursor.String(),
		"total":         found.Total,
	})
}

// parseAfterBatch reads an after_batch as sent: a string, "" or a Log batch
// id, after. ok is false for anything else, null included.
func parseAfterBatch(raw json.RawMessage) (text string, after uuid.UUID, ok bool) {
	if string(raw) == "null" || json.Unmarshal(raw, &text) != nil {
		return "", uuid.Nil, false
	}
	if text == "" {
		return "", uuid.Nil, true
	}
	after, err := uuid.Parse(text)
	return text, after, err == nil
}

// limitOf is how many Logs a run returns at most for a request's limit: the
// default for none, and never more than maxQueryLogs. ok is false for a
// negative limit.
func limitOf(requested int) (limit int, ok bool) {
	if requested < 0 {
		return 0, false
	}
	if requested == 0 {
		return defaultQueryLogs, true
	}
	return min(requested, maxQueryLogs), true
}

// table answers a run of a Query that returns a table: {"table": {"columns":
// [{"name", "type", "agg", "bin_seconds"}], "rows": [[…]], "truncated",
// "fill_gaps", "from", "to"}}, at most maxTableRows rows, truncated when
// there were more. agg and bin_seconds say how to chart a column
// (logquery.Column); fill_gaps, whether a chart may fill the time bins with
// no row; from and to are the range the Query read, the request's as a
// leading where on time narrows it.
//
// A chart's table made bin by bin takes the bins of hours that have ended
// from the bin cache, when it has them (logquery.Query.Bins).
//
// With totals asked for, the table of a time chart, a Query whose last stage
// is a summarize by a time bin, also has "totals": {"columns", "rows",
// "truncated"}, a row for each group with its aggregates over the whole range
// (logquery.Query.Totals), largest first, at most maxTableRows rows. Totals
// are worked out from the Logs, never from the bin cache, in the same run.
func (h *LogsHandler) table(c *fiber.Ctx, run *run, projectID uint32, query *logquery.Query, from, to time.Time, totals bool) error {
	rows, truncated, err := h.scanTable(run, projectID, query, from, to, true)
	if err != nil {
		return h.tableFailed(c, run, projectID, err)
	}
	table := fiber.Map{
		"columns":   query.Columns(),
		"rows":      rows,
		"truncated": truncated,
		// Cut short, the table lacks bins that have Logs: unsorted, its
		// rows go in order of their values, so the newest are cut.
		"fill_gaps": query.FillGaps() && !truncated,
		"from":      from.UTC().Format(time.RFC3339Nano),
		"to":        to.UTC().Format(time.RFC3339Nano),
	}
	if totalsQuery, ok := query.Totals(); totals && ok {
		rows, truncated, err := h.scanTable(run, projectID, totalsQuery, from, to, false)
		if err != nil {
			return h.tableFailed(c, run, projectID, err)
		}
		table["totals"] = fiber.Map{"columns": totalsQuery.Columns(), "rows": rows, "truncated": truncated}
	}
	return run.answer(c, fiber.Map{"table": table})
}

// scanTable runs a Query's table over [from, to): at most maxTableRows rows,
// and whether there were more. With bins, a table made bin by bin may take
// bins from the bin cache.
func (h *LogsHandler) scanTable(run *run, projectID uint32, query *logquery.Query, from, to time.Time, bins bool) ([][]any, bool, error) {
	scan := storage.TableScan{
		ProjectID: projectID,
		From:      from,
		To:        to,
		Where:     func(cols storage.LogColumns) storage.Filter { return query.Filter(cols) },
		Select:    func(cols storage.LogColumns) string { return query.Select(cols) },
		Query:     func(logs string) string { return query.TableSQL(logs, maxTableRows+1) },
		Now:       h.clock.Now(),
	}
	if b, ok := query.Bins(); ok && bins {
		scan.Bins = &storage.TableBins{Size: b.Size, Query: b.Key, Limit: maxTableRows + 1}
	}
	raw, err := h.clickHouse.ScanLogTable(run.ctx, scan)
	if err != nil {
		return nil, false, err
	}
	truncated := len(raw) > maxTableRows
	raw = raw[:min(len(raw), maxTableRows)]
	rows := make([][]any, len(raw))
	for i, r := range raw {
		rows[i] = query.Row(r)
	}
	return rows, truncated, nil
}

// tableFailed answers a run whose table failed: 504 once it has timed out,
// a busy refusal once a newer run has replaced it, else 500.
func (h *LogsHandler) tableFailed(c *fiber.Ctx, run *run, projectID uint32, err error) error {
	if run.timedOut() {
		return run.stopped(c, shorterRange)
	}
	if run.replaced() {
		return run.answerReplaced(c)
	}
	logger.L.Error("Log table query failed", "project_id", projectID, "error", err)
	return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
}

// POST /api/:project_id/logs/validate
//
// Checks a Query, {"query"}, without running it. Answers {"errors": […],
// "source", "chips", "stages", "parameters"}: each error with its message and
// the line and column (from 1, in characters) where it starts and ends; the
// name the Query gives its source, such as a Project's slug, else null, even
// when it names another Project; chips when the Builder can show the Query,
// else null; and, when it has no errors, where each stage is and whether it
// is a leading filter the Builder can show (logquery.Stage), and the
// Dashboard filters the Query names, in order of first use.
func (h *LogsHandler) Validate(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	var req struct {
		Query string `json:"query"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with query"})
	}
	var source *string
	if name := logquery.SourceOf(req.Query); name != "" {
		source = &name
	}
	query, err := h.compile(projectID, req.Query, logquery.Bounds{Now: h.clock.Now()}, nil)
	if err != nil {
		return c.JSON(fiber.Map{
			"errors": queryErrors(req.Query, err), "source": source, "chips": nil, "stages": []logquery.Stage{}, "parameters": []string{},
		})
	}
	parameters := query.Parameters()
	if parameters == nil {
		parameters = []string{}
	}
	return c.JSON(fiber.Map{
		"errors": []logquery.Problem{}, "source": source, "chips": query.Chips(), "stages": query.Stages(), "parameters": parameters,
	})
}

// compile compiles a Query for a Project: a source naming Logs other than
// the Project's is a mistake like any other. When Console's database can't
// be read the source goes unchecked, as the Query reads the Project's Logs
// whatever it names.
func (h *LogsHandler) compile(projectID uint32, text string, bounds logquery.Bounds, values map[string][]string) (*logquery.Query, error) {
	query, err := logquery.Compile(text, bounds, values)
	if err != nil {
		return nil, err
	}
	slug, err := h.settings.ProjectSlug(projectID)
	if err != nil {
		logger.L.Error("Reading the Project's slug failed", "project_id", projectID, "error", err)
		return query, nil
	}
	if err := logquery.CheckSource(text, slug); err != nil {
		return nil, err
	}
	return query, nil
}

// GET /api/:project_id/logs/:log_id?time=<RFC 3339>
//
// Returns one of a Project's Logs by id. With time, the Log's own time, only
// that day is read; without it, the days around its id's time, then every day.
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
	log, found, err := h.clickHouse.LogByID(storage.TagRead(c.UserContext(), projectID, storage.ReadLogByID), projectID, id, at)
	if err != nil {
		logger.L.Error("Log fetch failed", "project_id", projectID, "log_id", id, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	if !found {
		return c.Status(404).JSON(fiber.Map{"error": "Log not found"})
	}
	return c.JSON(log)
}

// rangeProblem is the answer to a request whose from and to aren't a time
// range, or nil if they are.
func rangeProblem(from, to time.Time) fiber.Map {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return fiber.Map{"error": "from and to must be RFC 3339 times, from no later than to"}
	}
	return nil
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
func encodeCursor(at storage.LogPosition, left int) string {
	text := fmt.Sprintf("%s|%s", at.Time.UTC().Format(time.RFC3339Nano), at.ID)
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
