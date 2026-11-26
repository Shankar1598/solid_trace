package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/agentprocessing"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/ingestgap"
	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/logtransform"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/cpuprofile"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/processing"
	"github.com/solidtrace/event_store/spanprocessing"
	"github.com/solidtrace/event_store/storage"
)

func main() {
	cfg := config.Load()
	logger.Init()
	for _, warning := range cfg.Warnings {
		logger.L.Warn(warning)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	run(ctx, cfg)
}

// run serves EventStore on cfg until ctx is done, and then shuts it down.
//
// It opens the stores on local disk and the ingest port first, and runs the
// steps that need ClickHouse in the background (clickHouseStart). Ingest
// writes only into Pebble, so a ClickHouse that is slow to start, as after a
// restart of the machine, does not stop it.
func run(ctx context.Context, cfg *config.Config) {
	// Initialize storage
	pebbleWriter, err := storage.NewPebbleWriter(cfg)
	if err != nil {
		logger.L.Fatal("Failed to open Pebble", "error", err)
	}
	defer pebbleWriter.Close()

	// Event processing stores Events' rows in ClickHouse, and Event reads
	// read them there. Log processing inserts Logs into it, and Log rows,
	// single Logs, Match monitors and Fields read them there.
	clickHouse, err := storage.NewClickHouse(cfg.ClickHouse, cfg.ObjectStore)
	if err != nil {
		logger.L.Fatal("Failed to open ClickHouse", "error", err)
	}
	defer clickHouse.Close()
	clickHouseLogs := storage.NewClickHouseLogs(clickHouse, pebbleWriter)
	clickHouseLogs.UseBinCache(cfg.LogsBinCacheBytes)

	sqliteWriter, err := storage.NewSQLiteWriter(cfg.SQLitePath)
	if err != nil {
		logger.L.Fatal("Failed to open SQLite", "error", err)
	}
	defer sqliteWriter.Close()

	evidenceFiles, err := storage.NewEvidenceFiles(sqliteWriter, storage.NewObjectStore(cfg.ObjectStore), cfg.EvidencePath)
	if err != nil {
		logger.L.Fatal("Failed to open the Evidence files", "error", err)
	}

	// Initialize Message Queue Writer (for Event Store -> Rails)
	messageQueueWriter, err := storage.NewMessageQueueWriter(cfg.MessageQueuePath)
	if err != nil {
		logger.L.Fatal("Failed to open Message Queue Writer", "error", err)
	}
	defer messageQueueWriter.Close()

	// Initialize auth
	projectAuth, err := auth.NewProjectAuth(cfg.SQLitePath)
	if err != nil {
		logger.L.Fatal("Failed to connect to SQLite", "error", err)
	}
	defer projectAuth.Close()

	consoleSettings, err := storage.NewConsoleSettings(cfg.SQLitePath)
	if err != nil {
		logger.L.Fatal("Failed to open Console settings", "error", err)
	}
	defer consoleSettings.Close()

	// The Event cleanup never runs inside a batch: they share batchLock.
	var batchLock sync.Mutex
	processor := processing.New(pebbleWriter, clickHouse, messageQueueWriter, sqliteWriter, &batchLock)
	// Deletes a deleted Project's Events, holding batchLock while it does.
	eventCleanup := processing.NewCleanup(pebbleWriter, clickHouse, sqliteWriter, consoleSettings, &batchLock, clock.System{})
	logTransforms := logtransform.New(consoleSettings, clock.System{})
	logProcessor := logprocessing.New(pebbleWriter, clickHouseLogs, logTransforms, clock.System{})
	spanProcessor := spanprocessing.New(pebbleWriter, clickHouse)
	// The parse makes each line that Agent file processing stores into a
	// Log of the Agent Project, with no Log transform.
	agentParser := agentprocessing.NewParser(pebbleWriter, clickHouseLogs, consoleSettings)
	agentFileProcessor := agentprocessing.New(pebbleWriter, clickHouse, agentParser.Notify)
	sqlQueries := storage.NewClickHouseSQL(clickHouse, storage.DefaultSQLLimits())

	// CPU profiles are written only when the file names their directory.
	var profiler *cpuprofile.Profiler
	if cfg.CPUProfileDir != "" {
		if profiler, err = cpuprofile.New(cfg.CPUProfileDir, clock.System{}); err != nil {
			logger.L.Fatal("Failed to set up CPU profiles", "error", err)
		}
		profiler.Start()
		logger.L.Info("Writing CPU profiles", "dir", cfg.CPUProfileDir)
	}

	ingestService := ingest.NewService(ingest.Config{
		Events:       pebbleWriter,
		NotifyEvents: processor.Notify,
		Logs:         pebbleWriter,
		NotifyLogs:   logProcessor.Notify,
		Transactions: pebbleWriter,
		NotifySpans:  spanProcessor.Notify,
		Clock:        clock.System{},
		Retention:    consoleSettings,
		MaxWaiting:   cfg.IngestMaxWaiting,
		IngestCounts: clickHouseLogs,
	})
	ingestHandler := handler.NewIngestHandler(projectAuth, ingestService)
	start := newClickHouseStart(clickHouse)
	healthHandler := handler.NewHealthHandler(pebbleWriter, start)

	// Setup Fiber: public ingest and loopback-only query API on separate listeners
	app := handler.NewIngestApp(ingestHandler, healthHandler, handler.IngestOptions{
		IdleTimeout: cfg.IngestIdleTimeout, RefuseForwardedHTTP: cfg.IngestRefuseForwardedHTTP,
	})
	// The agent ingest API checks the tokens that Console makes, from its
	// database, as Project keys are checked.
	agentTokens, err := auth.NewTokens(cfg.SQLitePath)
	if err != nil {
		logger.L.Fatal("Failed to open Console's tokens", "error", err)
	}
	defer agentTokens.Close()
	handler.RegisterAgentRoutes(app, handler.NewAgentHandler(handler.AgentConfig{
		Tokens: agentTokens, Settings: consoleSettings, Files: pebbleWriter, NotifyFiles: agentFileProcessor.Notify,
		Downloads: clickHouse, RunEvents: ingestService, Totals: clickHouseLogs, Evidence: evidenceFiles,
		ConsoleURL: os.Getenv("CONSOLE_URL"), Clock: clock.System{}, Version: serverVersion(),
	}))

	// The query routes are served once the ClickHouse start has passed.
	queryApp := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	queryApp.Get("/health", healthHandler.Health)
	queryApp.Use(start.serveQueries)

	// The query API has no authentication, so it is only reachable from this machine.
	queryAddr := "127.0.0.1:" + cfg.QueryPort
	go func() {
		logger.L.Info("Starting query server", "addr", queryAddr)
		if err := queryApp.Listen(queryAddr); err != nil {
			logger.L.Fatal("Query server error", "error", err)
		}
	}()

	// The ingest gap is measured as the ingest port opens, before the
	// ClickHouse start, so the time that ClickHouse takes is not in it.
	ingestGap, stopIngestBeats, err := ingestgap.Open(pebbleWriter, clock.System{})
	if err != nil {
		logger.L.Warn("Reading the last ingest heartbeat failed: this start holds no Monitor for an ingest gap", "error", err)
	}
	go func() {
		logger.L.Info("Starting ingest server", "port", cfg.Port)
		if err := app.Listen(":" + cfg.Port); err != nil {
			logger.L.Fatal("Server error", "error", err)
		}
	}()

	work := &clickHouseWork{
		cfg: cfg, pebble: pebbleWriter, clickHouse: clickHouse, logs: clickHouseLogs, sqlQueries: sqlQueries,
		settings: consoleSettings, evidenceFiles: evidenceFiles, logTransforms: logTransforms,
		ingest: ingestService, health: healthHandler, ingestGap: ingestGap,
		eventProcessor: processor, eventCleanup: eventCleanup, logProcessor: logProcessor,
		spanProcessor: spanProcessor, agentFileProcessor: agentFileProcessor, agentParser: agentParser,
	}
	go start.run(ctx, work.steps(ctx, start))

	<-ctx.Done()
	logger.L.Info("Shutting down...")

	// Concurrent, so the whole shutdown fits Docker's 10 s stop grace period.
	var queryErr, ingestErr error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); queryErr = queryApp.ShutdownWithTimeout(5 * time.Second) }()
	go func() {
		defer wg.Done()
		ingestErr = app.ShutdownWithTimeout(5 * time.Second)
		// Ingest takes no more requests: the last ingest heartbeat, so the
		// next start measures its gap from now, not from the end of the
		// shutdown.
		stopIngestBeats()
	}()
	go func() { defer wg.Done(); start.stop(5 * time.Second) }()
	wg.Wait()
	if queryErr != nil || ingestErr != nil {
		logger.L.Error("HTTP shutdown timed out", "query_error", queryErr, "ingest_error", ingestErr)
	}
	// The last seconds' ingest counts are written.
	ingestService.StopFlushingIngestCounts()

	// Every later write returns ErrShuttingDown, so nothing reaches Pebble
	// after the deferred Close.
	pebbleWriter.StopWrites()

	// Then the stores close through the defers above.
	for _, stop := range start.workerStops() {
		stop()
	}
	if profiler != nil {
		profiler.Stop()
	}
}

// serverVersion is the release that this binary is, as the image build sets
// SOLIDTRACE_VERSION (v0.19.0); dev for a build from a checkout.
func serverVersion() string {
	if version := os.Getenv("SOLIDTRACE_VERSION"); version != "" {
		return version
	}
	return "dev"
}
