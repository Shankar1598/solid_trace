package main_test

import (
	"maps"
	"reflect"
	"slices"
	"strings"
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

func TestAbortDropsALogAndABatchLeftEmptyStillMovesTheCursor(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, `if .message == "health check" { abort }`)
	storeLogBatch(t, env, 123, logAt(t, "health check", "2026-10-02T11:00:00Z"), logAt(t, "signed in", "2026-10-02T11:00:01Z"))
	storeLogBatch(t, env, 123, logAt(t, "health check", "2026-10-02T11:00:02Z"), logAt(t, "health check", "2026-10-02T11:00:03Z"))
	batches, err := env.PebbleWriter.LogBatchesAfter(123, uuid.Nil, 100)
	if err != nil || len(batches) != 2 {
		t.Fatalf("Expected 2 Log batches, got %d: %v", len(batches), err)
	}
	processLogsWithin(t, env, 10*time.Second)

	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"signed in"}) {
		t.Errorf("Expected only [signed in] stored, got %v", got)
	}
	if cursor, err := env.PebbleWriter.LogProcessingCursor(123); err != nil || cursor != batches[1].ID {
		t.Errorf("Expected the cursor past the batch left empty, %s, got %s (%v)", batches[1].ID, cursor, err)
	}
}

// transformError is the solidtrace.transform_error of a listed Log.
func transformError(log listedLog) string {
	why, _ := log.Attributes["solidtrace.transform_error"].(string)
	return why
}

func TestALogTheProgramFailsOnIsStoredAsItArrivedWithTheError(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, `.number = to_int!(.message)`)
	seedLogTransform(t, env, 456, `. = .message`)
	long := strings.Repeat("x", 2000)
	storeLogBatch(t, env, 123,
		logAt(t, "42", "2026-10-02T11:00:00Z"),
		logAt(t, "not a number", "2026-10-02T11:00:01Z"),
		logAt(t, long, "2026-10-02T11:00:02Z"))
	storeLogBatch(t, env, 456, logAt(t, "not an object", "2026-10-02T11:00:00Z"))
	env.ProcessLogs(t)

	if got := queryMessages(t, env, `where isnotnull(solidtrace.transform_error)`); !slices.Equal(got, []string{long, "not a number"}) {
		t.Errorf("Expected the two Logs the program failed on, got %d: %.40v", len(got), got)
	}
	logs := latestLogs(t, env, 123, 10)
	if len(logs) != 3 {
		t.Fatalf("Expected 3 Logs, got %d", len(logs))
	}
	if logs[2].Attributes["number"] != 42.0 || transformError(logs[2]) != "" {
		t.Errorf("Expected 42 parsed, got %+v", logs[2])
	}
	notANumber := logs[1]
	if _, ok := notANumber.Attributes["number"]; ok || !strings.Contains(transformError(notANumber), `function call error for "to_int"`) {
		t.Errorf("Expected the Log as it arrived with to_int's error, got %+v", notANumber)
	}
	if why := transformError(logs[0]); len(why) > 1024 || len(why) < 1000 || !strings.HasPrefix(why, `function call error for "to_int"`) {
		t.Errorf("Expected to_int's error cut to 1 KB, got %d bytes: %.80q", len(why), why)
	}

	got := latestLogs(t, env, 456, 10)
	if len(got) != 1 || got[0].Message != "not an object" || transformError(got[0]) != ". is a string, not an object" {
		t.Errorf("Expected the Log as it arrived, failed for leaving . a string, got %+v", got)
	}
}

func TestAVersionThatDoesNotCompileStoresEveryLogAsItArrived(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Neither the network nor the environment is there to call.
	seedLogTransform(t, env, 123, `.page = http_request("http://127.0.0.1:8081/api/123/logs/query")`)
	seedLogTransform(t, env, 456, `.home = get_env_var!("HOME")`)
	seedLogTransform(t, env, 789, `.x = `)
	for _, projectID := range []uint32{123, 456, 789} {
		storeLogBatch(t, env, projectID, logAt(t, "one", "2026-10-02T11:00:00Z"), logAt(t, "two", "2026-10-02T11:00:01Z"))
	}
	env.ProcessLogs(t)

	for projectID, want := range map[uint32]string{
		123: "error[E105]: call to undefined function",
		456: "error[E105]: call to undefined function",
		789: "syntax error",
	} {
		logs := latestLogs(t, env, projectID, 10)
		if got := messages(logs); !slices.Equal(got, []string{"two", "one"}) {
			t.Errorf("Project %d: expected both Logs stored, got %v", projectID, got)
		}
		for _, log := range logs {
			why := transformError(log)
			if !strings.HasPrefix(why, "the Log transform doesn't compile: ") || !strings.Contains(why, want) || len(log.Attributes) != 1 {
				t.Errorf("Project %d: expected %q as it arrived with %q, got %+v", projectID, log.Message, want, log)
			}
		}
	}
}

