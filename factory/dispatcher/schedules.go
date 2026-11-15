package dispatcher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata" // the IANA time zones, on a machine with none
	"unicode"
	"unicode/utf8"

	"github.com/hashicorp/cronexpr"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Schedules: each one starts a Run of its Project at each fire time of
// its cron expression in its time zone. The schedule loop takes the due
// schedules every scheduleEvery; for each, one transaction sets the next
// fire time and queues the Run, keyed by (schedule id, fire time), so a
// fire time gives at most one Run. A fire time missed while the
// Dispatcher was down gives one Run at start, if it is less than
// missedLimit old. A fire time while the schedule's last Run is active
// gives no Run, but a warning.

// scheduleEvery is the wait between two passes of the schedule loop.
const scheduleEvery = 30 * time.Second

// missedLimit is the age of a missed fire time that gives no Run.
const missedLimit = 24 * time.Hour

// activeStates are the states of an Active Run that the Dispatcher has
// now (a later state, waiting on an account limit, joins it). A Run that
// is not closed and has an open PR is active in each state: see
// activeRunSQL.
var activeStates = []string{dispatcherapi.StateQueued, dispatcherapi.StateRunning, dispatcherapi.StateWaitingOnPerson}

// The limits of a schedule's values.
const (
	maxScheduleName   = 100
	maxSchedulePrompt = 64 << 10
)

// scheduleLoop takes the due schedules at once, then every scheduleEvery.
func (d *Dispatcher) scheduleLoop(ctx context.Context) {
	for {
		now := d.now()
		d.fireDue(ctx, now)
		if d.scheduleChecked != nil {
			d.scheduleChecked(now)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.scheduleEvery):
		}
	}
}

