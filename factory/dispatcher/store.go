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
	// meta holds the key check. A connection's token is sealed (see
	// sealer). factory_settings are keyed by the SolidTrace Project id;
	// project_slug is the slug that SolidTrace last gave for it.
	// orphaned_at is set when a trigger found no such Project.
	`CREATE TABLE meta (name TEXT PRIMARY KEY, value BLOB NOT NULL);
	CREATE TABLE connections (
		id TEXT PRIMARY KEY,
		organization_id TEXT NOT NULL,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		token BLOB NOT NULL,
		updated_at INTEGER NOT NULL,
		updated_by_id TEXT NOT NULL,
		updated_by_name TEXT NOT NULL,
		UNIQUE (organization_id, name)
	);
	CREATE TABLE factory_settings (
		project_id TEXT PRIMARY KEY,
		organization_id TEXT NOT NULL,
		project_slug TEXT NOT NULL,
		repository TEXT NOT NULL,
		folder TEXT NOT NULL,
		base_branch TEXT NOT NULL,
		connection_id TEXT REFERENCES connections (id),
		orphaned_at INTEGER,
		updated_at INTEGER NOT NULL,
		updated_by_id TEXT NOT NULL,
		updated_by_name TEXT NOT NULL
	);
	CREATE INDEX factory_settings_by_organization ON factory_settings (organization_id);
	ALTER TABLE runs ADD COLUMN project_id TEXT NOT NULL DEFAULT '';`,
	// session_id is the Run's Agent session id, set at its first
	// Execution. An Execution's workload is the subject of its wrapper's
	// workload identity JWT, from the Runner. report_* is the last valid
	// report of the Execution.
	`ALTER TABLE runs ADD COLUMN reason TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN question TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN summary TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
	CREATE INDEX runs_by_state ON runs (state, created_at);
	CREATE TABLE executions (
		id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL REFERENCES runs (id),
		workload TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL,
		dispatched_at INTEGER NOT NULL,
		started_at INTEGER,
		ended_at INTEGER,
		task_delivered_at INTEGER,
		end_reason TEXT NOT NULL DEFAULT '',
		exit_code INTEGER,
		report_outcome TEXT NOT NULL DEFAULT '',
		report_summary TEXT NOT NULL DEFAULT '',
		report_question TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX executions_by_run ON executions (run_id, dispatched_at);`,
	// run_sessions are the Agent session ids of each Run: the first one,
	// and each one that a wrapper saw later (a /clear). An id belongs to
	// one Run. execution_tokens are the child tokens that the Dispatcher
	// made for each Execution; it revokes them at the Execution's end.
	`CREATE TABLE run_sessions (
		session_id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL REFERENCES runs (id),
		added_at INTEGER NOT NULL
	);
	CREATE INDEX run_sessions_by_run ON run_sessions (run_id, added_at);
	INSERT INTO run_sessions (session_id, run_id, added_at)
		SELECT session_id, id, created_at FROM runs WHERE session_id != '';
	CREATE TABLE execution_tokens (
		id TEXT PRIMARY KEY,
		execution_id TEXT NOT NULL REFERENCES executions (id),
		expires_at INTEGER NOT NULL,
		revoked_at INTEGER
	);
	CREATE INDEX execution_tokens_by_execution ON execution_tokens (execution_id);`,
}

// store is the Dispatcher's SQLite database.
type store struct {
	db     *sql.DB
	sealer *sealer
}

var errNotFound = errors.New("not found")

func openStore(path string, key []byte) (*store, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	s, err := newSealer(key)
	if err == nil {
		err = migrate(db)
		if err != nil {
			err = fmt.Errorf("open the database %s: %w", path, err)
		}
	}
	if err == nil {
		err = checkKey(db, s)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &store{db: db, sealer: s}, nil
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
	// projectID is the SolidTrace Project id of the Run's Factory
	// settings. The Run's Project is its slug when the Run started.
	projectID string
}

func (s *store) insertRun(r storedRun) error {
	_, err := s.db.Exec(`INSERT INTO runs (id, organization_id, organization_slug, project, project_id, prompt, state,
		started_by_id, started_by_name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.organizationID, r.Organization, r.Project, r.projectID, r.Prompt, r.State, r.StartedBy.ID, r.StartedBy.Name,
		r.CreatedAt.UnixMicro())
	return err
}

const runColumns = `id, organization_slug, project, prompt, state, started_by_id, started_by_name, created_at,
	reason, question, summary`

func scanRun(row interface{ Scan(...any) error }) (dispatcherapi.Run, error) {
	var r dispatcherapi.Run
	var created int64
	err := row.Scan(&r.ID, &r.Organization, &r.Project, &r.Prompt, &r.State, &r.StartedBy.ID, &r.StartedBy.Name, &created,
		&r.Reason, &r.Question, &r.Summary)
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
