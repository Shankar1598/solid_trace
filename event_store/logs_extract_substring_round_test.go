package main_test

import (
	"testing"
)

// seedMessageLogs stores three Logs whose messages hold values to take out:
// two flushes of a store's keys and a health check, at 10:10, 10:20 and
// 10:25. It leaves the clock at 10:30.
func seedMessageLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(10, 29))
	postAndProcess(t, env,
		logJSON(t, "flushed store_id=494 keys=12", languageAt(10, 10), map[string]any{"path": "/api/users"}),
		logJSON(t, "flushed store_id=7 keys=3", languageAt(10, 20), map[string]any{"path": "/health"}),
		logJSON(t, "health check ok", languageAt(10, 25), nil),
	)
	env.Clock.Set(languageAt(10, 30))
}

func TestExtractTakesWhatARegexsGroupMatched(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedMessageLogs(t, env)

	assertTables(t, env, map[string]tableCase{
		// Group 1, group 0 for the whole match, and no value without a match
		// or for a group the regex doesn't have.
		`sort by message asc | project message, id = extract("store_id=([0-9]+)", 1, message), ` +
			`whole = extract("store_id=[0-9]+", 0, message), none = extract("store_id=[0-9]+", 1, message), ` +
			`api = extract("^/api/([a-z]+)", 1, path)`: {
			"message,id,whole,none,api", []string{
				`["flushed store_id=494 keys=12","494","store_id=494",null,"users"]`,
				`["flushed store_id=7 keys=3","7","store_id=7",null,null]`,
				`["health check ok",null,null,null,null]`,
			}, false},
		`where isempty(extract("store_id=([0-9]+)", 1, message)) | project message`: {"message", []string{`["health check ok"]`}, false},
		// What no Log matched is one group.
		`summarize n = count() by store = extract("store_id=([0-9]+)", 1, message)`: {"store,n",
			[]string{`["494",1]`, `["7",1]`, `[null,1]`}, true},
		// toint gives a number to sum.
		`summarize keys = sum(toint(extract("keys=([0-9]+)", 1, message)))`: {"keys", []string{`[15]`}, false},
		`where toint(extract("keys=([0-9]+)", 1, message)) > 5 | project message`: {"message", []string{`["flushed store_id=494 keys=12"]`}, false},
	}, "once stored")
}

func TestSubstringCutsCharactersFromAZeroBasedIndex(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(languageAt(10, 29))
	postAndProcess(t, env,
		logJSON(t, "héllo wörld ✓", languageAt(10, 10), nil),
		logJSON(t, "123456", languageAt(10, 20), nil),
	)
	env.Clock.Set(languageAt(10, 30))

	assertTables(t, env, map[string]tableCase{
		// KQL's examples, on digits and on text that isn't ASCII.
		`sort by message asc | project rest = substring(message, 1), two = substring(message, 2, 2), ` +
			`first = substring(message, 0, 5), last = substring(message, -2, 2), end = substring(message, -1)`: {
			"rest,two,first,last,end", []string{
				`["23456","34","12345","56","6"]`,
				`["éllo wörld ✓","ll","héllo"," ✓","✓"]`,
			}, false},
		// A range past the end stops there, a start before the first
		// character starts at it, and no length gives nothing.
		`sort by message asc | project past = substring(message, 4, 100), beyond = substring(message, 20), ` +
			`before = substring(message, -20, 3), none = substring(message, 1, 0), negative = substring(message, 1, -1)`: {
			"past,beyond,before,none,negative", []string{
				`["56","","123","",""]`,
				`["o wörld ✓","","hél","",""]`,
			}, false},
		// The start and length may be worked out for each Log.
		`sort by message asc | project tail = substring(message, strlen(message) - 3), head = substring(message, 0, strlen(message) / 2)`: {
			"tail,head", []string{`["456","123"]`, `["d ✓","héllo "]`}, false},
		`summarize n = count() by short = substring(message, 0, 3)`: {"short,n", []string{`["123",1]`, `["hél",1]`}, true},
	}, "once stored")
}

func TestValidatePlacesMistakesInExtractSubstringAndRound(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	mistakes := map[string]queryProblem{
		// extract's regex and group are written in the Query.
		`project id = extract(service, 1, message)`: {Message: "extract needs its regex written in the Query, as a quoted string",
			Line: 1, Column: 22, EndLine: 1, EndColumn: 29},
		`project id = extract("x([0-9]+)", group, message)`: {Message: "extract needs its capture group written in the Query, as a whole number of 0 or more",
			Line: 1, Column: 35, EndLine: 1, EndColumn: 40},
		`project id = extract("x", -1, message)`: {Message: "extract needs its capture group written in the Query, as a whole number of 0 or more",
			Line: 1, Column: 27, EndLine: 1, EndColumn: 29},
		`project id = extract("x", 1.5, message)`: {Message: "extract needs its capture group written in the Query, as a whole number of 0 or more",
			Line: 1, Column: 27, EndLine: 1, EndColumn: 30},
		// The regex is checked as matches regex checks its pattern.
		`where extract("(", 1, message) == "a"`: {Message: "invalid regular expression: missing closing ): `(`",
			Line: 1, Column: 15, EndLine: 1, EndColumn: 18},
		`project id = extract("x", 1, 5)`: {Message: "extract takes a string, not a number", Line: 1, Column: 30, EndLine: 1, EndColumn: 31},
		// A wrong count says what the function takes.
		`project id = extract("x", 1)`: {Message: `extract takes a regex, a capture group and a string, as in extract("store_id=([0-9]+)", 1, message)`,
			Line: 1, Column: 14, EndLine: 1, EndColumn: 29},
		`project s = substring(message)`: {Message: "substring takes a string, a start and an optional length, as in substring(message, 0, 160)",
			Line: 1, Column: 13, EndLine: 1, EndColumn: 31},
		"project s = substring(message,\n  0, 1, 2)": {Message: "substring takes a string, a start and an optional length, as in substring(message, 0, 160)",
			Line: 1, Column: 13, EndLine: 2, EndColumn: 11},
		`project s = substring(message, "0")`: {Message: "substring's start is a number, not a string", Line: 1, Column: 32, EndLine: 1, EndColumn: 35},
	}
	for query, want := range mistakes {
		v := validateQuery(t, env, query)
		if len(v.Errors) != 1 || v.Errors[0] != want {
			t.Errorf("%q: expected %+v, got %+v", query, want, v.Errors)
		}
	}
}
