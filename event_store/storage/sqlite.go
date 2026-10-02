package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/solidtrace/event_store/models"
)

type SQLiteWriter struct {
	db *sql.DB
}

func NewSQLiteWriter(path string) (*SQLiteWriter, error) {
	// _txlock=immediate makes every transaction on this pool take SQLite's write
	// lock at BEGIN. FindOrCreateIssue relies on it. A read-only transaction here
	// would needlessly block writers, so use a separate pool for one.
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_txlock=immediate", path)
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
// it does not mutate Issue state. Callers (Event processing) decide whether to
// reopen a resolved Issue.
//
// Returns (issueID, fingerprintID, issueStatus, found, error).
func (w *SQLiteWriter) FindIssueByFingerprint(projectID uint32, fingerprint string) (int64, int64, int, bool, error) {
	var fingerprintID, issueID int64
	var issueStatus int

	err := w.db.QueryRow(findIssueByFingerprintQuery, projectID, fingerprint).Scan(&fingerprintID, &issueID, &issueStatus)

	if err == nil {
		return issueID, fingerprintID, issueStatus, true, nil
	}

	if err != sql.ErrNoRows {
		return 0, 0, 0, false, err
	}

	return 0, 0, 0, false, nil
}

const findIssueByFingerprintQuery = `
	SELECT ifp.id, ifp.issue_id, i.status
	FROM issue_fingerprints ifp
	JOIN issues i ON ifp.issue_id = i.id
	WHERE ifp.project_id = ? AND ifp.fingerprint = ?
`

// ReopenIssue transitions a resolved Issue back to open.
func (w *SQLiteWriter) ReopenIssue(issueID int64) error {
	_, err := w.db.Exec(
		"UPDATE issues SET status = 0, updated_at = ? WHERE id = ?",
		time.Now().Format("2006-01-02 15:04:05.000000"), issueID,
	)
	return err
}

// FindOrCreateIssue returns the Issue for a fingerprint, creating the Issue
// and its first fingerprint if none exists. created reports which happened.
//
// Event processing calls this only after FindIssueByFingerprint misses. Two Events
// for a new fingerprint can both miss, so the lookup runs again inside the
// transaction. The transaction is BEGIN IMMEDIATE (see the DSN), so it holds
// SQLite's single write lock from the start: no other writer can create the
// fingerprint between this lookup and the inserts.
//
// Returns (issueID, fingerprintID, issueStatus, created, error).
func (w *SQLiteWriter) FindOrCreateIssue(projectID uint32, fingerprint, title, culprit, kind string) (int64, int64, int, bool, error) {
	tx, err := w.db.Begin()
	if err != nil {
		return 0, 0, 0, false, err
	}
	defer tx.Rollback()

	var fingerprintID, issueID int64
	var issueStatus int
	err = tx.QueryRow(findIssueByFingerprintQuery, projectID, fingerprint).Scan(&fingerprintID, &issueID, &issueStatus)
	if err == nil {
		return issueID, fingerprintID, issueStatus, false, nil
	}
	if err != sql.ErrNoRows {
		return 0, 0, 0, false, err
	}

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
		return 0, 0, 0, false, fmt.Errorf("failed to generate issue number: %w", err)
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
		return 0, 0, 0, false, fmt.Errorf("failed to create issue: %w", err)
	}

	issueID, err = res.LastInsertId()
	if err != nil {
		return 0, 0, 0, false, err
	}

	// 3. Create Fingerprint
	res, err = tx.Exec(`
		INSERT INTO issue_fingerprints (issue_id, project_id, fingerprint, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, issueID, projectID, fingerprint, now, now)
	if err != nil {
		return 0, 0, 0, false, fmt.Errorf("failed to create fingerprint: %w", err)
	}

	fingerprintID, err = res.LastInsertId()
	if err != nil {
		return 0, 0, 0, false, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, 0, false, err
	}

	return issueID, fingerprintID, 0, true, nil
}

// RecordSeenEvents adds events to their Issues' times_seen, first_seen_at and
// last_seen_at, and moves the Project's row in project_seen_cursors to
// throughUUID, in one transaction.
//
// project_seen_cursors is the replay guard. Event processing moves its
// processing cursor in Pebble after this commits, so a crash in between
// replays the batch, and the Events at or before the guard are skipped.
func (w *SQLiteWriter) RecordSeenEvents(projectID uint32, events []models.Event, throughUUID string) error {
	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var countedThrough string
	err = tx.QueryRow("SELECT event_uuid FROM project_seen_cursors WHERE project_id = ?", projectID).Scan(&countedThrough)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if throughUUID <= countedThrough {
		return nil
	}

	type seen struct {
		count       int64
		first, last time.Time
	}
	tallies := make(map[int64]*seen)
	for _, event := range events {
		if event.EventUUID <= countedThrough {
			continue
		}
		s, ok := tallies[event.IssueID]
		if !ok {
			tallies[event.IssueID] = &seen{1, event.Timestamp, event.Timestamp}
			continue
		}
		s.count++
		if event.Timestamp.Before(s.first) {
			s.first = event.Timestamp
		}
		if event.Timestamp.After(s.last) {
			s.last = event.Timestamp
		}
	}

	for issueID, s := range tallies {
		// Rails' datetime format, so the strings compare in time order.
		first := s.first.UTC().Format("2006-01-02 15:04:05.000000")
		last := s.last.UTC().Format("2006-01-02 15:04:05.000000")
		_, err := tx.Exec(`
			UPDATE issues SET
				times_seen = times_seen + ?1,
				first_seen_at = MIN(COALESCE(first_seen_at, ?2), ?2),
				last_seen_at = MAX(COALESCE(last_seen_at, ?3), ?3)
			WHERE id = ?4
		`, s.count, first, last, issueID)
		if err != nil {
			return fmt.Errorf("record seen events of issue %d: %w", issueID, err)
		}
	}

	_, err = tx.Exec(`
		INSERT INTO project_seen_cursors (project_id, event_uuid)
		VALUES (?, ?)
		ON CONFLICT (project_id) DO UPDATE SET event_uuid = excluded.event_uuid
	`, projectID, throughUUID)
	if err != nil {
		return fmt.Errorf("move seen cursor: %w", err)
	}

	return tx.Commit()
}
