package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/vmihailenco/msgpack/v5"
)

// LogBatch is the normalised Logs of one ingest request, stored in Pebble as
// one entry until Log processing moves them to DuckDB. Its id is a UUIDv7
// generated when it is written, so a Project's batches sort in write order.
type LogBatch struct {
	ID   uuid.UUID
	Logs []models.Log
}

// logBatchKeyspace holds every Log batch: the reserved Project id 0, which
// Event iteration never reads, then "logs/". Processing cursors are under
// "cursor/", which sorts before it, so the two never overlap.
//
// A Log batch key is the keyspace, the Project id (4 bytes, big-endian), then
// the batch id (16 bytes).
var logBatchKeyspace = append(eventKeyPrefix(0), "logs/"...)

// logBatchKeyspaceEnd is the first key past the keyspace.
var logBatchKeyspaceEnd = append(eventKeyPrefix(0), "logs0"...)

func logBatchPrefix(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(bytes.Clone(logBatchKeyspace), projectID)
}

// logBatchUpperBound is the first key past every Log batch of a Project.
func logBatchUpperBound(projectID uint32) []byte {
	if projectID == math.MaxUint32 {
		return logBatchKeyspaceEnd
	}
	return logBatchPrefix(projectID + 1)
}

func logBatchKey(projectID uint32, batchID uuid.UUID) []byte {
	return append(logBatchPrefix(projectID), batchID[:]...)
}

// WriteLogBatch stores a request's normalised Logs as one Log batch. It goes
// through the same mutex as Events, and its id is generated under it, so
// within a Project, Log batches are committed in id order and a reader never
// sees a batch before a lower one.
func (w *PebbleWriter) WriteLogBatch(projectID uint32, logs []models.Log) error {
	value, err := encodeLogBatch(logs)
	if err != nil {
		return fmt.Errorf("encode log batch: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return ErrShuttingDown
	}

	batchID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate log batch id: %w", err)
	}

	// NoSync, as for Events: a process crash loses nothing.
	return w.db.Set(logBatchKey(projectID, batchID), value, pebble.NoSync)
}

// LogBatchesAfter returns a Project's Log batches that come after the batch
// with id after, in id order. uuid.Nil starts at the Project's first batch. It
// stops once the batches hold maxLogs Logs or more, so it returns at least one
// batch when there is one.
func (w *PebbleWriter) LogBatchesAfter(projectID uint32, after uuid.UUID, maxLogs int) ([]LogBatch, error) {
	lower := logBatchPrefix(projectID)
	if after != uuid.Nil {
		// The zero byte makes the bound exclusive of after's own key.
		lower = append(logBatchKey(projectID, after), 0)
	}
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: logBatchUpperBound(projectID)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var batches []LogBatch
	logs := 0
	for valid := iter.First(); valid && logs < maxLogs; valid = iter.Next() {
		key := iter.Key()
		batchID, err := uuid.FromBytes(key[len(key)-16:])
		if err != nil {
			return nil, fmt.Errorf("log batch key %x: %w", key, err)
		}
		batch, err := decodeLogBatch(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("log batch %x: %w", key, err)
		}
		batches = append(batches, LogBatch{ID: batchID, Logs: batch})
		logs += len(batch)
	}
	return batches, iter.Error()
}

// DeleteLogBatchesUpTo deletes a Project's Log batches up to and including the
// batch with id upTo. NoSync: a delete lost to a power loss leaves batches
// that Log processing has already handled, and its cursor skips them.
func (w *PebbleWriter) DeleteLogBatchesUpTo(projectID uint32, upTo uuid.UUID) error {
	return w.db.DeleteRange(logBatchPrefix(projectID), append(logBatchKey(projectID, upTo), 0), pebble.NoSync)
}

// ProjectsWithLogBatches returns every Project that has at least one Log
// batch, reading one key per Project.
func (w *PebbleWriter) ProjectsWithLogBatches() ([]uint32, error) {
	oldest, err := w.OldestLogBatches()
	projects := make([]uint32, len(oldest))
	for i, batch := range oldest {
		projects[i] = batch.ProjectID
	}
	return projects, err
}

// OldestLogBatch is the id of a Project's oldest Log batch.
type OldestLogBatch struct {
	ProjectID uint32
	BatchID   uuid.UUID
}

// OldestLogBatches returns the oldest Log batch of every Project that has
// one, in Project order, reading one key per Project.
func (w *PebbleWriter) OldestLogBatches() ([]OldestLogBatch, error) {
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: logBatchKeyspace, UpperBound: logBatchKeyspaceEnd})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var oldest []OldestLogBatch
	for valid := iter.First(); valid; {
		key := iter.Key()
		projectID := binary.BigEndian.Uint32(key[len(logBatchKeyspace):])
		batchID, err := uuid.FromBytes(key[len(key)-16:])
		if err != nil {
			return nil, fmt.Errorf("log batch key %x: %w", key, err)
		}
		oldest = append(oldest, OldestLogBatch{projectID, batchID})
		valid = iter.SeekGE(logBatchUpperBound(projectID))
	}
	return oldest, iter.Error()
}

// LogBatchWrittenAt is when the Log batch with id batchID was written, to the
// millisecond: the time in its UUIDv7.
func LogBatchWrittenAt(batchID uuid.UUID) time.Time {
	return time.Unix(batchID.Time().UnixTime())
}

// logBatchFormat is the first byte of every Log batch value, so a format
// change can tell old batches apart.
const logBatchFormat byte = 1

func encodeLogBatch(logs []models.Log) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(logBatchFormat)
	if err := msgpack.NewEncoder(&buf).Encode(logs); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeLogBatch(value []byte) ([]models.Log, error) {
	if len(value) == 0 || value[0] != logBatchFormat {
		return nil, fmt.Errorf("unknown log batch format")
	}
	var logs []models.Log
	if err := msgpack.Unmarshal(value[1:], &logs); err != nil {
		return nil, err
	}
	for i := range logs {
		// msgpack decodes times in the local zone.
		logs[i].Time = logs[i].Time.UTC()
	}
	return logs, nil
}
