package dispatcher_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// The clock of each Env starts at Saturday 2026-11-14 09:00 UTC.

var scheduleLine = regexp.MustCompile(`Schedule ([a-z0-9]+) is made\.`)

// makeSchedule makes a schedule on the Project checkout through the API.
func makeSchedule(t *testing.T, p *factorytest.Person, save dispatcherapi.SaveSchedule) dispatcherapi.Schedule {
	t.Helper()
	if save.Project == "" {
		save.Project = "checkout"
	}
	if save.Name == "" {
		save.Name = "Update dependencies"
	}
	if save.Prompt == "" {
		save.Prompt = "Update the dependencies and run the tests"
	}
	a := p.API("POST", "/api/v1/schedules", save)
	if a.Status != http.StatusCreated {
		t.Fatalf("make the schedule: %d %s", a.Status, a.Body)
	}
	var s dispatcherapi.Schedule
	a.JSON(t, &s)
	return s
}

// scheduleRuns are the Runs of the Organization that the schedule id
// started, newest first.
func scheduleRuns(t *testing.T, p *factorytest.Person, id string) []dispatcherapi.Run {
	t.Helper()
	var list dispatcherapi.Runs
	p.API("GET", "/api/v1/runs", nil).JSON(t, &list)
	var runs []dispatcherapi.Run
	for _, r := range list.Runs {
		if r.StartedBy.Kind == dispatcherapi.ActorSchedule && r.StartedBy.ID == id {
			runs = append(runs, r)
		}
	}
	return runs
}

func getSchedule(t *testing.T, p *factorytest.Person, id string) dispatcherapi.Schedule {
	t.Helper()
	var s dispatcherapi.Schedule
	a := p.API("GET", "/api/v1/schedules/"+id, nil)
	if a.Status != http.StatusOK {
		t.Fatalf("get the schedule: %d %s", a.Status, a.Body)
	}
	a.JSON(t, &s)
	return s
}

func TestAScheduleFiresAtItsTimeInItsTimeZoneAndQueuesOneRunWithTheScheduleAsActor(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	// 09:00 in New York is 14:00 UTC on 2026-11-14 (EST).
	s := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "0 9 * * *", TimeZone: "America/New_York",
		BaseBranch: "develop"})
	want := time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC)
	if s.NextAt == nil || !s.NextAt.Equal(want) || s.Paused || s.CreatedBy.Name != ana.Answer.Username {
		t.Fatalf("the new schedule is %+v, want the next time %s", s, want)
	}

	env.Clock.Advance(5*time.Hour - time.Second)
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, s.ID); len(runs) != 0 {
		t.Fatalf("one second before the fire time the schedule has Runs: %+v", runs)
	}

	env.Clock.Advance(time.Second)
	env.CheckSchedules()
	env.Clock.Advance(time.Minute)
	env.CheckSchedules()
	runs := scheduleRuns(t, ana, s.ID)
	if len(runs) != 1 {
		t.Fatalf("after the fire time the schedule has %d Runs, want 1: %+v", len(runs), runs)
	}
	run := runs[0]
	if run.State != dispatcherapi.StateQueued || run.Project != "checkout" || run.Prompt != s.Prompt ||
		run.StartedBy.Name != "Update dependencies" || run.ScheduledBy == nil ||
		run.ScheduledBy.Name != ana.Answer.Username || !run.CreatedAt.Equal(want) {
		t.Errorf("the schedule's Run is %+v", run)
	}
	got := getSchedule(t, ana, s.ID)
	if next := want.Add(24 * time.Hour); got.NextAt == nil || !got.NextAt.Equal(next) {
		t.Errorf("after the fire the next time is %v, want %s", got.NextAt, next)
	}
	if got.LastRun == nil || got.LastRun.ID != run.ID || !got.LastRun.FiredAt.Equal(want) {
		t.Errorf("the schedule's last Run is %+v, want %s", got.LastRun, run.ID)
	}

	events := env.WaitForRunEvents(run.ID, 2)
	for k, v := range map[string]any{
		"run.trigger":              "schedule",
		"run.base_branch":          "develop",
		"actor.kind":               "schedule",
		"actor.id":                 s.ID,
		"actor.name":               "Update dependencies",
		"run.started_by.kind":      "schedule",
		"run.started_by.id":        s.ID,
		"run.started_by.name":      "Update dependencies",
		"schedule.fire_time":       want.Format(time.RFC3339),
		"schedule.created_by.id":   ana.Answer.Sub,
		"schedule.created_by.name": ana.Answer.Username,
	} {
		if events[0][k] != v {
			t.Errorf("the created event's %s = %#v, want %#v", k, events[0][k], v)
		}
	}
}

