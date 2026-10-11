package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
)

// maxTimeoutSeconds is the longest timeout a run may ask for, a day.
const maxTimeoutSeconds = 24 * 60 * 60

// run is one run of a Query, a histogram or SQL: when it began, for its
// answer's elapsed_ms, and the context its scan stops by, which ends after
// the request's timeout_seconds, if it has one.
//
// Fiber gives a handler no context that ends when the client goes away, so a
// scan without a timeout runs to its end whether or not anyone is waiting.
// Past the timeout the context ends: duckdb-go interrupts the query it is
// running, a scan of Log rows stops before its next hour, and the run
// answers 504.
type run struct {
	ctx     context.Context
	cancel  context.CancelFunc
	began   time.Time
	timeout float64
}

// startRun starts a run with a request's timeout_seconds, nil for none. If
// the timeout isn't a positive number of seconds, at most a day, it returns
// the answer to give instead. The caller must end the run.
func startRun(c *fiber.Ctx, timeoutSeconds *float64) (*run, fiber.Map) {
	r := &run{began: time.Now()}
	if timeoutSeconds == nil {
		r.ctx, r.cancel = context.WithCancel(c.UserContext())
		return r, nil
	}
	if *timeoutSeconds <= 0 || *timeoutSeconds > maxTimeoutSeconds {
		return nil, fiber.Map{"error": "timeout_seconds must be a positive number of seconds, at most a day"}
	}
	r.timeout = *timeoutSeconds
	r.ctx, r.cancel = context.WithTimeout(c.UserContext(), time.Duration(r.timeout*float64(time.Second)))
	return r, nil
}

// end ends the run's context.
func (r *run) end() {
	r.cancel()
}

// timedOut reports whether the run has gone past its timeout, so that a
// scan that failed was stopped.
func (r *run) timedOut() bool {
	return errors.Is(r.ctx.Err(), context.DeadlineExceeded)
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
