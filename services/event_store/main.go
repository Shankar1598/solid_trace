package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pipeline"
	"github.com/solidtrace/event_store/storage"
)

func main() {
	cfg := config.Load()

	// Initialize storage
	rocksdbWriter, err := storage.NewRocksDBWriter(cfg.RocksDBPath)
	if err != nil {
		log.Fatalf("Failed to open RocksDB: %v", err)
	}
	defer rocksdbWriter.Close()

	duckdbWriter, err := storage.NewDuckDBWriter(cfg.DuckDBPath)
	if err != nil {
		log.Fatalf("Failed to open DuckDB: %v", err)
	}
	defer duckdbWriter.Close()

	sqliteWriter, err := storage.NewSQLiteWriter(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("Failed to open SQLite: %v", err)
	}
	defer sqliteWriter.Close()

	messageQueueWriter, err := storage.NewMessageQueueWriter(cfg.MessageQueuePath)
	if err != nil {
		log.Fatalf("Failed to open Message Queue: %v", err)
	}
	defer messageQueueWriter.Close()

	// Initialize auth
	projectAuth, err := auth.NewProjectAuth(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("Failed to connect to SQLite: %v", err)
	}
	defer projectAuth.Close()

	// Create channels
	rocksdbChan := make(chan models.Event, cfg.RocksDBChannelSize)
	duckdbChan := make(chan models.Event, cfg.DuckDBChannelSize)

	// Start ingesters
	rocksdbIngester := pipeline.NewRocksDBIngester(
		rocksdbChan, duckdbChan, rocksdbWriter,
		cfg.RocksDBBatchSize, cfg.RocksDBFlushTimeout,
	)
	go rocksdbIngester.Run()

	duckdbIngester := pipeline.NewDuckDBIngester(duckdbChan, duckdbWriter, messageQueueWriter, cfg.DuckDBFlushTimeout)
	go duckdbIngester.Run()

	ingestHandler := handler.NewIngestHandler(projectAuth, sqliteWriter, rocksdbChan)
	eventsHandler := handler.NewEventsHandler(rocksdbWriter, duckdbWriter)

	// Setup Fiber
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Post("/api/:project_id/store", ingestHandler.Store)
	app.Post("/api/:project_id/envelope", ingestHandler.Envelope)
	app.Get("/api/events/:event_uuid", eventsHandler.GetEvent)

	// Query API
	app.Get("/api/:project_id/events", eventsHandler.List)
	app.Get("/api/:project_id/events/count", eventsHandler.Count)

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down...")
		app.Shutdown()
	}()

	log.Printf("Starting ingest server on :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