func TestAFireTimeWhileTheLastRunIsActiveIsSkippedWithAWarning(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	s := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "0 * * * *"})

	env.Clock.Advance(time.Hour)
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, s.ID); len(runs) != 1 {
		t.Fatalf("after the first fire time the schedule has %d Runs, want 1", len(runs))
	}
	// With no Runner the Run stays queued: it is active.
	env.Clock.Advance(time.Hour)
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, s.ID); len(runs) != 1 {
		t.Fatalf("a fire time while the last Run is active made a Run: %d Runs", len(runs))
	}
	warning := env.WaitForWarning("skipped: last Run is active")
	if warning["level"] != "warn" || warning["schedule.id"] != s.ID || warning["factory.project"] != "checkout" ||
		warning["schedule.fire_time"] != "2026-11-14T11:00:00Z" {
		t.Errorf("the warning is %v", warning)
	}
	if got := getSchedule(t, ana, s.ID); got.NextAt == nil || !got.NextAt.Equal(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("after the skip the next time is %v, want 12:00", got.NextAt)
	}
}

func TestAFireTimeMissedWhileTheDispatcherWasDownGivesOneRunAtStartIfItIsLessThan24HoursOld(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	hourly := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Name: "Hourly", Cron: "0 * * * *"})
	// Each Saturday at 10:00: the next one is 1 hour away.
	weekly := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Name: "Weekly", Cron: "0 10 * * 6", Project: "search"})
	// Each Sunday at 10:00: the next one is Sunday 15 November.
	sunday := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Name: "Sunday", Cron: "0 10 * * 0", Project: "search"})

	// Down from 09:00 to 14:30: the hourly schedule missed 10:00 to 14:00.
	env.RestartAfter(5*time.Hour + 30*time.Minute)
	env.CheckSchedules()
	runs := scheduleRuns(t, ana, hourly.ID)
	if len(runs) != 1 {
		t.Fatalf("after the start the hourly schedule has %d Runs, want 1: %+v", len(runs), runs)
	}
	if got := getSchedule(t, ana, hourly.ID); got.LastRun == nil ||
		!got.LastRun.FiredAt.Equal(time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC)) ||
		got.NextAt == nil || !got.NextAt.Equal(time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("the hourly schedule after the start is %+v", got)
	}
	if runs := scheduleRuns(t, ana, weekly.ID); len(runs) != 1 {
		t.Fatalf("the weekly fire time 4.5 hours old gave %d Runs, want 1", len(runs))
	}

	// Down from Saturday 14:30 to Monday 16 November 11:00: the Sunday
	// fire time of 15 November 10:00 is 25 hours old.
	env.RestartAfter(44*time.Hour + 30*time.Minute)
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, sunday.ID); len(runs) != 0 {
		t.Errorf("a fire time 25 hours old gave %d Runs, want none", len(runs))
	}
	if got := getSchedule(t, ana, sunday.ID); got.NextAt == nil ||
		!got.NextAt.Equal(time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("after the old fire time the next time is %v, want 2026-11-22 10:00", got.NextAt)
	}
}

func TestPauseStopsTheNextFireAtOnceAndContinueStartsItAgain(t *testing.T) {
	env := newEnv(t)
	ana, ben := env.Member("Ana"), env.Member("Ben")
	s := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "0 10 * * *"})

	a := ben.API("POST", "/api/v1/schedules/"+s.ID+"/pause", nil)
	var paused dispatcherapi.Schedule
	a.JSON(t, &paused)
	if a.Status != http.StatusOK || !paused.Paused || paused.NextAt != nil || paused.UpdatedBy.Name != ben.Answer.Username {
		t.Fatalf("pause: %d %s", a.Status, a.Body)
	}
	env.Clock.Advance(2 * time.Hour)
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, s.ID); len(runs) != 0 {
		t.Fatalf("a paused schedule made %d Runs", len(runs))
	}

	// Continue at 11:00: the fire time of 10:00 was while it was paused.
	a = ben.API("POST", "/api/v1/schedules/"+s.ID+"/continue", nil)
	var continued dispatcherapi.Schedule
	a.JSON(t, &continued)
	tomorrow := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	if a.Status != http.StatusOK || continued.Paused || continued.NextAt == nil || !continued.NextAt.Equal(tomorrow) {
		t.Fatalf("continue: %d %s", a.Status, a.Body)
	}
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, s.ID); len(runs) != 0 {
		t.Fatalf("continue fired the fire time of the pause: %d Runs", len(runs))
	}
	env.Clock.Advance(23 * time.Hour)
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, s.ID); len(runs) != 1 {
		t.Errorf("after continue the next fire time gave %d Runs, want 1", len(runs))
	}
}

