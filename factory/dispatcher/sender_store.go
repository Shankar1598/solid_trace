package dispatcher

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

func senderPurpose(id string) string { return "sender " + id }

// senderProject is a Project in a sender's list: its SolidTrace id, which
// the check uses, and its slug when the Admin gave it.
type senderProject struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// storedSender is a sender with its Organization and its list of
// Projects (nil is each Project).
type storedSender struct {
	dispatcherapi.Sender
	organization organization
	projects     []senderProject
}

// starter is the sender as the starter of its Runs.
func (s storedSender) starter() starter {
	st := starter{organization: s.organization, kind: actorSender, id: s.ID, name: s.Name}
	if s.projects != nil {
		st.projects = []string{}
		for _, p := range s.projects {
			st.projects = append(st.projects, p.ID)
		}
	}
	return st
}

const senderColumns = `id, organization_id, organization_slug, name, projects, created_at, created_by_id,
	created_by_name, last_delivery_at`

func scanSender(row interface{ Scan(...any) error }) (storedSender, error) {
	var s storedSender
	var projects string
	var created int64
	var last sql.NullInt64
	err := row.Scan(&s.ID, &s.organization.id, &s.organization.slug, &s.Name, &projects, &created, &s.CreatedBy.ID,
		&s.CreatedBy.Name, &last)
	if err != nil {
		return s, err
	}
	s.CreatedAt = time.UnixMicro(created).UTC()
	if last.Valid {
		at := time.UnixMicro(last.Int64).UTC()
		s.LastDeliveryAt = &at
	}
	s.Projects = []string{}
	if projects != "" {
		if err := json.Unmarshal([]byte(projects), &s.projects); err != nil {
			return s, err
		}
		for _, p := range s.projects {
			s.Projects = append(s.Projects, p.Slug)
		}
	}
	return s, nil
}

// insertSender keeps a new sender with its secret sealed.
func (s *store) insertSender(sender storedSender, secret string) error {
	projects := ""
	if sender.projects != nil {
		b, err := json.Marshal(sender.projects)
		if err != nil {
			return err
		}
		projects = string(b)
	}
	_, err := s.db.Exec(`INSERT INTO senders (id, organization_id, organization_slug, name, secret, projects,
		created_at, created_by_id, created_by_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sender.ID, sender.organization.id, sender.organization.slug, sender.Name,
		s.sealer.seal(senderPurpose(sender.ID), []byte(secret)), projects, sender.CreatedAt.UnixMicro(),
		sender.CreatedBy.ID, sender.CreatedBy.Name)
	return err
}

// senders reads the Organization's senders, by name.
func (s *store) senders(organizationID string) ([]storedSender, error) {
	rows, err := s.db.Query(`SELECT `+senderColumns+` FROM senders WHERE organization_id = ? ORDER BY name, id`,
		organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []storedSender
	for rows.Next() {
		sender, err := scanSender(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sender)
	}
	return list, rows.Err()
}

// sender reads the sender with id, and opens its secret.
func (s *store) sender(id string) (storedSender, []byte, error) {
	var sealed []byte
	sender, err := scanSender(scanWith{s.db.QueryRow(`SELECT `+senderColumns+`, secret FROM senders WHERE id = ?`, id),
		&sealed})
	if errors.Is(err, sql.ErrNoRows) {
		return sender, nil, errNotFound
	}
	if err != nil {
		return sender, nil, err
	}
	secret, err := s.sealer.open(senderPurpose(id), sealed)
	return sender, secret, err
}

// deleteSender deletes the Organization's sender with id, and its
// delivery ids. Its Runs stay.
func (s *store) deleteSender(organizationID, id string) error {
	result, err := s.db.Exec(`DELETE FROM senders WHERE organization_id = ? AND id = ?`, organizationID, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		err = errNotFound
	}
	return err
}

// senderDelivered keeps the time of the sender's last delivery with a
// valid signature.
func (s *store) senderDelivered(id string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE senders SET last_delivery_at = ? WHERE id = ?`, at.UnixMicro(), id)
	return err
}

// deliveryRun gives the Run of the sender's delivery with the delivery id
// or the signature in the last dedupeFor ("" is none).
func deliveryRun(q interface {
	QueryRow(string, ...any) *sql.Row
}, senderID, delivery, signature string, at time.Time) (string, error) {
	var runID string
	err := q.QueryRow(`SELECT run_id FROM sender_deliveries WHERE sender_id = ? AND (delivery = ? OR signature = ?)
		AND received_at >= ? LIMIT 1`, senderID, delivery, signature, at.Add(-dedupeFor).UnixMicro()).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return runID, err
}

