package main_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// rateAt is a time on October 2nd, 2026, from 10:00.
func rateAt(minute, second, ms int) time.Time {
	return time.Date(2026, 10, 2, 10, minute, second, ms*int(time.Millisecond), time.UTC)
}

// rateRange is the range the rate tests read: 10:00:30 to 10:03:15, so the
// 1m bins of 10:00 and 10:03 lie half and a quarter in it.
var rateRange = struct{ from, to time.Time }{from: rateAt(0, 30, 0), to: rateAt(3, 15, 0)}

// seedRateLogs stores requests to /v2/search from shops a, b and c:
//   - 10:00, 30s in the range: 2 at 10:00:40 and 1 at 10:00:41, from c;
//   - 10:01: at 10:01:05, 1 from a and 5 from b; at 10:01:30, 5 from a and
//     1 from b; so no second but two has a request;
//   - 10:02: 4 at 10:02:07.100, 5 at 10:02:07.900 and 6 at 10:02:08, from c;
//   - 10:03, 15s in the range: 3 at 10:03:10, from c.
//
// One request before the range and one after it, and a health check in it,
// are never counted.
func seedRateLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	var logs []string
	add := func(n int, at time.Time, shop string) {
		for range n {
			logs = append(logs, logJSON(t, "request", at, map[string]any{"path": "/v2/search", "shop": shop}))
		}
	}
	add(1, rateAt(0, 10, 0), "c")
	add(2, rateAt(0, 40, 0), "c")
	add(1, rateAt(0, 41, 0), "c")
	add(1, rateAt(1, 5, 0), "a")
	add(5, rateAt(1, 5, 0), "b")
	add(5, rateAt(1, 30, 0), "a")
	add(1, rateAt(1, 30, 0), "b")
	add(4, rateAt(2, 7, 100), "c")
	add(5, rateAt(2, 7, 900), "c")
	add(6, rateAt(2, 8, 0), "c")
	add(3, rateAt(3, 10, 0), "c")
	add(1, rateAt(3, 20, 0), "c")
	logs = append(logs, logJSON(t, "health", rateAt(1, 5, 0), map[string]any{"path": "/health", "shop": "a"}))
	postAndProcess(t, env, logs...)
}

// rateTable runs a Query over [from, to) and returns its table's column
// names, joined by commas, and its rows as JSON.
func rateTable(t *testing.T, env *TestEnv, query string, from, to time.Time) (string, []string) {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: query, From: from, To: to})
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
	}
	var answer tableAnswer
	if err := json.Unmarshal(raw, &answer); err != nil || answer.Table == nil {
		t.Fatalf("Query %q expected a table, got %s (%v)", query, raw, err)
	}
	var columns, rows []string
	for _, col := range answer.Table.Columns {
		columns = append(columns, col.Name)
	}
	for _, row := range answer.Table.Rows {
		rows = append(rows, string(row))
	}
	return strings.Join(columns, ","), rows
}

// assertRates runs each Query over rateRange and checks its table.
func assertRates(t *testing.T, env *TestEnv, cases map[string]tableCase, when string) {
	t.Helper()
	for query, want := range cases {
		columns, rows := rateTable(t, env, query, rateRange.from, rateRange.to)
		if columns != want.columns || !slices.Equal(rows, want.rows) {
			t.Errorf("%s: %s\n want %s %v\n got  %s %v", when, query, want.columns, want.rows, columns, rows)
		}
	}
}

func TestRateAndPeakRateCountRequestsPerSecondOfEachBin(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedRateLogs(t, env)

	search := `where path == "/v2/search" | `
	cases := map[string]tableCase{
		// A bin's rate is its count over its seconds in the range, empty
		// seconds too; its peak, its busiest second's count.
		search + `summarize count(), rate(), peak_rate() by bin(time, 1m)`: {"time,count_,rate_,peak_rate_", []string{
			`["2026-10-02T10:00:00Z",3,0.1,2]`,
			`["2026-10-02T10:01:00Z",12,0.2,6]`,
			`["2026-10-02T10:02:00Z",15,0.25,9]`,
			`["2026-10-02T10:03:00Z",3,0.2,3]`,
		}, false},
	}
	assertRates(t, env, cases, "in the hot table")
}
