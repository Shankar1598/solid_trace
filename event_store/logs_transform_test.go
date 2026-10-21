package main_test

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

// seedLogTransform saves a version of a Project's Log transform in Console's
// database, as Console does when an admin saves one. The newest is current.
func seedLogTransform(t *testing.T, env *TestEnv, projectID uint32, program string) {
	t.Helper()
	consoleExec(t, env,
		"INSERT INTO log_transform_versions (project_id, program, created_at) VALUES (?, ?, ?)",
		projectID, program, time.Now())
}

// jobLine parses a job line such as "HardWorker done: 120 ms".
const jobLine = `
parsed = parse_regex!(.message, r'^(?P<worker>\S+) (?P<status>done|fail): (?P<ms>\d+) ms$')
.worker = parsed.worker
.status = parsed.status
.duration_ms = to_int!(parsed.ms)
`

func TestALogTransformParsesMessageIntoAttributesAQueryFiltersOn(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, jobLine)
	postAndProcess(t, env,
		logJSON(t, "HardWorker done: 120 ms", testNow, nil),
		logJSON(t, "SlowWorker fail: 900 ms", testNow, nil))

	assertQueries(t, env, map[string][]string{
		`where status == "fail" and duration_ms > 500`: {"SlowWorker fail: 900 ms"},
		`where worker == "HardWorker"`:                 {"HardWorker done: 120 ms"},
	}, "after the program parsed each message")
	if got := queryMessages(t, env, `where isnotnull(solidtrace.transform_error)`); !slices.Equal(got, nil) {
		t.Errorf("Expected no Log the program failed on, got %v", got)
	}
}

func TestWhatAProgramReturnsIsNormalisedAsASendersLogIs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, `
del(.message)
.http = {"method": "GET", "response": {"status": 200}}
.level = "loud"
.solidtrace.x = 1
.service = 42
`)
	postAndProcess(t, env, `{"message": "GET / 200", "msg": "not the message", "time": "2026-10-02T11:00:00Z", "level": "info"}`)

	want := []listedLog{{
		Time: time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC),
		Attributes: map[string]any{
			"msg":                   "not the message",
			"http.method":           "GET",
			"http.response.status":  200.0,
			"original.level":        "loud",
			"original.solidtrace.x": 1.0,
			"original.service":      42.0,
		},
	}}
	got := latestLogs(t, env, 123, 10)
	for i := range got {
		got[i].ID = ""
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
}
