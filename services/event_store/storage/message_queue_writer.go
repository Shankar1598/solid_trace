package storage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "github.com/mattn/go-sqlite3"
	"github.com/solidtrace/event_store/pkg/msgpacker"
)

type MessageQueueWriter struct {
	db     *sql.DB
	packer *msgpacker.Packer
}

func NewMessageQueueWriter(path string) (*MessageQueueWriter, error) {
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	// Use Zstd level 3 (SpeedDefault) as requested (-3 or -5).
	packer := msgpacker.New(msgpacker.ModeZstd, msgpacker.Options{
		ZstdLevel: zstd.SpeedFastest,
	})

	return &MessageQueueWriter{
		db:     db,
		packer: packer,
	}, nil
}

func (w *MessageQueueWriter) Close() error {
	return w.db.Close()
}

func (w *MessageQueueWriter) EnqueueMessage(messageType string, payload interface{}) error {
	encodedPayload, err := w.packer.Pack(payload)
	if err != nil {
		return fmt.Errorf("failed to pack message payload: %w", err)
	}

	query := `
		INSERT INTO event_store_messages (message_type, payload, status, attempts, created_at, updated_at)
		VALUES (?, ?, 0, 0, ?, ?)
	`
	now := time.Now().Format("2006-01-02 15:04:05.000000")
	_, err = w.db.Exec(query, messageType, encodedPayload, now, now)
	return err
}
