package main

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/agentprocessing"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/ingestgap"
	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/logtransform"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/processing"
	"github.com/solidtrace/event_store/spanprocessing"
	"github.com/solidtrace/event_store/storage"
	"github.com/valyala/fasthttp"
)

// After a step fails, the start waits and tries it again. The wait starts at
// firstStepWait and doubles up to maxStepWait. Thus the start sees a ready
// ClickHouse at most about maxStepWait late.
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
	// done is closed when run returns.
	done chan struct{}
	// stopped is closed at shutdown. After it, the start starts no step and
	// no worker.
	stopped chan struct{}

	mu sync.Mutex
	// failed is the error of the step that failed last, while it retries.
	failed error
	// stops are those of the workers the start started, in that order.
	stops []func()
}

func newClickHouseStart(clickHouse handler.Pinger) *clickHouseStart {
	return &clickHouseStart{clickHouse: clickHouse, done: make(chan struct{}), stopped: make(chan struct{})}
}

// run runs steps in order, and stops when ctx is done or at stop.
func (s *clickHouseStart) run(ctx context.Context, steps []startStep) {
	defer close(s.done)
	for _, step := range steps {
		if s.stopping() {
			return
		}
		if step.try != nil && !s.retry(ctx, step.name, step.try) {
			return
		}
		if step.start != nil && !s.begin(step.start, step.stop) {
			return
		}
	}
}

// retry runs try until it passes. It returns false if ctx is done or the
// start stops first.
func (s *clickHouseStart) retry(ctx context.Context, name string, try func() error) bool {
	wait := firstStepWait
	for {
		err := try()
		s.mu.Lock()
		s.failed = nil
		if err != nil {
			s.failed = fmt.Errorf("%w. The last try to %s failed: %w", handler.ErrClickHouseStarting, name, err)
		}
		s.mu.Unlock()
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
		case <-s.stopped:
			return false
		case <-time.After(wait):
		}
		wait = min(2*wait, maxStepWait)
	}
}

// begin starts a worker, unless the start has stopped, and keeps its stop.
func (s *clickHouseStart) begin(start, stop func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping() {
		return false
	}
	start()
	if stop != nil {
		s.stops = append(s.stops, stop)
	}
	return true
}

func (s *clickHouseStart) stopping() bool {
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
}

// serve makes the query port serve app's routes from now on.
func (s *clickHouseStart) serve(app *fiber.App) {
	queries := app.Handler()
	s.queries.Store(&queries)
	logger.L.Info("ClickHouse is ready: the query port answers reads")
}

