package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/objstorage/remote"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/models"
)

type PebbleWriter struct {
	db *pebble.DB
}

func NewPebbleWriter(cfg *config.Config) (*PebbleWriter, error) {
	if err := os.MkdirAll(cfg.PebblePath, 0755); err != nil {
		return nil, err
	}

	opts := &pebble.Options{
		LBaseMaxBytes:         64 << 20, // 64 MB
		L0CompactionThreshold: 4,
		L0StopWritesThreshold: 12,
	}
	opts.ApplyCompressionSettings(func() pebble.DBCompressionSettings { return pebble.DBCompressionBalanced })

	if len(cfg.StorageTiers) > 0 {
		factoryMap := make(map[remote.Locator]remote.Storage)
		var mainLocator remote.Locator
		var strategy remote.CreateOnSharedStrategy

		for _, tier := range cfg.StorageTiers {
			var remoteStore remote.Storage
			var err error

			switch tier.Kind {
			case "s3":
				remoteStore, err = NewS3Storage(context.Background(), tier.Bucket, tier.Prefix, tier.Endpoint)
			case "gcs":
				remoteStore, err = NewGCSStorage(context.Background(), tier.Bucket, tier.Prefix)
			case "local":
				remoteStore = remote.NewLocalFS(tier.Bucket, vfs.Default)
			default:
				return nil, fmt.Errorf("unknown storage tier kind: %s", tier.Kind)
			}

			if err != nil {
				return nil, err
			}

			loc := remote.Locator(tier.Locator)
			factoryMap[loc] = remoteStore

			if mainLocator == "" {
				mainLocator = loc
				if tier.Level > 0 {
					strategy = remote.CreateOnSharedLower // For L5 and L6
				} else {
					strategy = remote.CreateOnSharedAll
				}
			}
		}

		opts.Experimental.RemoteStorage = remote.MakeSimpleFactory(factoryMap)
		opts.Experimental.CreateOnShared = strategy
		opts.Experimental.CreateOnSharedLocator = mainLocator
	}

	db, err := pebble.Open(cfg.PebblePath, opts)
	if err != nil {
		return nil, err
	}

	if len(cfg.StorageTiers) > 0 {
		// Set a creator ID to enable remote storage.
		// In a real cluster, this would be a unique node ID.
		// For SolidTrace, 1 is sufficient as it's a singleton.
		if err := db.SetCreatorID(1); err != nil {
			db.Close()
			return nil, err
		}
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
