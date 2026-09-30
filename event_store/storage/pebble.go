package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/objstorage/remote"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/models"
)

// ErrShuttingDown is returned by WriteEvent after StopWrites or Close.
var ErrShuttingDown = errors.New("event writer is shutting down")

type PebbleWriter struct {
	db *pebble.DB

	// mu serialises Event writes, so Event keys commit in UUID order.
	mu      sync.Mutex
	stopped bool
}

func NewPebbleWriter(cfg *config.Config) (*PebbleWriter, error) {
	if err := os.MkdirAll(cfg.PebblePath, 0755); err != nil {
		return nil, err
	}

	opts := &pebble.Options{
		// Pinned, not FormatNewest: ratcheting is one-way, so it moves only on purpose.
		// Value separation stays off (Experimental.ValueSeparationPolicy is unset).
		FormatMajorVersion: pebble.FormatValueSeparation,
		// Interim value until the sizing benchmarks in issue 011 pick one.
		MemTableSize:          64 << 20, // 64 MB
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

// WriteEvent stores one Event's raw payload and sets its EventUUID.
// The UUIDv7 is generated under the lock, so within a Project, Event keys are
// committed in UUID order and a reader never sees a key before a lower one.
func (w *PebbleWriter) WriteEvent(event *models.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return ErrShuttingDown
	}

	eventUUID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate event uuid: %w", err)
	}
	event.EventUUID = eventUUID.String()

	batch := w.db.NewBatch()
	defer batch.Close()
	if err := batch.Set(KeyForEvent(event.ProjectID, event.EventUUID), event.RawJSON, nil); err != nil {
		return err
	}

	// NoSync: a process crash loses nothing, because the write is in the
	// page cache. A power loss or kernel crash can lose the last few ms.
	return batch.Commit(pebble.NoSync)
}

// StopWrites makes every later WriteEvent return ErrShuttingDown. It waits
// for a write in progress to finish.
func (w *PebbleWriter) StopWrites() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
}

// GetEvent retrieves an event by its key (called by Rails via HTTP)
func (w *PebbleWriter) GetEvent(key []byte) ([]byte, error) {
	return w.get(key)
}

// get returns a copy of the value at key, or nil if there is none.
func (w *PebbleWriter) get(key []byte) ([]byte, error) {
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
	w.StopWrites()
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

// eventKeyPrefix is the first 4 bytes of every Event key of a Project.
func eventKeyPrefix(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(make([]byte, 0, 4), projectID)
}

// eventKeyUpperBound is the first key past every Event key of a Project, or nil
// for the last possible Project.
func eventKeyUpperBound(projectID uint32) []byte {
	if projectID == math.MaxUint32 {
		return nil
	}
	return eventKeyPrefix(projectID + 1)
}

// EventsAfter returns up to limit of a Project's Events whose UUID comes after
// afterUUID, in UUID order. An empty afterUUID starts at the Project's first
// Event. Each Event has only ProjectID, EventUUID and RawJSON set.
func (w *PebbleWriter) EventsAfter(projectID uint32, afterUUID string, limit int) ([]models.Event, error) {
	lower := eventKeyPrefix(projectID)
	if afterUUID != "" {
		// The zero byte makes the bound exclusive of afterUUID's own key.
		lower = append(KeyForEvent(projectID, afterUUID), 0)
	}
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: eventKeyUpperBound(projectID)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var events []models.Event
	for valid := iter.First(); valid && len(events) < limit; valid = iter.Next() {
		key := iter.Key()
		eventUUID, err := uuid.FromBytes(key[4:])
		if err != nil {
			return nil, fmt.Errorf("event key %x: %w", key, err)
		}
		events = append(events, models.Event{
			ProjectID: projectID,
			EventUUID: eventUUID.String(),
			RawJSON:   bytes.Clone(iter.Value()),
		})
	}
	return events, iter.Error()
}

// ProjectsWithEvents returns every Project that has at least one Event key.
// It seeks from one Project's key prefix to the next, so it reads one key per
// Project.
func (w *PebbleWriter) ProjectsWithEvents() ([]uint32, error) {
	// Project id 0 is reserved for processing cursors.
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: eventKeyPrefix(1)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var projects []uint32
	for valid := iter.First(); valid; {
		projectID := binary.BigEndian.Uint32(iter.Key()[:4])
		projects = append(projects, projectID)
		upper := eventKeyUpperBound(projectID)
		if upper == nil {
			break
		}
		valid = iter.SeekGE(upper)
	}
	return projects, iter.Error()
}

// processingCursorKey is the reserved Project id 0, "cursor/", then the Project
// id. Rails never assigns Project id 0, so it can't collide with an Event key.
func processingCursorKey(projectID uint32) []byte {
	key := append(eventKeyPrefix(0), "cursor/"...)
	return binary.BigEndian.AppendUint32(key, projectID)
}

// ProcessingCursor returns the UUID of the last Event that Event processing
// handled for a Project, or "" if it has handled none.
func (w *PebbleWriter) ProcessingCursor(projectID uint32) (string, error) {
	value, err := w.get(processingCursorKey(projectID))
	if err != nil || value == nil {
		return "", err
	}
	cursor, err := uuid.FromBytes(value)
	if err != nil {
		return "", fmt.Errorf("processing cursor of project %d: %w", projectID, err)
	}
	return cursor.String(), nil
}

// SetProcessingCursor records the UUID of the last Event that Event processing
// handled for a Project. It is written with NoSync: a power loss can roll it
// back, which only makes Event processing handle some Events again.
func (w *PebbleWriter) SetProcessingCursor(projectID uint32, eventUUID string) error {
	cursor, err := uuid.Parse(eventUUID)
	if err != nil {
		return err
	}
	return w.db.Set(processingCursorKey(projectID), cursor[:], pebble.NoSync)
}

// KeyForEvent generates a binary key for Pebble.
// Format: project ID (4 bytes, big-endian) + UUID (16 bytes)
// The project prefix makes a lookup under the wrong project miss, and keeps each
// project's events together in time order.
func KeyForEvent(projectID uint32, eventUUID string) []byte {
	key := binary.BigEndian.AppendUint32(make([]byte, 0, 20), projectID)
	u, err := uuid.Parse(eventUUID)
	if err != nil {
		return append(key, eventUUID...)
	}
	return append(key, u[:]...)
}

// ReceiveTimeForEventKey returns the time EventStore received an Event.
// Event keys hold UUIDv7s generated when the Event is written, so the
// millisecond Unix timestamp in the UUID's first 48 bits is the receive time.
func ReceiveTimeForEventKey(key []byte) time.Time {
	var ms [8]byte
	copy(ms[2:], key[4:10])
	return time.UnixMilli(int64(binary.BigEndian.Uint64(ms[:])))
}
