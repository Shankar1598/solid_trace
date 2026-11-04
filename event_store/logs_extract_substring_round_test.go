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
