package main_test

import (
	"context"
	"encoding/json"
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
