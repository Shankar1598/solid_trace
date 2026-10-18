package main_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// chartedTable is a run's table with what it says about charting it.
type chartedTable struct {
	Table struct {
		Columns []struct {
			Name       string  `json:"name"`
			Agg        string  `json:"agg"`
			BinSeconds float64 `json:"bin_seconds"`
		} `json:"columns"`
		Rows      []json.RawMessage `json:"rows"`
		Truncated bool              `json:"truncated"`
		FillGaps  *bool             `json:"fill_gaps"`
		From      string            `json:"from"`
		To        string            `json:"to"`
	} `json:"table"`
}

// runCharted runs a Query that returns a table over [from, to) for Project
// 123.
func runCharted(t *testing.T, env *TestEnv, query string, from, to time.Time) chartedTable {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: query, From: from, To: to})
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
	}
	var answer chartedTable
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode table %q: %v", raw, err)
	}
	if answer.Table.FillGaps == nil {
		t.Fatalf("Query %q expected a table with fill_gaps, got %s", query, raw)
	}
	return answer
}

// hintsOf writes a table's columns with their hints, as "time[300s]
// service count_[count]", and whether it fills gaps, as "fill" or "no fill".
func hintsOf(answer chartedTable) string {
	var columns []string
	for _, col := range answer.Table.Columns {
		switch {
		case col.Agg != "" && col.BinSeconds != 0:
			columns = append(columns, fmt.Sprintf("%s[%s %vs]", col.Name, col.Agg, col.BinSeconds))
		case col.Agg != "":
			columns = append(columns, fmt.Sprintf("%s[%s]", col.Name, col.Agg))
		case col.BinSeconds != 0:
			columns = append(columns, fmt.Sprintf("%s[%vs]", col.Name, col.BinSeconds))
		default:
			columns = append(columns, col.Name)
		}
	}
	fill := "no fill"
	if *answer.Table.FillGaps {
		fill = "fill"
	}
	return strings.Join(columns, " ") + ", " + fill
}

// assertHints runs each Query over all time and checks its columns' hints
// and fill_gaps.
func assertHints(t *testing.T, env *TestEnv, cases map[string]string) {
	t.Helper()
	for query, want := range cases {
		if got := hintsOf(runCharted(t, env, query, allTime.from, allTime.to)); got != want {
			t.Errorf("%s\n want %s\n got  %s", query, want, got)
		}
	}
}

func TestASummarizeByATimeBinSaysHowToChartIt(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	assertHints(t, env, map[string]string{
		`summarize count() by bin(time, 5m), service`: "time[300s] service count_[count], fill",
		`summarize count() by floor(time, 1m)`:        "time[60s] count_[count], fill",
		`summarize n = count(), errors = countif(level == "error"), users = dcount(user), sum(duration), avg(duration), ` +
			`min(duration), max(duration), percentile(duration, 95) by bin(time, 1h), level`: "time[3600s] level n[count] errors[countif] " +
			"users[dcount] sum_duration[sum] avg_duration[avg] min_duration[min] max_duration[max] percentile_duration_95[percentile], fill",
		// The first time bin is the time axis; another is a group.
		`summarize count() by hour = bin(time, 1h), day = bin(time, 1d)`: "hour[3600s] day count_[count], fill",
		// With no time bin there are no bins to fill.
		`summarize count() by service`:              "service count_[count], no fill",
		`summarize count() by bin(duration, 100)`:   "duration count_[count], no fill",
		`summarize count()`:                         "count_[count], no fill",
		`summarize last = max(time) by service`:     "service last[max], no fill",
		`project message, duration`:                 "message duration, no fill",
		`where duration > 100 | project message`:    "message, no fill",
		`top 1 by duration | project message, time`: "message time, no fill",
	})

	// bin_auto's size is the one it picks for the range: 30 minutes for a
	// day.
	day := runCharted(t, env, `summarize count() by service, bin_auto(time)`, languageAt(0, 0), languageAt(0, 0).AddDate(0, 0, 1))
	if got, want := hintsOf(day), "service time[1800s] count_[count], fill"; got != want {
		t.Errorf("bin_auto over a day: want %s, got %s", want, got)
	}
	twoHours := runCharted(t, env, `summarize count() by bin_auto(time)`, languageAt(10, 0), languageAt(12, 0))
	if got, want := hintsOf(twoHours), "time[300s] count_[count], fill"; got != want {
		t.Errorf("bin_auto over two hours: want %s, got %s", want, got)
	}

	// The range is the one the Query reads, as a leading where on time
	// narrows it: two hours of the day have 5-minute bins, ada's, bob's,
	// cy's and ed's.
	narrowed := runCharted(t, env, `where time >= datetime(2026-10-02T10:00:00Z) and time < datetime(2026-10-02T12:00:00Z) `+
		`| summarize count() by bin_auto(time)`, languageAt(0, 0), languageAt(0, 0).AddDate(0, 0, 1))
	if got, want := hintsOf(narrowed), "time[300s] count_[count], fill"; got != want || len(narrowed.Table.Rows) != 4 {
		t.Errorf("bin_auto over two hours of a day: want %s in 4 rows, got %s in %d", want, got, len(narrowed.Table.Rows))
	}
	// The last hour and a half of a week, with the clock at 11:30, has
	// 1-minute bins.
	lastHour := runCharted(t, env, `where time > ago(1h) | summarize count() by bin_auto(time)`,
		languageAt(12, 0).AddDate(0, 0, -7), languageAt(12, 0))
	if got, want := hintsOf(lastHour), "time[60s] count_[count], fill"; got != want {
		t.Errorf("bin_auto after ago(1h) in a week: want %s, got %s", want, got)
	}
}

