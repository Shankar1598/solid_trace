package main_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/config"
)

// The bin cache keeps charts' bins of hours that have ended in memory, until
// a Log is inserted into one of their hours. Whatever it holds, a run answers
// exactly as a run without it would.

// binCachedQueries are charts made bin by bin: by an hour, a bin smaller and
// one larger than an hour, a day, and a column binned before the
// summarize, with aggregates of every kind that can't be added up across
// bins, rates, a where that drops Logs, and text matches.
var binCachedQueries = []string{
	"summarize count() by bin(time, 1h), user",
	"summarize count(), dcount(user) by bin(time, 20m)",
	"summarize n = count(), slowest = max(ms), mean = avg(ms) by bin(time, 2h)",
	"summarize count() by bin(time, 1d), user",
	`where user != "nobody" | extend half = bin(time, 30m) | summarize count(), p = percentile(ms, 50) by half`,
	`where user == "ada" | summarize count(), slowest = max(ms) by bin(time, 1h)`,
	"summarize rate(), peak_rate() by bin(time, 1h), user",
	`where message !contains "B" and user !startswith_cs "c" | summarize count() by bin(time, 1h), user`,
	`search "B" | summarize count() by bin(time, 20m)`,
}

// binCachedHistograms are the explorer's histograms: filtered, so made bin
// by bin as those charts are, with buckets of an hour and of 20 minutes, and
// of the size picked for the range; and one with no filter, which reads
// counts kept by minute instead.
var binCachedHistograms = []histogramRequest{
	{Query: `where user != "nobody"`, BucketSeconds: 3600},
	{Query: `where user == "ada"`, BucketSeconds: 1200},
	{Query: `search "B"`},
	{BucketSeconds: 3600},
}

// histogramName names a histogram in failures and in the hits
// assertSameWithAndWithoutTheBinCache returns.
func histogramName(req histogramRequest) string {
	return fmt.Sprintf("histogram of %q by %ds", req.Query, req.BucketSeconds)
}

// binnedHistogram is a histogram over binCacheRange.
func binnedHistogram(t *testing.T, env *TestEnv, req histogramRequest) histogramAnswer {
	t.Helper()
	req.From, req.To = binCacheRange.from, binCacheRange.to
	return histogram(t, env, req)
}

// binCacheRange is the range every bin cache test charts: from part-way
// into an hour before the first Log, to after the last.
var binCacheRange = struct{ from, to time.Time }{
	from: time.Date(2026, 10, 1, 19, 40, 0, 0, time.UTC),
	to:   time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC),
}

// chartAt is a time on October 1st or 2nd, 2026.
func chartAt(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

// chartLog is a Log of a user that took ms.
func chartLog(t *testing.T, at time.Time, user string, ms int) string {
	return logJSON(t, user, at, map[string]any{"user": user, "ms": ms})
}

// postChartLogs sends Logs from 20:00 on October 1st to 12:30 on the 2nd,
// those of 11:00 at 11:29 and the rest at noon, where it leaves the clock: so
// the hours up to 11:00 have ended, and 12:00 hasn't.
func postChartLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	var eleven, logs []string
	for at, i := chartAt(1, 20, 5), 0; at.Before(chartAt(2, 12, 40)); at, i = at.Add(25*time.Minute), i+1 {
		log := chartLog(t, at, []string{"ada", "bob", "cy"}[i%3], 10*(i%7))
		if at.Truncate(time.Hour).Equal(chartAt(2, 11, 0)) {
			eleven = append(eleven, log)
		} else {
			logs = append(logs, log)
		}
	}
	env.Clock.Set(chartAt(2, 11, 29))
	postAndProcess(t, env, eleven...)
	env.Clock.Set(testNow)
	postAndProcess(t, env, logs...)
}

// binnedTable runs a Query over binCacheRange and returns its table as
// EventStore answered it.
func binnedTable(t *testing.T, env *TestEnv, query string) string {
	t.Helper()
	return binnedTableIn(t, env, query, binCacheRange.from, binCacheRange.to)
}

// binnedTableIn is binnedTable over [from, to).
func binnedTableIn(t *testing.T, env *TestEnv, query string, from, to time.Time) string {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: query, From: from, To: to})
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
	}
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &answer); err != nil || answer["table"] == nil {
		t.Fatalf("Query %q expected a table, got %s (%v)", query, raw, err)
	}
	return string(answer["table"])
}

