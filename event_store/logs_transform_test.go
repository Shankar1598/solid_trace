package main_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
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

// listedWithout lists a Project's Logs, newest first, without their ids.
func listedWithout(t *testing.T, env *TestEnv, projectID uint32) []listedLog {
	t.Helper()
	logs := latestLogs(t, env, projectID, 100)
	for i := range logs {
		logs[i].ID = ""
		if len(logs[i].Attributes) == 0 {
			logs[i].Attributes = nil
		}
	}
	return logs
}

func TestAProgramThatChangesNothingLeavesEveryFieldAsItWas(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Project 123 has no Log transform, 456 an empty one, and 789 one that
	// only adds an Attribute.
	seedLogTransform(t, env, 456, "")
	seedLogTransform(t, env, 789, ".touched = true")

	at := time.Date(2026, 10, 2, 11, 59, 0, 123456000, time.UTC)
	full := models.Log{
		Time: at, Level: "warn", Message: "full", Service: "web", Environment: "production",
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7",
		Attributes: models.Attributes{
			String: map[string]string{
				"user": "ada", "big": "9007199254740993",
				// Each can't nest under a value a shorter name holds.
				"a": "holds a value", "service.name": "api", "time.zone": "UTC",
				// Only a Sentry record can have an Attribute named as a
				// built-in field.
				"level": "a Sentry attribute",
			},
			Number: map[string]float64{"a.b": 1, "status": 200, "ratio": 0.5, "http.response.ms": 12.5},
			Bool:   map[string]bool{"cached": true, "solidtrace.truncated": true},
			JSON:   map[string]string{"tags": `["a",1,2.5,true,null,{"k":"<v>"}]`},
		},
	}
	bare := models.Log{Time: testNow, Message: "bare"}
	for _, projectID := range []uint32{123, 456, 789} {
		full.ID, bare.ID = uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		storeLogBatch(t, env, projectID, full, bare)
	}
	env.ProcessLogs(t)

	want := []listedLog{
		{Time: testNow, Message: "bare"},
		{
			Time: at, Level: "warn", Message: "full", Service: "web", Environment: "production",
			TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7",
			Attributes: map[string]any{
				"user": "ada", "big": "9007199254740993",
				"a": "holds a value", "service.name": "api", "time.zone": "UTC",
				"level": "a Sentry attribute",
				"a.b":   1.0, "status": 200.0, "ratio": 0.5, "http.response.ms": 12.5,
				"cached": true, "solidtrace.truncated": true,
				"tags": []any{"a", 1.0, 2.5, true, nil, map[string]any{"k": "<v>"}},
			},
		},
	}
	for _, projectID := range []uint32{123, 456} {
		if got := listedWithout(t, env, projectID); !reflect.DeepEqual(got, want) {
			t.Errorf("Project %d: expected\n%+v\ngot\n%+v", projectID, want, got)
		}
	}
	want[0].Attributes = map[string]any{"touched": true}
	want[1].Attributes["touched"] = true
	if got := listedWithout(t, env, 789); !reflect.DeepEqual(got, want) {
		t.Errorf("Project 789: expected\n%+v\ngot\n%+v", want, got)
	}
}