func TestHintsSurviveOnlyPlainRenames(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	byHour := `summarize count() by bin(time, 1h), service`
	assertHints(t, env, map[string]string{
		byHour + ` | project t = time, s = service, n = count_`:               "t[3600s] s n[count], fill",
		byHour + ` | project count_, time`:                                    "count_[count] time[3600s], fill",
		byHour + ` | extend total = count_`:                                   "time[3600s] service count_[count] total[count], fill",
		byHour + ` | extend count_ = count_ * 2`:                              "time[3600s] service count_, fill",
		byHour + ` | extend time = tostring(time)`:                            "service count_[count] time, no fill",
		byHour + ` | project day = bin(time, 1d), count_`:                     "day count_[count], no fill",
		byHour + ` | project service, count_`:                                 "service count_[count], no fill",
		byHour + ` | extend n = count_, count_ = 1 | project time, n, count_`: "time[3600s] n[count] count_, fill",
		// A table has one time axis: the first column of time bins. A copy
		// of it is a time like any other.
		byHour + ` | extend t = time`:                           "time[3600s] service count_[count] t, fill",
		byHour + ` | project a = time, b = time, count_`:        "a[3600s] b count_[count], fill",
		byHour + ` | project count_, t = time, time`:            "count_[count] t[3600s] time, fill",
		byHour + ` | extend t = time | project t, time, count_`: "t[3600s] time count_[count], fill",
		// An aggregate inside arithmetic is no longer one.
		`summarize n = count() * 2, avg(duration) by bin(time, 1h)`: "time[3600s] n avg_duration[avg], fill",
		`summarize n = -max(duration) by bin(time, 1h)`:             "time[3600s] n, fill",
		// The last summarize gives the hints; a column of time bins it groups
		// by, as it is, stays one.
		`summarize n = count() by bin(time, 5m), service | summarize total = sum(n) by time`: "time[300s] total[sum], fill",
		`summarize n = count() by bin(time, 5m), service | summarize sum(n) by service`:      "service sum_n[sum], no fill",
		`summarize n = count() by bin(time, 5m) | summarize count() by n`:                    "n count_[count], no fill",
		// So does a column an earlier stage bins a time into.
		`extend hour = bin(time, 1h) | summarize count() by hour, service`:                          "hour[3600s] service count_[count], fill",
		`extend time = bin(time, 1d) | summarize count() by time`:                                   "time[86400s] count_[count], fill",
		`summarize n = count() by bin(time, 5m) | extend h = bin(time, 1h) | summarize sum(n) by h`: "h[3600s] sum_n[sum], fill",
		`extend t = bin(time, 1h) + 1h | summarize count() by t`:                                    "t count_[count], no fill",
		// Binned again, coarser bins stay as they are, unless the new size
		// doesn't divide theirs: then the times move onto its bins.
		`summarize n = count() by bin(time, 1h) | summarize sum(n) by bin(time, 5m)`:    "time[3600s] sum_n[sum], fill",
		`summarize n = count() by bin(time, 1h) | summarize sum(n) by bin(time, 7m)`:    "time[420s] sum_n[sum], fill",
		`summarize n = count() by bin(time, 5m) | summarize sum(n) by bin(time, 1h)`:    "time[3600s] sum_n[sum], fill",
		`extend time = bin(time, 1d) | summarize count() by bin(time, 1h)`:              "time[86400s] count_[count], fill",
		`extend day = bin(time, 1d) | extend t = bin(day, 1h) | summarize count() by t`: "t[86400s] count_[count], fill",
		// Hints come only from a summarize.
		`extend t = bin(time, 1h) | project t, duration`: "t duration, no fill",
		byHour + ` | extend day = bin(time, 1d)`:         "time[3600s] service count_[count] day, fill",
	})
}

