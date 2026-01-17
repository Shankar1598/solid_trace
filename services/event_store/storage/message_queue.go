package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type MessageQueueWriter struct {
	db *sql.DB
}

func NewMessageQueueWriter(path string) (*MessageQueueWriter, error) {
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-25000", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &MessageQueueWriter{db: db}, nil
}

func (w *MessageQueueWriter) Close() error {
	return w.db.Close()
}

func (w *MessageQueueWriter) EnqueueMessage(messageType string, payload []byte) error {
	query := `
		INSERT INTO event_store_messages (message_type, payload, status, attempts, created_at, updated_at)
		VALUES (?, ?, 0, 0, ?, ?)
	`
	now := time.Now()
	_, err := w.db.Exec(query, messageType, payload, now, now)
	return err
}
