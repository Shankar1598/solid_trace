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
	"github.com/solidtrace/event_store/pipeline"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/processing"
	"github.com/solidtrace/event_store/storage"
)

func main() {
	cfg := config.Load()
	logger.Init()

	// Initialize storage
	pebbleWriter, err := storage.NewPebbleWriter(cfg)
	if err != nil {
		logger.L.Fatal("Failed to open Pebble", "error", err)
	}
	defer pebbleWriter.Close()

	duckdbWriter, err := storage.NewDuckDBWriter(cfg.DuckDBPath, cfg.ParquetStoragePath, cfg.DuckDBTempDirectory, cfg.DuckDBMemoryLimit)
	if err != nil {
		logger.L.Fatal("Failed to open DuckDB", "error", err)
	}
	defer duckdbWriter.Close()

	logStore, err := storage.NewLogStore(duckdbWriter, cfg.LogsDuckDBPath)
	if err != nil {
		logger.L.Fatal("Failed to open logs DuckDB", "error", err)
	}

	sqliteWriter, err := storage.NewSQLiteWriter(cfg.SQLitePath)
	if err != nil {
		logger.L.Fatal("Failed to open SQLite", "error", err)
	}
	defer sqliteWriter.Close()

	// Initialize Message Queue Reader (for Console -> Event Store)
	messageQueueReader, err := storage.NewMessageQueueReader(cfg.MessageQueuePath)
	if err != nil {
		logger.L.Fatal("Failed to open Message Queue Reader", "error", err)
	}
	defer messageQueueReader.Close()

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

	// Prepare must run before the worker and the archive job start. The
	// archive never runs inside a batch: they share batchLock.
	var batchLock sync.Mutex
	processor := processing.New(pebbleWriter, duckdbWriter, messageQueueWriter, sqliteWriter, &batchLock)
	if err := processor.Prepare(); err != nil {
		logger.L.Fatal("Failed to prepare Event processing", "error", err)
	}
	processor.Start()

	archiveConsumer := pipeline.NewArchiveConsumer(messageQueueReader, duckdbWriter, &batchLock)
	go archiveConsumer.Run()

	logProcessor := logprocessing.New(pebbleWriter, logStore, clock.System{})
	if err := logProcessor.Prepare(); err != nil {
		logger.L.Fatal("Failed to prepare Log processing", "error", err)
	}
	logProcessor.Start()

	ingestService := ingest.NewService(ingest.Config{
		Events:       pebbleWriter,
		NotifyEvents: processor.Notify,
		Logs:         pebbleWriter,
		NotifyLogs:   logProcessor.Notify,
		Clock:        clock.System{},
		Retention:    ingest.FixedRetention(30), // the instance default; Projects' own Retention isn't read yet
		MaxWaiting:   cfg.IngestMaxWaiting,
	})
	ingestHandler := handler.NewIngestHandler(projectAuth, ingestService)
	eventsHandler := handler.NewEventsHandler(pebbleWriter, duckdbWriter)
	logsHandler := handler.NewLogsHandler(logStore)
	healthHandler := handler.NewHealthHandler(pebbleWriter, duckdbWriter)

	// Setup Fiber: public ingest and loopback-only query API on separate listeners
	app := handler.NewIngestApp(ingestHandler, healthHandler)

	queryApp := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	handler.RegisterQueryRoutes(queryApp, eventsHandler, logsHandler, healthHandler)

	// Maintenance API
	// app.Post("/api/maintenance/archive-events", archiverHandler.ArchiveEvents)

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

	// Every later write returns ErrShuttingDown, so nothing reaches Pebble
	// after the deferred Close.
	pebbleWriter.StopWrites()

	// Then the stores close through the defers above.
	processor.Stop()
	logProcessor.Stop()
	archiveConsumer.Stop()
}
