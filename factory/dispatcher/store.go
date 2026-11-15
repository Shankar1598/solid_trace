package dispatcher

import (
	"database/sql"
	"encoding/json"
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
	// Execution. An Execution's workload is the Runner's name of the
	// workload that runs its wrapper (Runner.Dispatch). report_* is the last valid
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
	// seq is the count of the Run's run events (run.seq). repository,
	// folder and base_branch are the Factory settings when the Run
	// started; trigger_kind is what started it. outbox holds the run
	// events and warning Logs that SolidTrace does not have yet, oldest
	// first; each body is one Log as JSON.
	`ALTER TABLE runs ADD COLUMN seq INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE runs ADD COLUMN repository TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN folder TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN base_branch TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT '';
	CREATE TABLE outbox (id INTEGER PRIMARY KEY AUTOINCREMENT, body BLOB NOT NULL);`,
	// background_tasks_killed is the count of the agent's background
	// tasks that the end of the Execution killed.
	`ALTER TABLE executions ADD COLUMN background_tasks_killed INTEGER NOT NULL DEFAULT 0;`,
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
	// A connection's webhook_secret is sealed (see sealer); last_webhook_at
	// is the time of its last verified webhook. organization_slug is the
	// slug when an Admin last saved the connection; it is data only, for
	// the warnings. webhook_deliveries are the delivery ids of the last 7
	// days, for the dedupe.
	`ALTER TABLE connections ADD COLUMN webhook_secret BLOB;
	ALTER TABLE connections ADD COLUMN last_webhook_at INTEGER;
	ALTER TABLE connections ADD COLUMN organization_slug TEXT NOT NULL DEFAULT '';
	CREATE TABLE webhook_deliveries (
		connection_id TEXT NOT NULL REFERENCES connections (id),
		delivery TEXT NOT NULL,
		received_at INTEGER NOT NULL,
		PRIMARY KEY (connection_id, delivery)
	);
	CREATE INDEX webhook_deliveries_by_time ON webhook_deliveries (received_at);`,
	// schedules start Runs of a Project at the fire times of their cron
	// expressions. next_at is the next fire time (NULL while paused).
	// organization_slug is the slug when the schedule was made (data
	// only). A Run that a schedule started has started_by_kind schedule,
	// the schedule as started_by_*, its fire time as fired_at (each fire
	// time gives at most one Run), and the member who made the schedule
	// as scheduled_by_*. started_by_kind '' is a person.
	`CREATE TABLE schedules (
		id TEXT PRIMARY KEY,
		organization_id TEXT NOT NULL,
		organization_slug TEXT NOT NULL,
		project_id TEXT NOT NULL,
		name TEXT NOT NULL,
		cron TEXT NOT NULL,
		time_zone TEXT NOT NULL,
		prompt TEXT NOT NULL,
		base_branch TEXT NOT NULL,
		paused INTEGER NOT NULL DEFAULT 0,
		next_at INTEGER,
		created_at INTEGER NOT NULL,
		created_by_id TEXT NOT NULL,
		created_by_name TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		updated_by_id TEXT NOT NULL,
		updated_by_name TEXT NOT NULL
	);
	CREATE INDEX schedules_by_organization ON schedules (organization_id, project_id, name);
	CREATE INDEX schedules_by_next ON schedules (next_at) WHERE next_at IS NOT NULL;
	ALTER TABLE runs ADD COLUMN started_by_kind TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN schedule_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN fired_at INTEGER;
	ALTER TABLE runs ADD COLUMN scheduled_by_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN scheduled_by_name TEXT NOT NULL DEFAULT '';
	CREATE UNIQUE INDEX runs_by_fire ON runs (schedule_id, fired_at) WHERE schedule_id != '';`,
	// commands are what the Dispatcher tells the wrapper of an Execution,
	// which takes them with its long poll. total_limit is the Execution's
	// total limit in seconds, set at its task call; the backstop reads it.
	`CREATE TABLE commands (
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		execution_id TEXT NOT NULL REFERENCES executions (id),
		kind TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX commands_by_execution ON commands (execution_id, seq);
	ALTER TABLE executions ADD COLUMN total_limit INTEGER NOT NULL DEFAULT 0;`,
	// senders start Runs with the generic webhook. A sender's secret is
	// sealed (see sealer); projects is a JSON array of {id, slug}, the
	// Projects that it can start Runs in ('' is each Project).
	// sender_deliveries are the delivery ids and signatures of the last 7
	// days, with the Run that each one gave. A Run that a sender started
	// has started_by_kind sender and the sender as started_by_*; title,
	// link, attributes (a JSON object or '') and dedupe_key are what its
	// trigger gave.
	`CREATE TABLE senders (
		id TEXT PRIMARY KEY,
		organization_id TEXT NOT NULL,
		organization_slug TEXT NOT NULL,
		name TEXT NOT NULL,
		secret BLOB NOT NULL,
		projects TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		created_by_id TEXT NOT NULL,
		created_by_name TEXT NOT NULL,
		last_delivery_at INTEGER
	);
	CREATE INDEX senders_by_organization ON senders (organization_id, name);
	CREATE TABLE sender_deliveries (
		sender_id TEXT NOT NULL REFERENCES senders (id) ON DELETE CASCADE,
		delivery TEXT NOT NULL,
		signature TEXT NOT NULL,
		run_id TEXT NOT NULL REFERENCES runs (id),
		received_at INTEGER NOT NULL,
		PRIMARY KEY (sender_id, delivery)
	);
	CREATE INDEX sender_deliveries_by_signature ON sender_deliveries (sender_id, signature);
	CREATE INDEX sender_deliveries_by_time ON sender_deliveries (received_at);
	ALTER TABLE runs ADD COLUMN title TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN link TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN attributes TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN dedupe_key TEXT NOT NULL DEFAULT '';
	CREATE INDEX runs_by_starter ON runs (started_by_kind, started_by_id, created_at) WHERE started_by_kind != '';`,
	// A Run that a tracker ticket started has started_by_kind tracker (the
	// tracker user who set the ready status) and the page of the ticket as
	// its link. ticket_* is the tracker ticket that started the Run, and its
	// edit. A connection's ready_status and
	// routing_field are its tracker settings ('' is the kind's default).
	// routes send a tracker connection's tickets to a Project. A
	// ticket_notice is the last edit of a ticket that got the "no route"
	// comment or warning.
	`ALTER TABLE runs ADD COLUMN ticket_connection_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN ticket_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN ticket_edit TEXT NOT NULL DEFAULT '';
	CREATE INDEX runs_by_ticket ON runs (ticket_connection_id, ticket_id) WHERE ticket_id != '';
	ALTER TABLE connections ADD COLUMN ready_status TEXT NOT NULL DEFAULT '';
	ALTER TABLE connections ADD COLUMN routing_field TEXT NOT NULL DEFAULT '';
	CREATE TABLE routes (
		connection_id TEXT NOT NULL REFERENCES connections (id),
		match_value TEXT NOT NULL,
		project_id TEXT NOT NULL REFERENCES factory_settings (project_id),
		PRIMARY KEY (connection_id, match_value)
	);
	CREATE INDEX routes_by_project ON routes (project_id);
	CREATE TABLE ticket_notices (
		connection_id TEXT NOT NULL REFERENCES connections (id),
		ticket_id TEXT NOT NULL,
		edit TEXT NOT NULL,
		noticed_at INTEGER NOT NULL,
		PRIMARY KEY (connection_id, ticket_id)
	);`,
	// The Run's PR (ticket 35): its id on the forge (0 = none) and its
	// link.
	`ALTER TABLE runs ADD COLUMN pr_id INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE runs ADD COLUMN pr_url TEXT NOT NULL DEFAULT '';`,
	// factory_file is the factory file that the Dispatcher read from the
	// default branch at the Execution's dispatch, as JSON ('' is the
	// defaults). repository_hosts are the extra allowed hosts of the last
	// factory file read for each repository and Project folder (a JSON
	// list), for the Squid allowlist. Two Projects can name one repository
	// with different folders, and the file of each folder can differ.
	`ALTER TABLE executions ADD COLUMN factory_file TEXT NOT NULL DEFAULT '';
	CREATE TABLE repository_hosts (
		repository TEXT NOT NULL,
		folder TEXT NOT NULL,
		hosts TEXT NOT NULL,
		read_at INTEGER NOT NULL,
		PRIMARY KEY (repository, folder)
	);`,
	// The write-back (ticket 73). A connection's working_status,
	// review_status and waiting_status are its tracker settings ('' is the
	// kind's default). ticket_writes is the outbox of the write-back to
	// the tickets of tracker Runs: each row is one step, with the status
	// to set ('' is none: working, review or waiting, whose names the
	// sender reads from the connection's settings at the send) and
	// whether it posts the step's comment. text is the summary, the
	// question or the reason of the step. status_set and sent_at record
	// what the sender did; a row that the sender gave up has sent_at and
	// gave_up 1.
	`ALTER TABLE connections ADD COLUMN working_status TEXT NOT NULL DEFAULT '';
	ALTER TABLE connections ADD COLUMN review_status TEXT NOT NULL DEFAULT '';
	ALTER TABLE connections ADD COLUMN waiting_status TEXT NOT NULL DEFAULT '';
	CREATE TABLE ticket_writes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id TEXT NOT NULL REFERENCES runs (id),
		execution_id TEXT NOT NULL,
		connection_id TEXT NOT NULL,
		ticket_id TEXT NOT NULL,
		step TEXT NOT NULL,
		status TEXT NOT NULL,
		comment INTEGER NOT NULL,
		text TEXT NOT NULL,
		pr_url TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		status_set INTEGER NOT NULL DEFAULT 0,
		failures INTEGER NOT NULL DEFAULT 0,
		sent_at INTEGER,
		gave_up INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX ticket_writes_pending ON ticket_writes (id) WHERE sent_at IS NULL;
	CREATE INDEX ticket_writes_by_run ON ticket_writes (run_id, step);`,
}