func TestAnUpdateAtTheSameTimeAsAPauseDoesNotUndoThePause(t *testing.T) {
	env := newEnv(t)
	ana, ben := env.Member("Ana"), env.Member("Ben")
	s := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "0 10 * * *"})
	for round := range 30 {
		name := fmt.Sprintf("Update dependencies %d", round)
		var wg sync.WaitGroup
		wg.Go(func() {
			if a := ben.API("PATCH", "/api/v1/schedules/"+s.ID, dispatcherapi.UpdateSchedule{Name: &name}); a.Status != http.StatusOK {
				t.Errorf("update: %d %s", a.Status, a.Body)
			}
		})
		wg.Go(func() {
			if a := ana.API("POST", "/api/v1/schedules/"+s.ID+"/pause", nil); a.Status != http.StatusOK {
				t.Errorf("pause: %d %s", a.Status, a.Body)
			}
		})
		wg.Wait()
		got := getSchedule(t, ana, s.ID)
		if !got.Paused || got.NextAt != nil || got.Name != name {
			t.Fatalf("round %d: after an update and a pause at the same time the schedule is paused %t, next %v, "+
				"name %q; want paused, no next time, name %q", round, got.Paused, got.NextAt, got.Name, name)
		}
		if a := ana.API("POST", "/api/v1/schedules/"+s.ID+"/continue", nil); a.Status != http.StatusOK {
			t.Fatalf("continue: %d %s", a.Status, a.Body)
		}
	}
}

func TestAnIntervalShorterThanTheMinimumIsRefusedAtSaveAndTheOperatorCanChangeIt(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	save := dispatcherapi.SaveSchedule{Project: "checkout", Name: "Often", Cron: "*/30 * * * *", Prompt: "p"}
	a := ana.API("POST", "/api/v1/schedules", save)
	if a.Status != http.StatusBadRequest || !strings.Contains(string(a.Body), "1h0m0s") {
		t.Errorf("a schedule every 30 minutes: %d %s, want 400 that names the minimum", a.Status, a.Body)
	}
	// One gap of 59 minutes, one time each day, is too short.
	save.Cron = "0,59 3 * * *"
	if a := ana.API("POST", "/api/v1/schedules", save); a.Status != http.StatusBadRequest {
		t.Errorf("a schedule with a gap of 59 minutes: %d %s, want 400", a.Status, a.Body)
	}
	hourly := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "0 * * * *"})
	short := "*/10 * * * *"
	if a := ana.API("PATCH", "/api/v1/schedules/"+hourly.ID, dispatcherapi.UpdateSchedule{Cron: &short}); a.Status != http.StatusBadRequest {
		t.Errorf("an update to every 10 minutes: %d %s, want 400", a.Status, a.Body)
	}
	if got := getSchedule(t, ana, hourly.ID); got.Cron != "0 * * * *" {
		t.Errorf("a refused update changed the cron expression to %q", got.Cron)
	}

	env.Config.ScheduleMinInterval = 15 * time.Minute
	env.Restart()
	save.Cron = "*/30 * * * *"
	if a := ana.API("POST", "/api/v1/schedules", save); a.Status != http.StatusCreated {
		t.Errorf("with a minimum of 15 minutes, a schedule every 30 minutes: %d %s, want 201", a.Status, a.Body)
	}
}

