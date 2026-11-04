package main_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// seedJSONLogs stores Logs whose messages are JSON, but di's, each named by
// its name Attribute.
func seedJSONLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(11, 29))
	named := func(name string) map[string]any { return map[string]any{"name": name} }
	postAndProcess(t, env,
		logJSON(t, `{"resource_type":"product","result":{"cache_keys_expired":3},"store_id":494,"ids":[7,8,9],"ok":true,`+
			`"store-id":"s-1","it's.$[0]":1,"a\\b":2}`,
			languageAt(10, 10), map[string]any{"name": "ada", "tags": map[string]any{"store_id": 7}}),
		logJSON(t, `{"resource_type":"product","result":{"cache_keys_expired":"2"},"store_id":"494","ids":[],"ok":"true"}`,
			languageAt(10, 20), map[string]any{"name": "bob", "codes": []any{1, 2}}),
		logJSON(t, `{"resource_type":"category","result":{"cache_keys_expired":5,"keys":["a","b"]},"ids":"7 8"}`,
			languageAt(10, 30), named("cy")),
		logJSON(t, `not json: {"resource_type":"product"}`, languageAt(10, 40), named("di")),
		logJSON(t, `[ {"id": 1}, {"id": 2} ]`, languageAt(10, 50), named("ed")),
	)
}

func TestParseJSONReadsAMessagesMembersAsAnAttributeIsRead(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedJSONLogs(t, env)

	assertTables(t, env, map[string]tableCase{
		// A nested member, summed: bob's "2" is read as a number by toint.
		`summarize expired = sum(toint(parse_json(message).result.cache_keys_expired)) by tostring(parse_json(message).resource_type)`: {
			"resource_type,expired", []string{`["product",5]`, `["category",5]`, `[null,null]`}, true},
		`summarize count() by tostring(parse_json(message).resource_type)`: {
			"resource_type,count_", []string{`["product",2]`, `["category",1]`, `[null,2]`}, true},
		`summarize count() by parse_json(message).resource_type`: {
			"resource_type,count_", []string{`["product",2]`, `["category",1]`, `[null,2]`}, true},
		// As a Chart over time, in bins.
		`summarize n = avg(parse_json(message).result.cache_keys_expired) by bin(time, 1h), tostring(parse_json(message).resource_type)`: {
			"time,resource_type,n", []string{`["2026-10-02T10:00:00Z","product",3]`, `["2026-10-02T10:00:00Z","category",5]`, `["2026-10-02T10:00:00Z",null,null]`}, true},
	}, "once stored")
}

func TestPathsReachMembersAndElementsAndGiveNoValueWhereThereIsNone(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedJSONLogs(t, env)

	for condition, want := range map[string][]string{
		// A member of each type matches a value of its own type.
		`parse_json(message).store_id == 494`:                       {"ada"},
		`parse_json(message).store_id == "494"`:                     {"bob"},
		`494 == parse_json(message).store_id`:                       {"ada"},
		`parse_json(message).store_id != 494`:                       {"bob", "cy", "di", "ed"},
		`parse_json(message).store_id in (494, "494")`:              {"ada", "bob"},
		`parse_json(message).ok == true`:                            {"ada"},
		`parse_json(message).ok == "true"`:                          {"bob"},
		`parse_json(message).resource_type == "category"`:           {"cy"},
		`parse_json(message).result.cache_keys_expired > 2`:         {"ada", "cy"},
		`toint(parse_json(message).result.cache_keys_expired) >= 2`: {"ada", "bob", "cy"},
		// Brackets, and elements from the start and from the end.
		`parse_json(message)["store-id"] == "s-1"`:           {"ada"},
		`parse_json(message)['resource_type'] == "category"`: {"cy"},
		`parse_json(message).ids[0] == 7`:                    {"ada"},
		`parse_json(message).ids[-1] == 9`:                   {"ada"},
		`parse_json(message)[0].id == 1`:                     {"ed"},
		`parse_json(message)[-1]["id"] == 2`:                 {"ed"},
		`parse_json(message).result.keys[1] == "b"`:          {"cy"},
		// No value: past the end, a missing member, a message that isn't
		// JSON, a member of what isn't an object, an element of what isn't
		// an array.
		`isnotempty(parse_json(message).ids[2])`:             {"ada"},
		`isnotempty(parse_json(message).ids[3])`:             {},
		`isnotempty(parse_json(message).ids[0])`:             {"ada"},
		`isnotempty(parse_json(message).missing)`:            {},
		`isempty(parse_json(message).missing)`:               {"ada", "bob", "cy", "di", "ed"},
		`isnotempty(parse_json(message).resource_type)`:      {"ada", "bob", "cy"},
		`isnotempty(parse_json(message).resource_type.kind)`: {},
		`isnotempty(parse_json(message).result[0])`:          {},
		// The path is data: a name's quotes, dots, $ and brackets are its own.
		`parse_json(message)["it's.$[0]"] == 1`: {"ada"},
		`parse_json(message)["a\\b"] == 2`:      {"ada"},
		// A dotted name on its own is still an Attribute's, and an Attribute
		// that holds an array is reached into too.
		`tags.store_id == 7`:       {"ada"},
		`codes[1] == 2`:            {"bob"},
		`array_length(codes) == 2`: {"bob"},
	} {
		if got := namesWhere(t, env, condition); !slices.Equal(got, want) {
			t.Errorf("where %s: expected %v, got %v", condition, want, got)
		}
	}

	assertTables(t, env, map[string]tableCase{
		// Each value as it is, and a message that isn't JSON as itself.
		`where name == "ada" | project ids = parse_json(message).ids, ok = parse_json(message).ok, ` +
			`id = parse_json(message).store_id, type = parse_json(message).resource_type, none = parse_json(message).missing`: {
			"ids,ok,id,type,none", []string{`[[7,8,9],true,494,"product",null]`}, false},
		`where name in ("di", "ed") | sort by name asc | project name, text = tostring(parse_json(message)), first = parse_json(message)[0]`: {
			"name,text,first", []string{`["di","not json: {\"resource_type\":\"product\"}",null]`, `["ed","[{\"id\":1},{\"id\":2}]",{"id":1}]`}, false},
		// array_length is an array's length, and no value for anything else.
		`sort by name asc | project name, n = array_length(parse_json(message).ids), all = array_length(parse_json(message))`: {
			"name,n,all", []string{`["ada",3,null]`, `["bob",0,null]`, `["cy",null,null]`, `["di",null,null]`, `["ed",null,2]`}, false},
		`summarize ids = sum(array_length(parse_json(message).ids)), stores = dcount(parse_json(message).store_id)`: {
			"ids,stores", []string{`[3,2]`}, false},
	}, "once stored")
}

