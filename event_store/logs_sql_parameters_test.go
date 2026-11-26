package main_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The Project's SQL user, read-only with readonly = 1 CONST, takes query
// parameters over the native protocol, and ClickHouse reads each value as
// the text form of its type.
func TestSQLUserTakesQueryParametersOfEachFilterType(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	cases := []struct {
		sql        string
		parameters map[string]string
		want       string
	}{
		{"SELECT {x:Array(String)}", map[string]string{"x": `['a','b']`}, `[[["a","b"]]]`},
		{"SELECT {x:Array(String)}", map[string]string{"x": `[]`}, `[[[]]]`},
		{"SELECT {x:Array(String)}", map[string]string{"x": `['it\'s','back\\slash']`}, `[[["it's","back\\slash"]]]`},
		{"SELECT {x:String}", map[string]string{"x": "a b"}, `[["a b"]]`},
		{"SELECT {x:String}", map[string]string{"x": `it's \ here`}, `[["it's \\ here"]]`},
		{"SELECT {x:String}", map[string]string{"x": ""}, `[[""]]`},
		{"SELECT {x:Nullable(String)}", map[string]string{"x": "a"}, `[["a"]]`},
		{"SELECT {x:Nullable(String)}", map[string]string{"x": `\N`}, `[[null]]`},
		{"SELECT 'r1' IN {x:Array(String)}", map[string]string{"x": `['r1','r2']`}, `[[1]]`},
	}
	for _, c := range cases {
		result, err := env.SQL.RunSQL(context.Background(), 123, allTime.from, allTime.to, c.sql, c.parameters, 10)
		if err != nil {
			t.Errorf("%s with %v: %v", c.sql, c.parameters, err)
			continue
		}
		if got, _ := json.Marshal(result.Rows); string(got) != c.want {
			t.Errorf("%s with %v: want %s, got %s", c.sql, c.parameters, c.want, got)
		}
	}
}

// The SQL a Dashboard's SQL Chart saves: a select of services, a search box
// and an optional level, each written to match every Log at All.
const filteredSQL = `SELECT message FROM logs
	WHERE (empty({service:Array(String)}) OR service IN {service:Array(String)})
	AND ({q:String} = '' OR positionCaseInsensitive(attributes.path.:String, {q:String}) > 0)
	AND ({level:Nullable(String)} IS NULL OR level = {level:Nullable(String)})
	-- {not_a_filter:UInt8} is a comment
	AND message != '{nor:Date}'
	ORDER BY time`

func TestSQLNarrowsByTheFiltersItNamesAndAllMatchesEveryLog(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	seedLanguageLogs(t, env)

	cases := []struct {
		parameters map[string][]string
		want       string
	}{
		{nil, "di ada bob cy ed"},
		{map[string][]string{"service": {}, "q": {}, "level": {}}, "di ada bob cy ed"},
		{map[string][]string{"service": {"api"}}, "ada cy ed"},
		{map[string][]string{"service": {"web", "worker"}}, "di bob"},
		{map[string][]string{"q": {"API/users"}}, "ada ed"},
		{map[string][]string{"q": {"api", "users"}}, ""},
		{map[string][]string{"level": {"error"}, "service": {"api"}}, "cy ed"},
		{map[string][]string{"service": {"it's \\ no service"}}, ""},
		{map[string][]string{"other": {"x"}}, "di ada bob cy ed"},
	}
	for _, c := range cases {
		status, raw := runSQL(t, env, 123, sqlRequest{SQL: filteredSQL, From: allTime.from, To: allTime.to, Parameters: c.parameters})
		if status != 200 {
			t.Fatalf("%v: expected 200, got %d: %s", c.parameters, status, raw)
		}
		var answer struct {
			Table struct {
				Rows [][]string `json:"rows"`
			} `json:"table"`
			Parameters []string `json:"parameters"`
		}
		if err := json.Unmarshal(raw, &answer); err != nil {
			t.Fatalf("Decode %s: %v", raw, err)
		}
		var messages []string
		for _, row := range answer.Table.Rows {
			messages = append(messages, row[0])
		}
		if got := strings.Join(messages, " "); got != c.want {
			t.Errorf("%v: want %q, got %q", c.parameters, c.want, got)
		}
		if !slices.Equal(answer.Parameters, []string{"service", "q", "level"}) {
			t.Errorf("Expected the filters the SQL names, got %v", answer.Parameters)
		}
	}
}

func TestSQLNamingAFilterWithAnotherTypeOrTooManyValuesIsRefused(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	cases := map[string]sqlRequest{
		"{n:UInt32}: a Dashboard filter's type must be Array(String), String or Nullable(String)": {SQL: "SELECT {n:UInt32}"},
		"n is named as String and as Array(String)":                                               {SQL: "SELECT {n:String}, {n:Array(String)}"},
		"parameters: q has more than 100 values": {
			SQL: "SELECT {q:Array(String)}", Parameters: map[string][]string{"q": make([]string, 101)},
		},
	}
	for want, req := range cases {
		req.From, req.To = allTime.from, allTime.to
		status, raw := runSQL(t, env, 123, req)
		if status != 400 || !strings.Contains(string(raw), want) {
			t.Errorf("%s: expected 400 with %q, got %d: %s", req.SQL, want, status, raw)
		}
	}
}
