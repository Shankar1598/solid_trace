package pipeline

import (
	"encoding/json"
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type DuckDBIngester struct {
	events       <-chan models.Event
	writer       *storage.DuckDBWriter
	messageQueue *storage.MessageQueueWriter
	flushTimeout time.Duration
}

func NewDuckDBIngester(
	events <-chan models.Event,
	writer *storage.DuckDBWriter,
	messageQueue *storage.MessageQueueWriter,
	flushTimeout time.Duration,
) *DuckDBIngester {
	return &DuckDBIngester{
		events:       events,
		writer:       writer,
		messageQueue: messageQueue,
		flushTimeout: flushTimeout,
	}
}

func (w *DuckDBIngester) Run() {
	batch := make([]models.Event, 0, 10000)
	flushTicker := time.NewTicker(w.flushTimeout)
	defer flushTicker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if err := w.writer.WriteBatch(batch); err != nil {
			logger.L.Error("DuckDB write error", "error", err)
			batch = batch[:0]
			return
		}

		logger.L.Info("DuckDB: flushed events", "count", len(batch))

		// AFTER successful DuckDB indexing, enqueue messages
		w.enqueueBatchMessages(batch)

		batch = batch[:0]
	}

	for {
		select {
		case event := <-w.events:
			batch = append(batch, event)

		case <-flushTicker.C:
			flush()
		}
	}
}

func (w *DuckDBIngester) enqueueBatchMessages(batch []models.Event) {
	// Group by new issues
	newIssues := make(map[int64]bool)
	existingIssues := make(map[int64]bool)

	for _, event := range batch {
		if event.IsNewIssue {
			newIssues[event.IssueID] = true
		} else {
			existingIssues[event.IssueID] = true
		}
	}

	// Enqueue issue_created messages
	if len(newIssues) > 0 {
		ids := make([]int64, 0, len(newIssues))
		for id := range newIssues {
			ids = append(ids, id)
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"issue_ids": ids,
		})
		if err := w.messageQueue.EnqueueMessage("issue_created", payload); err != nil {
			logger.L.Error("Failed to enqueue issue_created message", "error", err)
		}
	}

	// Enqueue issue_received_event messages for existing issues only
	// (exclude issues that were just created in this batch)
	existingOnly := make([]int64, 0)
	for id := range existingIssues {
		if !newIssues[id] {
			existingOnly = append(existingOnly, id)
		}
	}
	if len(existingOnly) > 0 {
		payload, _ := json.Marshal(map[string]interface{}{
			"issue_ids": existingOnly,
		})
		if err := w.messageQueue.EnqueueMessage("issue_received_event", payload); err != nil {
			logger.L.Error("Failed to enqueue issue_received_event message", "error", err)
		}
	}
}
