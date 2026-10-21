package main_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// attributesByMessage maps each listed Log's message to its Attributes.
func attributesByMessage(logs []listedLog) map[string]map[string]any {
	out := make(map[string]map[string]any, len(logs))
	for _, log := range logs {
		out[log.Message] = log.Attributes
	}
	return out
}

// assertProjectQueries checks each Query's messages for a Project over all
// time, newest first.
func assertProjectQueries(t *testing.T, env *TestEnv, projectID uint32, cases map[string][]string, when string) {
	t.Helper()
	for query, want := range cases {
		logs := runQueryPages(t, env, projectID, queryRequest{Query: query, From: allTime.from, To: allTime.to, Limit: 1000})
		if got := messages(logs); !slices.Equal(got, want) {
			t.Errorf("%s: Project %d: %s\n want %v\n got  %v", when, projectID, query, want, got)
		}
	}
}

func TestALogIsFoundByEachOfItsKeysAndReadBackUnchanged(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postLogs(t, env, "application/json", `{
		"message": "checkout",
		"time": "2026-10-02T11:59:00Z",
		"user": "ada",
		"status": 200,
		"cached": true,
		"roles": ["admin", "dev"]
	}`)
	env.ProcessLogs(t)

	// Built-in fields aren't Attributes, and arrays are never promoted.
	want := map[string]any{"user": "ada", "status": 200.0, "cached": true, "roles": []any{"admin", "dev"}}
	queries := map[string][]string{
		`where user == "ada"`:          {"checkout"},
		`where status == 200`:          {"checkout"},
		`where cached == true`:         {"checkout"},
		`where roles contains "admin"`: {"checkout"},
		`where user != "ada"`:          {},
		`where status == "200"`:        {},
	}
	check := func(when string) {
		t.Helper()
		if got := attributesByMessage(latestLogs(t, env, 123, 10))["checkout"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected Attributes %v, got %v", when, want, got)
		}
		assertQueries(t, env, queries, when)
	}
	check("in the hot table")

	// At 13:00 the export moves hour 11 to its file.
	env.Clock.Set(testNow.Add(time.Hour))
	env.ExportLogs(t)
	check("in its file")
	env.Reopen(t)
	check("after a restart")
}

// logJSON is one Log as a sender would send it: a message, a time and
// Attributes.
func logJSON(t *testing.T, message string, at time.Time, attributes map[string]any) string {
	t.Helper()
	object := map[string]any{"message": message, "time": at.Format(time.RFC3339Nano)}
	for name, value := range attributes {
		object[name] = value
	}
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("Encode Log: %v", err)
	}
	return string(raw)
}

// numberedKeys returns Attributes named <prefix>000, <prefix>001 … each with
// value.
func numberedKeys(prefix string, n int, value any) map[string]any {
	attributes := make(map[string]any, n)
	for i := range n {
		attributes[fmt.Sprintf("%s%03d", prefix, i)] = value
	}
	return attributes
}

func TestLogsWithMoreKeysThanASetHoldsAreFoundByEachKey(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Every built-in field, and 150 keys: more than a set holds, so some
	// stay in the maps (storage's TestASetTakesNewKeysUntilItHolds100 checks
	// which).
	wide := numberedKeys("k", 150, "v")
	wide["level"] = "info"
	wide["service"] = "web"
	wide["environment"] = "production"
	wide["trace_id"] = "4bf92f3577b34da6a3ce929d0e0e4736"
	wide["span_id"] = "00f067aa0ba902b7"
	postLogs(t, env, "application/json", logJSON(t, "wide", testNow.Add(-time.Minute), wide))
	env.ProcessLogs(t)

	// Then a key that arrives after the set is full.
	env.Clock.Advance(time.Minute)
	postLogs(t, env, "application/json", logJSON(t, "after", testNow, map[string]any{"late": 1, "k000": "w"}))
	env.ProcessLogs(t)

	queries := map[string][]string{
		`where k000 == "v"`: {"wide"},
		`where k000 == "w"`: {"after"},
		`where k099 == "v"`: {"wide"},
		`where k100 == "v"`: {"wide"},
		`where k149 == "v"`: {"wide"},
		`where k120 != "v"`: {"after"},
		`where late == 1`:   {"after"},
	}
	check := func(when string) {
		t.Helper()
		attributes := attributesByMessage(latestLogs(t, env, 123, 10))
		if got, want := attributes["wide"], numberedKeys("k", 150, "v"); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected all 150 Attributes back, got %d: %v", when, len(got), got)
		}
		if got, want := attributes["after"], map[string]any{"late": 1.0, "k000": "w"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		assertQueries(t, env, queries, when)
	}
	check("in the hot table")
	env.Clock.Set(testNow.Add(2 * time.Hour))
	env.ExportLogs(t)
	check("in their files")
}

