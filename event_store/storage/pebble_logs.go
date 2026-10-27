package storage

import (
	"bytes"
	"crypto/rand"
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
// one entry until Log processing moves them to ClickHouse. Its id is a UUIDv7
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

// WriteLogBatch stores a request's normalised Logs, received at receivedAt, as
// one Log batch. It goes through the same mutex as Events, and its id is
// generated under it, so within a Project, Log batches are committed in id
// order and a reader never sees a batch before a lower one.
//
// The id is a UUIDv7 of receivedAt, unless that doesn't come after every id
// written before, or seen in Pebble or a Log processing cursor
// (KeepLogBatchIDsAbove): then it is the next id after them. So a clock that
// steps back never puts a batch below Log processing's cursor, where it would
// be skipped and deleted.
func (w *PebbleWriter) WriteLogBatch(projectID uint32, logs []models.Log, receivedAt time.Time) error {
	value, err := encodeLogBatch(logs)
	if err != nil {
		return fmt.Errorf("encode log batch: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return ErrShuttingDown
	}

	batchID, err := newLogBatchID(receivedAt)
	if err != nil {
		return fmt.Errorf("generate log batch id: %w", err)
	}
	if bytes.Compare(batchID[:], w.logBatchFloor[:]) <= 0 {
		batchID = nextLogBatchID(w.logBatchFloor)
	}
	w.logBatchFloor = batchID

	// NoSync, as for Events: a process crash loses nothing.
	return w.db.Set(logBatchKey(projectID, batchID), value, pebble.NoSync)
}

// newLogBatchID is a UUIDv7 of time at.
func newLogBatchID(at time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	if _, err := rand.Read(id[:]); err != nil {
		return uuid.Nil, err
	}
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(at.UnixMilli()))
	copy(id[:6], ms[2:])
	id[6] = 0x70 | id[6]&0x0f // version 7
	id[8] = 0x80 | id[8]&0x3f // RFC 9562 variant
	return id, nil
}

// nextLogBatchID is the id right after id: one more in its random bits, with
// its time, version and variant kept.
func nextLogBatchID(id uuid.UUID) uuid.UUID {
	for i := 15; i > 8; i-- {
		if id[i]++; id[i] != 0 {
			return id
		}
	}
	id[8] = 0x80 | (id[8]+1)&0x3f
	return id
}

// KeepLogBatchIDsAbove makes every Log batch id written from now on come
// after id, such as a Log processing cursor read at startup.
func (w *PebbleWriter) KeepLogBatchIDsAbove(id uuid.UUID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if bytes.Compare(id[:], w.logBatchFloor[:]) > 0 {
		w.logBatchFloor = id
	}
}

// seedLogBatchFloor keeps new batch ids above the newest stored Log batch
// and Transaction batch of every Project, reading one key per Project.
func (w *PebbleWriter) seedLogBatchFloor() error {
	for _, keyspace := range [][2][]byte{
		{logBatchKeyspace, logBatchKeyspaceEnd},
		{transactionBatchKeyspace, transactionBatchKeyspaceEnd},
	} {
		if err := w.seedBatchFloor(keyspace[0], keyspace[1]); err != nil {
			return err
		}
	}
	return nil
}

// seedBatchFloor keeps new batch ids above the newest batch of every Project
// in the keyspace from lower to upper.
func (w *PebbleWriter) seedBatchFloor(lower, upper []byte) error {
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return err
	}
	defer iter.Close()
	for valid := iter.Last(); valid; {
		key := iter.Key()
		batchID, err := uuid.FromBytes(key[len(key)-16:])
		if err != nil {
			return fmt.Errorf("batch key %x: %w", key, err)
		}
		if bytes.Compare(batchID[:], w.logBatchFloor[:]) > 0 {
			w.logBatchFloor = batchID
		}
		projectStart := binary.BigEndian.AppendUint32(bytes.Clone(lower), binary.BigEndian.Uint32(key[len(lower):]))
		valid = iter.SeekLT(projectStart)
	}
	return iter.Error()
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

// Log processing keeps two ids per Project in Pebble, beside Event
// processing's cursors: its Processing cursor, the last Log batch whose Logs
// are in ClickHouse, and the last batch of the range it is inserting. Each is
// the reserved Project id 0, its prefix, then the Project id. The prefixes sort
// before "logs/", outside the Log batches' keyspace.
func logProcessingCursorKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "logcursor/"...), projectID)
}

func logInsertKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "loginsert/"...), projectID)
}

// LogProcessingCursor returns the id of the last Log batch whose Logs Log
// processing has stored for a Project, or uuid.Nil if it has stored none.
func (w *PebbleWriter) LogProcessingCursor(projectID uint32) (uuid.UUID, error) {
	return w.logBatchID(logProcessingCursorKey(projectID))
}

// SetLogProcessingCursor moves a Project's Log processing cursor to the
// batch with id cursor, once its Logs are stored. NoSync: a cursor lost to a
// power loss is behind its range (RecordLogInsert), which is inserted again,
// and ClickHouse drops the repeat.
func (w *PebbleWriter) SetLogProcessingCursor(projectID uint32, cursor uuid.UUID) error {
	return w.db.Set(logProcessingCursorKey(projectID), cursor[:], pebble.NoSync)
}

// SetLogProcessingCursors moves several Projects' Log processing cursors at
// once, synced, so that they are on disk before what follows records that
// they moved, as loading the prepared cursors records its marker.
func (w *PebbleWriter) SetLogProcessingCursors(cursors map[uint32]uuid.UUID) error {
	batch := w.db.NewBatch()
	defer batch.Close()
	for projectID, cursor := range cursors {
		if err := batch.Set(logProcessingCursorKey(projectID), cursor[:], nil); err != nil {
			return err
		}
	}
	return batch.Commit(pebble.Sync)
}

// LogProcessingCursors returns the Log processing cursor of every Project
// that has one.
func (w *PebbleWriter) LogProcessingCursors() ([]uuid.UUID, error) {
	prefix := append(eventKeyPrefix(0), "logcursor/"...)
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: append(eventKeyPrefix(0), "logcursor0"...)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var cursors []uuid.UUID
	for valid := iter.First(); valid; valid = iter.Next() {
		cursor, err := uuid.FromBytes(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("log processing cursor %x: %w", iter.Key(), err)
		}
		cursors = append(cursors, cursor)
	}
	return cursors, iter.Error()
}

// RecordLogInsert records, before Log processing inserts a Project's Log
// batches, the last batch of the range it inserts: the batches after its
// cursor up to upTo. Synced, so that after any crash the range is inserted
// again whole, never a different one.
func (w *PebbleWriter) RecordLogInsert(projectID uint32, upTo uuid.UUID) error {
	return w.db.Set(logInsertKey(projectID), upTo[:], pebble.Sync)
}

// RecordedLogInsert returns the last batch of the range Log processing last
// recorded for a Project, or uuid.Nil if it has recorded none. A range past
// the cursor may have been inserted, whole or in part, or not at all.
func (w *PebbleWriter) RecordedLogInsert(projectID uint32) (uuid.UUID, error) {
	return w.logBatchID(logInsertKey(projectID))
}

// logBatchID reads a Log batch id stored at key, uuid.Nil if there is none.
func (w *PebbleWriter) logBatchID(key []byte) (uuid.UUID, error) {
	value, err := w.get(key)
	if err != nil || value == nil {
		return uuid.Nil, err
	}
	id, err := uuid.FromBytes(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("log batch id at %x: %w", key, err)
	}
	return id, nil
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
