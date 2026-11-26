package main

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/valyala/fasthttp"
)

// The wait after a step of the ClickHouse start fails doubles from
// firstStepWait up to maxStepWait. So EventStore sees a ClickHouse that has
// loaded its tables within about maxStepWait.
const (
	firstStepWait = time.Second
	maxStepWait   = 10 * time.Second
)

// startStep is one step of the ClickHouse start. try, if set, runs again
// with backoff until it passes. Then start, if set, starts a worker, and
// stop, if set, stops it at shutdown.
type startStep struct {
	name        string
	try         func() error
	start, stop func()
}

// clickHouseStart runs the steps of EventStore's start that need ClickHouse
// in the background, while ingest takes Logs and Events into Pebble. The
// steps run in order, and a step runs only after the one before it passed.
// Until the last step passes, the query port answers each read with 503
// (handler.ErrClickHouseStarting), and /health says ClickHouse is starting.
type clickHouseStart struct {
	clickHouse handler.Pinger
	// queries serves the query routes, once the last step has passed.
	queries atomic.Pointer[fasthttp.RequestHandler]
	done    chan struct{}

	mu sync.Mutex
	// stopping is set at shutdown. After it, the start starts no worker.
	stopping bool
	// stops are those of the workers the start started, in that order.
	stops []func()
}

func newClickHouseStart(clickHouse handler.Pinger) *clickHouseStart {
	return &clickHouseStart{clickHouse: clickHouse, done: make(chan struct{})}
}

// run runs steps in order, and stops when ctx is done.
func (s *clickHouseStart) run(ctx context.Context, steps []startStep) {
	defer close(s.done)
	for _, step := range steps {
		if step.try != nil && !s.retry(ctx, step.name, step.try) {
			return
		}
		if step.start != nil && !s.begin(step.start, step.stop) {
			return
		}
	}
}

// retry runs try until it passes, and returns false if ctx is done first.
func (s *clickHouseStart) retry(ctx context.Context, name string, try func() error) bool {
	wait := firstStepWait
	for {
		err := try()
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		logger.L.Warn("A step of the ClickHouse start failed; it runs again", "step", name, "error", err, "in", wait)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(wait):
		}
		wait = min(2*wait, maxStepWait)
	}
}

// begin starts a worker, unless shutdown has begun, and keeps its stop.
func (s *clickHouseStart) begin(start, stop func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return false
	}
	start()
	if stop != nil {
		s.stops = append(s.stops, stop)
	}
	return true
}

// serve makes the query port serve app's routes from now on.
func (s *clickHouseStart) serve(app *fiber.App) {
	queries := app.Handler()
	s.queries.Store(&queries)
	logger.L.Info("ClickHouse is ready: the query port answers reads")
}

// Ping is ClickHouse's ping once the start has passed, for /health.
func (s *clickHouseStart) Ping() error {
	if s.queries.Load() == nil {
		return handler.ErrClickHouseStarting
	}
	return s.clickHouse.Ping()
}

// serveQueries gives a request on the query port to the query routes, or
// answers 503 while ClickHouse starts.
func (s *clickHouseStart) serveQueries(c *fiber.Ctx) error {
	queries := s.queries.Load()
	if queries == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": handler.ErrClickHouseStarting.Error()})
	}
	(*queries)(c.Context())
	return nil
}

// stop ends the start: from now on it starts no worker. It waits up to
// wait for a step that runs, so that the step ends before the stores close,
// and returns the stops of the workers it started, the last first.
func (s *clickHouseStart) stop(wait time.Duration) []func() {
	s.mu.Lock()
	s.stopping = true
	stops := slices.Clone(s.stops)
	s.mu.Unlock()
	select {
	case <-s.done:
	case <-time.After(wait):
		logger.L.Warn("A step of the ClickHouse start still runs at shutdown", "waited", wait)
	}
	slices.Reverse(stops)
	return stops
}