func TestLogsOfANewDayAfterASetThatWasNotFullAreFoundByTheirKeys(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Today's set starts as yesterday's, which wasn't full.
	yesterday := testNow.AddDate(0, 0, -1)
	env.Clock.Set(yesterday)
	postLogs(t, env, "application/json", logJSON(t, "yesterday", yesterday, map[string]any{"user": "ada", "status": 200}))
	env.ProcessLogs(t)

	env.Clock.Set(testNow)
	postLogs(t, env, "application/json", logJSON(t, "today", testNow, map[string]any{"region": "eu", "user": "bob"}))
	env.ProcessLogs(t)

	queries := map[string][]string{
		`where user == "ada"`:  {"yesterday"},
		`where user == "bob"`:  {"today"},
		`where status == 200`:  {"yesterday"},
		`where region == "eu"`: {"today"},
		`where region != "eu"`: {"yesterday"},
	}
	check := func(when string) {
		t.Helper()
		attributes := attributesByMessage(latestLogs(t, env, 123, 10))
		if got, want := attributes["yesterday"], map[string]any{"user": "ada", "status": 200.0}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		if got, want := attributes["today"], map[string]any{"region": "eu", "user": "bob"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		assertQueries(t, env, queries, when)
	}
	check("in the hot table")
	env.Reopen(t)
	check("after a restart")
}

// postAndProcess sends one request of Logs and processes it before the next,
// so each is its own Log processing transaction.
func postAndProcess(t *testing.T, env *TestEnv, logs ...string) {
	t.Helper()
	body := "[" + strings.Join(logs, ",") + "]"
	if counts := postLogs(t, env, "application/json", body); counts.Accepted != len(logs) {
		t.Fatalf("Expected %d Logs accepted, got %+v", len(logs), counts)
	}
	env.ProcessLogs(t)
}

func TestLogsOfANewDayAfterAFullSetAreFoundByTheirKeys(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	yesterday := testNow.AddDate(0, 0, -1)
	env.Clock.Set(yesterday)

	// The first Log fills the set with k000…k099.
	postAndProcess(t, env, logJSON(t, "first", yesterday, numberedKeys("k", 100, "v")))

	// A burst of 1,000 keys seen once each.
	postAndProcess(t, env,
		logJSON(t, "junk 1", yesterday, numberedKeys("j0", 500, "x")),
		logJSON(t, "junk 2", yesterday, numberedKeys("j1", 500, "x")))

	// Then five Logs, one at a time, carrying k000…k089 and ten keys that
	// came too late to be promoted.
	common := numberedKeys("k", 90, "v")
	for name, value := range numberedKeys("x", 10, 1) {
		common[name] = value
	}
	for i := range 5 {
		postAndProcess(t, env, logJSON(t, fmt.Sprintf("common %d", i), yesterday.Add(time.Duration(i)*time.Second), common))
	}

	// Today's first Log re-ranks: today's set is the 100 keys most Logs
	// carried yesterday, x000 in and k095 out.
	env.Clock.Set(testNow)
	postAndProcess(t, env, logJSON(t, "today", testNow, map[string]any{"x000": 7, "k095": "v"}))

	commons := []string{"common 4", "common 3", "common 2", "common 1", "common 0"}
	queries := map[string][]string{
		`where x000 == 7`:     {"today"},
		`where x000 == 1`:     commons,
		`where x009 == 1`:     commons,
		`where k095 == "v"`:   {"today", "first"},
		`where k089 == "v"`:   append(slices.Clone(commons), "first"),
		`where j0499 == "x"`:  {"junk 1"},
		`where j1000 == "x"`:  {"junk 2"},
		`where k099 != "v"`:   {"today", "common 4", "common 3", "common 2", "common 1", "common 0", "junk 2", "junk 1"},
		`where isempty(x000)`: {"junk 2", "junk 1", "first"},
	}
	wantCommon := numberedKeys("k", 90, "v")
	for name := range numberedKeys("x", 10, 1) {
		wantCommon[name] = 1.0
	}
	check := func(when string) {
		t.Helper()
		// Every Log reads back as it was sent, whichever day's set it
		// follows.
		attributes := attributesByMessage(latestLogs(t, env, 123, 100))
		if got, want := attributes["today"], map[string]any{"x000": 7.0, "k095": "v"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		if got, want := attributes["first"], numberedKeys("k", 100, "v"); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected the first Log's 100 Attributes back, got %v", when, got)
		}
		if got := attributes["common 4"]; !reflect.DeepEqual(got, wantCommon) {
			t.Errorf("%s: expected\n%v\ngot\n%v", when, wantCommon, got)
		}
		if got := len(attributes["junk 2"]); got != 500 {
			t.Errorf("%s: expected the junk Log's 500 Attributes back, got %d", when, got)
		}
		assertQueries(t, env, queries, when)
	}
	check("in the hot table")
	env.Clock.Set(testNow.Add(2 * time.Hour))
	env.ExportLogs(t)
	check("in their files")
}

func TestAKeyFirstSeenPartwayThroughADayFindsTheLogsBeforeAndAfter(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }

	// The day's first Log makes its set, with user.
	env.Clock.Set(at(11, 0))
	postAndProcess(t, env, logJSON(t, "early", at(11, 0), map[string]any{"user": "ada"}))

	// A late Log carries cart, which the set doesn't have, and goes to hour
	// 10's file with cart in its map.
	env.Clock.Set(at(12, 30))
	postAndProcess(t, env, logJSON(t, "older", at(10, 30), map[string]any{"user": "bo", "cart": 1}))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, at(10, 0), 1)

	// Then a hot Log promotes cart partway through the day.
	postAndProcess(t, env, logJSON(t, "later", at(12, 10), map[string]any{"user": "bob", "cart": 3}))

	values := []fieldValue{
		{"user", "ada", []string{"early"}},
		{"user", "bo", []string{"older"}},
		{"user", "bob", []string{"later"}},
	}
	attributes := map[string]map[string]any{
		"early": {"user": "ada"},
		"older": {"user": "bo", "cart": 1.0},
		"later": {"user": "bob", "cart": 3.0},
	}
	queries := map[string][]string{
		`where cart == 1`:              {"older"},
		`where cart == 3`:              {"later"},
		`where cart > 0`:               {"later", "older"},
		`where isempty(cart)`:          {"early"},
		`where cart == 1 or cart == 3`: {"later", "older"},
	}
	check := func(when string) {
		t.Helper()
		assertEveryReadFinds(t, env, values, attributes, when)
		assertQueries(t, env, queries, when)
		if _, rows := runTable(t, env, `summarize count() by cart`); !slices.Equal(slices.Sorted(slices.Values(rows)), []string{`[1,1]`, `[3,1]`, `[null,1]`}) {
			t.Errorf("%s: expected a Log for each cart and one without, got %v", when, rows)
		}
	}
	check("with cart promoted partway")
	env.Reopen(t)
	check("after a restart")

	// At 14:00 the export moves hours 11 and 12 to their files.
	env.Clock.Set(at(14, 0))
	env.ExportLogs(t)
	check("after the export")
}

