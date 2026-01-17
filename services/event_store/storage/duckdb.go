package storage

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/solidtrace/event_store/models"
)

type DuckDBWriter struct {
	db *sql.DB
}

type QueryParams struct {
	ProjectID           uint32
	IssueFingerprintIDs []int64
	UUID                string
	NewerThan           time.Time
	OlderThan           time.Time
	Limit               int
	Offset              int
	SortDesc            bool
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

	// Create table if not exists with correct schema
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS events (
            uuid VARCHAR,
            project_id BIGINT,
            issue_fingerprint_id BIGINT,
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

	stmt, err := tx.Prepare("INSERT INTO events (uuid, project_id, issue_fingerprint_id, timestamp, tags) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, event := range events {
		_, err = stmt.Exec(event.EventUUID, event.ProjectID, event.IssueFingerprintID, event.Timestamp, event.Tags)
		if err != nil {
			log.Printf("DuckDB insert error: %v", err)
		}
	}

	return tx.Commit()
}

func (w *DuckDBWriter) QueryEvents(params QueryParams) ([]models.Event, error) {
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString("SELECT uuid, project_id, issue_fingerprint_id, timestamp, tags FROM events WHERE project_id = ?")
	args := []interface{}{params.ProjectID}

	if params.UUID != "" {
		queryBuilder.WriteString(" AND uuid = ?")
		args = append(args, params.UUID)
	}

	if len(params.IssueFingerprintIDs) > 0 {
		placeholders := make([]string, len(params.IssueFingerprintIDs))
		for i, id := range params.IssueFingerprintIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		queryBuilder.WriteString(fmt.Sprintf(" AND issue_fingerprint_id IN (%s)", strings.Join(placeholders, ",")))
	}

	if !params.NewerThan.IsZero() {
		queryBuilder.WriteString(" AND timestamp > ?")
		args = append(args, params.NewerThan)
	}

	if !params.OlderThan.IsZero() {
		queryBuilder.WriteString(" AND timestamp < ?")
		args = append(args, params.OlderThan)
	}

	if params.SortDesc {
		queryBuilder.WriteString(" ORDER BY timestamp DESC")
	} else {
		queryBuilder.WriteString(" ORDER BY timestamp ASC")
	}

	if params.Limit > 0 {
		queryBuilder.WriteString(" LIMIT ?")
		args = append(args, params.Limit)
	}

	if params.Offset > 0 {
		queryBuilder.WriteString(" OFFSET ?")
		args = append(args, params.Offset)
	}

	rows, err := w.db.Query(queryBuilder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var e models.Event
		// Note: We don't have IssueID (stored in SQLite) or RawJSON (stored in RocksDB)
		// We only populate what we have in DuckDB
		err := rows.Scan(&e.EventUUID, &e.ProjectID, &e.IssueFingerprintID, &e.Timestamp, &e.Tags)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

func (w *DuckDBWriter) CountEvents(params QueryParams) (int64, error) {
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString("SELECT COUNT(*) FROM events WHERE project_id = ?")
	args := []interface{}{params.ProjectID}

	if len(params.IssueFingerprintIDs) > 0 {
		placeholders := make([]string, len(params.IssueFingerprintIDs))
		for i, id := range params.IssueFingerprintIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		queryBuilder.WriteString(fmt.Sprintf(" AND issue_fingerprint_id IN (%s)", strings.Join(placeholders, ",")))
	}

	var count int64
	err := w.db.QueryRow(queryBuilder.String(), args...).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (w *DuckDBWriter) Close() {
	w.db.Close()
}
