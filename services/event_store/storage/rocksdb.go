package storage

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"time"

	"github.com/linxGnu/grocksdb"
	"github.com/solidtrace/event_store/models"
)

type RocksDBWriter struct {
	db *grocksdb.DB
	wo *grocksdb.WriteOptions
}

func NewRocksDBWriter(path string) (*RocksDBWriter, error) {
	// Ensure directory exists
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetCompression(grocksdb.LZ4Compression)
	opts.SetBottommostCompression(grocksdb.ZSTDCompression)
	opts.EnableBlobFiles(true)
	opts.SetMinBlobSize(4096)
	opts.SetBlobFileSize(268435456) // 256MB
	opts.SetBlobCompressionType(grocksdb.ZSTDCompression)
	opts.EnableBlobGC(true)

	db, err := grocksdb.OpenDb(opts, path)
	if err != nil {
		return nil, err
	}

	wo := grocksdb.NewDefaultWriteOptions()
	wo.SetSync(false) // Fast writes, WAL provides durability

	return &RocksDBWriter{db: db, wo: wo}, nil
}

func (w *RocksDBWriter) WriteBatch(events []models.Event) error {
	batch := grocksdb.NewWriteBatch()
	defer batch.Destroy()

	for _, event := range events {
		key := KeyForEvent(event.ProjectID, event.EventUUID, event.Timestamp)
		batch.Put(key, event.RawJSON)
	}

	return w.db.Write(w.wo, batch)
}

func (w *RocksDBWriter) FlushWAL() error {
	return w.db.FlushWAL(true)
}

// GetEvent retrieves an event by its key (called by Rails via HTTP)
func (w *RocksDBWriter) GetEvent(key []byte) ([]byte, error) {
	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	slice, err := w.db.Get(ro, key)
	if err != nil {
		return nil, err
	}
	defer slice.Free()

	if !slice.Exists() {
		return nil, nil
	}

	// Copy data before freeing
	data := make([]byte, slice.Size())
	copy(data, slice.Data())
	return data, nil
}

func (w *RocksDBWriter) Close() {
	w.db.Close()
}

const MaxUint64 = ^uint64(0)

// KeyForEvent generates a binary key for RocksDB.
// Format: ProjectID (4 bytes BE) + ReverseTimestamp (8 bytes BE) + EventUUID (16 bytes)
//
// Why reverse timestamp? So that scanning by prefix (ProjectID) returns
// events in newest-first order.
func KeyForEvent(projectID uint32, eventUUID string, timestamp time.Time) []byte {
	key := make([]byte, 28) // 4 + 8 + 16 = 28 bytes

	// 1. ProjectID as 4-byte big-endian
	binary.BigEndian.PutUint32(key[0:4], projectID)

	// 2. Reverse timestamp (microseconds since epoch, inverted)
	microSeconds := uint64(timestamp.UnixMicro())
	reverseTimestamp := MaxUint64 - microSeconds
	binary.BigEndian.PutUint64(key[4:12], reverseTimestamp)

	// 3. EventUUID as 16 raw bytes (decode from hex string)
	//    Event UUID comes as "8e06f9c623114e978329e37700b5f261" (32 hex chars)
	uuidBytes, _ := hex.DecodeString(eventUUID)
	copy(key[12:28], uuidBytes)

	return key
}
