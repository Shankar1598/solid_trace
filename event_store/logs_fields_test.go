package main_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"
)

// fieldsAnswer is the query app's answer to a Project's Fields.
type fieldsAnswer struct {
	BuiltIn    []builtInField   `json:"built_in"`
	Attributes []attributeCount `json:"attributes"`
	Truncated  bool             `json:"truncated"`
}

type builtInField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type attributeCount struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Logs int64  `json:"logs"`
}

// everyBuiltInField is the built-in fields as docs/logs/query-language.md
// lists them.
var everyBuiltInField = []builtInField{
	{"time", "datetime"}, {"level", "string"}, {"message", "string"}, {"service", "string"},
	{"environment", "string"}, {"trace_id", "string"}, {"span_id", "string"},
}

// fieldsPath is the path of a Project's Fields over [from, to), with limit
// unless it is "".
func fieldsPath(projectID uint32, from, to time.Time, limit string) string {
	params := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {to.Format(time.RFC3339Nano)}}
	if limit != "" {
		params.Set("limit", limit)
	}
	return fmt.Sprintf("/api/%d/logs/fields?%s", projectID, params.Encode())
}

// getFields returns a Project's Fields over [from, to) from the query app.
func getFields(t *testing.T, env *TestEnv, projectID uint32, from, to time.Time, limit string) fieldsAnswer {
	t.Helper()
	path := fieldsPath(projectID, from, to, limit)
	status, raw := getQuery(t, env, path)
	if status != 200 {
		t.Fatalf("GET %s expected 200, got %d: %s", path, status, raw)
	}
	var answer fieldsAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode Fields %q: %v", raw, err)
	}
	return answer
}

// postHotTo sends a Project one Log per set of Attributes, all at one time,
// with the clock at that time, so that they are hot and counted for their
// day.
func postHotTo(t *testing.T, env *TestEnv, projectID uint32, key string, at time.Time, attributes ...map[string]any) {
	t.Helper()
	env.Clock.Set(at)
	logs := make([]string, len(attributes))
	for i, a := range attributes {
		logs[i] = logJSON(t, fmt.Sprintf("log %d", i), at, a)
	}
	postAndProcessTo(t, env, projectID, key, logs...)
}

// assertAttributes checks an answer's Attributes, in order; want nil means
// none, which is an empty list, not null.
func assertAttributes(t *testing.T, got fieldsAnswer, want []attributeCount, when string) {
	t.Helper()
	if want == nil {
		want = []attributeCount{}
	}
	if !reflect.DeepEqual(got.Attributes, want) {
		t.Errorf("%s: expected Attributes %v, got %v", when, want, got.Attributes)
	}
}

func TestFieldsSumEachAttributesLogsAcrossTheRangesDays(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	postHotTo(t, env, 123, "test_public_key", day.Add(10*time.Hour),
		map[string]any{"status": 200, "route": "/a"},
		map[string]any{"status": 200, "route": "/b"})
	postHotTo(t, env, 123, "test_public_key", day.Add(34*time.Hour),
		map[string]any{"status": 500},
		map[string]any{"status": "200"},
		map[string]any{"ok": true})

	got := getFields(t, env, 123, day, day.Add(48*time.Hour), "")
	if !reflect.DeepEqual(got.BuiltIn, everyBuiltInField) {
		t.Errorf("Expected the built-in fields %v, got %v", everyBuiltInField, got.BuiltIn)
	}
	// One name can be two Attributes, one per type of its values.
	assertAttributes(t, got, []attributeCount{
		{"status", "number", 3},
		{"route", "string", 2},
		{"ok", "bool", 1},
		{"status", "string", 1},
	}, "over both days")
	if got.Truncated {
		t.Errorf("Expected every Attribute, got truncated")
	}

	// A limit of as many Attributes as there are is not truncated; one
	// fewer is.
	if got := getFields(t, env, 123, day, day.Add(48*time.Hour), "4"); len(got.Attributes) != 4 || got.Truncated {
		t.Errorf("limit 4: expected 4 Attributes, not truncated, got %+v", got)
	}
	if got := getFields(t, env, 123, day, day.Add(48*time.Hour), "3"); len(got.Attributes) != 3 || !got.Truncated {
		t.Errorf("limit 3: expected 3 Attributes, truncated, got %+v", got)
	}
}

