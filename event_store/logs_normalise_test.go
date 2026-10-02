package main_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// logCase is one Log a sender sends, and the Log it expects listed back.
type logCase struct {
	name   string
	object string
	want   listedLog
}

// storeCases sends each case's Log, marked with its name in a "case"
// Attribute, to Project 123 in one JSON array, runs Log processing, and checks
// that each Log is listed as its case expects, apart from its id. It returns
// the route's counts.
func storeCases(t *testing.T, env *TestEnv, cases []logCase, extra ...string) logCounts {
	t.Helper()
	elements := make([]string, 0, len(cases)+len(extra))
	for _, c := range cases {
		elements = append(elements, withCase(t, c.name, c.object))
	}
	elements = append(elements, extra...)
	counts := postLogs(t, env, "application/json", "["+strings.Join(elements, ",\n")+"]")
	env.ProcessLogs(t)

	listed := make(map[string]listedLog)
	for _, log := range latestLogs(t, env, 123, 10000) {
		name, _ := log.Attributes["case"].(string)
		delete(log.Attributes, "case")
		log.ID = ""
		if len(log.Attributes) == 0 {
			log.Attributes = nil
		}
		listed[name] = log
	}
	for _, c := range cases {
		got, ok := listed[c.name]
		if !ok {
			t.Errorf("%s: not listed", c.name)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: expected\n%+v\ngot\n%+v", c.name, c.want, got)
		}
	}
	return counts
}

// withCase adds a "case" Attribute holding name to a JSON object.
func withCase(t *testing.T, name, object string) string {
	t.Helper()
	quoted, _ := json.Marshal(name)
	rest, ok := strings.CutPrefix(strings.TrimSpace(object), "{")
	if !ok {
		t.Fatalf("%s: not an object: %s", name, object)
	}
	if strings.TrimSpace(rest) == "}" {
		return fmt.Sprintf(`{"case": %s}`, quoted)
	}
	return fmt.Sprintf(`{"case": %s, %s`, quoted, rest)
}

func TestJSONAliasesFillTheBuiltInFields(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	const traceID, spanID = "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"
	at := time.Date(2026, 10, 2, 11, 59, 0, 0, time.UTC)
	cases := []logCase{
		{"message", `{"message": "m"}`, listedLog{Time: testNow, Message: "m"}},
		{"msg", `{"msg": "m"}`, listedLog{Time: testNow, Message: "m"}},
		{"log", `{"log": "m"}`, listedLog{Time: testNow, Message: "m"}},
		{"body", `{"body": "m"}`, listedLog{Time: testNow, Message: "m"}},
		{"time", `{"time": "2026-10-02T11:59:00Z"}`, listedLog{Time: at}},
		{"timestamp", `{"timestamp": "2026-10-02T11:59:00Z"}`, listedLog{Time: at}},
		{"ts", `{"ts": "2026-10-02T11:59:00Z"}`, listedLog{Time: at}},
		{"@timestamp", `{"@timestamp": "2026-10-02T11:59:00Z"}`, listedLog{Time: at}},
		{"_time", `{"_time": "2026-10-02T11:59:00Z"}`, listedLog{Time: at}},
		{"level", `{"level": "info"}`, listedLog{Time: testNow, Level: "info"}},
		{"severity", `{"severity": "info"}`, listedLog{Time: testNow, Level: "info"}},
		{"lvl", `{"lvl": "info"}`, listedLog{Time: testNow, Level: "info"}},
		{"log.level", `{"log.level": "info"}`, listedLog{Time: testNow, Level: "info"}},
		{"service", `{"service": "web"}`, listedLog{Time: testNow, Service: "web"}},
		{"service.name", `{"service.name": "web"}`, listedLog{Time: testNow, Service: "web"}},
		{"environment", `{"environment": "prod"}`, listedLog{Time: testNow, Environment: "prod"}},
		{"env", `{"env": "prod"}`, listedLog{Time: testNow, Environment: "prod"}},
		{"deployment.environment", `{"deployment.environment": "prod"}`, listedLog{Time: testNow, Environment: "prod"}},
		{"trace_id", `{"trace_id": "` + traceID + `"}`, listedLog{Time: testNow, TraceID: traceID}},
		{"traceId", `{"traceId": "` + traceID + `"}`, listedLog{Time: testNow, TraceID: traceID}},
		{"span_id", `{"span_id": "` + spanID + `"}`, listedLog{Time: testNow, SpanID: spanID}},
		{"spanId", `{"spanId": "` + spanID + `"}`, listedLog{Time: testNow, SpanID: spanID}},
		{
			"the first alias present wins and the rest stay Attributes",
			`{"body": "b", "msg": "m", "ts": "2026-10-02T11:59:00Z", "@timestamp": "2026-10-02T11:00:00Z", "env": "e", "deployment.environment": "d"}`,
			listedLog{Time: at, Message: "m", Environment: "e", Attributes: map[string]any{
				"body": "b", "@timestamp": "2026-10-02T11:00:00Z", "deployment.environment": "d",
			}},
		},
		{
			"aliases in their nested form",
			`{"log": {"level": "warn", "file": "app.go"}, "service": {"name": "web", "version": "1.2"}, "deployment": {"environment": "prod"}}`,
			listedLog{Time: testNow, Level: "warn", Service: "web", Environment: "prod", Attributes: map[string]any{
				"log.file": "app.go", "service.version": "1.2",
			}},
		},
	}

	if counts := storeCases(t, env, cases); counts != (logCounts{Accepted: len(cases)}) {
		t.Errorf("Expected every Log accepted, got %+v", counts)
	}
}