// liveStates are the states of a Run with an Execution queued or live.
var liveStates = []string{dispatcherapi.StateQueued, dispatcherapi.StateRunning}

// inList is "(?, ?, ...)" for n values.
func inList(n int) string { return "(" + strings.TrimSuffix(strings.Repeat("?, ", n), ", ") + ")" }

func anys(values []string) []any {
	list := make([]any, len(values))
	for i, v := range values {
		list[i] = v
	}
	return list
}

// senderStart is the result of a delivery that can start a Run.
type senderStart struct {
	// runID is the Run that the delivery gives; started is true when
	// the delivery started it.
	runID   string
	started bool
	// limit says which limit refused the delivery ("" is none), and wait
	// is the time until a delivery can start a Run again.
	limit string
	wait  time.Duration
}

// startForSender starts the sender's Run r for its delivery, in one
// transaction: a delivery id or signature that the sender sent in the
// last dedupeFor gives that delivery's Run; an Active Run of the sender
// with r's dedupe key gives that Run; else, the sender's limits refuse
// it, or r starts. The delivery is kept with the Run that it gave. A
// refused delivery is not kept, so that the sender can send it again.
func (s *store) startForSender(r storedRun, delivery, signature string, at time.Time, starts, active int) (senderStart, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return senderStart{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM sender_deliveries WHERE received_at < ?`, at.Add(-dedupeFor).UnixMicro()); err != nil {
		return senderStart{}, err
	}
	sender := r.StartedBy.ID
	runID, err := deliveryRun(tx, sender, delivery, signature, at)
	if err != nil || runID != "" {
		return senderStart{runID: runID}, err
	}
	var result senderStart
	if r.dedupeKey != "" {
		err := tx.QueryRow(`SELECT id FROM runs WHERE started_by_kind = ? AND started_by_id = ? AND dedupe_key = ?
			AND state IN `+inList(len(activeStates))+` ORDER BY created_at DESC LIMIT 1`,
			append([]any{actorSender, sender, r.dedupeKey}, anys(activeStates)...)...).Scan(&result.runID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return senderStart{}, err
		}
	}
	if result.runID == "" {
		if result, err = senderLimits(tx, sender, at, starts, active); err != nil || result.limit != "" {
			return result, err
		}
		if _, err := insertRun(tx, r); err != nil {
			return senderStart{}, err
		}
		result = senderStart{runID: r.ID, started: true}
	}
	if _, err := tx.Exec(`INSERT INTO sender_deliveries (sender_id, delivery, signature, run_id, received_at)
		VALUES (?, ?, ?, ?, ?)`, sender, delivery, signature, result.runID, at.UnixMicro()); err != nil {
		return senderStart{}, err
	}
	return result, s.commit(tx)
}

// senderLimits refuses a start of the sender at at when it started starts
// Runs in the last minute, or has active Runs with an Execution queued or
// live.
func senderLimits(tx *sql.Tx, sender string, at time.Time, starts, active int) (senderStart, error) {
	rows, err := tx.Query(`SELECT created_at FROM runs WHERE started_by_kind = ? AND started_by_id = ?
		AND created_at > ? ORDER BY created_at DESC LIMIT ?`, actorSender, sender, at.Add(-time.Minute).UnixMicro(), starts)
	if err != nil {
		return senderStart{}, err
	}
	var recent []int64
	for rows.Next() {
		var created int64
		if err := rows.Scan(&created); err != nil {
			rows.Close()
			return senderStart{}, err
		}
		recent = append(recent, created)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return senderStart{}, err
	}
	if len(recent) >= starts {
		// The oldest of the last starts Runs leaves the minute first.
		wait := time.UnixMicro(recent[len(recent)-1]).Add(time.Minute).Sub(at)
		return senderStart{limit: "this sender started " + strconv.Itoa(starts) + " Runs in the last minute",
			wait: wait}, nil
	}
	var live int
	err = tx.QueryRow(`SELECT COUNT(*) FROM runs WHERE started_by_kind = ? AND started_by_id = ? AND state IN `+
		inList(len(liveStates)), append([]any{actorSender, sender}, anys(liveStates)...)...).Scan(&live)
	if err != nil {
		return senderStart{}, err
	}
	if live >= active {
		return senderStart{limit: "this sender has " + strconv.Itoa(active) + " Runs with an Execution queued or live",
			wait: time.Minute}, nil
	}
	return senderStart{}, nil
}
