package pipeline

import (
	"log"
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
)

type RocksDBIngester struct {
	events       <-chan models.Event
	duckdbEvents chan<- models.Event // Forward to DuckDB after commit
	writer       *storage.RocksDBWriter
	batchSize    int
	flushTimeout time.Duration
}

func NewRocksDBIngester(
	events <-chan models.Event,
	duckdbEvents chan<- models.Event,
	writer *storage.RocksDBWriter,
	batchSize int,
	flushTimeout time.Duration,
) *RocksDBIngester {
	return &RocksDBIngester{
		events:       events,
		duckdbEvents: duckdbEvents,
		writer:       writer,
		batchSize:    batchSize,
		flushTimeout: flushTimeout,
	}
}

func (w *RocksDBIngester) Run() {
	batch := make([]models.Event, 0, w.batchSize)
	flushTicker := time.NewTicker(w.flushTimeout)
	syncTicker := time.NewTicker(1 * time.Second)
	defer flushTicker.Stop()
	defer syncTicker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if err := w.writer.WriteBatch(batch); err != nil {
			log.Printf("RocksDB write error: %v", err)
			return
		}

		// Forward to DuckDB channel AFTER successful RocksDB commit
		for _, event := range batch {
			select {
			case w.duckdbEvents <- event:
			default:
				log.Println("DuckDB channel full, dropping event")
			}
		}

		batch = batch[:0]
	}

	for {
		select {
		case event := <-w.events:
			batch = append(batch, event)
			if len(batch) >= w.batchSize {
				flush()
			}

		case <-flushTicker.C:
			flush()

		case <-syncTicker.C:
			w.writer.FlushWAL()
		}
	}
}
