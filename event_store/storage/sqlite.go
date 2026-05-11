package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteWriter struct {
	db *sql.DB
}

func NewSQLiteWriter(path string) (*SQLiteWriter, error) {
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &SQLiteWriter{db: db}, nil
}

func (w *SQLiteWriter) Close() error {
	return w.db.Close()
}

// FindOrCreateIssueFingerprint returns the ID of the issue_fingerprint.
func (w *SQLiteWriter) FindOrCreateIssueFingerprint(projectID uint32, fingerprint string) (int64, error) {
	var id int64
	err := w.db.QueryRow("SELECT id FROM issue_fingerprints WHERE project_id = ? AND fingerprint = ?", projectID, fingerprint).Scan(&id)
	if err == nil {
		return id, nil
	} else if err != sql.ErrNoRows {
		return 0, err
	}
	return 0, sql.ErrNoRows
}

// FindIssueByFingerprint checks if a fingerprint exists and returns the
// associated issue ID plus the current issue status. This is a pure read —
// it does not mutate Issue state. Callers (Event ingest) decide whether to
// reopen a resolved Issue.
//
// Returns (issueID, fingerprintID, issueStatus, found, error).
func (w *SQLiteWriter) FindIssueByFingerprint(projectID uint32, fingerprint string) (int64, int64, int, bool, error) {
	var fingerprintID, issueID int64
	var issueStatus int

	query := `
		SELECT ifp.id, ifp.issue_id, i.status
		FROM issue_fingerprints ifp
		JOIN issues i ON ifp.issue_id = i.id
		WHERE ifp.project_id = ? AND ifp.fingerprint = ?
	`
	err := w.db.QueryRow(query, projectID, fingerprint).Scan(&fingerprintID, &issueID, &issueStatus)

	if err == nil {
		return issueID, fingerprintID, issueStatus, true, nil
	}

	if err != sql.ErrNoRows {
		return 0, 0, 0, false, err
	}

	return 0, 0, 0, false, nil
}

// ReopenIssue transitions a resolved Issue back to open.
func (w *SQLiteWriter) ReopenIssue(issueID int64) error {
	_, err := w.db.Exec(
		"UPDATE issues SET status = 0, updated_at = ? WHERE id = ?",
		time.Now().Format("2006-01-02 15:04:05.000000"), issueID,
	)
	return err
}

// CreateIssueWithFingerprint creates a new issue and its first fingerprint
func (w *SQLiteWriter) CreateIssueWithFingerprint(projectID uint32, fingerprint, title, culprit, kind string) (int64, int64, error) {
	tx, err := w.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	// 1. Get next issue number using atomic upsert
	var issueNumber int
	query := `
		INSERT INTO project_issue_counters (project_id, value)
		VALUES (?, 1)
		ON CONFLICT (project_id) DO UPDATE
		SET value = project_issue_counters.value + 1
		RETURNING value
	`
	err = tx.QueryRow(query, projectID).Scan(&issueNumber)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to generate issue number: %w", err)
	}

	// 2. Create Issue
	now := time.Now().Format("2006-01-02 15:04:05.000000")
	// Map kind string to integer enum
	kindInt := 0
	switch kind {
	case "error":
		kindInt = 1
	case "csp":
		kindInt = 2
	}

	res, err := tx.Exec(`
		INSERT INTO issues (project_id, number, title, culprit, kind, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`, projectID, issueNumber, title, culprit, kindInt, now, now)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create issue: %w", err)
	}

	issueID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	// 3. Create Fingerprint
	res, err = tx.Exec(`
		INSERT INTO issue_fingerprints (issue_id, project_id, fingerprint, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, issueID, projectID, fingerprint, now, now)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create fingerprint: %w", err)
	}

	fingerprintID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}

	return issueID, fingerprintID, nil
}