func TestLevelSpellingsAndValuesThatCannotFillTheirField(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	spellings := map[string]string{
		"trace": "trace", "debug": "debug", "info": "info", "warn": "warn", "error": "error", "fatal": "fatal",
		"TRACE": "trace", "Debug": "debug", "INFO": "info", "Warn": "warn", "ERROR": "error", "Fatal": "fatal",
		"warning": "warn", "Warning": "warn", "err": "error", "ERR": "error",
		"critical": "fatal", "crit": "fatal", "panic": "fatal", "emergency": "fatal", "EMERGENCY": "fatal",
		"notice": "info", "Notice": "info",
	}
	var cases []logCase
	for sent, level := range spellings {
		cases = append(cases, logCase{"level " + sent, `{"level": "` + sent + `"}`, listedLog{Time: testNow, Level: level}})
	}
	cases = append(cases,
		logCase{"no level", `{"msg": "none"}`, listedLog{Time: testNow, Message: "none"}},
		logCase{"unrecognised level", `{"level": "verbose"}`, listedLog{Time: testNow, Attributes: map[string]any{"original.level": "verbose"}}},
		logCase{"numeric level", `{"lvl": 30}`, listedLog{Time: testNow, Attributes: map[string]any{"original.level": 30.0}}},
		logCase{"malformed trace id", `{"trace_id": "abc-123"}`, listedLog{Time: testNow, Attributes: map[string]any{"original.trace_id": "abc-123"}}},
		logCase{"malformed span id", `{"spanId": 42}`, listedLog{Time: testNow, Attributes: map[string]any{"original.span_id": 42.0}}},
		logCase{"no message", `{"user": "ada"}`, listedLog{Time: testNow, Message: "", Attributes: map[string]any{"user": "ada"}}},
		logCase{"empty message", `{"message": ""}`, listedLog{Time: testNow, Message: ""}},
		logCase{"message that isn't a string", `{"msg": {"text": "hi"}, "message": ["a", "b"]}`, listedLog{Time: testNow, Message: "", Attributes: map[string]any{
			"original.message": []any{"a", "b"}, "msg.text": "hi",
		}}},
		logCase{"Service that isn't a string", `{"service": 7}`, listedLog{Time: testNow, Attributes: map[string]any{"original.service": 7.0}}},
	)

	if counts := storeCases(t, env, cases); counts != (logCounts{Accepted: len(cases)}) {
		t.Errorf("Expected every Log accepted, got %+v", counts)
	}
}

