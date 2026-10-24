package main

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/logtransform"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/cpuprofile"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/processing"
	"github.com/solidtrace/event_store/storage"
)

func main() {
	cfg := config.Load()
	logger.Init()
	for _, warning := range cfg.Warnings {
		logger.L.Warn(warning)
	}

	// Initialize storage
	pebbleWriter, err := storage.NewPebbleWriter(cfg)
	if err != nil {
		logger.L.Fatal("Failed to open Pebble", "error", err)
	}
	defer pebbleWriter.Close()

	// Event processing stores Events' rows in ClickHouse, and Event reads
	// read them there. Log processing inserts Logs into it, and Log rows,
	// single Logs, Match monitors and Fields read them there.
	clickHouse, err := storage.OpenClickHouse(cfg.ClickHouse, cfg.ObjectStore)
	if err != nil {
		logger.L.Fatal("Failed to open ClickHouse", "error", err)
	}
	defer clickHouse.Close()
	clickHouseLogs := storage.NewClickHouseLogs(clickHouse, pebbleWriter)
	clickHouseLogs.UseBinCache(cfg.LogsBinCacheBytes)

	// The Logs from before the switch, in the DuckDB release's Log files,
	// are read in place. This loads what prepare-clickhouse wrote, and stops
	// the start on DuckDB data it didn't prepare. It runs before Log
	// processing, which reads the Processing cursors it loads.
	if err := clickHouseLogs.UseOldLogFiles(storage.OldLogFiles{
		LogsDuckDB: cfg.LogsDuckDBPath,
		// Compose mounts logs_parquet_path at ClickHouse's user_files_path,
		// where deploy/install finds it, and bin/clickhouse-dev the
		// development store, so ClickHouseDir is "": file() reads each file
		// by its path in the store.
		LocalDir: cfg.LogsParquetPath,
		Bucket:   cfg.ObjectStore,
	}); err != nil {
		logger.L.Fatal("Failed to ready the Logs from before the switch to ClickHouse", "error", err)
	}

	sqliteWriter, err := storage.NewSQLiteWriter(cfg.SQLitePath)
	if err != nil {
		logger.L.Fatal("Failed to open SQLite", "error", err)
	}
	defer sqliteWriter.Close()

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

	// Prepare must run before the worker starts. The Event cleanup never
	// runs inside a batch: they share batchLock.
	var batchLock sync.Mutex
	processor := processing.New(pebbleWriter, clickHouse, messageQueueWriter, sqliteWriter, &batchLock)
	if err := processor.Prepare(); err != nil {
		logger.L.Fatal("Failed to prepare Event processing", "error", err)
	}
	processor.Start()

	// Deletes a deleted Project's Events, holding batchLock while it does.
	eventCleanup := processing.NewCleanup(pebbleWriter, clickHouse, sqliteWriter, consoleSettings, &batchLock, clock.System{})
	eventCleanup.Start()

	logTransforms := logtransform.New(consoleSettings, clock.System{})
	logProcessor := logprocessing.New(pebbleWriter, clickHouseLogs, logTransforms, clock.System{})
	if err := logProcessor.Prepare(); err != nil {
		logger.L.Fatal("Failed to prepare Log processing", "error", err)
	}
	logProcessor.Start()

	// Retention and deleted Projects once a day, the disk limit hourly.
	diskLimit, err := clickHouseLogs.DiskLimit(cfg.LogsDiskLimitBytes)
	if err != nil {
		logger.L.Fatal("Failed to work out the Log disk limit", "error", err)
	}
	logger.L.Info("Log disk limit", "bytes", diskLimit.Bytes)
	sqlQueries := storage.NewClickHouseSQL(clickHouse, storage.DefaultSQLLimits())
	logCleanup := logprocessing.NewCleanup(clickHouseLogs, sqlQueries, consoleSettings, diskLimit, clock.System{})
	logCleanup.Start()

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
		Clock:        clock.System{},
		Retention:    consoleSettings,
		MaxWaiting:   cfg.IngestMaxWaiting,
		IngestCounts: clickHouseLogs,
	})
	ingestService.StartFlushingIngestCounts()
	ingestHandler := handler.NewIngestHandler(projectAuth, ingestService)
	eventsHandler := handler.NewEventsHandler(pebbleWriter, clickHouse)
	logsHandler := handler.NewLogsHandler(clickHouseLogs, sqlQueries, consoleSettings, clock.System{})
	transformHandler := handler.NewLogTransformHandler(logTransforms)
	overviewHandler := handler.NewOverviewHandler(handler.OverviewConfig{
		Logs: clickHouseLogs, Clock: clock.System{}, DiskLimit: diskLimit, Retention: consoleSettings,
	})
	healthHandler := handler.NewHealthHandler(pebbleWriter, clickHouse)

	// Setup Fiber: public ingest and loopback-only query API on separate listeners
	app := handler.NewIngestApp(ingestHandler, healthHandler)

	queryApp := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	handler.RegisterQueryRoutes(queryApp, eventsHandler, logsHandler, transformHandler, overviewHandler, healthHandler, handler.NewConfigHandler(cfg, diskLimit.Bytes))

	// The query API has no authentication, so it is only reachable from this machine.
	queryAddr := "127.0.0.1:" + cfg.QueryPort
	go func() {
		logger.L.Info("Starting query server", "addr", queryAddr)
		if err := queryApp.Listen(queryAddr); err != nil {
			logger.L.Fatal("Query server error", "error", err)
		}
	}()

	go func() {
		logger.L.Info("Starting ingest server", "port", cfg.Port)
		if err := app.Listen(":" + cfg.Port); err != nil {
			logger.L.Fatal("Server error", "error", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	logger.L.Info("Shutting down...")

	// Concurrent, so the whole shutdown fits Docker's 10 s stop grace period.
	var queryErr, ingestErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); queryErr = queryApp.ShutdownWithTimeout(5 * time.Second) }()
	go func() { defer wg.Done(); ingestErr = app.ShutdownWithTimeout(5 * time.Second) }()
	wg.Wait()
	if queryErr != nil || ingestErr != nil {
		logger.L.Error("HTTP shutdown timed out", "query_error", queryErr, "ingest_error", ingestErr)
	}
	// Ingest takes no more requests: the last seconds' counts are written.
	ingestService.StopFlushingIngestCounts()

	// Every later write returns ErrShuttingDown, so nothing reaches Pebble
	// after the deferred Close.
	pebbleWriter.StopWrites()

	// Then the stores close through the defers above.
	processor.Stop()
	logProcessor.Stop()
	logCleanup.Stop()
	eventCleanup.Stop()
	if profiler != nil {
		profiler.Stop()
	}
}
