package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/solidtrace/event_store/pkg/msgpacker"
)

// Message status constants matching Rails AdminMessage model
const (
	StatusPending int = iota
	StatusProcessing
	StatusProcessed
	StatusFailed
)

// ConsoleMessage represents a message from Rails for Go to process
type ConsoleMessage struct {
	ID          int64
	MessageType string
	Payload     []byte
	Status      int
	Attempts    int
	ProcessedAt sql.NullString
	Error       sql.NullString
	CreatedAt   string
	UpdatedAt   string
}

// MessageQueueReader reads messages from the console_messages table
type MessageQueueReader struct {
	db       *sql.DB
	unpacker *msgpacker.Packer
}

// NewMessageQueueReader creates a new reader for console messages
func NewMessageQueueReader(path string) (*MessageQueueReader, error) {
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	// Use the same unpacker settings as the writer for consistency
	unpacker := msgpacker.New(msgpacker.ModeZstd)

	return &MessageQueueReader{
		db:       db,
		unpacker: unpacker,
	}, nil
}

// Close closes the database connection
func (r *MessageQueueReader) Close() error {
	return r.db.Close()
}

// FetchPendingMessages fetches pending or failed messages with < 5 attempts
func (r *MessageQueueReader) FetchPendingMessages(messageType string, limit int) ([]ConsoleMessage, error) {
	query := `
		SELECT id, message_type, payload, status, attempts, processed_at, error_message, created_at, updated_at
		FROM console_messages
		WHERE message_type = ?
		  AND status IN (?, ?)
		  AND attempts < 5
		ORDER BY created_at ASC
		LIMIT ?
	`

	rows, err := r.db.Query(query, messageType, StatusPending, StatusFailed, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}
	defer rows.Close()

	var messages []ConsoleMessage
	for rows.Next() {
		var m ConsoleMessage
		err := rows.Scan(
			&m.ID,
			&m.MessageType,
			&m.Payload,
			&m.Status,
			&m.Attempts,
			&m.ProcessedAt,
			&m.Error,
			&m.CreatedAt,
			&m.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, m)
	}

	return messages, nil
}

// MarkProcessing marks a message as processing and increments attempts
func (r *MessageQueueReader) MarkProcessing(id int64) error {
	query := `
		UPDATE console_messages
		SET status = ?, attempts = attempts + 1, updated_at = ?
		WHERE id = ?
	`
	_, err := r.db.Exec(query, StatusProcessing, time.Now().Format("2006-01-02 15:04:05.000000"), id)
	return err
}

// MarkProcessed marks a message as successfully processed
func (r *MessageQueueReader) MarkProcessed(id int64) error {
	query := `
		UPDATE console_messages
		SET status = ?, processed_at = ?, updated_at = ?
		WHERE id = ?
	`
	now := time.Now().Format("2006-01-02 15:04:05.000000")
	_, err := r.db.Exec(query, StatusProcessed, now, now, id)
	return err
}

// MarkFailed marks a message as failed with an error message
func (r *MessageQueueReader) MarkFailed(id int64, errorMsg string) error {
	query := `
		UPDATE console_messages
		SET status = ?, error_message = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := r.db.Exec(query, StatusFailed, errorMsg, time.Now().Format("2006-01-02 15:04:05.000000"), id)
	return err
}

// UnpackPayload unpacks a message payload into the given struct
func (r *MessageQueueReader) UnpackPayload(payload []byte, v interface{}) error {
	return r.unpacker.Unpack(payload, v)
}
