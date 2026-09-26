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
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pipeline"
	"github.com/solidtrace/event_store/pkg/logger"
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

	ingestService := ingest.NewService(sqliteWriter, pebbleChan)
	ingestHandler := handler.NewIngestHandler(projectAuth, ingestService)
	eventsHandler := handler.NewEventsHandler(pebbleWriter, duckdbWriter)
	healthHandler := handler.NewHealthHandler(pebbleWriter, duckdbWriter)

	// Setup Fiber: public ingest and loopback-only query API on separate listeners
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	handler.RegisterIngestRoutes(app, ingestHandler, healthHandler)

	queryApp := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	handler.RegisterQueryRoutes(queryApp, eventsHandler, healthHandler)

	// Maintenance API
	// app.Post("/api/maintenance/archive-events", archiverHandler.ArchiveEvents)

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		logger.L.Info("Shutting down...")
		queryApp.Shutdown()
		app.Shutdown()
	}()

	// The query API has no authentication, so it is only reachable from this machine.
	queryAddr := "127.0.0.1:" + cfg.QueryPort
	go func() {
		logger.L.Info("Starting query server", "addr", queryAddr)
		if err := queryApp.Listen(queryAddr); err != nil {
			logger.L.Fatal("Query server error", "error", err)
		}
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
