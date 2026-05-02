package storage

import (
	"os"

	"github.com/cockroachdb/pebble/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

type PebbleWriter struct {
	db *pebble.DB
}

func NewPebbleWriter(path string) (*PebbleWriter, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	opts := &pebble.Options{
		LBaseMaxBytes:         64 << 20, // 64 MB
		L0CompactionThreshold: 4,
		L0StopWritesThreshold: 12,
	}
	opts.ApplyCompressionSettings(func() pebble.DBCompressionSettings { return pebble.DBCompressionBalanced })

	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}

	return &PebbleWriter{db: db}, nil
}

func (w *PebbleWriter) WriteBatch(events []models.Event) error {
	batch := w.db.NewBatch()
	defer batch.Close()

	for _, event := range events {
		key := KeyForEvent(event.EventUUID)
		if err := batch.Set(key, event.RawJSON, nil); err != nil {
			return err
		}
	}

	// NoSync: throughput-oriented, matches prior RocksDB posture.
	// Pebble still writes to the WAL; data is durable after the next
	// OS-level fsync or when the WAL is rotated by compaction.
	return batch.Commit(pebble.NoSync)
}

// GetEvent retrieves an event by its key (called by Rails via HTTP)
func (w *PebbleWriter) GetEvent(key []byte) ([]byte, error) {
	val, closer, err := w.db.Get(key)
	if err == pebble.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	data := make([]byte, len(val))
	copy(data, val)
	return data, nil
}

func (w *PebbleWriter) Close() {
	w.db.Close()
}

// Ping checks if Pebble is healthy by attempting a read operation
func (w *PebbleWriter) Ping() error {
	_, closer, err := w.db.Get([]byte("__health_check__"))
	if err == pebble.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	closer.Close()
	return nil
}

// KeyForEvent generates a binary key for Pebble.
// Format: UUID (16 bytes)
func KeyForEvent(eventUUID string) []byte {
	u, err := uuid.Parse(eventUUID)
	if err != nil {
		return []byte(eventUUID)
	}
	return u[:]
}
