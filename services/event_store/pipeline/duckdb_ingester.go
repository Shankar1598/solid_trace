package pipeline

import (
	"log"
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
)

type DuckDBIngester struct {
	events       <-chan models.Event
	writer       *storage.DuckDBWriter
	flushTimeout time.Duration
}

func NewDuckDBIngester(
	events <-chan models.Event,
	writer *storage.DuckDBWriter,
	flushTimeout time.Duration,
) *DuckDBIngester {
	return &DuckDBIngester{
		events:       events,
		writer:       writer,
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
			log.Printf("DuckDB write error: %v", err)
		}

		log.Printf("DuckDB: flushed %d events", len(batch))
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