func TestParseJSONOfMessageOverABusyWeekIsRefusedAndRunsOnAFileThatFits(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedBusyProject(t, env, 123, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)
	cron := map[string]any{"service": "worker", "file": "cron.log"}
	postAndProcessTo(t, env, 123, "test_public_key",
		logJSON(t, `{"resource_type":"product","store_id":494,"result":{"cache_keys_expired":3}}`, testNow.Add(-time.Hour), cron),
		logJSON(t, `{"resource_type":"product","result":{"cache_keys_expired":2}}`, testNow.Add(-2*time.Hour), cron))
	search := refusalOf(t, checkedRun(t, env, `search "timeout"`, lastWeek.from, lastWeek.to, nil))

	for _, c := range []struct {
		query string
		// found is what the Query's answer holds on cron.log.
		found string
	}{
		{`summarize expired = sum(toint(parse_json(message).result.cache_keys_expired)) by tostring(parse_json(message).resource_type)`, `["product",5]`},
		{`where parse_json(message).store_id == 494 | project name = parse_json(message)["resource_type"]`, `["product"]`},
	} {
		// Over the week it reads as much as a search would, and is refused.
		refusal := refusalOf(t, checkedRun(t, env, c.query, lastWeek.from, lastWeek.to, nil))
		if e := refusal.Estimate; e != search.Estimate || !e.TextMatch || !e.OverBudget {
			t.Errorf("%s: expected the estimate of a search over the week, %+v, got %+v", c.query, search.Estimate, e)
		}
		narrower := refusal.NarrowerQuery
		if narrower == nil {
			t.Fatalf("%s: expected a narrower Query, got %+v", c.query, refusal)
		}
		answer := checkedRun(t, env, narrower.Query, narrower.From, narrower.To, nil)
		ran(t, "the narrower Query "+narrower.Query, answer)
		if !strings.Contains(string(answer["table"]), c.found) {
			t.Errorf("%s: expected the narrower Query %q to find %s, got %s", c.query, narrower.Query, c.found, mustMarshal(t, answer))
		}

		// On cron.log, whose week fits, it runs.
		onFile := `where file == "cron.log" | ` + c.query
		answer = checkedRun(t, env, onFile, lastWeek.from, lastWeek.to, nil)
		ran(t, onFile, answer)
		if !strings.Contains(string(answer["table"]), c.found) {
			t.Errorf("%s: expected %s, got %s", onFile, c.found, mustMarshal(t, answer))
		}
	}
}