func TestAClockTimeThatComesTwoTimesWhenTheClocksGoBackFiresOneTime(t *testing.T) {
	env := newEnv(t)
	// On Sunday 2027-11-07 the clocks of New York go back at 02:00 EDT:
	// 01:30 comes at 05:30 UTC (EDT) and again at 06:30 UTC (EST).
	env.Clock.Advance(time.Date(2027, 11, 6, 12, 0, 0, 0, time.UTC).Sub(env.Clock.Now()))
	ana := env.Member("Ana")
	s := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "30 1 * * *", TimeZone: "America/New_York"})
	first := time.Date(2027, 11, 7, 5, 30, 0, 0, time.UTC)
	if s.NextAt == nil || !s.NextAt.Equal(first) {
		t.Fatalf("the next time is %v, want %s", s.NextAt, first)
	}
	env.Clock.Advance(first.Sub(env.Clock.Now()))
	env.CheckSchedules()
	runs := scheduleRuns(t, ana, s.ID)
	if len(runs) != 1 {
		t.Fatalf("at 01:30 EDT the schedule has %d Runs, want 1", len(runs))
	}
	tomorrow := time.Date(2027, 11, 8, 6, 30, 0, 0, time.UTC)
	if got := getSchedule(t, ana, s.ID); got.NextAt == nil || !got.NextAt.Equal(tomorrow) {
		t.Errorf("after 01:30 EDT the next time is %v, want 01:30 EST of the next day (%s)", got.NextAt, tomorrow)
	}
	env.Clock.Advance(90 * time.Minute)
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, s.ID); len(runs) != 1 {
		t.Errorf("at 01:30 EST of the same day the schedule has %d Runs, want 1", len(runs))
	}
	if strings.Contains(env.Logs(), "skipped: last Run is active") {
		t.Errorf("the second 01:30 fired: it gave a skip warning, as the first Run is active")
	}
}

func TestADailyScheduleInATimeZoneWithClockChangesPassesAMinimumOfTwoHours(t *testing.T) {
	env := newEnv(t)
	env.Config.ScheduleMinInterval = 2 * time.Hour
	env.Restart()
	ana := env.Member("Ana")
	for _, zone := range []string{"America/New_York", "Europe/Berlin"} {
		save := dispatcherapi.SaveSchedule{Project: "checkout", Name: "Daily", Cron: "30 1 * * *", TimeZone: zone, Prompt: "p"}
		if zone == "Europe/Berlin" {
			save.Cron = "30 2 * * *"
		}
		if a := ana.API("POST", "/api/v1/schedules", save); a.Status != http.StatusCreated {
			t.Errorf("a daily schedule at %s in %s with a minimum of 2 hours: %d %s, want 201", save.Cron, zone,
				a.Status, a.Body)
		}
	}
}

func TestASaveRefusesAWrongScheduleAndNamesTheValue(t *testing.T) {
	env := newEnv(t)
	env.SolidTraceProject("bare")
	ana := env.Member("Ana")
	valid := dispatcherapi.SaveSchedule{Project: "checkout", Name: "n", Cron: "0 9 * * 1", Prompt: "p"}
	cases := []struct {
		name   string
		change func(*dispatcherapi.SaveSchedule)
		status int
		text   string
	}{
		{"six fields", func(s *dispatcherapi.SaveSchedule) { s.Cron = "0 0 9 * * 1" }, 400, "five fields"},
		{"a macro", func(s *dispatcherapi.SaveSchedule) { s.Cron = "@daily" }, 400, "five fields"},
		{"a wrong field", func(s *dispatcherapi.SaveSchedule) { s.Cron = "0 25 * * *" }, 400, "cron"},
		{"no fire time", func(s *dispatcherapi.SaveSchedule) { s.Cron = "0 0 30 2 *" }, 400, "never"},
		{"a time zone", func(s *dispatcherapi.SaveSchedule) { s.TimeZone = "Mars/Olympus" }, 400, "time zone"},
		{"Local", func(s *dispatcherapi.SaveSchedule) { s.TimeZone = "Local" }, 400, "time zone"},
		{"no name", func(s *dispatcherapi.SaveSchedule) { s.Name = " " }, 400, "name"},
		{"no prompt", func(s *dispatcherapi.SaveSchedule) { s.Prompt = "" }, 400, "prompt"},
		{"a base branch", func(s *dispatcherapi.SaveSchedule) { s.BaseBranch = "a..b" }, 400, "base branch"},
		{"no Project", func(s *dispatcherapi.SaveSchedule) { s.Project = "nope" }, 404, "nope"},
		{"no Factory settings", func(s *dispatcherapi.SaveSchedule) { s.Project = "bare" }, 422, "Factory settings"},
	}
	for _, c := range cases {
		save := valid
		c.change(&save)
		a := ana.API("POST", "/api/v1/schedules", save)
		if a.Status != c.status || !strings.Contains(string(a.Body), c.text) {
			t.Errorf("%s: %d %s, want %d with %q", c.name, a.Status, a.Body, c.status, c.text)
		}
	}
	if s := makeSchedule(t, ana, valid); s.TimeZone != "UTC" {
		t.Errorf("a schedule with no time zone has %q, want UTC", s.TimeZone)
	}
}

