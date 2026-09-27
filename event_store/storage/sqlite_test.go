package storage

import (
	"path/filepath"
	"sync"
	"testing"
)

// issueTablesDDL copies the three tables SQLiteWriter writes from
// console/db/schema.rb, which owns them. Keep the unique indexes in step: the
// concurrency test depends on them.
const issueTablesDDL = `
CREATE TABLE issues (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	assignee_id INTEGER,
	created_at DATETIME NOT NULL,
	culprit VARCHAR,
	kind INTEGER DEFAULT 0 NOT NULL,
	number BIGINT NOT NULL,
	project_id INTEGER NOT NULL,
	status INTEGER DEFAULT 0 NOT NULL,
	title VARCHAR NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE UNIQUE INDEX index_issues_on_project_id_and_number ON issues (project_id, number);

CREATE TABLE issue_fingerprints (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME NOT NULL,
	fingerprint VARCHAR,
	issue_id INTEGER NOT NULL,
	project_id INTEGER NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE UNIQUE INDEX index_issue_fingerprints_on_project_id_and_fingerprint ON issue_fingerprints (project_id, fingerprint);

CREATE TABLE project_issue_counters (
	project_id INTEGER PRIMARY KEY,
	value BIGINT DEFAULT 0 NOT NULL
);
`

func newTestSQLiteWriter(t *testing.T) *SQLiteWriter {
	t.Helper()
	writer, err := NewSQLiteWriter(filepath.Join(t.TempDir(), "console.sqlite3"))
	if err != nil {
		t.Fatalf("Failed to open SQLite: %v", err)
	}
	t.Cleanup(func() { writer.Close() })

	if _, err := writer.db.Exec(issueTablesDDL); err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}
	return writer
}

func TestFindOrCreateIssue_ConcurrentCallsForNewFingerprintCreateOneIssue(t *testing.T) {
	writer := newTestSQLiteWriter(t)

	const callers = 50
	type result struct {
		issueID, fingerprintID int64
		created                bool
		err                    error
	}
	results := make([]result, callers)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			issueID, fingerprintID, _, created, err := writer.FindOrCreateIssue(7, "fp-new", "RuntimeError: boom", "app.rb", "error")
			results[i] = result{issueID, fingerprintID, created, err}
		}()
	}
	close(start)
	wg.Wait()

	createdCount := 0
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("caller %d: %v", i, r.err)
		}
		if r.created {
			createdCount++
		}
		if r.issueID != results[0].issueID || r.fingerprintID != results[0].fingerprintID {
			t.Errorf("caller %d got issue %d / fingerprint %d, caller 0 got %d / %d",
				i, r.issueID, r.fingerprintID, results[0].issueID, results[0].fingerprintID)
		}
	}
	if createdCount != 1 {
		t.Errorf("expected exactly 1 caller to create the issue, got %d", createdCount)
	}

	var issues, counter int
	if err := writer.db.QueryRow("SELECT COUNT(*) FROM issues").Scan(&issues); err != nil {
		t.Fatal(err)
	}
	if err := writer.db.QueryRow("SELECT value FROM project_issue_counters WHERE project_id = 7").Scan(&counter); err != nil {
		t.Fatal(err)
	}
	if issues != 1 {
		t.Errorf("expected 1 issue row, got %d", issues)
	}
	if counter != 1 {
		t.Errorf("expected issue counter 1, got %d", counter)
	}
}

func TestFindOrCreateIssue_ReturnsExistingIssueWithStatus(t *testing.T) {
	writer := newTestSQLiteWriter(t)

	issueID, fingerprintID, _, created, err := writer.FindOrCreateIssue(7, "fp", "title", "culprit", "error")
	if err != nil || !created {
		t.Fatalf("first call: created=%v err=%v", created, err)
	}
	if _, err := writer.db.Exec("UPDATE issues SET status = 1 WHERE id = ?", issueID); err != nil {
		t.Fatal(err)
	}

	gotIssueID, gotFingerprintID, status, created, err := writer.FindOrCreateIssue(7, "fp", "title", "culprit", "error")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if created {
		t.Error("expected created=false for an existing fingerprint")
	}
	if gotIssueID != issueID || gotFingerprintID != fingerprintID {
		t.Errorf("expected issue %d / fingerprint %d, got %d / %d", issueID, fingerprintID, gotIssueID, gotFingerprintID)
	}
	if status != 1 {
		t.Errorf("expected resolved status 1, got %d", status)
	}
}