// binCacheStats are the bin cache's hits and misses so far.
func binCacheStats(env *TestEnv) (hits, misses int64) {
	return env.ClickHouseLogs.BinCacheStats()
}

// assertSameWithAndWithoutTheBinCache runs each Query and histogram with the
// bin cache as earlier runs left it, then without the cache, then twice with
// a new one, and checks every run answers the same. The last run must take
// every bin it can from the cache. It returns the bins the last run of each
// took from the cache, by Query or histogramName.
func assertSameWithAndWithoutTheBinCache(t *testing.T, env *TestEnv, when string) map[string]int64 {
	t.Helper()
	// runs are the Queries' tables and the histograms, each answered as
	// text.
	type run struct {
		name   string
		answer func() string
	}
	var runs []run
	for _, query := range binCachedQueries {
		runs = append(runs, run{query, func() string { return binnedTable(t, env, query) }})
	}
	for _, req := range binCachedHistograms {
		runs = append(runs, run{histogramName(req), func() string { return fmt.Sprintf("%+v", binnedHistogram(t, env, req)) }})
	}

	hits := map[string]int64{}
	cached := map[string]string{}
	for _, r := range runs {
		cached[r.name] = r.answer()
	}
	env.ClickHouseLogs.UseBinCache(0)
	uncached := map[string]string{}
	for _, r := range runs {
		uncached[r.name] = r.answer()
		if cached[r.name] != uncached[r.name] {
			t.Errorf("%s, %s\n with the bin cache: %s\n without it:         %s", when, r.name, cached[r.name], uncached[r.name])
		}
	}
	env.ClickHouseLogs.UseBinCache(config.DefaultLogsBinCacheMB << 20)
	for _, r := range runs {
		if got := r.answer(); got != uncached[r.name] {
			t.Errorf("%s, %s, filling a new bin cache\n got:  %s\n want: %s", when, r.name, got, uncached[r.name])
		}
		hitsBefore, missesBefore := binCacheStats(env)
		if got := r.answer(); got != uncached[r.name] {
			t.Errorf("%s, %s, run again\n got:  %s\n want: %s", when, r.name, got, uncached[r.name])
		}
		hitsAfter, missesAfter := binCacheStats(env)
		if missesAfter != missesBefore {
			t.Errorf("%s, %s: expected a second run to miss no bin, got %d misses", when, r.name, missesAfter-missesBefore)
		}
		hits[r.name] = hitsAfter - hitsBefore
	}
	return hits
}

func TestTheBinCacheAnswersAsARunWithoutItAsLogsArriveAndHoursEnd(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// At noon, the hours up to 11:00 have ended.
	postChartLogs(t, env)
	hits := assertSameWithAndWithoutTheBinCache(t, env, "once stored")
	// Of the 29 whole hours of the range, the 16 up to 11:00 have ended and
	// come from the cache; noon and the hours after it are worked out on
	// every run.
	if got := hits[binCachedQueries[0]]; got != 16 {
		t.Errorf("Expected a second run to take 16 hours from the cache, got %d", got)
	}
	// Of the 14 whole 2h bins, the 8 up to 10:00 to 12:00 have ended.
	if got := hits[binCachedQueries[2]]; got != 8 {
		t.Errorf("Expected a second run to take 8 two-hour bins from the cache, got %d", got)
	}
	// October 2nd hasn't ended.
	if got := hits[binCachedQueries[3]]; got != 0 {
		t.Errorf("Expected no whole day from the cache, got %d", got)
	}
	// A Chart filtered on text takes its hours from the cache too.
	if got := hits[binCachedQueries[7]]; got != 16 {
		t.Errorf("Expected a text-filtered Chart to take 16 hours from the cache, got %d", got)
	}
	// So does the explorer's filtered histogram; the histogram of every Log
	// reads counts kept by minute.
	if got := hits[histogramName(binCachedHistograms[0])]; got != 16 {
		t.Errorf("Expected a filtered hourly histogram to take 16 hours from the cache, got %d", got)
	}
	if got := hits[histogramName(binCachedHistograms[3])]; got != 0 {
		t.Errorf("Expected the histogram of every Log to take nothing from the cache, got %d", got)
	}

	// Logs of 03:10 and 05:30 arrive late, into hours whose bins the cache
	// holds.
	postAndProcess(t, env, chartLog(t, chartAt(2, 3, 10), "dee", 500))
	postAndProcess(t, env, chartLog(t, chartAt(2, 5, 30), "dee", 700))
	assertSameWithAndWithoutTheBinCache(t, env, "after late Logs")

	// An hour and a half later, noon has ended, and Logs arrive in an hour
	// that was empty.
	env.Clock.Set(testNow.Add(90 * time.Minute))
	postAndProcess(t, env, chartLog(t, chartAt(2, 13, 10), "ada", 30))
	hits = assertSameWithAndWithoutTheBinCache(t, env, "an hour and a half later")
	if got := hits[binCachedQueries[0]]; got != 17 {
		t.Errorf("Expected a second run to take 17 hours from the cache once noon has ended, got %d", got)
	}

	// A restart empties the cache, and the runs fill it again.
	env.Reopen(t)
	assertSameWithAndWithoutTheBinCache(t, env, "after a restart")
}