func TestFieldsCountTheUTCDaysTheRangeTouches(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	sep30 := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	oct1, oct2 := sep30.AddDate(0, 0, 1), sep30.AddDate(0, 0, 2)
	postHotTo(t, env, 123, "test_public_key", sep30.Add(23*time.Hour+30*time.Minute), map[string]any{"sep30": 1})
	postHotTo(t, env, 123, "test_public_key", oct1.Add(30*time.Minute), map[string]any{"oct1": 1})
	postHotTo(t, env, 123, "test_public_key", oct2.Add(30*time.Minute), map[string]any{"oct2": 1})

	sep30Key := attributeCount{"sep30", "number", 1}
	oct1Key := attributeCount{"oct1", "number", 1}
	oct2Key := attributeCount{"oct2", "number", 1}
	cases := []struct {
		name     string
		from, to time.Time
		want     []attributeCount
	}{
		{"a day from midnight to midnight", oct1, oct2, []attributeCount{oct1Key}},
		{"a second either side of midnight", oct1.Add(-time.Second), oct1.Add(time.Second), []attributeCount{oct1Key, sep30Key}},
		{"an hour with none of the day's Logs", oct1.Add(6 * time.Hour), oct1.Add(7 * time.Hour), []attributeCount{oct1Key}},
		{"an empty range", oct2.Add(time.Hour), oct2.Add(time.Hour), []attributeCount{oct2Key}},
		{"every day", sep30, oct2.Add(time.Nanosecond), []attributeCount{oct1Key, oct2Key, sep30Key}},
		{"days with no counts", oct2.AddDate(0, 0, 1), oct2.AddDate(0, 0, 3), nil},
	}
	for _, c := range cases {
		assertAttributes(t, getFields(t, env, 123, c.from, c.to, ""), c.want, c.name)
	}
}

func TestFieldsAnswerTheTopAttributesUpToTheLimitAndItsCap(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// k000 … k599 on one Log each, and k599 on two more, so that it ranks
	// first and the others by name.
	postHotTo(t, env, 123, "test_public_key", testNow, numberedKeys("k", 600, 1),
		map[string]any{"k599": 1}, map[string]any{"k599": 1})
	from, to := testNow.Add(-time.Hour), testNow.Add(time.Hour)

	// Each limit is fewer than the 600 Attributes, so each answer is
	// truncated.
	for limit, want := range map[string]int{"": 100, "3": 3, "500": 500, "501": 500, "10000": 500} {
		got := getFields(t, env, 123, from, to, limit)
		if len(got.Attributes) != want || !got.Truncated {
			t.Errorf("limit %q: expected %d Attributes, truncated, got %d, truncated %v", limit, want, len(got.Attributes), got.Truncated)
			continue
		}
		if first, second := got.Attributes[0], got.Attributes[1]; first != (attributeCount{"k599", "number", 3}) || second != (attributeCount{"k000", "number", 1}) {
			t.Errorf("limit %q: expected k599 then k000 first, got %v, %v", limit, first, second)
		}
	}

	for _, limit := range []string{"0", "-1", "ten", "1.5"} {
		path := fieldsPath(123, from, to, limit)
		if status, raw := getQuery(t, env, path); status != 400 {
			t.Errorf("limit %q: expected 400, got %d: %s", limit, status, raw)
		}
	}
}

func TestFieldsOfAProjectWithNoCountsAreTheBuiltInFields(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	got := getFields(t, env, 123, testNow.Add(-time.Hour), testNow, "")
	if !reflect.DeepEqual(got.BuiltIn, everyBuiltInField) || got.Truncated {
		t.Errorf("Expected the built-in fields, not truncated, got %+v", got)
	}
	assertAttributes(t, got, nil, "a Project with no Logs")
}

func TestFieldsNeverCountAnotherProjectsLogs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	postHotTo(t, env, 123, "test_public_key", testNow, map[string]any{"mine": 1, "shared": "a"})
	postHotTo(t, env, 456, "other_public_key", testNow,
		map[string]any{"theirs": 1, "shared": "b"}, map[string]any{"shared": "c"})

	from, to := testNow.Add(-time.Hour), testNow.Add(time.Hour)
	assertAttributes(t, getFields(t, env, 123, from, to, ""), []attributeCount{
		{"mine", "number", 1}, {"shared", "string", 1},
	}, "Project 123")
	assertAttributes(t, getFields(t, env, 456, from, to, ""), []attributeCount{
		{"shared", "string", 2}, {"theirs", "number", 1},
	}, "Project 456")
	assertAttributes(t, getFields(t, env, 789, from, to, ""), nil, "Project 789, with no Logs")
}

func TestFieldsRefuseARangeThatIsNotOne(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	for name, path := range map[string]string{
		"no range":        "/api/123/logs/fields",
		"no to":           "/api/123/logs/fields?from=2026-10-02T00:00:00Z",
		"no from":         "/api/123/logs/fields?to=2026-10-02T00:00:00Z",
		"not times":       "/api/123/logs/fields?from=yesterday&to=today",
		"from after to":   "/api/123/logs/fields?from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z",
		"a date, no time": "/api/123/logs/fields?from=2026-10-01&to=2026-10-02",
		"no Project id":   "/api/abc/logs/fields?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z",
	} {
		if status, raw := getQuery(t, env, path); status != 400 {
			t.Errorf("%s: expected 400, got %d: %s", name, status, raw)
		}
	}
}
