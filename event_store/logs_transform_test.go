package main_test

import (
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