func TestACachedBinMissesOnceALogIsInsertedIntoItsHour(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	postChartLogs(t, env)

	query := binCachedQueries[0]
	binnedTable(t, env, query)
	binnedTable(t, env, query)

	// A Log of 03:10 arrives late: 03:00's bin is worked out again, and the
	// other hours still come from the cache.
	postAndProcess(t, env, chartLog(t, chartAt(2, 3, 10), "dee", 500))
	hitsBefore, missesBefore := binCacheStats(env)
	got := binnedTable(t, env, query)
	hitsAfter, missesAfter := binCacheStats(env)
	if hits, misses := hitsAfter-hitsBefore, missesAfter-missesBefore; hits != 15 || misses != 1 {
		t.Errorf("Expected 03:00's bin missed and 15 hours from the cache, got %d hits and %d misses", hits, misses)
	}
	if !strings.Contains(got, `["2026-10-02T03:00:00Z","dee",1]`) {
		t.Errorf("Expected 03:00's row of dee's late Log, got %s", got)
	}
	env.ClickHouseLogs.UseBinCache(0)
	if uncached := binnedTable(t, env, query); got != uncached {
		t.Errorf("%s\n from the bin cache: %s\n without it:         %s", query, got, uncached)
	}
}

func TestCachedBinsMissOnceRetentionDeletesTheirDay(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	postChartLogs(t, env)
	assertSameWithAndWithoutTheBinCache(t, env, "before Retention")

	// Past midnight on November 1st, 30 days back is October 2nd, so
	// October 1st's Logs go, while the cache holds bins of its hours.
	env.Clock.Set(time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC))
	env.DeleteLogsPastRetention(t)
	assertSameWithAndWithoutTheBinCache(t, env, "after Retention")
	if got := binnedTable(t, env, binCachedQueries[0]); strings.Contains(got, `["2026-10-01`) || !strings.Contains(got, `["2026-10-02`) {
		t.Errorf("Expected October 2nd's rows alone after Retention, got %s", got)
	}
}

func TestCachedBinsStayWhenTraceRetentionDeletesTheirDaysSpans(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	// Project 123 keeps its Spans a day and its Logs 30.
	seedProject(t, env.SQLitePath, 123, 0)
	consoleExec(t, env, "UPDATE projects SET trace_retention_days = 1 WHERE id = 123")
	postChartLogs(t, env)
	sent := sentTransaction{fixture: "sentry-ruby-6.3-rails", traceID: traceIDOf(1), at: chartAt(1, 21, 0)}
	postEnvelopeWithKey(t, env, "test_public_key", sent.envelope(t))
	env.ProcessSpans(t)

	// Past midnight on October 4th, October 1st's Spans are past a day,
	// and its Logs aren't. The run before Retention caches every hour that
	// has ended by then.
	env.Clock.Set(time.Date(2026, 10, 4, 0, 30, 0, 0, time.UTC))
	query := binCachedQueries[0]
	binnedTable(t, env, query)
	env.DeleteLogsPastRetention(t)
	if got := readTrace(t, env, traceIDOf(1), 123).Spans; len(got) != 0 {
		t.Fatalf("Expected October 1st's Spans dropped, got %v", rows(got))
	}

	_, missesBefore := binCacheStats(env)
	got := binnedTable(t, env, query)
	if _, missesAfter := binCacheStats(env); missesAfter != missesBefore {
		t.Errorf("Expected every hour from the cache after the Spans' day went, got %d misses", missesAfter-missesBefore)
	}
	env.ClickHouseLogs.UseBinCache(0)
	if uncached := binnedTable(t, env, query); got != uncached {
		t.Errorf("%s\n from the bin cache: %s\n without it:         %s", query, got, uncached)
	}
}

