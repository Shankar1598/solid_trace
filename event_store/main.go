package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

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

	duckdbChan := make(chan models.Event, cfg.DuckDBChannelSize)
	duckdbIngester := pipeline.NewDuckDBIngester(duckdbChan, duckdbWriter, messageQueueWriter, cfg.DuckDBFlushTimeout)
	duckdbIngesterDone := make(chan struct{})
	go func() {
		duckdbIngester.Run()
		close(duckdbIngesterDone)
	}()

	// Start consumers
	archiveConsumer := pipeline.NewArchiveConsumer(messageQueueReader, duckdbWriter)
	go archiveConsumer.Run()
	defer archiveConsumer.Stop()

	ingestService := ingest.NewService(sqliteWriter, pebbleWriter, duckdbChan, cfg.IngestMaxWaiting)
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
		if err := listenIngest(app, cfg); err != nil {
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

	// A request still in flight after a timed-out shutdown may yet send to
	// duckdbChan, so it is only closed once ingest has fully stopped. After a
	// timeout the DuckDB buffer is not flushed; its Events are in Pebble.
	if ingestErr == nil {
		close(duckdbChan)
		<-duckdbIngesterDone
	}
}

func listenIngest(app *fiber.App, cfg *config.Config) error {
	if cfg.SocketPath == "" {
		return app.Listen(":" + cfg.Port)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.Remove(cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing socket: %w", err)
	}
	listener, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on socket: %w", err)
	}
	if err := os.Chmod(cfg.SocketPath, 0o660); err != nil {
		return fmt.Errorf("chmod socket: %w", err)
	}
	logger.L.Info("Listening on unix socket", "socket", cfg.SocketPath)
	return app.Listener(listener)
}
