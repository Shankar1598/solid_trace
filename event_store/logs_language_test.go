package main_test

import (
	"testing"
	"time"
)

// languageDay is the day the language tests' Logs are on.
func languageAt(hour, minute int) time.Time {
	return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC)
}

// seedLanguageLogs stores five Logs, newest first ed, cy, bob, ada and di
// (the day before), with the clock at 11:30, so all but di are in the hot
// table.
func seedLanguageLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(11, 30))
	postAndProcess(t, env,
		logJSON(t, "ada", languageAt(10, 10), map[string]any{
			"status": 200, "user": "ada", "duration": 120, "path": "/api/users", "service": "api"}),
		logJSON(t, "bob", languageAt(10, 20), map[string]any{
			"status": "200", "user": "Bob", "duration": 80.5, "path": "/health", "service": "web"}),
		logJSON(t, "cy", languageAt(10, 30), map[string]any{
			"user": "cy", "level": "error", "duration": 300, "path": "/api/orders", "note": "", "service": "api"}),
		logJSON(t, "di", languageAt(10, 40).AddDate(0, 0, -1), map[string]any{
			"status": 500, "user": "di", "duration": 50, "service": "worker"}),
		logJSON(t, "ed", languageAt(11, 10), map[string]any{
			"status": 404, "user": "ed", "level": "error", "duration": 900, "note": "Disk FULL", "path": "/API/users", "service": "api"}),
	)
}

// exportLanguageLogs moves hour 10 and the day before to files, and adds fy,
// a late Log of 10:45 beside hour 10's file.
func exportLanguageLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(12, 5))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, languageAt(10, 0), 1)
	postAndProcess(t, env, logJSON(t, "fy", languageAt(10, 45), map[string]any{
		"status": 200, "user": "fy", "duration": 10, "path": "/api/users/7", "service": "api"}))
}

func TestWhereComparesMatchesAndTestsFields(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	cases := map[string][]string{
		`where duration > 100`:                         {"ed", "cy", "ada"},
		`where duration >= 300`:                        {"ed", "cy"},
		`where duration < 100`:                         {"bob", "di"},
		`where duration <= 80.5`:                       {"bob", "di"},
		`where status < 300`:                           {"ada"},
		`where status >= 404`:                          {"ed", "di"},
		`where not(duration > 100)`:                    {"bob", "di"},
		`where path contains "USERS"`:                  {"ed", "ada"},
		`where path contains_cs "API"`:                 {"ed"},
		`where path !contains "users"`:                 {"cy", "bob", "di"},
		`where path !contains_cs "API"`:                {"cy", "bob", "ada", "di"},
		`where message contains "D"`:                   {"ed", "ada", "di"},
		`where path startswith "/api"`:                 {"ed", "cy", "ada"},
		`where path !startswith "/api"`:                {"bob", "di"},
		`where user matches regex "^[a-c]"`:            {"cy", "ada"},
		`where user in ("ada", "ed")`:                  {"ed", "ada"},
		`where status in (200, 404)`:                   {"ed", "ada"},
		`where status !in (200, 404)`:                  {"cy", "bob", "di"},
		`where level in ("error")`:                     {"ed", "cy"},
		`where isempty(note)`:                          {"cy", "bob", "ada", "di"},
		`where isnotempty(note)`:                       {"ed"},
		`where isnotempty(status)`:                     {"ed", "bob", "ada", "di"},
		`where duration > 100 and path contains "api"`: {"ed", "cy", "ada"},
		`where user == "bob"`:                          {},
		`where user =~ "bob"`:                          {"bob"},
	}
	assertQueries(t, env, cases, "in the hot table")

	exportLanguageLogs(t, env)
	cases[`where path contains "USERS"`] = []string{"ed", "fy", "ada"}
	cases[`where path startswith "/api"`] = []string{"ed", "fy", "cy", "ada"}
	cases[`where user matches regex "^f"`] = []string{"fy"}
	cases[`where duration < 100`] = []string{"fy", "bob", "di"}
	cases[`where duration <= 80.5`] = []string{"fy", "bob", "di"}
	cases[`where status < 300`] = []string{"fy", "ada"}
	cases[`where not(duration > 100)`] = []string{"fy", "bob", "di"}
	cases[`where path !contains_cs "API"`] = []string{"fy", "cy", "bob", "ada", "di"}
	cases[`where status in (200, 404)`] = []string{"ed", "fy", "ada"}
	cases[`where isempty(note)`] = []string{"fy", "cy", "bob", "ada", "di"}
	cases[`where isnotempty(status)`] = []string{"ed", "fy", "bob", "ada", "di"}
	cases[`where duration > 100 and path contains "api"`] = []string{"ed", "cy", "ada"}
	assertQueries(t, env, cases, "across files and a late Log")
}
