package storage

import (
	"os"

	"github.com/google/uuid"
	"github.com/linxGnu/grocksdb"
	"github.com/solidtrace/event_store/models"
)

type RocksDBWriter struct {
	db *grocksdb.DB
	wo *grocksdb.WriteOptions
}

type RocksDBPathConfig struct {
	Path            string
	TargetSizeBytes uint64
}

func NewRocksDBWriter(path string, dbPaths []RocksDBPathConfig) (*RocksDBWriter, error) {
	// Ensure directory exists
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}
	for _, dbPath := range dbPaths {
		if dbPath.Path == "" {
			continue
		}
		if err := os.MkdirAll(dbPath.Path, 0755); err != nil {
			return nil, err
		}
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
	opts.SetLevelCompactionDynamicLevelBytes(true)
	if len(dbPaths) > 0 {
		paths := make([]*grocksdb.DBPath, 0, len(dbPaths))
		for _, dbPath := range dbPaths {
			if dbPath.Path == "" {
				continue
			}
			rocksPath := grocksdb.NewDBPath(dbPath.Path, dbPath.TargetSizeBytes)
			paths = append(paths, rocksPath)
		}
		opts.SetDBPaths(paths)
		defer func() {
			for _, rocksPath := range paths {
				rocksPath.Destroy()
			}
		}()
	}

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
		key := KeyForEvent(event.EventUUID)
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

// Ping checks if RocksDB is healthy by attempting a read operation
func (w *RocksDBWriter) Ping() error {
	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	// Try to get a non-existent key - this exercises the read path
	slice, err := w.db.Get(ro, []byte("__health_check__"))
	if err != nil {
		return err
	}
	defer slice.Free()
	return nil
}

// KeyForEvent generates a binary key for RocksDB.
// Format: UUID (16 bytes)
func KeyForEvent(eventUUID string) []byte {
	// Parse UUID string (handles dashes and hex)
	u, err := uuid.Parse(eventUUID)
	if err != nil {
		// Fallback for invalid UUIDs? Or return error?
		// For now we assume valid UUIDs from ingestion.
		// If invalid, return nil or empty slice (DB write might fail or store empty key)
		// Better to return bytes.
		return []byte(eventUUID) // Fallback to raw bytes if parse fails (legacy behavior expectation?)
	}
	// Return the 16 bytes
	return u[:]
}
