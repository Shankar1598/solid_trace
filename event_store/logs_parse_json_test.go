package main_test

import (
	"encoding/json"
	"testing"
)

// seedJSONLogs stores Logs whose messages are JSON, but di's, each named by
// its name Attribute.
func seedJSONLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	env.Clock.Set(languageAt(11, 29))
	named := func(name string) map[string]any { return map[string]any{"name": name} }
	postAndProcess(t, env,
		logJSON(t, `{"resource_type":"product","result":{"cache_keys_expired":3},"store_id":494,"ids":[7,8,9],"ok":true}`,
			languageAt(10, 10), named("ada")),
		logJSON(t, `{"resource_type":"product","result":{"cache_keys_expired":"2"},"store_id":"494","ids":[],"ok":"true"}`,
			languageAt(10, 20), named("bob")),
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
	}, "once stored")
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
