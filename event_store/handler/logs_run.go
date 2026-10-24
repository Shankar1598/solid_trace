package handler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// maxTimeoutSeconds is the longest timeout a run may ask for, a day.
const maxTimeoutSeconds = 24 * 60 * 60

// run is one run of a Query, a histogram or SQL: when it began, for its
// answer's elapsed_ms, and the context its scan stops by, which ends after
// the request's timeout_seconds, if it has one. When Console asks with
// X-Log-SQL: 1, as it does for the reads it times, its end logs each SQL
// statement the run made ClickHouse run, with the X-Request-Id Console sent,
// which Console's call timing of the run also has, and the user it ran for,
// X-User-Email and X-User-Name, percent-encoded
// (.scratch/call-timings/spec.md).
//
// Fiber gives a handler no context that ends when the client goes away, so a
// scan without a timeout runs to its end whether or not anyone is waiting.
// Past the timeout the context ends: clickhouse-go cancels the query it is
// running, a scan of Log rows stops before its next span, and the run
// answers 504.
type run struct {
	ctx        context.Context
	cancel     context.CancelFunc
	began      time.Time
	timeout    float64
	read       string
	projectID  string
	requestID  string
	userEmail  string
	userName   string
	statements *storage.ReadStatements
}

// readKinds are the kinds of the runs whose ClickHouse statements are tagged
// with their Project, for the Overview's read times. A Query that returns a
// table is tagged again as one. SQL isn't tagged: its Project's SQL user
// names it.
var readKinds = map[string]string{"query": storage.ReadNewestPage, "match": storage.ReadMatch, "histogram": storage.ReadHistogram}

// startRun starts a run, a read of query, match, histogram or sql, with a
// request's timeout_seconds, nil for none. If the timeout isn't a positive
// number of seconds, at most a day, it returns the answer to give instead.
// The caller must end the run.
func startRun(c *fiber.Ctx, read string, timeoutSeconds *float64) (*run, fiber.Map) {
	// Fiber reuses a request's buffers once its handler returns.
	r := &run{began: time.Now(), read: read, projectID: strings.Clone(c.Params("project_id")), requestID: strings.Clone(c.Get("X-Request-Id"))}
	ctx := c.UserContext()
	if kind, ok := readKinds[read]; ok {
		if projectID, err := parseProjectID(c); err == nil {
			ctx = storage.TagRead(ctx, projectID, kind)
		}
	}
	if c.Get("X-Log-SQL") == "1" {
		ctx, r.statements = storage.WithReadStatements(ctx)
		r.userEmail = strings.Clone(c.Get("X-User-Email"))
		r.userName = strings.Clone(c.Get("X-User-Name"))
		if name, err := url.PathUnescape(r.userName); err == nil {
			r.userName = name
		}
	}
	if timeoutSeconds == nil {
		r.ctx, r.cancel = context.WithCancel(ctx)
		return r, nil
	}
	if *timeoutSeconds <= 0 || *timeoutSeconds > maxTimeoutSeconds {
		return nil, fiber.Map{"error": "timeout_seconds must be a positive number of seconds, at most a day"}
	}
	r.timeout = *timeoutSeconds
	r.ctx, r.cancel = context.WithTimeout(ctx, time.Duration(r.timeout*float64(time.Second)))
	return r, nil
}

// end ends the run's context and, if it was asked to, logs each statement
// it ran: its SQL, how long it took, its error and who it ran for, numbered
// in the order they ran.
func (r *run) end() {
	r.cancel()
	if r.statements == nil {
		return
	}
	statements := r.statements.List()
	for i, statement := range statements {
		fields := []any{
			"kind", "log_read_sql", "read", r.read, "project_id", r.projectID, "request_id", r.requestID,
			"statement", i + 1, "statements", len(statements),
			"duration_ms", float64(statement.Duration.Microseconds()) / 1000, "sql", statement.SQL,
		}
		if r.userEmail != "" {
			fields = append(fields, "user_email", r.userEmail)
		}
		if r.userName != "" {
			fields = append(fields, "user_name", r.userName)
		}
		if statement.Err != nil {
			logger.L.Warn("Log read SQL", append(fields, "error", statement.Err)...)
		} else {
			logger.L.Info("Log read SQL", fields...)
		}
	}
}

// timedOut reports whether the run has gone past its timeout, so that a
// scan that failed was stopped.
func (r *run) timedOut() bool {
	// clickhouse-go gives its connection the run's deadline, which can
	// fail a read a moment before the context says it is past.
	deadline, ok := r.ctx.Deadline()
	return errors.Is(r.ctx.Err(), context.DeadlineExceeded) || ok && !time.Now().Before(deadline)
}

// shorterRange is the advice for a run of a time range stopped at its
// timeout.
const shorterRange = "try a shorter time range"

// stopped answers a run stopped at its timeout: 504, with {"errors":
// [{"message"}]}, the message ending with advice.
func (r *run) stopped(c *fiber.Ctx, advice string) error {
	message := fmt.Sprintf("the run was stopped after %s s, its timeout: %s",
		strconv.FormatFloat(r.timeout, 'f', -1, 64), advice)
	return c.Status(fiber.StatusGatewayTimeout).JSON(fiber.Map{"errors": []fiber.Map{{"message": message}}})
}

// answer answers the run with body and elapsed_ms, how long it took in whole
// milliseconds.
func (r *run) answer(c *fiber.Ctx, body fiber.Map) error {
	body["elapsed_ms"] = time.Since(r.began).Milliseconds()
	return c.JSON(body)
}
