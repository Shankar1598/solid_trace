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

// TransactionBatch is the Transactions of one envelope, stored in Pebble as
// one entry until Span processing moves them to ClickHouse. Its id is drawn
// as a Log batch's is, from the same floor, so a Project's batches sort in
// write order.
type TransactionBatch struct {
	ID           uuid.UUID
	Transactions []models.Transaction
}

// transactionBatchKeyspace holds every Transaction batch: the reserved
// Project id 0, then "txns/", which sorts after the Log batches' "logs/" and
// before Span processing's "txncursor/" and "txninsert/", apart from each.
// A key is the keyspace, the Project id (4 bytes, big-endian), then the batch
// id (16 bytes).
var (
	transactionBatchKeyspace    = append(eventKeyPrefix(0), "txns/"...)
	transactionBatchKeyspaceEnd = append(eventKeyPrefix(0), "txns0"...)
)

func transactionBatchPrefix(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(bytes.Clone(transactionBatchKeyspace), projectID)
}

func transactionBatchUpperBound(projectID uint32) []byte {
	if projectID == math.MaxUint32 {
		return transactionBatchKeyspaceEnd
	}
	return transactionBatchPrefix(projectID + 1)
}

func transactionBatchKey(projectID uint32, batchID uuid.UUID) []byte {
	return append(transactionBatchPrefix(projectID), batchID[:]...)
}

// WriteTransactionBatch stores an envelope's Transactions, received at
// receivedAt, as one Transaction batch, under the same mutex and id floor as
// Log batches (WriteLogBatch).
func (w *PebbleWriter) WriteTransactionBatch(projectID uint32, transactions []models.Transaction, receivedAt time.Time) error {
	var buf bytes.Buffer
	buf.WriteByte(transactionBatchFormat)
	if err := msgpack.NewEncoder(&buf).Encode(transactions); err != nil {
		return fmt.Errorf("encode transaction batch: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return ErrShuttingDown
	}
	batchID, err := newLogBatchID(receivedAt)
	if err != nil {
		return fmt.Errorf("generate transaction batch id: %w", err)
	}
	if bytes.Compare(batchID[:], w.logBatchFloor[:]) <= 0 {
		batchID = nextLogBatchID(w.logBatchFloor)
	}
	w.logBatchFloor = batchID
	// NoSync, as for Events and Logs.
	return w.db.Set(transactionBatchKey(projectID, batchID), buf.Bytes(), pebble.NoSync)
}

// TransactionBatchesAfter returns a Project's Transaction batches after the
// batch with id after, in id order, uuid.Nil starting at its first. It stops
// once their payloads hold maxBytes or more, so it returns at least one
// batch when there is one.
func (w *PebbleWriter) TransactionBatchesAfter(projectID uint32, after uuid.UUID, maxBytes int) ([]TransactionBatch, error) {
	lower := transactionBatchPrefix(projectID)
	if after != uuid.Nil {
		lower = append(transactionBatchKey(projectID, after), 0)
	}
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: transactionBatchUpperBound(projectID)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var batches []TransactionBatch
	size := 0
	for valid := iter.First(); valid && size < maxBytes; valid = iter.Next() {
		key := iter.Key()
		batchID, err := uuid.FromBytes(key[len(key)-16:])
		if err != nil {
			return nil, fmt.Errorf("transaction batch key %x: %w", key, err)
		}
		value := iter.Value()
		if len(value) == 0 || value[0] != transactionBatchFormat {
			return nil, fmt.Errorf("transaction batch %x: unknown format", key)
		}
		var transactions []models.Transaction
		if err := msgpack.Unmarshal(value[1:], &transactions); err != nil {
			return nil, fmt.Errorf("transaction batch %x: %w", key, err)
		}
		batches = append(batches, TransactionBatch{ID: batchID, Transactions: transactions})
		size += len(value)
	}
	return batches, iter.Error()
}

// DeleteTransactionBatchesUpTo deletes a Project's Transaction batches up to
// and including the batch with id upTo. NoSync, as DeleteLogBatchesUpTo.
func (w *PebbleWriter) DeleteTransactionBatchesUpTo(projectID uint32, upTo uuid.UUID) error {
	return w.db.DeleteRange(transactionBatchPrefix(projectID), append(transactionBatchKey(projectID, upTo), 0), pebble.NoSync)
}

// ProjectsWithTransactionBatches returns every Project that has at least one
// Transaction batch, reading one key per Project.
func (w *PebbleWriter) ProjectsWithTransactionBatches() ([]uint32, error) {
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: transactionBatchKeyspace, UpperBound: transactionBatchKeyspaceEnd})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var projects []uint32
	for valid := iter.First(); valid; {
		projectID := binary.BigEndian.Uint32(iter.Key()[len(transactionBatchKeyspace):])
		projects = append(projects, projectID)
		valid = iter.SeekGE(transactionBatchUpperBound(projectID))
	}
	return projects, iter.Error()
}

// Span processing keeps two ids per Project, as Log processing does: its
// cursor, the last Transaction batch whose Spans are in ClickHouse, and the
// last batch of the range it is inserting.
func spanProcessingCursorKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "txncursor/"...), projectID)
}

func spanInsertKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "txninsert/"...), projectID)
}

// SpanProcessingCursor returns the id of the last Transaction batch whose
// Spans Span processing has stored for a Project, or uuid.Nil.
func (w *PebbleWriter) SpanProcessingCursor(projectID uint32) (uuid.UUID, error) {
	return w.logBatchID(spanProcessingCursorKey(projectID))
}

// SetSpanProcessingCursor moves a Project's Span processing cursor once its
// range is stored. NoSync, as SetLogProcessingCursor.
func (w *PebbleWriter) SetSpanProcessingCursor(projectID uint32, cursor uuid.UUID) error {
	return w.db.Set(spanProcessingCursorKey(projectID), cursor[:], pebble.NoSync)
}

// KeepBatchIDsAboveSpanCursors makes every batch id written from now on come
// after every Project's Span processing cursor.
func (w *PebbleWriter) KeepBatchIDsAboveSpanCursors() error {
	iter, err := w.db.NewIter(&pebble.IterOptions{
		LowerBound: append(eventKeyPrefix(0), "txncursor/"...),
		UpperBound: append(eventKeyPrefix(0), "txncursor0"...),
	})
	if err != nil {
		return err
	}
	defer iter.Close()
	for valid := iter.First(); valid; valid = iter.Next() {
		cursor, err := uuid.FromBytes(iter.Value())
		if err != nil {
			return fmt.Errorf("span processing cursor %x: %w", iter.Key(), err)
		}
		w.KeepLogBatchIDsAbove(cursor)
	}
	return iter.Error()
}

// RecordSpanInsert records, synced, the last batch of the range Span
// processing is about to insert, as RecordLogInsert.
func (w *PebbleWriter) RecordSpanInsert(projectID uint32, upTo uuid.UUID) error {
	return w.db.Set(spanInsertKey(projectID), upTo[:], pebble.Sync)
}

// RecordedSpanInsert returns the last batch of the range Span processing
// last recorded for a Project, or uuid.Nil.
func (w *PebbleWriter) RecordedSpanInsert(projectID uint32) (uuid.UUID, error) {
	return w.logBatchID(spanInsertKey(projectID))
}

// transactionBatchFormat is the first byte of every Transaction batch value.
const transactionBatchFormat byte = 1