// Ping is ClickHouse's ping once the start has passed, for /health. Before
// that, it is ErrClickHouseStarting, with the error of the step that failed
// last.
func (s *clickHouseStart) Ping() error {
	if s.queries.Load() == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.failed != nil {
			return s.failed
		}
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

// stop ends the start: from now on it starts no step and no worker. It waits
// up to wait for a step that runs. Thus the step ends before the stores close.
func (s *clickHouseStart) stop(wait time.Duration) {
	s.mu.Lock()
	if !s.stopping() {
		close(s.stopped)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
	case <-time.After(wait):
		logger.L.Warn("A step of the ClickHouse start still runs at shutdown", "waited", wait)
	}
}

// workerStops are the stops of the workers the start started, the last
// first.
func (s *clickHouseStart) workerStops() []func() {
	s.mu.Lock()
	stops := slices.Clone(s.stops)
	s.mu.Unlock()
	slices.Reverse(stops)
	return stops
}

// clickHouseWork is the part of EventStore that needs ClickHouse: the
// workers that store into it, and the query routes that read it. Its steps
// set localCache, diskLimit and logCleanup, and the routes step reads them.
type clickHouseWork struct {
	cfg                *config.Config
	pebble             *storage.PebbleWriter
	clickHouse         *storage.ClickHouse
	logs               *storage.ClickHouseLogs
	sqlQueries         *storage.ClickHouseSQL
	settings           *storage.ConsoleSettings
	evidenceFiles      *storage.EvidenceFiles
	logTransforms      *logtransform.Transforms
	ingest             *ingest.Service
	health             *handler.HealthHandler
	ingestGap          ingestgap.Gap
	eventProcessor     *processing.Processor
	eventCleanup       *processing.Cleanup
	logProcessor       *logprocessing.Processor
	spanProcessor      *spanprocessing.Processor
	agentFileProcessor *agentprocessing.Processor
	agentParser        *agentprocessing.Parser

	localCache *storage.LocalCacheFollower
	diskLimit  storage.LogDiskLimit
	logCleanup *logprocessing.Cleanup
}

// steps are the steps of the ClickHouse start, in order. The last one makes
// start serve the query routes.
func (w *clickHouseWork) steps(ctx context.Context, start *clickHouseStart) []startStep {
	return []startStep{
		{name: "ready EventStore's database", try: func() error { return w.clickHouse.Prepare(ctx) }},
		// The Logs from before the switch, in the DuckDB release's Log files,
		// are read in place. This loads what prepare-clickhouse wrote. On
		// DuckDB data it didn't prepare it fails, and the start goes no
		// further. It runs before Log processing, which reads the Processing
		// cursors it loads.
		{name: "ready the Logs from before the switch to ClickHouse", try: func() error {
			return w.logs.UseOldLogFiles(storage.OldLogFiles{
				LogsDuckDB: w.cfg.LogsDuckDBPath,
				// Compose mounts logs_parquet_path at ClickHouse's
				// user_files_path, where deploy/install finds it, and
				// bin/clickhouse-dev the development store, so ClickHouseDir
				// is "": file() reads each file by its path in the store.
				LocalDir: w.cfg.LogsParquetPath,
				Bucket:   w.cfg.ObjectStore,
			})
		}},
		// With a bucket, the day tables' parts move there once as old as
		// Settings → Instance says, read now and every minute.
		{
			start: func() {
				w.localCache = storage.FollowLocalCacheHours(w.clickHouse, w.settings, storage.LocalCacheHoursInterval)
			},
			stop: func() { w.localCache.Stop() },
		},
		{name: "prepare Event processing", try: w.eventProcessor.Prepare, start: w.eventProcessor.Start, stop: w.eventProcessor.Stop},
		{start: w.eventCleanup.Start, stop: w.eventCleanup.Stop},
		{name: "prepare Log processing", try: w.logProcessor.Prepare, start: w.logProcessor.Start, stop: w.logProcessor.Stop},
		{name: "prepare Span processing", try: w.spanProcessor.Prepare, start: w.spanProcessor.Start, stop: w.spanProcessor.Stop},
		{name: "prepare Agent file processing", try: w.agentFileProcessor.Prepare},
		{name: "prepare the parse of Agent sessions", try: w.agentParser.Prepare},
		{start: w.agentFileProcessor.Start, stop: w.agentFileProcessor.Stop},
		{start: w.agentParser.Start, stop: w.agentParser.Stop},
		{name: "work out the Log disk limit", try: func() error {
			limit, err := w.logs.DiskLimit(w.cfg.LogsDiskLimitBytes)
			if err != nil {
				return err
			}
			w.diskLimit = limit
			logger.L.Info("Log disk limit", "bytes", limit.Bytes)
			return nil
		}},
		{
			start: func() {
				w.logCleanup = logprocessing.NewCleanup(w.logs, w.sqlQueries, w.diskLimit, clock.System{},
					&logprocessing.Evidence{Files: w.evidenceFiles, Retention: w.settings.EvidenceRetention},
					logprocessing.Family{Days: storage.LogDays, Retention: w.settings.ProjectLogRetention},
					logprocessing.Family{Days: storage.SpanDays, Retention: w.settings.ProjectTraceRetention},
					// The Agent Project's Log Retention, which its derived Logs keep too.
					logprocessing.Family{Days: storage.AgentDays, Retention: w.settings.ProjectLogRetention})
				w.logCleanup.Start()
			},
			stop: func() { w.logCleanup.Stop() },
		},
		// Its stop is StopFlushingIngestCounts in run, which also writes the
		// last seconds' counts.
		{start: w.ingest.StartFlushingIngestCounts},
		{start: func() { start.serve(w.queryRoutes()) }},
	}
}

// queryRoutes are the query port's routes, once ClickHouse is ready.
func (w *clickHouseWork) queryRoutes() *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	logsHandler := handler.NewLogsHandler(w.logs, w.sqlQueries, w.settings, clock.System{})
	logsHandler.HeavyReads = handler.NewHeavyReads(w.cfg.LogsHeavyReadSlots)
	logsHandler.IngestGap = w.ingestGap
	overviewHandler := handler.NewOverviewHandler(handler.OverviewConfig{
		Logs: w.logs, Clock: clock.System{}, DiskLimit: w.diskLimit, Retention: w.settings,
		LocalCacheHours: w.clickHouse.LocalCacheHours,
	})
	handler.RegisterQueryRoutes(app, handler.NewEventsHandler(w.pebble, w.clickHouse), logsHandler,
		handler.NewLogTransformHandler(w.logTransforms), overviewHandler, handler.NewTracesHandler(w.clickHouse),
		w.health, handler.NewConfigHandler(w.cfg, w.diskLimit.Bytes))
	handler.RegisterPerformanceRoutes(app, handler.NewPerformanceHandler(w.clickHouse))
	handler.RegisterRedactionRoutes(app)
	handler.RegisterAgentQueryRoutes(app, handler.NewAgentSessionsHandler(w.logs))
	handler.RegisterReviewRecordRoutes(app, handler.NewReviewRecordsHandler(w.logs))
	handler.RegisterRunsQueryRoutes(app, handler.NewRunsHandler(w.logs, clock.System{}))
	handler.RegisterAgentEvidenceRoute(app, handler.NewAgentEvidenceHandler(w.clickHouse))
	handler.RegisterEvidenceQueryRoutes(app, handler.NewEvidenceFilesHandler(w.evidenceFiles))
	handler.RegisterRetentionRoute(app, w.logCleanup)
	return app
}