func TestEachChangeToAScheduleIsAnEventWithThePerson(t *testing.T) {
	env := newEnv(t)
	ana, ben := env.Member("Ana"), env.Member("Ben")
	s := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "0 10 * * *"})
	name := "Weekly upkeep"
	steps := []struct {
		p            *factorytest.Person
		method, path string
		body         any
		kind         string
	}{
		{ben, "PATCH", "/api/v1/schedules/" + s.ID, dispatcherapi.UpdateSchedule{Name: &name}, "schedule_updated"},
		{ben, "POST", "/api/v1/schedules/" + s.ID + "/pause", nil, "schedule_paused"},
		{ana, "POST", "/api/v1/schedules/" + s.ID + "/continue", nil, "schedule_continued"},
		{ben, "DELETE", "/api/v1/schedules/" + s.ID, nil, "schedule_deleted"},
	}
	for _, step := range steps {
		if a := step.p.API(step.method, step.path, step.body); a.Status != http.StatusOK {
			t.Fatalf("%s %s: %d %s", step.method, step.path, a.Status, a.Body)
		}
	}
	steps = append(steps, struct {
		p            *factorytest.Person
		method, path string
		body         any
		kind         string
	}{p: ana, kind: "schedule_created"})
	for _, step := range steps {
		ev := env.WaitForWarning("schedule " + s.ID + ": " + strings.TrimPrefix(step.kind, "schedule_"))
		if ev["event.kind"] != step.kind || ev["level"] != "info" || ev["schedule.id"] != s.ID ||
			ev["factory.project"] != "checkout" || ev["actor.kind"] != "person" ||
			ev["actor.id"] != step.p.Answer.Sub || ev["actor.name"] != step.p.Answer.Username {
			t.Errorf("the %s event is %v", step.kind, ev)
		}
	}
	if a := ana.API("GET", "/api/v1/schedules/"+s.ID, nil); a.Status != http.StatusNotFound {
		t.Errorf("a deleted schedule: %d %s, want 404", a.Status, a.Body)
	}
}

func TestAMemberManagesSchedulesWithTheCLI(t *testing.T) {
	env := newEnv(t)
	ana := member(t, env)

	made := mustCLI(t, ana, "schedules", "create", "checkout", "--name", "Upkeep", "--cron", "0 9 * * 1",
		"--time-zone", "Europe/Berlin", "--base-branch", "develop", "Update the dependencies")
	m := scheduleLine.FindStringSubmatch(made.Stdout)
	if m == nil {
		t.Fatalf("no 'Schedule <id> is made.' line in:\n%s", made.Stdout)
	}
	id := m[1]
	got := mustCLI(t, ana, "schedules", "get", id)
	for _, want := range []string{"Upkeep", "0 9 * * 1", "Europe/Berlin", "develop", "Update the dependencies",
		"2026-11-16 08:00 UTC", ana.Answer.Username} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("schedules get has no %q:\n%s", want, got.Stdout)
		}
	}
	mustCLI(t, ana, "schedules", "update", id, "--cron", "0 7 * * 1", "--prompt", "Update the gems")
	if s := getSchedule(t, ana, id); s.Cron != "0 7 * * 1" || s.Prompt != "Update the gems" || s.Name != "Upkeep" ||
		s.BaseBranch != "develop" {
		t.Errorf("after update the schedule is %+v", s)
	}
	mustCLI(t, ana, "schedules", "pause", id)
	list := mustCLI(t, ana, "schedules", "list")
	if !strings.Contains(list.Stdout, id) || !strings.Contains(list.Stdout, "paused") {
		t.Errorf("schedules list does not show the paused schedule:\n%s", list.Stdout)
	}
	mustCLI(t, ana, "schedules", "continue", id)
	if list := mustCLI(t, ana, "schedules", "list", "--project", "search"); strings.Contains(list.Stdout, id) {
		t.Errorf("schedules list of search shows a schedule of checkout:\n%s", list.Stdout)
	}
	mustCLI(t, ana, "schedules", "delete", id)
	if r := ana.CLI("schedules", "get", id); r.Code != 1 || !strings.Contains(r.Stderr, "404") {
		t.Errorf("schedules get of a deleted schedule: exit %d, stderr %q", r.Code, r.Stderr)
	}
	if r := ana.CLI("schedules", "create", "checkout", "--cron", "0 9 * * 1", "p"); r.Code != 2 {
		t.Errorf("schedules create with no --name: exit %d, want 2", r.Code)
	}
}