func TestStagesThatLeaveOutRowsAfterTheSummarizeStopGapsBeingFilled(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	byHour := `summarize n = count() by bin(time, 1h), service`
	assertHints(t, env, map[string]string{
		byHour:                                "time[3600s] service n[count], fill",
		byHour + ` | sort by time asc`:        "time[3600s] service n[count], fill",
		byHour + ` | project time, n`:         "time[3600s] n[count], fill",
		byHour + ` | extend slow = n > 1`:     "time[3600s] service n[count] slow, fill",
		byHour + ` | where n > 1`:             "time[3600s] service n[count], no fill",
		byHour + ` | take 100`:                "time[3600s] service n[count], no fill",
		byHour + ` | sort by n desc | take 2`: "time[3600s] service n[count], no fill",
		byHour + ` | top 100 by n`:            "time[3600s] service n[count], no fill",
		byHour + ` | count`:                   "Count, no fill",
		byHour + ` | distinct time, service`:  "time[3600s] service, no fill",
		byHour + ` | distinct time, n`:        "time[3600s] n[count], no fill",
		// A filter before the summarize leaves out Logs, not bins.
		`where level == "error" | ` + byHour: "time[3600s] service n[count], fill",
	})
}

func TestATruncatedTableStopsGapsBeingFilled(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// A Log a second, one more than a table holds.
	start := testNow.Add(-4 * time.Hour)
	logs := make([]string, 10001)
	for i := range logs {
		logs[i] = logJSON(t, "tick", start.Add(time.Duration(i)*time.Second), nil)
	}
	postAndProcess(t, env, logs...)

	perSecond := runCharted(t, env, `summarize count() by bin(time, 1s)`, allTime.from, allTime.to)
	if !perSecond.Table.Truncated || len(perSecond.Table.Rows) != 10000 {
		t.Fatalf("Expected 10,000 rows, truncated, got %d, truncated %v", len(perSecond.Table.Rows), perSecond.Table.Truncated)
	}
	if got, want := hintsOf(perSecond), "time[1s] count_[count], no fill"; got != want {
		t.Errorf("Truncated: want %s, got %s", want, got)
	}
	perMinute := runCharted(t, env, `summarize count() by bin(time, 1m)`, allTime.from, allTime.to)
	if got, want := hintsOf(perMinute), "time[60s] count_[count], fill"; perMinute.Table.Truncated || got != want {
		t.Errorf("Not truncated: want %s, got %s, truncated %v", want, got, perMinute.Table.Truncated)
	}
}

func TestATablesRangeIsTheOneItsQueryRead(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	day := []time.Time{languageAt(0, 0), languageAt(0, 0).AddDate(0, 0, 1)}
	cases := []struct {
		query    string
		from, to time.Time
		want     string
	}{
		{`summarize count() by bin(time, 5m)`, day[0], day[1], "2026-10-02T00:00:00Z 2026-10-03T00:00:00Z"},
		{`where time >= datetime(2026-10-02T10:15:00Z) and time < datetime(2026-10-02T11:00) | summarize count() by bin(time, 5m)`,
			day[0], day[1], "2026-10-02T10:15:00Z 2026-10-02T11:00:00Z"},
		{`where time > ago(1h) | summarize count() by bin_auto(time)`, day[0], day[1], "2026-10-02T10:30:00.000001Z 2026-10-03T00:00:00Z"},
		// Never wider than the request's range.
		{`where time > datetime(2026-10-01) | summarize count() by service`, languageAt(10, 0), languageAt(11, 0),
			"2026-10-02T10:00:00Z 2026-10-02T11:00:00Z"},
		// Only a leading where narrows it.
		{`summarize count() by bin(time, 5m) | where time > datetime(2026-10-02T10:15:00Z)`, day[0], day[1],
			"2026-10-02T00:00:00Z 2026-10-03T00:00:00Z"},
	}
	for _, c := range cases {
		answer := runCharted(t, env, c.query, c.from, c.to)
		if got := answer.Table.From + " " + answer.Table.To; got != c.want {
			t.Errorf("%s\n want %s\n got  %s", c.query, c.want, got)
		}
	}
}

// totalsTable is a run's table with the totals asked for.
type totalsTable struct {
	Table struct {
		Rows   []json.RawMessage `json:"rows"`
		Totals *struct {
			Columns []struct {
				Name string `json:"name"`
				Agg  string `json:"agg"`
			} `json:"columns"`
			Rows      []json.RawMessage `json:"rows"`
			Truncated bool              `json:"truncated"`
		} `json:"totals"`
	} `json:"table"`
}

// runTotals runs a Query that returns a table over all time for Project
// 123, asking for totals when ask is set.
func runTotals(t *testing.T, env *TestEnv, query string, ask bool) totalsTable {
	t.Helper()
	body := map[string]any{"query": query, "from": allTime.from, "to": allTime.to}
	if ask {
		body["totals"] = true
	}
	status, raw := postQueryApp(t, env, "/api/123/logs/query", body)
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
	}
	var answer totalsTable
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode table %q: %v", raw, err)
	}
	return answer
}