// namesWhere runs where condition over all time and returns the names of the
// Logs it keeps, in order.
func namesWhere(t *testing.T, env *TestEnv, condition string) []string {
	t.Helper()
	_, rows := runTable(t, env, "where "+condition+" | sort by name asc | project name")
	names := []string{}
	for _, row := range rows {
		var cells []string
		if err := json.Unmarshal([]byte(row), &cells); err != nil || len(cells) != 1 {
			t.Fatalf("where %s: expected a row of one name, got %s", condition, row)
		}
		names = append(names, cells[0])
	}
	return names
}

func TestValidatePlacesMistakesInPathsAndJSONFunctions(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	mistakes := map[string]queryProblem{
		// .name and […] only follow a dynamic value.
		`where strlen(message).a == 1`: {Message: `can't reach into a number; .name and […] follow a dynamic value, such as parse_json(message)`,
			Line: 1, Column: 22, EndLine: 1, EndColumn: 24},
		`where message["a"] == 1`: {Message: `can't reach into a string; .name and […] follow a dynamic value, such as parse_json(message)`,
			Line: 1, Column: 14, EndLine: 1, EndColumn: 19},
		`where tostring(parse_json(message)).a == 1`: {Message: `can't reach into a string; .name and […] follow a dynamic value, such as parse_json(message)`,
			Line: 1, Column: 36, EndLine: 1, EndColumn: 38},
		// An index is a whole number or a name, written in the Query.
		`where parse_json(message)[x] == 1`: {Message: `expected an index written in the Query: a whole number, such as [0], or a name in quotes, such as ["store-id"]`,
			Line: 1, Column: 27, EndLine: 1, EndColumn: 28},
		`where parse_json(message)[1.5] == 1`: {Message: `expected an index written in the Query: a whole number, such as [0], or a name in quotes, such as ["store-id"]`,
			Line: 1, Column: 27, EndLine: 1, EndColumn: 30},
		// The functions' arguments.
		`where parse_json(5).a == 1`: {Message: `parse_json takes a string, such as message, not a number`,
			Line: 1, Column: 18, EndLine: 1, EndColumn: 19},
		`where array_length(message) > 1`: {Message: `array_length takes a dynamic value, such as parse_json(message).ids, not a string`,
			Line: 1, Column: 20, EndLine: 1, EndColumn: 27},
		// What follows a value.
		`where parse_json(message). == 1`: {Message: `expected a name after the dot; write other names in brackets, as in ["store-id"]`,
			Line: 1, Column: 26, EndLine: 1, EndColumn: 27},
		`where parse_json(message)[] == 1`: {Message: `expected an index or a name in quotes after [, as in [0] or ["store-id"]`,
			Line: 1, Column: 26, EndLine: 1, EndColumn: 27},
		`summarize count() by parse_json(message)[0`: {Message: `expected ], got the end of the Query`,
			Line: 1, Column: 43, EndLine: 1, EndColumn: 43},
	}
	for query, want := range mistakes {
		v := validateQuery(t, env, query)
		if len(v.Errors) != 1 || v.Errors[0] != want {
			t.Errorf("%q: expected %+v, got %+v", query, want, v.Errors)
		}
	}

	// A wrong count of arguments, at the call.
	for query, want := range map[string]queryProblem{
		`where parse_json(message, 1) == 1`: {Line: 1, Column: 7, EndLine: 1, EndColumn: 29},
		`where parse_json() == 1`:           {Line: 1, Column: 7, EndLine: 1, EndColumn: 19},
		`where array_length() == 1`:         {Line: 1, Column: 7, EndLine: 1, EndColumn: 21},
	} {
		v := validateQuery(t, env, query)
		if len(v.Errors) != 1 {
			t.Fatalf("%q: expected a mistake, got %+v", query, v.Errors)
		}
		got := v.Errors[0]
		name, _, _ := strings.Cut(strings.TrimPrefix(query, "where "), "(")
		place := queryProblem{Line: got.Line, Column: got.Column, EndLine: got.EndLine, EndColumn: got.EndColumn}
		if !strings.HasPrefix(got.Message, name+" takes") || place != want {
			t.Errorf("%q: expected %q at %+v, got %+v", query, name+" takes …", want, got)
		}
	}

	// Queries with them are valid, and the Builder can't show them.
	for _, query := range []string{
		`where parse_json(message).store_id == 494`,
		`where tags.store_id == 7 and ['tags'][0] == 1`,
		`summarize count() by tostring(parse_json(message)["resource-type"])`,
		`extend j = parse_json(message) | where j["a"].b[-1] == 2 | project n = array_length(j["ids"])`,
	} {
		if v := validateQuery(t, env, query); len(v.Errors) != 0 || builderChips(v) != "null" {
			t.Errorf("%q: expected no chips and no errors, got %+v", query, v)
		}
	}
}