func TestANewVersionAppliesOnlyToBatchesProcessedAfterIt(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, ".version = 1")
	storeLogBatch(t, env, 123, logAt(t, "first", "2026-10-02T11:00:00Z"))
	env.ProcessLogs(t)

	seedLogTransform(t, env, 123, ".version = 2")
	storeLogBatch(t, env, 123, logAt(t, "second", "2026-10-02T11:00:01Z"))
	env.ProcessLogs(t)

	seedLogTransform(t, env, 123, "")
	storeLogBatch(t, env, 123, logAt(t, "third", "2026-10-02T11:00:02Z"))
	env.ProcessLogs(t)

	assertQueries(t, env, map[string][]string{
		`where version == 1`:    {"first"},
		`where version == 2`:    {"second"},
		`where isnull(version)`: {"third"},
	}, "after three versions")
}

func TestAnAttributeExtendingABuiltInFieldsNameStaysUnderItsDottedName(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// The Log has no Service, Level or trace id, but Attributes whose names
	// extend theirs. Project 123's program looks at them, 456's sets the
	// Service, and 789's only adds an Attribute.
	seedLogTransform(t, env, 123, `
.seen_service = exists(.service)
.seen_version = ."service.version"
.seen_level_code = ."level.code"
`)
	seedLogTransform(t, env, 456, `.service = "web"`)
	seedLogTransform(t, env, 789, ".touched = true")
	log := models.Log{
		Time: testNow, Message: "no service",
		Attributes: models.Attributes{
			String: map[string]string{"service.version": "1.2"},
			Number: map[string]float64{"level.code": 3},
			Bool:   map[string]bool{"trace_id.sampled": true},
		},
	}
	for _, projectID := range []uint32{123, 456, 789} {
		log.ID = uuid.Must(uuid.NewV7())
		storeLogBatch(t, env, projectID, log)
	}
	env.ProcessLogs(t)

	stored := func(service string, added map[string]any) []listedLog {
		attributes := map[string]any{"service.version": "1.2", "level.code": 3.0, "trace_id.sampled": true}
		maps.Copy(attributes, added)
		return []listedLog{{Time: testNow, Message: "no service", Service: service, Attributes: attributes}}
	}
	for projectID, want := range map[uint32][]listedLog{
		123: stored("", map[string]any{"seen_service": false, "seen_version": "1.2", "seen_level_code": 3.0}),
		456: stored("web", nil),
		789: stored("", map[string]any{"touched": true}),
	} {
		if got := listedWithout(t, env, projectID); !reflect.DeepEqual(got, want) {
			t.Errorf("Project %d: expected\n%+v\ngot\n%+v", projectID, want, got)
		}
	}
}

func TestAProgramSeesBuiltInFieldsAtTheTopAndAttributesNested(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, `
.seen_hour = format_timestamp!(.time, "%H")
.seen_level = exists(.level)
.seen_id = exists(.id)
.seen_method = .http.method
.seen_status_is_integer = is_integer(.status)
.seen_tags = length!(.tags)
.seen_a = .a
.seen_ab = ."a.b"
.seen_truncated = .solidtrace.truncated
`)
	storeLogBatch(t, env, 123, models.Log{
		ID: uuid.Must(uuid.NewV7()), Time: time.Date(2026, 10, 2, 11, 30, 0, 0, time.UTC), Message: "no level",
		Attributes: models.Attributes{
			String: map[string]string{"http.method": "GET", "a": "x"},
			Number: map[string]float64{"status": 200, "a.b": 1},
			Bool:   map[string]bool{"solidtrace.truncated": true},
			JSON:   map[string]string{"tags": `["a","b"]`},
		},
	})
	env.ProcessLogs(t)

	logs := latestLogs(t, env, 123, 10)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 Log, got %d", len(logs))
	}
	seen := map[string]any{}
	for name, value := range logs[0].Attributes {
		if after, ok := strings.CutPrefix(name, "seen_"); ok {
			seen[after] = value
		}
	}
	want := map[string]any{
		"hour": "11", "level": false, "id": false, "method": "GET", "status_is_integer": true,
		"tags": 2.0, "a": "x", "ab": 1.0, "truncated": true,
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("Expected the program to see\n%v\ngot\n%v (%v)", want, seen, transformError(logs[0]))
	}
}
