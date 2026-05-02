package pipeline

import (
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type PebbleIngester struct {
	events       <-chan models.Event
	duckdbEvents chan<- models.Event // Forward to DuckDB after commit
	writer       *storage.PebbleWriter
	batchSize    int
	flushTimeout time.Duration
}

func NewPebbleIngester(
	events <-chan models.Event,
	duckdbEvents chan<- models.Event,
	writer *storage.PebbleWriter,
	batchSize int,
	flushTimeout time.Duration,
) *PebbleIngester {
	return &PebbleIngester{
		events:       events,
		duckdbEvents: duckdbEvents,
		writer:       writer,
		batchSize:    batchSize,
		flushTimeout: flushTimeout,
	}
}

func (w *PebbleIngester) Run() {
	batch := make([]models.Event, 0, w.batchSize)
	flushTicker := time.NewTicker(w.flushTimeout)
	defer flushTicker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if err := w.writer.WriteBatch(batch); err != nil {
			logger.L.Error("Pebble write error", "error", err)
			return
		}

		// Forward to DuckDB channel AFTER successful Pebble commit
		for _, event := range batch {
			select {
			case w.duckdbEvents <- event:
			default:
				logger.L.Warn("DuckDB channel full, dropping event")
			}
		}

		logger.L.Info("Flushed events to Pebble", "count", len(batch))
		batch = batch[:0]
	}

	for {
		select {
		case event, ok := <-w.events:
			if !ok {
				flush()
				return
			}
			batch = append(batch, event)
			if len(batch) >= w.batchSize {
				flush()
			}

		case <-flushTicker.C:
			flush()
		}
	}
}
