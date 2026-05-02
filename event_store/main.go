package main

import (
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pipeline"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

func main() {
	cfg := config.Load()
	logger.Init()

	// Initialize storage
	pebbleWriter, err := storage.NewPebbleWriter(cfg.PebblePath)
	if err != nil {
		logger.L.Fatal("Failed to open Pebble", "error", err)
	}
	defer pebbleWriter.Close()

	duckdbWriter, err := storage.NewDuckDBWriter(cfg.DuckDBPath, cfg.ParquetStoragePath, cfg.DuckDBTempDirectory, cfg.DuckDBMemoryLimit)
	if err != nil {
		logger.L.Fatal("Failed to open DuckDB", "error", err)
	}
	defer duckdbWriter.Close()

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

	// Create channels
	pebbleChan := make(chan models.Event, cfg.PebbleChannelSize)
	duckdbChan := make(chan models.Event, cfg.DuckDBChannelSize)

	// Start ingesters
	pebbleIngester := pipeline.NewPebbleIngester(
		pebbleChan, duckdbChan, pebbleWriter,
		cfg.PebbleBatchSize, cfg.PebbleFlushTimeout,
	)
	go pebbleIngester.Run()

	duckdbIngester := pipeline.NewDuckDBIngester(duckdbChan, duckdbWriter, messageQueueWriter, cfg.DuckDBFlushTimeout)
	go duckdbIngester.Run()

	// Start consumers
	archiveConsumer := pipeline.NewArchiveConsumer(messageQueueReader, duckdbWriter)
	go archiveConsumer.Run()
	defer archiveConsumer.Stop()

	ingestHandler := handler.NewIngestHandler(projectAuth, sqliteWriter, pebbleChan)
	eventsHandler := handler.NewEventsHandler(pebbleWriter, duckdbWriter)
	healthHandler := handler.NewHealthHandler(pebbleWriter, duckdbWriter)

	// Setup Fiber
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	// Health check
	app.Get("/health", healthHandler.Health)

	app.Post("/api/:project_id/store", ingestHandler.Store)
	app.Post("/api/:project_id/envelope", ingestHandler.Envelope)
	app.Get("/api/events/:event_uuid", eventsHandler.GetEvent)

	// Query API
	app.Get("/api/:project_id/events/context", eventsHandler.GetEventWithContext)
	app.Get("/api/:project_id/events/count", eventsHandler.Count)
	app.Get("/api/:project_id/events", eventsHandler.List)

	// Maintenance API
	// app.Post("/api/maintenance/archive-events", archiverHandler.ArchiveEvents)

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		logger.L.Info("Shutting down...")
		app.Shutdown()
	}()

	logger.L.Info("Starting ingest server", "port", cfg.Port)
	if cfg.SocketPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o755); err != nil {
			logger.L.Fatal("Failed to create socket directory", "error", err)
		}
		if err := os.Remove(cfg.SocketPath); err != nil && !os.IsNotExist(err) {
			logger.L.Fatal("Failed to remove existing socket", "error", err)
		}
		listener, err := net.Listen("unix", cfg.SocketPath)
		if err != nil {
			logger.L.Fatal("Failed to listen on socket", "error", err)
		}
		if err := os.Chmod(cfg.SocketPath, 0o660); err != nil {
			logger.L.Fatal("Failed to chmod socket", "error", err)
		}
		logger.L.Info("Listening on unix socket", "socket", cfg.SocketPath)
		if err := app.Listener(listener); err != nil {
			logger.L.Fatal("Server error", "error", err)
		}
		return
	}

	if err := app.Listen(":" + cfg.Port); err != nil {
		logger.L.Fatal("Server error", "error", err)
	}
}