// store is the Dispatcher's SQLite database.
type store struct {
	db     *sql.DB
	sealer *sealer
	// sendWake wakes the event sender after a commit that can hold run
	// events.
	sendWake chan struct{}
	// writeBackWake wakes the write-back loop after a commit that can hold
	// write-back steps.
	writeBackWake chan struct{}
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
	return &store{db: db, sealer: s, sendWake: make(chan struct{}, 1), writeBackWake: make(chan struct{}, 1)}, nil
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
	// repository, folder and baseBranch are of the Factory settings when
	// the Run started; trigger is the trigger kind.
	repository, folder, baseBranch, trigger string
	// dedupeKey is the dedupe_key of a sender's delivery ("" is none).
	dedupeKey string
	// scheduleID and firedAt are the schedule that started the Run and
	// its fire time ("" and zero for another trigger).
	scheduleID string
	firedAt    time.Time
	// ticketConnectionID, ticketID and ticketEdit are the tracker ticket
	// that started the Run, and its edit ("" for another trigger).
	ticketConnectionID, ticketID, ticketEdit string
}

// insertRun adds a queued Run, with its created and queued events at its
// CreatedAt.
func (s *store) insertRun(r storedRun) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := insertRun(tx, r); err != nil {
		return err
	}
	return s.commit(tx)
}