func TestARateChartRunOverALaterRangeTakesTheBinsBothHold(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	postChartLogs(t, env)

	// Ten minutes later, as a Dashboard refreshes, the range's ends cut
	// other hours, whose rates are worked out afresh.
	query := "summarize count(), rate(), peak_rate() by bin(time, 1h)"
	later := struct{ from, to time.Time }{binCacheRange.from.Add(10 * time.Minute), binCacheRange.to.Add(10 * time.Minute)}
	binnedTable(t, env, query)
	hitsBefore, _ := binCacheStats(env)
	refreshed := binnedTableIn(t, env, query, later.from, later.to)
	hitsAfter, _ := binCacheStats(env)
	// Of the 29 whole hours both ranges hold, 16 have ended.
	if got := hitsAfter - hitsBefore; got != 16 {
		t.Errorf("Expected the later run to take 16 hours from the cache, got %d", got)
	}
	env.ClickHouseLogs.UseBinCache(0)
	if uncached := binnedTableIn(t, env, query, later.from, later.to); refreshed != uncached {
		t.Errorf("%s\n from the bin cache: %s\n without it:         %s", query, refreshed, uncached)
	}
}

func TestTheBinCacheFiltersEveryStretchARunWorksOut(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	postChartLogs(t, env)
	postAndProcess(t, env, chartLog(t, chartAt(2, 2, 50), "ada", 900))

	// From part-way into 02:00: a run with the cache works out that part
	// and the hours from noon, two stretches of one day, and takes the rest
	// from the cache.
	query := `where user == "ada" | summarize count(), slowest = max(ms) by bin(time, 1h)`
	from, to := chartAt(2, 2, 40), chartAt(2, 23, 0)
	filling := binnedTableIn(t, env, query, from, to)
	hitsBefore, _ := binCacheStats(env)
	again := binnedTableIn(t, env, query, from, to)
	hitsAfter, _ := binCacheStats(env)
	if got := hitsAfter - hitsBefore; got != 9 {
		t.Errorf("Expected a second run to take the 9 hours from 03:00 to 11:00 from the cache, got %d", got)
	}
	env.ClickHouseLogs.UseBinCache(0)
	uncached := binnedTableIn(t, env, query, from, to)
	if filling != uncached || again != uncached {
		t.Errorf("%s\n filling the bin cache: %s\n run again:             %s\n without it:            %s", query, filling, again, uncached)
	}
	if !strings.Contains(uncached, `["2026-10-02T02:00:00Z",1,900]`) {
		t.Errorf("Expected 02:50's Log in the part of 02:00 the range holds, got %s", uncached)
	}
}

func TestQueriesNotMadeBinByBinNeverUseTheBinCache(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	postChartLogs(t, env)

	for _, query := range []string{
		"summarize count() by user, bin(time, 1h)",
		"summarize count() by bin(time, 1h) | where count_ > 1",
		"take 5 | summarize count() by bin(time, 1h)",
		"summarize count() by bin(time, 1h), user | summarize count() by bin(time, 1h)",
		"extend time = time + 1h | summarize count() by bin(time, 1h)",
		"extend at = now() | summarize count() by bin(at, 1h)",
		"summarize count() by user",
	} {
		for run := range 2 {
			binnedTable(t, env, query)
			if hits, misses := binCacheStats(env); hits != 0 || misses != 0 {
				t.Errorf("%s, run %d: expected no bin from or into the cache, got %d hits and %d misses", query, run+1, hits, misses)
			}
		}
	}
}