func TestKeysThatCannotHaveAColumnOfTheirOwnStayAttributes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// DuckDB column names ignore case, so only one of User and user can be
	// promoted. One key name can carry two types, each promoted on its own.
	postAndProcess(t, env,
		`{"message": "one", "time": "2026-10-02T11:59:00Z", "User": "a", "user": "b", "code": 5, "say \"hi\"": "q", "nul\u0000key": "n"}`,
		`{"message": "two", "time": "2026-10-02T11:59:01Z", "code": "E5"}`)

	queries := map[string][]string{
		`where User == "a"`:         {"one"},
		`where user == "b"`:         {"one"},
		`where user == "a"`:         {},
		`where code == 5`:           {"one"},
		`where code == "E5"`:        {"two"},
		`where ['say "hi"'] == "q"`: {"one"},
	}
	check := func(when string) {
		t.Helper()
		attributes := attributesByMessage(latestLogs(t, env, 123, 10))
		if got, want := attributes["one"], map[string]any{"User": "a", "user": "b", "code": 5.0, "say \"hi\"": "q", "nul\u0000key": "n"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		if got, want := attributes["two"], map[string]any{"code": "E5"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		assertQueries(t, env, queries, when)
	}
	check("in the hot table")
	env.Clock.Set(testNow.Add(2 * time.Hour))
	env.ExportLogs(t)
	check("in their files")
}

func TestLogsEitherSideOfMidnightAreFoundByTheirKeys(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	midnight := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	env.Clock.Set(midnight.Add(30 * time.Second))

	// One batch with the first Logs of two days: today's set starts from
	// yesterday's, made in the same transaction.
	postAndProcess(t, env,
		logJSON(t, "before midnight", midnight.Add(-10*time.Second), map[string]any{"a": 1, "b": "x"}),
		logJSON(t, "after midnight", midnight.Add(10*time.Second), map[string]any{"c": true, "a": 2}))

	// Another Project's first Log of yesterday arrives after its first Log
	// of today: yesterday gets a set of its own.
	for _, log := range []string{
		logJSON(t, "today first", midnight.Add(20*time.Second), map[string]any{"t": "1"}),
		logJSON(t, "yesterday after", midnight.Add(-20*time.Second), map[string]any{"y": "2", "t": "0"}),
	} {
		resp := sendLogs(t, env, "/api/456/logs", "application/json", log, map[string]string{"Authorization": "Bearer other_public_key"})
		decodeLogCounts(t, resp)
		env.ProcessLogs(t)
	}

	check := func(when string) {
		t.Helper()
		attributes := attributesByMessage(latestLogs(t, env, 123, 10))
		if got, want := attributes["before midnight"], map[string]any{"a": 1.0, "b": "x"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		if got, want := attributes["after midnight"], map[string]any{"c": true, "a": 2.0}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expected %v, got %v", when, want, got)
		}
		assertProjectQueries(t, env, 123, map[string][]string{
			`where a == 1`:     {"before midnight"},
			`where a == 2`:     {"after midnight"},
			`where b == "x"`:   {"before midnight"},
			`where c == true`:  {"after midnight"},
			`where isempty(c)`: {"before midnight"},
		}, when)
		assertProjectQueries(t, env, 456, map[string][]string{
			`where t == "1"`: {"today first"},
			`where t == "0"`: {"yesterday after"},
			`where y == "2"`: {"yesterday after"},
		}, when)
	}
	check("in the hot table")

	// At 02:00 the export moves both days' Logs to their files.
	env.Clock.Set(midnight.Add(2 * time.Hour))
	env.ExportLogs(t)
	check("in their files")
	env.Reopen(t)
	check("after a restart")
}

// fieldValue is a Field's value and the messages of the Logs that carry it,
// newest first.
type fieldValue struct {
	field, value string
	want         []string
}

// assertEveryReadFinds checks that every way of reading Project 123's Logs
// over all time finds the Logs that carry each value, and only those: a run
// of Log rows, a Builder Query with a chip, a table counting by the Field,
// the histogram, a run after the nil batch and SQL mode's logs view. A run
// with no filter gives each Log every Attribute it was sent with, for the
// field list.
func assertEveryReadFinds(t *testing.T, env *TestEnv, values []fieldValue, attributes map[string]map[string]any, when string) {
	t.Helper()
	if got := attributesByMessage(latestLogs(t, env, 123, 100)); !reflect.DeepEqual(got, attributes) {
		t.Errorf("%s: expected the Logs' Attributes\n %v\ngot\n %v", when, attributes, got)
	}
	for _, v := range values {
		where := fmt.Sprintf("where %s == %q", v.field, v.value)
		if got := queryMessages(t, env, where); !slices.Equal(got, v.want) {
			t.Errorf("%s: a run of %s\n want %v\n got  %v", when, where, v.want, got)
		}
		builder := where + ` | where message != ""`
		if chips := builderChips(validateQuery(t, env, builder)); chips == "null" {
			t.Errorf("%s: expected chips for %s", when, builder)
		}
		if got := queryMessages(t, env, builder); !slices.Equal(got, v.want) {
			t.Errorf("%s: the Builder's %s\n want %v\n got  %v", when, builder, v.want, got)
		}
		var wantRows []string
		if len(v.want) > 0 {
			wantRows = []string{fmt.Sprintf("[%q,%d]", v.value, len(v.want))}
		}
		table := where + " | summarize count() by " + v.field
		if _, rows := runTable(t, env, table); !slices.Equal(rows, wantRows) {
			t.Errorf("%s: a table of %s\n want %v\n got  %v", when, table, wantRows, rows)
		}
		if got := histogram(t, env, histogramRequest{Query: where, From: testNow.AddDate(0, 0, -2), To: testNow.AddDate(0, 0, 1)}).Count; got != len(v.want) {
			t.Errorf("%s: a histogram of %s\n want %d Logs\n got  %d", when, where, len(v.want), got)
		}
		if got := messages(runAfterBatch(t, env, where, nilBatch, nil).Logs); !slices.Equal(got, v.want) {
			t.Errorf("%s: a run of %s after the nil batch\n want %v\n got  %v", when, where, v.want, got)
		}
		sql := fmt.Sprintf("SELECT message FROM logs WHERE attrs_string[%s] = %s ORDER BY time DESC",
			sqlString(v.field), sqlString(v.value))
		var wantSQL []string
		for _, message := range v.want {
			wantSQL = append(wantSQL, fmt.Sprintf("[%q]", message))
		}
		if _, rows := sqlTable(t, env, sqlRequest{SQL: sql, From: allTime.from, To: allTime.to}); !slices.Equal(rows, wantSQL) {
			t.Errorf("%s: SQL %s\n want %v\n got  %v", when, sql, wantSQL, rows)
		}
	}
}

// sqlString is s as a SQL string literal.
func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func TestLateLogsStoredBeforeTheirDaysSetAreFoundByTheKeysItPromotes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	yesterdayAt := func(hour, minute int) time.Time { return at(hour, minute).AddDate(0, 0, -1) }

	// Yesterday's first Log is late, and waits in logs_late while a hot Log
	// makes yesterday's set, with user.
	env.Clock.Set(yesterdayAt(15, 30))
	postAndProcess(t, env, logJSON(t, "yan", yesterdayAt(13, 10), map[string]any{"user": "yan"}))
	postAndProcess(t, env, logJSON(t, "zed", yesterdayAt(15, 10), map[string]any{"user": "zed"}))
	assertEveryReadFinds(t, env, []fieldValue{
		{"user", "yan", []string{"yan"}},
		{"user", "zed", []string{"zed"}},
	}, map[string]map[string]any{
		"yan": {"user": "yan"},
		"zed": {"user": "zed"},
	}, "with yesterday's late Log beside its set")

	// Today's first Log is late too, and the export at 12:30 writes it to
	// hour 10's file before today has a set. Another late Log of hour 10
	// waits for a late file.
	env.Clock.Set(at(12, 30))
	postAndProcess(t, env, logJSON(t, "ada", at(10, 10), map[string]any{"user": "ada", "plan": "pro"}))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, at(10, 0), 1)
	postAndProcess(t, env, logJSON(t, "bea", at(10, 20), map[string]any{"user": "bea", "plan": "pro"}))

	// Then a hot Log makes today's set: user from yesterday's, and plan.
	postAndProcess(t, env, logJSON(t, "cy", at(12, 10), map[string]any{"user": "cy", "plan": "free"}))

	values := []fieldValue{
		{"user", "yan", []string{"yan"}},
		{"user", "zed", []string{"zed"}},
		{"user", "ada", []string{"ada"}},
		{"user", "bea", []string{"bea"}},
		{"user", "cy", []string{"cy"}},
		{"plan", "pro", []string{"bea", "ada"}},
		{"plan", "free", []string{"cy"}},
	}
	attributes := map[string]map[string]any{
		"yan": {"user": "yan"},
		"zed": {"user": "zed"},
		"ada": {"user": "ada", "plan": "pro"},
		"bea": {"user": "bea", "plan": "pro"},
		"cy":  {"user": "cy", "plan": "free"},
	}
	assertEveryReadFinds(t, env, values, attributes, "with today's set made after the file")

	env.Reopen(t)
	assertEveryReadFinds(t, env, values, attributes, "after a restart")

	// At 14:00 the export writes hour 12's file, and hour 10's late file
	// with today's set.
	env.Clock.Set(at(14, 0))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, at(10, 0), 2)
	assertHourFiles(t, env, 123, at(12, 0), 1)
	assertEveryReadFinds(t, env, values, attributes, "after hour 12 and the late file are exported")
}