// insertRun adds a queued Run in tx, with its created and queued events.
// Its Title is the title that the trigger gave ("" is the first line of
// the prompt). It adds nothing and returns false when the Run's schedule
// has a Run for the same fire time.
func insertRun(tx *sql.Tx, r storedRun) (bool, error) {
	var firedAt *int64
	if r.scheduleID != "" {
		at := r.firedAt.UnixMicro()
		firedAt = &at
	}
	var scheduledBy dispatcherapi.Person
	if r.ScheduledBy != nil {
		scheduledBy = *r.ScheduledBy
	}
	var attributes []byte
	if len(r.Attributes) > 0 {
		var err error
		if attributes, err = json.Marshal(r.Attributes); err != nil {
			return false, err
		}
	}
	result, err := tx.Exec(`INSERT INTO runs (id, organization_id, organization_slug, project, project_id, prompt, state,
		started_by_kind, started_by_id, started_by_name, created_at, repository, folder, base_branch, trigger_kind,
		schedule_id, fired_at, scheduled_by_id, scheduled_by_name, title, link, attributes, dedupe_key,
		ticket_connection_id, ticket_id, ticket_edit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (schedule_id, fired_at) WHERE schedule_id != '' DO NOTHING`,
		r.ID, r.organizationID, r.Organization, r.Project, r.projectID, r.Prompt, r.State, r.StartedBy.Kind,
		r.StartedBy.ID, r.StartedBy.Name, r.CreatedAt.UnixMicro(), r.repository, r.folder, r.baseBranch, r.trigger,
		r.scheduleID, firedAt, scheduledBy.ID, scheduledBy.Name, r.Title, r.Link, string(attributes), r.dedupeKey,
		r.ticketConnectionID, r.ticketID, r.ticketEdit)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	for _, kind := range []string{eventCreated, eventQueued} {
		if err := recordEvent(tx, r.CreatedAt, r.ID, kind, nil); err != nil {
			return false, err
		}
	}
	return true, nil
}

const runColumns = `id, organization_slug, project, prompt, state, started_by_kind, started_by_id, started_by_name,
	created_at, reason, question, summary, scheduled_by_id, scheduled_by_name, title, link, attributes, pr_url`

func scanRun(row interface{ Scan(...any) error }) (dispatcherapi.Run, error) {
	var r dispatcherapi.Run
	var created int64
	var scheduledBy dispatcherapi.Person
	var attributes string
	err := row.Scan(&r.ID, &r.Organization, &r.Project, &r.Prompt, &r.State, &r.StartedBy.Kind, &r.StartedBy.ID,
		&r.StartedBy.Name, &created, &r.Reason, &r.Question, &r.Summary, &scheduledBy.ID, &scheduledBy.Name,
		&r.Title, &r.Link, &attributes, &r.PRURL)
	if err != nil {
		return r, err
	}
	r.CreatedAt = time.UnixMicro(created).UTC()
	if scheduledBy.ID != "" {
		r.ScheduledBy = &scheduledBy
	}
	if r.Title == "" {
		r.Title = title(r.Prompt)
	}
	if attributes != "" {
		err = json.Unmarshal([]byte(attributes), &r.Attributes)
	}
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
