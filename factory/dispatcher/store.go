package dispatcher

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/solidtrace/cli/dispatcherapi"
)

// migrations are the database's schema steps, in order. PRAGMA user_version
// is the count of steps that the database has. Add steps at the end only.
var migrations = []string{
	// organization_slug is the slug when the Run started; it is data only.
	// The API gives the current slug from the token check.
	`CREATE TABLE runs (
		id TEXT PRIMARY KEY,
		organization_id TEXT NOT NULL,
		organization_slug TEXT NOT NULL,
		project TEXT NOT NULL,
		prompt TEXT NOT NULL,
		state TEXT NOT NULL,
		started_by_id TEXT NOT NULL,
		started_by_name TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX runs_by_organization ON runs (organization_id, created_at);`,
}

// store is the Dispatcher's SQLite database.
type store struct {
	db *sql.DB
}

var errNotFound = errors.New("not found")

func openStore(path string) (*store, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("open the database %s: %w", path, err)
	}
	return &store{db: db}, nil
}

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("the database is from a newer Dispatcher (schema %d, this one knows %d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		if _, err := tx.Exec(migrations[i]); err != nil {
			return fmt.Errorf("schema step %d: %w", i+1, err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations))); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) close() error { return s.db.Close() }

// storedRun is a Run with the Organization id that scopes it.
type storedRun struct {
	dispatcherapi.Run
	organizationID string
}

func (s *store) insertRun(r storedRun) error {
	_, err := s.db.Exec(`INSERT INTO runs (id, organization_id, organization_slug, project, prompt, state,
		started_by_id, started_by_name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.organizationID, r.Organization, r.Project, r.Prompt, r.State, r.StartedBy.ID, r.StartedBy.Name,
		r.CreatedAt.UnixMicro())
	return err
}

const runColumns = `id, organization_slug, project, prompt, state, started_by_id, started_by_name, created_at`

func scanRun(row interface{ Scan(...any) error }) (dispatcherapi.Run, error) {
	var r dispatcherapi.Run
	var created int64
	err := row.Scan(&r.ID, &r.Organization, &r.Project, &r.Prompt, &r.State, &r.StartedBy.ID, &r.StartedBy.Name, &created)
	r.CreatedAt = time.UnixMicro(created).UTC()
	return r, err
}

// run reads one Run of the Organization.
func (s *store) run(organizationID, id string) (dispatcherapi.Run, error) {
	r, err := scanRun(s.db.QueryRow(`SELECT `+runColumns+` FROM runs WHERE organization_id = ? AND id = ?`,
		organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errNotFound
	}
	return r, err
}

// runs reads the Organization's newest Runs, at most limit.
func (s *store) runs(organizationID string, limit int) ([]dispatcherapi.Run, error) {
	rows, err := s.db.Query(`SELECT `+runColumns+` FROM runs WHERE organization_id = ?
		ORDER BY created_at DESC, id DESC LIMIT ?`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []dispatcherapi.Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}