func (d *Dispatcher) fireDue(ctx context.Context, now time.Time) {
	ids, err := d.store.dueSchedules(now)
	if err != nil {
		d.log.Error("could not read the due schedules", "error", err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		queued, err := d.fire(id, now)
		if err != nil {
			d.log.Error("could not fire a schedule", "schedule", id, "error", err)
		}
		if queued {
			d.wakeDispatch()
		}
	}
}

// fire takes the due fire times of the schedule id at now, in one
// transaction: it sets the next fire time, and queues a Run for the
// latest fire time that is less than missedLimit old, unless the
// schedule's last Run is active. It returns true when it queued a Run.
func (d *Dispatcher) fire(id string, now time.Time) (bool, error) {
	tx, err := d.store.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	sc, err := scanSchedule(tx.QueryRow(`SELECT `+scheduleColumns+scheduleFrom+
		`WHERE sc.id = ? AND sc.paused = 0 AND sc.next_at IS NOT NULL AND sc.next_at <= ?`, id, now.UnixMicro()))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // paused, deleted or fired since the list
	}
	if err != nil {
		return false, err
	}
	expr, loc, err := parseSchedule(sc.Cron, sc.TimeZone)
	if err != nil {
		return false, err
	}
	fired, next := fireTimes(expr, loc, *sc.NextAt, now)
	if _, err := tx.Exec(`UPDATE schedules SET next_at = ? WHERE id = ?`, micros(next), id); err != nil {
		return false, err
	}
	if fired.IsZero() {
		d.log.Info("a schedule's fire times were missed by more than 24 hours: no Run", "schedule", id,
			"name", sc.Name, "next", next)
		return false, d.store.commit(tx)
	}
	attrs := map[string]any{"organization": sc.organizationSlug, "factory.project": sc.Project,
		"schedule.id": sc.ID, "schedule.name": sc.Name, "schedule.fire_time": fired.UTC().Format(time.RFC3339),
		"actor.kind": actorSchedule, "actor.id": sc.ID, "actor.name": sc.Name}
	skip := func(message string) (bool, error) {
		if err := addOwnLog(tx, now, "warn", message, attrs); err != nil {
			return false, err
		}
		if err := d.store.commit(tx); err != nil {
			return false, err
		}
		d.log.Warn(message, "schedule", sc.ID, "name", sc.Name, "factory.project", sc.Project, "fire_time", fired)
		return false, nil
	}
	active, err := activeRunOf(tx, sc.ID)
	switch {
	case err == nil:
		attrs["schedule.active_run"] = active
		return skip("skipped: last Run is active")
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	settings, err := scanSettings(tx.QueryRow(`SELECT `+settingsColumns+settingsFrom+
		`WHERE s.organization_id = ? AND s.project_id = ?`, sc.organizationID, sc.projectID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return skip("skipped: the Project has no Factory settings")
	case err != nil:
		return false, err
	case settings.Orphaned:
		return skip("skipped: SolidTrace no longer has the Project, and its Factory settings are orphaned")
	}
	baseBranch := settings.BaseBranch
	if sc.BaseBranch != "" {
		baseBranch = sc.BaseBranch
	}
	creator := sc.CreatedBy
	run := storedRun{
		Run: dispatcherapi.Run{
			ID:           newRunID(),
			State:        dispatcherapi.StateQueued,
			Project:      settings.Project,
			Prompt:       sc.Prompt,
			Organization: sc.organizationSlug,
			StartedBy:    dispatcherapi.Person{Kind: actorSchedule, ID: sc.ID, Name: sc.Name},
			ScheduledBy:  &creator,
			CreatedAt:    now.UTC().Truncate(time.Microsecond),
		},
		organizationID: sc.organizationID,
		projectID:      sc.projectID,
		repository:     settings.Repository,
		folder:         settings.Folder,
		baseBranch:     baseBranch,
		trigger:        triggerSchedule,
		scheduleID:     sc.ID,
		firedAt:        fired,
	}
	queued, err := insertRun(tx, run)
	if err != nil {
		return false, err
	}
	if err := d.store.commit(tx); err != nil {
		return false, err
	}
	if queued {
		d.log.Info("a schedule queued a Run", "schedule", sc.ID, "name", sc.Name, "run", run.ID, "fire_time", fired)
	}
	return queued, nil
}

// activeRunOf is the id of an Active Run that the schedule id started,
// or sql.ErrNoRows when it has none (see activeRunSQL).
func activeRunOf(tx *sql.Tx, scheduleID string) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM runs WHERE schedule_id = ? AND `+activeRunSQL()+` LIMIT 1`,
		append([]any{scheduleID}, activeRunArgs()...)...).Scan(&id)
	return id, err
}

// fireTimes gives the latest fire time at now or earlier, from nextAt on,
// that is less than missedLimit old (zero for none), and the first fire
// time after now (nil for none).
func fireTimes(expr *cronexpr.Expression, loc *time.Location, nextAt, now time.Time) (time.Time, *time.Time) {
	t := nextAt.In(loc)
	if cutoff := now.Add(-missedLimit).In(loc); !t.After(cutoff) {
		t = nextAfter(expr, loc, cutoff)
	}
	var fired time.Time
	for !t.IsZero() && !t.After(now) {
		fired = t
		t = nextAfter(expr, loc, t)
	}
	if t.IsZero() {
		return fired.UTC(), nil
	}
	next := t.UTC()
	return fired.UTC(), &next
}

// nextFire is the first fire time after now (nil for none).
func nextFire(expr *cronexpr.Expression, loc *time.Location, now time.Time) *time.Time {
	t := nextAfter(expr, loc, now)
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

// nextAfter is the first fire time after t in loc (zero for none). A clock
// time that comes two times, when the clocks go back, fires one time
// only: at the first of the two, as cron does. cronexpr gives both.
func nextAfter(expr *cronexpr.Expression, loc *time.Location, t time.Time) time.Time {
	n := expr.Next(t.In(loc))
	for !n.IsZero() && repeatedClockTime(n, loc) {
		n = expr.Next(n)
	}
	return n
}

// repeatedClockTime is true when the clock time of t in loc came before,
// in the hours before t, at an earlier instant: t is in the hour that
// comes again when the clocks go back.
func repeatedClockTime(t time.Time, loc *time.Location) bool {
	t = t.In(loc)
	_, offset := t.Zone()
	_, before := t.Add(-6 * time.Hour).Zone()
	if before <= offset {
		return false
	}
	earlier := t.Add(-time.Duration(before-offset) * time.Second)
	y1, m1, d1 := earlier.Date()
	y2, m2, d2 := t.Date()
	return y1 == y2 && m1 == m2 && d1 == d2 && earlier.Hour() == t.Hour() && earlier.Minute() == t.Minute()
}

// parseSchedule reads a five-field cron expression and an IANA time zone.
func parseSchedule(cron, zone string) (*cronexpr.Expression, *time.Location, error) {
	if len(strings.Fields(cron)) != 5 {
		return nil, nil, fmt.Errorf("the cron expression must have five fields (minute, hour, day of the month, "+
			"month, day of the week), such as \"0 9 * * 1\", not %s", quote(cron))
	}
	expr, err := cronexpr.Parse(cron)
	if err != nil {
		return nil, nil, fmt.Errorf("the cron expression %s is not valid: %v", quote(cron), err)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "Local" || zone == "" {
		return nil, nil, fmt.Errorf("the time zone must be an IANA time zone, such as Europe/Berlin, not %s",
			quote(zone))
	}
	return expr, loc, nil
}

// shortestGap is the shortest time between two fire times in the year
// after from, at most scanFires of them; it stops at a gap shorter than
// limit. It is 0 when there are fewer than two fire times.
func shortestGap(expr *cronexpr.Expression, loc *time.Location, from time.Time, limit time.Duration) time.Duration {
	const scanFires = 10000
	t := nextAfter(expr, loc, from)
	if t.IsZero() {
		return 0
	}
	until := t.AddDate(1, 0, 1)
	var shortest time.Duration
	for range scanFires {
		n := nextAfter(expr, loc, t)
		if n.IsZero() || n.After(until) {
			break
		}
		if gap := n.Sub(t); gap > 0 && (shortest == 0 || gap < shortest) {
			shortest = gap
			if shortest < limit {
				break
			}
		}
		t = n
	}
	return shortest
}

// cleanSchedule puts a schedule's values in their stored form, or says
// what is wrong. It gives the next fire time after now.
func (d *Dispatcher) cleanSchedule(s *dispatcherapi.Schedule, now time.Time) (*time.Time, string) {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || utf8.RuneCountInString(s.Name) > maxScheduleName ||
		strings.ContainsFunc(s.Name, unicode.IsControl) {
		return nil, fmt.Sprintf("a schedule needs a name of one line, at most %d characters", maxScheduleName)
	}
	if strings.TrimSpace(s.Prompt) == "" || len(s.Prompt) > maxSchedulePrompt {
		return nil, "a schedule needs a prompt, at most 64 KiB"
	}
	s.Cron = strings.Join(strings.Fields(s.Cron), " ")
	s.TimeZone = strings.TrimSpace(s.TimeZone)
	if s.TimeZone == "" {
		s.TimeZone = "UTC"
	}
	s.BaseBranch = strings.TrimSpace(s.BaseBranch)
	if s.BaseBranch != "" && !validBranch(s.BaseBranch) {
		return nil, "the base branch must be a branch name, such as main or release/2.0, not " + quote(s.BaseBranch)
	}
	expr, loc, err := parseSchedule(s.Cron, s.TimeZone)
	if err != nil {
		return nil, err.Error()
	}
	next := nextFire(expr, loc, now)
	if next == nil {
		return nil, "the cron expression " + quote(s.Cron) + " never fires"
	}
	minimum := d.config.scheduleMinInterval()
	if gap := shortestGap(expr, loc, now, minimum); gap > 0 && gap < minimum {
		return nil, fmt.Sprintf("the cron expression %s fires %s after a fire time; the shortest interval is %s "+
			"(the operator sets it with %s)", quote(s.Cron), gap, minimum, EnvScheduleMinInterval)
	}
	return next, ""
}

func (d *Dispatcher) listSchedules(w http.ResponseWriter, r *http.Request, p person) {
	var projectID string
	if slug := r.URL.Query().Get("project"); slug != "" {
		project, ok := d.projectBySlug(w, r, p, slug)
		if !ok {
			return
		}
		projectID = project.id
	}
	list, err := d.store.schedules(p.organization.id, projectID)
	if err != nil {
		d.serverError(w, "list the schedules", err)
		return
	}
	writeJSON(w, http.StatusOK, dispatcherapi.Schedules{Schedules: list})
}

func (d *Dispatcher) createSchedule(w http.ResponseWriter, r *http.Request, p person) {
	var save dispatcherapi.SaveSchedule
	if !decode(w, r, &save, "the body must be a JSON object with project, name, cron, time_zone, prompt and base_branch") {
		return
	}
	now := d.now().UTC().Truncate(time.Microsecond)
	me := dispatcherapi.Person{ID: p.id, Name: p.name}
	sc := storedSchedule{
		Schedule: dispatcherapi.Schedule{ID: newRunID(), Name: save.Name, Cron: save.Cron, TimeZone: save.TimeZone,
			Prompt: save.Prompt, BaseBranch: save.BaseBranch, CreatedAt: now, CreatedBy: me, UpdatedAt: now, UpdatedBy: me},
		organizationID: p.organization.id, organizationSlug: p.organization.slug,
	}
	next, problem := d.cleanSchedule(&sc.Schedule, now)
	if problem != "" {
		writeProblem(w, http.StatusBadRequest, problem)
		return
	}
	sc.NextAt = next
	project, ok := d.projectBySlug(w, r, p, strings.TrimSpace(save.Project))
	if !ok {
		return
	}
	settings, err := d.store.settings(p.organization.id, project.id)
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusUnprocessableEntity, noSettings(project.slug))
		return
	}
	if err != nil {
		d.serverError(w, "make the schedule", err)
		return
	}
	if settings.Orphaned {
		writeProblem(w, http.StatusUnprocessableEntity, "the Factory settings of "+project.slug+" are orphaned")
		return
	}
	sc.projectID, sc.Project = project.id, project.slug
	if err := d.store.insertSchedule(sc, p); err != nil {
		d.serverError(w, "make the schedule", err)
		return
	}
	d.answerSchedule(w, http.StatusCreated, p, sc.ID)
}

func (d *Dispatcher) getSchedule(w http.ResponseWriter, r *http.Request, p person) {
	if _, ok := d.findSchedule(w, r, p); ok {
		d.answerSchedule(w, http.StatusOK, p, r.PathValue("id"))
	}
}

// problem is an error of a change that the person must correct.
type problem string

func (p problem) Error() string { return string(p) }

func (d *Dispatcher) updateSchedule(w http.ResponseWriter, r *http.Request, p person) {
	var update dispatcherapi.UpdateSchedule
	if !decode(w, r, &update, "the body must be a JSON object with any of name, cron, time_zone, prompt and base_branch") {
		return
	}
	now := d.now().UTC().Truncate(time.Microsecond)
	d.changeSchedule(w, r, p, eventScheduleUpdated, now, func(sc *storedSchedule) error {
		before := sc.Schedule
		for value, to := range map[*string]*string{&sc.Name: update.Name, &sc.Cron: update.Cron,
			&sc.TimeZone: update.TimeZone, &sc.Prompt: update.Prompt, &sc.BaseBranch: update.BaseBranch} {
			if to != nil {
				*value = *to
			}
		}
		next, why := d.cleanSchedule(&sc.Schedule, now)
		if why != "" {
			return problem(why)
		}
		// A new cron expression or time zone starts its fire times from now.
		if !sc.Paused && (sc.Cron != before.Cron || sc.TimeZone != before.TimeZone) {
			sc.NextAt = next
		}
		return nil
	})
}

// pauseSchedule stops the schedule's fire times at once. A pause of a
// paused schedule changes nothing.
func (d *Dispatcher) pauseSchedule(w http.ResponseWriter, r *http.Request, p person) {
	now := d.now().UTC().Truncate(time.Microsecond)
	d.changeSchedule(w, r, p, eventSchedulePaused, now, func(sc *storedSchedule) error {
		if sc.Paused {
			return errNoChange
		}
		sc.Paused, sc.NextAt = true, nil
		return nil
	})
}

// continueSchedule starts a paused schedule's fire times again, from now:
// the fire times of the pause give no Run.
func (d *Dispatcher) continueSchedule(w http.ResponseWriter, r *http.Request, p person) {
	now := d.now().UTC().Truncate(time.Microsecond)
	d.changeSchedule(w, r, p, eventScheduleContinued, now, func(sc *storedSchedule) error {
		if !sc.Paused {
			return errNoChange
		}
		expr, loc, err := parseSchedule(sc.Cron, sc.TimeZone)
		if err != nil {
			return err
		}
		sc.Paused, sc.NextAt = false, nextFire(expr, loc, now)
		return nil
	})
}

func (d *Dispatcher) deleteSchedule(w http.ResponseWriter, r *http.Request, p person) {
	sc, ok := d.findSchedule(w, r, p)
	if !ok {
		return
	}
	err := d.store.deleteSchedule(sc, p, d.now().UTC().Truncate(time.Microsecond))
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusNotFound, "no schedule "+sc.ID+" in this Organization")
		return
	}
	if err != nil {
		d.serverError(w, "delete the schedule", err)
		return
	}
	writeJSON(w, http.StatusOK, sc.Schedule)
}

// changeSchedule applies change to the schedule of the request's {id}
// and answers the request with the schedule.
func (d *Dispatcher) changeSchedule(w http.ResponseWriter, r *http.Request, p person, kind string, now time.Time,
	change func(sc *storedSchedule) error) {
	id := r.PathValue("id")
	err := d.store.changeSchedule(p.organization.id, id, kind, now, p, change)
	var wrong problem
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "no schedule "+id+" in this Organization")
	case errors.As(err, &wrong):
		writeProblem(w, http.StatusBadRequest, string(wrong))
	case err != nil:
		d.serverError(w, "save the schedule", err)
	default:
		d.answerSchedule(w, http.StatusOK, p, id)
	}
}

// findSchedule reads the schedule of the request's {id}. It answers the
// request itself when there is none.
func (d *Dispatcher) findSchedule(w http.ResponseWriter, r *http.Request, p person) (storedSchedule, bool) {
	sc, err := d.store.schedule(p.organization.id, r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusNotFound, "no schedule "+r.PathValue("id")+" in this Organization")
		return sc, false
	}
	if err != nil {
		d.serverError(w, "read the schedule", err)
		return sc, false
	}
	return sc, true
}

func (d *Dispatcher) answerSchedule(w http.ResponseWriter, status int, p person, id string) {
	sc, err := d.store.schedule(p.organization.id, id)
	if err != nil {
		d.serverError(w, "read the schedule", err)
		return
	}
	writeJSON(w, status, sc.Schedule)
}
