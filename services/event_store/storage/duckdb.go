package storage

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/solidtrace/event_store/models"
)

type DuckDBWriter struct {
	db *sql.DB
}

func NewDuckDBWriter(path string) (*DuckDBWriter, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}

	// Create table if not exists
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS events (
            uuid VARCHAR,
            project_id BIGINT,
            timestamp TIMESTAMP,
            tags MAP(VARCHAR, VARCHAR)
        )
    `)
	if err != nil {
		return nil, err
	}

	return &DuckDBWriter{db: db}, nil
}

func (w *DuckDBWriter) WriteBatch(events []models.Event) error {
	tx, err := w.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare("INSERT INTO events (uuid, project_id, timestamp, tags) VALUES (?, ?, ?, ?)")
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, event := range events {
		// Convert tags map to DuckDB map format (handled by driver usually, or cast)
		// Check if driver supports map directly. The marcboeker/go-duckdb driver supports Map via structs or map[K]V?
		// Docs say: "Maps are supported as map[K]V".
		_, err = stmt.Exec(event.EventUUID, event.ProjectID, event.Timestamp, event.Tags)
		if err != nil {
			log.Printf("DuckDB insert error: %v", err)
			// Continue or abort? Ideally we want to potentially proceed with other events?
			// But batch insert via Exec usually fails the batch.
			// We'll log and continue. If we want atomic batch, we fail locally.
		}
	}

	return tx.Commit()
}

func (w *DuckDBWriter) Close() {
	w.db.Close()
}
