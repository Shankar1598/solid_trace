package pipeline

import (
	"context"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// archiveMessageType is the message type for archive requests
	archiveMessageType = "archive_events"
	// pollInterval is how often to check for new messages
	pollInterval = 5 * time.Second
	// batchSize is how many messages to fetch at once
	batchSize = 10
)

// ArchivePayload represents the payload of an archive_events message
type ArchivePayload struct {
	Date string `msgpack:"date"`
}

// ArchiveConsumer polls for archive_events messages and processes them
type ArchiveConsumer struct {
	mqReader *storage.MessageQueueReader
	duckdb   *storage.DuckDBWriter
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewArchiveConsumer creates a new archive consumer
func NewArchiveConsumer(mqReader *storage.MessageQueueReader, duckdb *storage.DuckDBWriter) *ArchiveConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ArchiveConsumer{
		mqReader: mqReader,
		duckdb:   duckdb,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Run starts the consumer loop
func (c *ArchiveConsumer) Run() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	logger.L.Info("Archive consumer started", "poll_interval", pollInterval)

	for {
		select {
		case <-c.ctx.Done():
			logger.L.Info("Archive consumer stopped")
			return
		case <-ticker.C:
			c.processBatch()
		}
	}
}

// Stop stops the consumer
func (c *ArchiveConsumer) Stop() {
	c.cancel()
}

func (c *ArchiveConsumer) processBatch() {
	messages, err := c.mqReader.FetchPendingMessages(archiveMessageType, batchSize)
	if err != nil {
		logger.L.Error("Failed to fetch archive messages", "error", err)
		return
	}

	for _, msg := range messages {
		c.processMessage(msg)
	}
}

func (c *ArchiveConsumer) processMessage(msg storage.ConsoleMessage) {
	logger.L.Info("Processing archive message", "id", msg.ID)

	// Mark as processing
	if err := c.mqReader.MarkProcessing(msg.ID); err != nil {
		logger.L.Error("Failed to mark message as processing", "id", msg.ID, "error", err)
		return
	}

	// Unpack payload
	var payload ArchivePayload
	if err := c.mqReader.UnpackPayload(msg.Payload, &payload); err != nil {
		errMsg := "Failed to unpack payload: " + err.Error()
		logger.L.Error(errMsg, "id", msg.ID)
		c.mqReader.MarkFailed(msg.ID, errMsg)
		return
	}

	// Parse date
	date, err := time.Parse("2006-01-02", payload.Date)
	if err != nil {
		errMsg := "Invalid date format: " + err.Error()
		logger.L.Error(errMsg, "id", msg.ID, "date", payload.Date)
		c.mqReader.MarkFailed(msg.ID, errMsg)
		return
	}

	// Don't allow archiving today or future dates
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if !date.Before(today) {
		errMsg := "Cannot archive today's or future dates"
		logger.L.Error(errMsg, "id", msg.ID, "date", payload.Date)
		c.mqReader.MarkFailed(msg.ID, errMsg)
		return
	}

	switch msg.MessageType {
	case "archive_events":
		// Perform the archive
		logger.L.Info("Starting archive", "date", payload.Date)
		if err := c.duckdb.ArchiveEventsForDate(date); err != nil {
			errMsg := "Archive failed: " + err.Error()
			logger.L.Error(errMsg, "id", msg.ID, "date", payload.Date)
			c.mqReader.MarkFailed(msg.ID, errMsg)
			return
		}
	default:
		errMsg := "Unknown message type: " + msg.MessageType
		logger.L.Error(errMsg, "id", msg.ID)
		c.mqReader.MarkFailed(msg.ID, errMsg)
		return
	}

	// Mark as processed
	if err := c.mqReader.MarkProcessed(msg.ID); err != nil {
		logger.L.Error("Failed to mark message as processed", "id", msg.ID, "error", err)
		return
	}

	logger.L.Info("Archive completed", "id", msg.ID, "date", payload.Date)
}
