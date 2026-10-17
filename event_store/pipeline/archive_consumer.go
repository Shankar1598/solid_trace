package pipeline

import (
	"context"
	"sync"
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
	// retryUploadAfter is how soon the Event archive's failed uploads are
	// tried again, as the Log files' are.
	retryUploadAfter = time.Minute
)

// ArchivePayload represents the payload of an archive_events message.
// Date is a cutoff: every Event dated on or before it is archived.
type ArchivePayload struct {
	Date string `msgpack:"date"`
}

// ArchiveConsumer polls for archive_events messages and processes them.
// With a bucket, it also uploads the archive files (ADR 0008): once when it
// starts, to catch up on files a restart left, right after each archive
// run, and a minute after a pass that failed.
type ArchiveConsumer struct {
	mqReader *storage.MessageQueueReader
	duckdb   *storage.DuckDBWriter
	// batchLock is shared with Event processing: the archive moves rows by
	// their timestamp, so it must never run between a batch's DuckDB commit
	// and its cursor move.
	batchLock sync.Locker
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	// retryUploadAt is when to try failed uploads again, or zero if none
	// failed.
	retryUploadAt time.Time
}

// NewArchiveConsumer creates a new archive consumer
func NewArchiveConsumer(mqReader *storage.MessageQueueReader, duckdb *storage.DuckDBWriter, batchLock sync.Locker) *ArchiveConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ArchiveConsumer{
		mqReader:  mqReader,
		duckdb:    duckdb,
		batchLock: batchLock,
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
}

// Run starts the consumer loop
func (c *ArchiveConsumer) Run() {
	defer close(c.done)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	logger.L.Info("Archive consumer started", "poll_interval", pollInterval)
	c.uploadArchive()

	for {
		select {
		case <-c.ctx.Done():
			logger.L.Info("Archive consumer stopped")
			return
		case <-ticker.C:
			archived := c.processBatch()
			if archived || (!c.retryUploadAt.IsZero() && !time.Now().Before(c.retryUploadAt)) {
				c.uploadArchive()
			}
		}
	}
}

// uploadArchive uploads the archive files still on local disk, outside
// batchLock, so that Event processing never waits on the bucket. The store
// logs each failure.
func (c *ArchiveConsumer) uploadArchive() {
	if err := c.duckdb.UploadEventArchive(c.ctx); err != nil {
		c.retryUploadAt = time.Now().Add(retryUploadAfter)
		return
	}
	c.retryUploadAt = time.Time{}
}

// Stop stops the consumer and waits for Run to return.
func (c *ArchiveConsumer) Stop() {
	c.cancel()
	<-c.done
}

// processBatch processes the pending archive messages, and reports whether
// any archive run published files.
func (c *ArchiveConsumer) processBatch() bool {
	messages, err := c.mqReader.FetchPendingMessages(archiveMessageType, batchSize)
	if err != nil {
		logger.L.Error("Failed to fetch archive messages", "error", err)
		return false
	}

	archived := false
	for _, msg := range messages {
		if c.processMessage(msg) {
			archived = true
		}
	}
	return archived
}

// processMessage processes one archive message, and reports whether its
// archive run succeeded.
func (c *ArchiveConsumer) processMessage(msg storage.ConsoleMessage) bool {
	logger.L.Info("Processing archive message", "id", msg.ID)

	// Mark as processing
	if err := c.mqReader.MarkProcessing(msg.ID); err != nil {
		logger.L.Error("Failed to mark message as processing", "id", msg.ID, "error", err)
		return false
	}

	// Unpack payload
	var payload ArchivePayload
	if err := c.mqReader.UnpackPayload(msg.Payload, &payload); err != nil {
		errMsg := "Failed to unpack payload: " + err.Error()
		logger.L.Error(errMsg, "id", msg.ID)
		c.mqReader.MarkFailed(msg.ID, errMsg)
		return false
	}

	// Parse date
	date, err := time.Parse("2006-01-02", payload.Date)
	if err != nil {
		errMsg := "Invalid date format: " + err.Error()
		logger.L.Error(errMsg, "id", msg.ID, "date", payload.Date)
		c.mqReader.MarkFailed(msg.ID, errMsg)
		return false
	}

	// Don't allow a cutoff of today or a future date
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if !date.Before(today) {
		errMsg := "Archive cutoff must be before today"
		logger.L.Error(errMsg, "id", msg.ID, "date", payload.Date)
		c.mqReader.MarkFailed(msg.ID, errMsg)
		return false
	}

	switch msg.MessageType {
	case "archive_events":
		logger.L.Info("Starting archive", "cutoff", payload.Date)
		c.batchLock.Lock()
		err := c.duckdb.ArchiveEventsUpTo(date)
		c.batchLock.Unlock()
		if err != nil {
			errMsg := "Archive failed: " + err.Error()
			logger.L.Error(errMsg, "id", msg.ID, "cutoff", payload.Date)
			c.mqReader.MarkFailed(msg.ID, errMsg)
			return false
		}
	default:
		errMsg := "Unknown message type: " + msg.MessageType
		logger.L.Error(errMsg, "id", msg.ID)
		c.mqReader.MarkFailed(msg.ID, errMsg)
		return false
	}

	// Mark as processed
	if err := c.mqReader.MarkProcessed(msg.ID); err != nil {
		logger.L.Error("Failed to mark message as processed", "id", msg.ID, "error", err)
		return true
	}

	logger.L.Info("Archive completed", "id", msg.ID, "cutoff", payload.Date)
	return true
}