func TestLogTimeFormsAndTimesThatBecomeTheReceiptTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	receivedAt := time.Date(2026, 10, 2, 12, 0, 5, 0, time.UTC)
	env.Clock.Set(receivedAt)
	at := time.Date(2026, 10, 2, 11, 59, 30, 0, time.UTC) // 1790942370
	replaced := func(original any) listedLog {
		return listedLog{Time: receivedAt, Attributes: map[string]any{"original.time": original}}
	}
	cases := []logCase{
		{"RFC 3339", `{"time": "2026-10-02T11:59:30Z"}`, listedLog{Time: at}},
		{"RFC 3339 with fraction and offset", `{"time": "2026-10-02T12:59:30.123456+01:00"}`, listedLog{Time: at.Add(123456 * time.Microsecond)}},
		{"ISO 8601 offset without colon", `{"time": "2026-10-02T12:59:30+0100"}`, listedLog{Time: at}},
		{"ISO 8601 with a space and no zone", `{"time": "2026-10-02 11:59:30.5"}`, listedLog{Time: at.Add(500 * time.Millisecond)}},
		{"epoch seconds", `{"time": 1790942370}`, listedLog{Time: at}},
		{"epoch seconds with fraction", `{"time": 1790942370.25}`, listedLog{Time: at.Add(250 * time.Millisecond)}},
		{"epoch milliseconds", `{"time": 1790942370250}`, listedLog{Time: at.Add(250 * time.Millisecond)}},
		{"epoch microseconds", `{"time": 1790942370250001}`, listedLog{Time: at.Add(250001 * time.Microsecond)}},
		{"epoch nanoseconds", `{"time": 1790942370250001000}`, listedLog{Time: at.Add(250001 * time.Microsecond)}},
		{"missing", `{}`, listedLog{Time: receivedAt}},
		{"within Retention", `{"time": "2026-09-03T12:00:00Z"}`, listedLog{Time: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)}},
		{"older than Retention", `{"time": "2026-09-01T12:00:00Z"}`, replaced("2026-09-01T12:00:00Z")},
		{"an old epoch", `{"time": 1000000000}`, replaced(1000000000.0)},
		{"under an hour ahead", `{"time": "2026-10-02T12:59:00Z"}`, listedLog{Time: time.Date(2026, 10, 2, 12, 59, 0, 0, time.UTC)}},
		{"more than an hour ahead", `{"time": "2026-10-02T13:30:00Z"}`, replaced("2026-10-02T13:30:00Z")},
		{"not a time", `{"time": "yesterday"}`, replaced("yesterday")},
	}

	if counts := storeCases(t, env, cases); counts != (logCounts{Accepted: len(cases)}) {
		t.Errorf("Expected every Log accepted, got %+v", counts)
	}
}

func TestAttributesAreFlattenedAndKeepTheirTypes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	cases := []logCase{
		{
			"nested objects become dotted names",
			`{"http": {"method": "GET", "response": {"status": 200}}, "user": {"id": null}, "empty": {}}`,
			listedLog{Time: testNow, Attributes: map[string]any{"http.method": "GET", "http.response.status": 200.0}},
		},
		{
			"the literal dotted key wins",
			`{"http": {"method": "nested", "path": "/"}, "http.method": "literal"}`,
			listedLog{Time: testNow, Attributes: map[string]any{"http.method": "literal", "http.path": "/"}},
		},
		{
			"an array is one value, objects inside it not flattened",
			`{"tags": ["a", "b"], "items": [{"sku": 1}, {"sku": 2}]}`,
			listedLog{Time: testNow, Attributes: map[string]any{
				"tags": []any{"a", "b"}, "items": []any{map[string]any{"sku": 1.0}, map[string]any{"sku": 2.0}},
			}},
		},
		{
			"each value keeps its type",
			`{"as_string": "200", "as_number": 200, "fraction": 0.5, "as_bool": true, "mixed": [1, "1", false]}`,
			listedLog{Time: testNow, Attributes: map[string]any{
				"as_string": "200", "as_number": 200.0, "fraction": 0.5, "as_bool": true, "mixed": []any{1.0, "1", false},
			}},
		},
		{
			"a whole number a DOUBLE can't hold is a string",
			`{"order_id": 9007199254740993, "exact": 9007199254740992}`,
			listedLog{Time: testNow, Attributes: map[string]any{"order_id": "9007199254740993", "exact": 9007199254740992.0}},
		},
		{
			"a sender's solidtrace keys are kept as original",
			`{"solidtrace.truncated": true, "solidtrace": {"agent": "x"}}`,
			listedLog{Time: testNow, Attributes: map[string]any{
				"original.solidtrace.truncated": true, "original.solidtrace.agent": "x",
			}},
		},
	}

	if counts := storeCases(t, env, cases); counts != (logCounts{Accepted: len(cases)}) {
		t.Errorf("Expected every Log accepted, got %+v", counts)
	}
}

