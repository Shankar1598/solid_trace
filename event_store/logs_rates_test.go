package main_test

import (
	"encoding/json"
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

func TestRateAndPeakRateCountRequestsPerSecondOfEachBin(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedRateLogs(t, env)

	search := `where path == "/v2/search" | `
	cases := map[string]tableCase{
		// A bin's rate is its count over its seconds in the range, empty
		// seconds too, so 10:01's is 0.2 though its two busy seconds
		// average 6; its peak is its busiest second's count.
		search + `summarize count(), rate(), peak_rate() by bin(time, 1m)`: {"time,count_,rate_,peak_rate_", []string{
			`["2026-10-02T10:00:00Z",3,0.1,2]`,
			`["2026-10-02T10:01:00Z",12,0.2,6]`,
			`["2026-10-02T10:02:00Z",15,0.25,9]`,
			`["2026-10-02T10:03:00Z",3,0.2,3]`,
		}, false},
		// Within groups, rates add up to the bin's and peaks don't: a and
		// b each peak at 5, in different seconds.
		search + `summarize rate(), peak_rate() by bin(time, 1m), shop`: {"time,shop,rate_,peak_rate_", []string{
			`["2026-10-02T10:00:00Z","c",0.1,2]`,
			`["2026-10-02T10:01:00Z","a",0.1,5]`,
			`["2026-10-02T10:01:00Z","b",0.1,5]`,
			`["2026-10-02T10:02:00Z","c",0.25,9]`,
			`["2026-10-02T10:03:00Z","c",0.2,3]`,
		}, false},
		search + `summarize per_minute = rate() * 60 by bin(time, 1m)`: {"time,per_minute", []string{
			`["2026-10-02T10:00:00Z",6]`, `["2026-10-02T10:01:00Z",12]`, `["2026-10-02T10:02:00Z",15]`, `["2026-10-02T10:03:00Z",12]`,
		}, false},
		// With no time bin, the bin is the range: 33 requests in 165s.
		search + `summarize count(), rate(), peak_rate()`:       {"count_,rate_,peak_rate_", []string{`[33,0.2,9]`}, false},
		`where path == "/none" | summarize rate(), peak_rate()`: {"rate_,peak_rate_", []string{`[0,0]`}, false},
	}
	assertTablesIn(t, env, cases, rateRange.from, rateRange.to, "in the hot table")

	// The chart's hints, and its totals over the whole range.
	query := search + `summarize rate(), peak_rate() by bin(time, 1m)`
	if got, want := hintsOf(runCharted(t, env, query, rateRange.from, rateRange.to)), "time[60s] rate_[rate] peak_rate_[peak_rate], fill"; got != want {
		t.Errorf("%s\n want %s\n got  %s", query, want, got)
	}
	status, raw := postQueryApp(t, env, "/api/123/logs/query", map[string]any{"query": query, "from": rateRange.from, "to": rateRange.to, "totals": true})
	var answer totalsTable
	if err := json.Unmarshal(raw, &answer); status != 200 || err != nil {
		t.Fatalf("Query %q expected totals, got %d: %s", query, status, raw)
	}
	if got, want := totalsOf(answer), "rate_[rate] peak_rate_[peak_rate]: [0.2,9]"; got != want {
		t.Errorf("Totals of %s\n want %s\n got  %s", query, want, got)
	}

	// Hour 10 goes to a file.
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, rateAt(0, 0, 0), 1)
	assertTablesIn(t, env, cases, rateRange.from, rateRange.to, "after the export")
}

func TestRatesNeedBinsOfTheLogsTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	mistakes := map[string]queryProblem{
		`summarize peak_rate() by bin(time, 500ms)`: {Message: `peak_rate() needs time bins of whole seconds, not 500ms`, Line: 1, Column: 11, EndLine: 1, EndColumn: 22},
		`summarize rate(), peak_rate() by service, bin(time, 1500ms)`: {
			Message: `peak_rate() needs time bins of whole seconds, not 1.5s`, Line: 1, Column: 19, EndLine: 1, EndColumn: 30},
		`summarize n = count() by bin(time, 1m) | summarize peak_rate()`: {
			Message: `peak_rate() counts rows in each second of the Logs' own time, which these rows don't have`, Line: 1, Column: 52, EndLine: 1, EndColumn: 63},
		`extend time = time | summarize n = count() by bin(time, 1m) | summarize peak_rate()`: {
			Message: `peak_rate() counts rows in each second of the Logs' own time, which these rows don't have`, Line: 1, Column: 73, EndLine: 1, EndColumn: 84},
		// rate() divides by the seconds of a bin of the Logs' own time, so
		// a bin of another time is refused, not read as the whole range.
		`extend time = time - 1h | summarize rate() by bin(time, 1m)`: {
			Message: `rate() needs a time bin of the Logs' own time, such as bin(time, 1m)`, Line: 1, Column: 37, EndLine: 1, EndColumn: 43},
		`extend t = time - 1h | summarize n = rate() by bin(t, 1m), service`: {
			Message: `rate() needs a time bin of the Logs' own time, such as bin(time, 1m)`, Line: 1, Column: 38, EndLine: 1, EndColumn: 44},
		`summarize rate(path)`: {
			Message: `rate takes no arguments; filter with where first, as in where path == "/" | summarize rate()`, Line: 1, Column: 11, EndLine: 1, EndColumn: 21},
	}
	for query, want := range mistakes {
		v := validateQuery(t, env, query)
		if len(v.Errors) != 1 || v.Errors[0] != want {
			t.Errorf("%q: expected %+v, got %+v", query, want, v.Errors)
		}
	}
	for _, query := range []string{
		`summarize peak_rate() by bin(time, 1s)`,
		`summarize peak_rate() by bin(time, 5m), service`,
		`summarize peak_rate() by bin_auto(time)`,
		`extend t = bin(time, 1m) | summarize peak_rate() by t`,
		`summarize rate() by bin(time, 500ms)`,
		`summarize n = count() by bin(time, 1m) | summarize rate()`,
		`summarize n = count() by bin(time, 1m) | summarize rate() by time`,
		`extend t = time - 1h | summarize rate() by bin(time, 1m), bin(t, 1h)`,
		// After distinct, peak_rate() counts the rows in each second.
		`distinct time, shop | summarize peak_rate() by bin(time, 1m)`,
	} {
		if v := validateQuery(t, env, query); len(v.Errors) != 0 {
			t.Errorf("%q: expected no errors, got %+v", query, v.Errors)
		}
	}
}

func TestTheBuilderShowsRates(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	query := `where path == "/v2/search" | summarize rate(), peak_rate() by bin_auto(time), shop`
	want := `{"filters":[{"field":"path","op":"==","value":"/v2/search"}],"search":null,` +
		`"summarize":{"aggregates":[{"fn":"rate"},{"fn":"peak_rate"}],"by":[{"field":"time","bin":"auto"},{"field":"shop"}]}}`
	if got := builderChips(validateQuery(t, env, query)); got != want {
		t.Errorf("%s\n want %s\n got  %s", query, want, got)
	}
	if got := builderChips(validateQuery(t, env, `summarize rate() by shop | where rate_ > 1`)); got != "null" {
		t.Errorf("Expected no chips for a where after the summarize, got %s", got)
	}
}