// totalsOf writes a table's totals as its columns, then its rows, as
// "level count_[count]: ["info",3] ["error",2]", or "none".
func totalsOf(answer totalsTable) string {
	totals := answer.Table.Totals
	if totals == nil {
		return "none"
	}
	var parts []string
	for _, col := range totals.Columns {
		if col.Agg != "" {
			parts = append(parts, fmt.Sprintf("%s[%s]", col.Name, col.Agg))
		} else {
			parts = append(parts, col.Name)
		}
	}
	text := strings.Join(parts, " ") + ":"
	for _, row := range totals.Rows {
		text += " " + string(row)
	}
	if totals.Truncated {
		text += " truncated"
	}
	return text
}

func TestATimeChartsTotalsAreItsGroupsOverTheRange(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	for query, want := range map[string]string{
		// Five Logs over three hours, three with no level: a row per bin
		// and level, but totals of one row per level, largest first.
		`summarize count() by bin(time, 1h), level`: `level count_[count]: [null,3] ["error",2]`,
		// An avg over the range, not of the bins': api's are 120 and 300 at
		// 10:00 and 900 at 11:00, (120 + 300 + 900) / 3 = 440, where the
		// bins' avgs, 210 and 900, would give 555.
		`summarize avg(duration) by bin_auto(time), service`: `service avg_duration[avg]: ["api",440] ["web",80.5] ["worker",50]`,
		`summarize dcount(service) by bin(time, 1h)`:         `dcount_service[dcount]: [3]`,
		// The bin may come last, as Chart this puts it; ties go by group.
		`summarize n = count() by service, bin(time, 1h)`: `service n[count]: ["api",3] ["web",1] ["worker",1]`,
		// By the bin alone, the totals are one row.
		`summarize count() by bin_auto(time)`: `count_[count]: [5]`,
		// A bin of a column an earlier stage binned the time into.
		`extend hour = bin(time, 1h) | summarize count() by hour, level`: `level count_[count]: [null,3] ["error",2]`,
		// Only a time chart has totals.
		`summarize count() by service`:                             "none",
		`summarize count() by bin(time, 1h) | where count_ > 1`:    "none",
		`summarize by bin(time, 1h)`:                               "none",
		`summarize count() by bin(time, 1h) | sort by count_ desc`: "none",
	} {
		if got := totalsOf(runTotals(t, env, query, true)); got != want {
			t.Errorf("%s\n want %s\n got  %s", query, want, got)
		}
	}

	// The table itself is the same with totals as without them, which
	// have none.
	with := runTotals(t, env, `summarize count() by bin(time, 1h), level`, true)
	without := runTotals(t, env, `summarize count() by bin(time, 1h), level`, false)
	if got := totalsOf(without); got != "none" {
		t.Errorf("Without totals asked for: want none, got %s", got)
	}
	if len(with.Table.Rows) != 4 || !reflect.DeepEqual(with.Table.Rows, without.Table.Rows) {
		t.Errorf("Expected the same 4 rows with and without totals, got %s and %s", with.Table.Rows, without.Table.Rows)
	}

	// A Query of Log rows answers as it did.
	status, raw := postQueryApp(t, env, "/api/123/logs/query",
		map[string]any{"query": `where level == "error"`, "from": allTime.from, "to": allTime.to, "totals": true})
	if status != 200 || strings.Contains(string(raw), "totals") || !strings.Contains(string(raw), `"logs"`) {
		t.Errorf("Log rows with totals asked for: expected Logs alone, got %d: %s", status, raw)
	}
}

func TestATimeChartsTotalsAreCutAtATablesRows(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// A Log a second in one hour, each its own group: one more than a
	// table holds.
	start := testNow.Add(-4 * time.Hour).Truncate(time.Hour)
	logs := make([]string, 10001)
	for i := range logs {
		logs[i] = logJSON(t, "tick", start.Add(time.Duration(i%3600)*time.Second), map[string]any{"n": i})
	}
	postAndProcess(t, env, logs...)

	cut := runTotals(t, env, `summarize count() by bin(time, 1d), n`, true)
	if totals := cut.Table.Totals; totals == nil || !totals.Truncated || len(totals.Rows) != 10000 {
		t.Fatalf("Expected 10,000 totals rows, truncated, got %s", totalsOf(cut))
	}
	whole := runTotals(t, env, `summarize count() by bin(time, 1d)`, true)
	if got, want := totalsOf(whole), `count_[count]: [10001]`; got != want {
		t.Errorf("By the bin alone: want %s, got %s", want, got)
	}
}
