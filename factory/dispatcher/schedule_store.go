package dispatcher

import (
	"cmp"
	"database/sql"
	"errors"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// storedSchedule is a schedule with the ids that scope it.
type storedSchedule struct {
	dispatcherapi.Schedule
	organizationID, organizationSlug, projectID string
}

// The Project's slug is the one that the Factory settings last saw. The
// last Run is the one with the latest fire time.
const scheduleColumns = `sc.id, sc.organization_id, sc.organization_slug, sc.project_id,
	COALESCE(fs.project_slug, ''), sc.name, sc.cron, sc.time_zone, sc.prompt, sc.base_branch, sc.paused, sc.next_at,
	sc.created_at, sc.created_by_id, sc.created_by_name, sc.updated_at, sc.updated_by_id, sc.updated_by_name,
	lr.id, lr.state, lr.fired_at`

const scheduleFrom = ` FROM schedules sc LEFT JOIN factory_settings fs ON fs.project_id = sc.project_id
	LEFT JOIN runs lr ON lr.id = (SELECT id FROM runs WHERE schedule_id = sc.id ORDER BY fired_at DESC LIMIT 1) `

func scanSchedule(row interface{ Scan(...any) error }) (storedSchedule, error) {
	var s storedSchedule
	var next, lastFired sql.NullInt64
	var created, updated int64
	var lastID, lastState sql.NullString
	err := row.Scan(&s.ID, &s.organizationID, &s.organizationSlug, &s.projectID, &s.Project, &s.Name, &s.Cron,
		&s.TimeZone, &s.Prompt, &s.BaseBranch, &s.Paused, &next, &created, &s.CreatedBy.ID, &s.CreatedBy.Name,
		&updated, &s.UpdatedBy.ID, &s.UpdatedBy.Name, &lastID, &lastState, &lastFired)
	if next.Valid {
		at := time.UnixMicro(next.Int64).UTC()
		s.NextAt = &at
	}
	if lastID.Valid {
		s.LastRun = &dispatcherapi.ScheduleRun{ID: lastID.String, State: lastState.String,
			FiredAt: time.UnixMicro(lastFired.Int64).UTC()}
	}
	s.CreatedAt = time.UnixMicro(created).UTC()
	s.UpdatedAt = time.UnixMicro(updated).UTC()
	return s, err
}

// schedule reads one schedule of the Organization.
func (s *store) schedule(organizationID, id string) (storedSchedule, error) {
	schedule, err := scanSchedule(s.db.QueryRow(`SELECT `+scheduleColumns+scheduleFrom+
		`WHERE sc.organization_id = ? AND sc.id = ?`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return schedule, errNotFound
	}
	return schedule, err
}

// schedules reads the schedules of the Organization, or of its Project
// with the id projectID ("" is each Project), by Project and name.
func (s *store) schedules(organizationID, projectID string) ([]dispatcherapi.Schedule, error) {
	rows, err := s.db.Query(`SELECT `+scheduleColumns+scheduleFrom+`WHERE sc.organization_id = ?
		AND (? = '' OR sc.project_id = ?) ORDER BY 5, sc.name, sc.id`, organizationID, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []dispatcherapi.Schedule{}
	for rows.Next() {
		schedule, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, schedule.Schedule)
	}
	return list, rows.Err()
}

// insertSchedule adds a schedule, with its schedule_created Log.
func (s *store) insertSchedule(sc storedSchedule, by person) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO schedules (id, organization_id, organization_slug, project_id, name, cron, time_zone,
		prompt, base_branch, paused, next_at, created_at, created_by_id, created_by_name, updated_at, updated_by_id,
		updated_by_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.ID, sc.organizationID, sc.organizationSlug, sc.projectID, sc.Name, sc.Cron, sc.TimeZone, sc.Prompt,
		sc.BaseBranch, sc.Paused, micros(sc.NextAt), sc.CreatedAt.UnixMicro(), sc.CreatedBy.ID, sc.CreatedBy.Name,
		sc.UpdatedAt.UnixMicro(), sc.UpdatedBy.ID, sc.UpdatedBy.Name)
	if err != nil {
		return err
	}
	if err := addScheduleLog(tx, sc, eventScheduleCreated, by); err != nil {
		return err
	}
	return s.commit(tx)
}

// errNoChange is the answer of a schedule change that changes nothing.
var errNoChange = errors.New("no change")

// changeSchedule reads the schedule id of the Organization, gives it to
// change, and writes the values that a person can change, with the Log of
// kind, at at. All of it is one transaction, so that a change works on the
// values of now: two changes at the same time do not undo each other, and
// a change does not undo a fire of the schedule loop. change returns
// errNoChange to write nothing; changeSchedule then returns nil.
func (s *store) changeSchedule(organizationID, id, kind string, at time.Time, by person,
	change func(sc *storedSchedule) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sc, err := scanSchedule(tx.QueryRow(`SELECT `+scheduleColumns+scheduleFrom+
		`WHERE sc.organization_id = ? AND sc.id = ?`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	if err := change(&sc); errors.Is(err, errNoChange) {
		return nil
	} else if err != nil {
		return err
	}
	sc.UpdatedAt, sc.UpdatedBy = at, dispatcherapi.Person{ID: by.id, Name: by.name}
	_, err = tx.Exec(`UPDATE schedules SET name = ?, cron = ?, time_zone = ?, prompt = ?, base_branch = ?,
		paused = ?, next_at = ?, updated_at = ?, updated_by_id = ?, updated_by_name = ?
		WHERE organization_id = ? AND id = ?`,
		sc.Name, sc.Cron, sc.TimeZone, sc.Prompt, sc.BaseBranch, sc.Paused, micros(sc.NextAt),
		sc.UpdatedAt.UnixMicro(), sc.UpdatedBy.ID, sc.UpdatedBy.Name, sc.organizationID, sc.ID)
	if err != nil {
		return err
	}
	if err := addScheduleLog(tx, sc, kind, by); err != nil {
		return err
	}
	return s.commit(tx)
}

// deleteSchedule deletes a schedule, with its schedule_deleted Log. The
// Runs that it started stay.
func (s *store) deleteSchedule(sc storedSchedule, by person, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM schedules WHERE organization_id = ? AND id = ?`, sc.organizationID, sc.ID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return cmp.Or(err, errNotFound)
	}
	sc.UpdatedAt = at
	if err := addScheduleLog(tx, sc, eventScheduleDeleted, by); err != nil {
		return err
	}
	return s.commit(tx)
}

// dueSchedules are the ids of the schedules whose next fire time is at
// now or earlier, oldest first.
func (s *store) dueSchedules(now time.Time) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM schedules WHERE next_at IS NOT NULL AND next_at <= ? AND paused = 0
		ORDER BY next_at, id`, now.UnixMicro())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Kinds of the Logs of a change to a schedule (event.kind).
const (
	eventScheduleCreated   = "schedule_created"
	eventScheduleUpdated   = "schedule_updated"
	eventSchedulePaused    = "schedule_paused"
	eventScheduleContinued = "schedule_continued"
	eventScheduleDeleted   = "schedule_deleted"
)

// addScheduleLog adds the Log of a person's change of kind to the
// schedule sc, at sc.UpdatedAt, in tx. It is one of the Dispatcher's own
// Logs (no run.id).
func addScheduleLog(tx *sql.Tx, sc storedSchedule, kind string, by person) error {
	attrs := map[string]any{
		"event.kind":         kind,
		"organization":       by.organization.slug,
		"factory.project":    sc.Project,
		"schedule.id":        sc.ID,
		"schedule.name":      sc.Name,
		"schedule.cron":      sc.Cron,
		"schedule.time_zone": sc.TimeZone,
		"schedule.paused":    sc.Paused,
		"actor.kind":         actorPerson,
		"actor.id":           by.id,
		"actor.name":         by.name,
	}
	message := "schedule " + sc.ID + ": " + kind[len("schedule_"):]
	return addOwnLog(tx, sc.UpdatedAt, "info", message, attrs)
}

// micros is the time as microseconds, or nil for none.
func micros(at *time.Time) *int64 {
	if at == nil {
		return nil
	}
	m := at.UnixMicro()
	return &m
}