func TestALateFileOfADayReRankedLaterIsFoundByTheKeysTheReRankPromotes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	yesterdayAt := func(hour, minute int) time.Time { return at(hour, minute).AddDate(0, 0, -1) }

	// Yesterday's set fills with k000…k099, then more Logs carry x000…x009,
	// so today's set is re-ranked: x000…x009 and k000…k089.
	env.Clock.Set(yesterdayAt(15, 0))
	postAndProcess(t, env, logJSON(t, "full", yesterdayAt(15, 0), numberedKeys("k", 100, "v")))
	for i := range 3 {
		postAndProcess(t, env, logJSON(t, fmt.Sprintf("common %d", i), yesterdayAt(15, i+1), numberedKeys("x", 10, "c")))
	}

	// Today's first Log is late, and the export at 12:30 writes it to hour
	// 10's file before the re-rank. Then a hot Log re-ranks.
	env.Clock.Set(at(12, 30))
	postAndProcess(t, env, logJSON(t, "ada", at(10, 10), map[string]any{"x000": "ada", "k000": "ada"}))
	env.ExportLogs(t)
	postAndProcess(t, env, logJSON(t, "cy", at(12, 10), map[string]any{"x000": "cy", "k000": "cy"}))

	values := []fieldValue{
		{"x000", "ada", []string{"ada"}},
		{"x000", "cy", []string{"cy"}},
		{"x000", "c", []string{"common 2", "common 1", "common 0"}},
		{"k000", "ada", []string{"ada"}},
		{"k000", "v", []string{"full"}},
	}
	attributes := map[string]map[string]any{
		"full":     numberedKeys("k", 100, "v"),
		"common 0": numberedKeys("x", 10, "c"),
		"common 1": numberedKeys("x", 10, "c"),
		"common 2": numberedKeys("x", 10, "c"),
		"ada":      {"x000": "ada", "k000": "ada"},
		"cy":       {"x000": "cy", "k000": "cy"},
	}
	assertEveryReadFinds(t, env, values, attributes, "with today's set re-ranked after the file")

	env.Clock.Set(at(14, 0))
	env.ExportLogs(t)
	assertEveryReadFinds(t, env, values, attributes, "after hour 12 is exported")
}