// nestedObject returns levels objects, one inside the next, around value.
func nestedObject(levels int, value string) string {
	object := value
	for i := levels; i >= 1; i-- {
		object = fmt.Sprintf(`{"l%d": %s}`, i, object)
	}
	return object
}

// manyAttributes returns a JSON object of n numbered Attributes, k0000 = 0
// and so on.
func manyAttributes(n int) string {
	pairs := make([]string, n)
	for i := range n {
		pairs[i] = fmt.Sprintf(`"k%04d": %d`, i, i)
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

func TestLogLimitsTruncateAndDropAndTheResponseCountsThem(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	const kb = 1024
	long := strings.Repeat("x", 70*kb)
	name := strings.Repeat("n", 256)
	huge := make([]string, 17)
	for i := range huge {
		huge[i] = fmt.Sprintf(`"s%02d": "%s"`, i, strings.Repeat("v", 64*kb))
	}

	cases := []logCase{
		{
			"a long message and value are cut at 64 KB",
			`{"msg": "` + long + `", "detail": "` + long + `"}`,
			listedLog{Time: testNow, Message: long[:64*kb], Attributes: map[string]any{
				"detail": long[:64*kb], "solidtrace.truncated": true,
			}},
		},
		{
			"a name over 256 bytes is dropped",
			`{"` + name + `": 1, "` + name + `x": 2}`,
			listedLog{Time: testNow, Attributes: map[string]any{name: 1.0, "solidtrace.truncated": true}},
		},
		{
			"past 1,000 Attributes the excess is dropped",
			// With the case Attribute, which sorts first, 1,004 in all.
			strings.Replace(manyAttributes(1004), `"k0000": 0, `, "", 1),
			listedLog{Time: testNow, Attributes: func() map[string]any {
				kept := make(map[string]any, 1000)
				for i := 1; i < 1000; i++ {
					kept[fmt.Sprintf("k%04d", i)] = float64(i)
				}
				kept["solidtrace.truncated"] = true
				return kept
			}()},
		},
		{
			"nesting past 10 levels is kept as JSON",
			`{"deep": ` + nestedObject(10, `"v"`) + `}`,
			listedLog{Time: testNow, Attributes: map[string]any{
				"deep.l1.l2.l3.l4.l5.l6.l7.l8.l9": map[string]any{"l10": "v"},
			}},
		},
		{"a Log within the limits", `{"msg": "fine"}`, listedLog{Time: testNow, Message: "fine"}},
	}
	overOneMB := "{" + strings.Join(huge, ", ") + "}"

	counts := storeCases(t, env, cases, overOneMB, `"not a Log"`)
	if want := (logCounts{Accepted: 5, Dropped: 2, Truncated: 3}); counts != want {
		t.Errorf("Expected %+v, got %+v", want, counts)
	}
	if got := len(latestLogs(t, env, 123, 100)); got != 5 {
		t.Errorf("Expected the Log over 1 MB not stored, got %d Logs", got)
	}
}