func TestAMissedFireTimeExactly24HoursOldGivesNoRunAndOneAMicrosecondYoungerGivesOneRun(t *testing.T) {
	// Each Saturday at 10:00: the fire time is Saturday 14 November 10:00.
	// Sunday 15 November 10:00 is exactly 24 hours after it.
	for _, c := range []struct {
		name string
		down time.Duration
		runs int
	}{
		{"exactly 24 hours old", 25 * time.Hour, 0},
		{"a microsecond younger", 25*time.Hour - time.Microsecond, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := newEnv(t)
			ana := env.Member("Ana")
			s := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "0 10 * * 6"})
			env.RestartAfter(c.down)
			env.CheckSchedules()
			if runs := scheduleRuns(t, ana, s.ID); len(runs) != c.runs {
				t.Errorf("a missed fire time %s gave %d Runs, want %d", c.name, len(runs), c.runs)
			}
			if got := getSchedule(t, ana, s.ID); got.NextAt == nil ||
				!got.NextAt.Equal(time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)) {
				t.Errorf("after the start the next time is %v, want 2026-11-21 10:00", got.NextAt)
			}
		})
	}
}

func TestAFireTimeOfAScheduleWhoseSettingsAreOrphanedIsSkippedWithAWarning(t *testing.T) {
	env := factorytest.New(t)
	project := env.Project("checkout")
	ana := env.Member("Ana")
	s := makeSchedule(t, ana, dispatcherapi.SaveSchedule{Cron: "0 10 * * *"})

	// The list of Factory settings marks the settings of a Project that
	// SolidTrace no longer has as orphaned.
	env.SolidTrace.DeleteProject(factorytest.Organization.Slug, project.ID)
	var list dispatcherapi.ProjectList
	ana.API("GET", "/api/v1/projects", nil).JSON(t, &list)
	if len(list.Projects) != 1 || !list.Projects[0].Orphaned {
		t.Fatalf("the settings are %+v, want orphaned", list.Projects)
	}

	env.Clock.Advance(time.Hour)
	env.CheckSchedules()
	if runs := scheduleRuns(t, ana, s.ID); len(runs) != 0 {
		t.Fatalf("a schedule of orphaned settings made %d Runs", len(runs))
	}
	warning := env.WaitForWarning("skipped: SolidTrace no longer has the Project")
	if warning["level"] != "warn" || warning["schedule.id"] != s.ID ||
		!strings.Contains(warning["message"].(string), "orphaned") {
		t.Errorf("the warning is %v", warning)
	}
}

func TestASaveOfAScheduleForOrphanedSettingsIsRefused(t *testing.T) {
	env := factorytest.New(t)
	project := env.Project("checkout")
	ana := env.Member("Ana")
	// SolidTrace deletes the Project, the list marks its settings orphaned,
	// and SolidTrace has the Project again: the settings stay orphaned
	// until an Admin saves them.
	env.SolidTrace.DeleteProject(factorytest.Organization.Slug, project.ID)
	var list dispatcherapi.ProjectList
	ana.API("GET", "/api/v1/projects", nil).JSON(t, &list)
	if len(list.Projects) != 1 || !list.Projects[0].Orphaned {
		t.Fatalf("the settings are %+v, want orphaned", list.Projects)
	}
	env.SolidTrace.SetProject(factorytest.Organization.Slug, project)

	a := ana.API("POST", "/api/v1/schedules", dispatcherapi.SaveSchedule{Project: "checkout", Name: "n",
		Cron: "0 9 * * 1", Prompt: "p"})
	if a.Status != http.StatusUnprocessableEntity || !strings.Contains(string(a.Body), "checkout") ||
		!strings.Contains(string(a.Body), "orphaned") {
		t.Errorf("save: %d %s, want 422 that names the Project and says orphaned", a.Status, a.Body)
	}
}
