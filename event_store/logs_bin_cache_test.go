package main_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/solidtrace/event_store/config"
)

// The bin cache keeps charts' bins of exported hours in memory. Whatever it
// holds, a run answers exactly as a run without it would.

// binCachedQueries are charts made bin by bin: by an hour, a bin smaller and
// one larger than an hour, a day, and a column binned before the
// summarize, with aggregates of every kind that can't be added up across
// bins.
var binCachedQueries = []string{
	"summarize count() by bin(time, 1h), user",
	"summarize count(), dcount(user) by bin(time, 20m)",
	"summarize n = count(), slowest = max(ms), mean = avg(ms) by bin(time, 2h)",
	"summarize count() by bin(time, 1d), user",
	`where user != "nobody" | extend half = bin(time, 30m) | summarize count(), p = percentile(ms, 50) by half`,
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
// at the clock's time.
func postChartLogs(t *testing.T, env *TestEnv) {
	t.Helper()
	var logs []string
	for at, i := chartAt(1, 20, 5), 0; at.Before(chartAt(2, 12, 40)); at, i = at.Add(25*time.Minute), i+1 {
		logs = append(logs, chartLog(t, at, []string{"ada", "bob", "cy"}[i%3], 10*(i%7)))
	}
	postAndProcess(t, env, logs...)
}

// binnedTable runs a Query over binCacheRange and returns its table as
// EventStore answered it.
func binnedTable(t *testing.T, env *TestEnv, query string) string {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{Query: query, From: binCacheRange.from, To: binCacheRange.to})
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
	return env.LogStore.BinCacheStats()
}

// assertSameWithAndWithoutTheBinCache runs each Query with the bin cache as
// earlier runs left it, then without the cache, then twice with a new one,
// and checks every run answers the same. The last run must take every bin
// it can from the cache. It returns the bins the last run of each Query
// took from the cache.
func assertSameWithAndWithoutTheBinCache(t *testing.T, env *TestEnv, when string) map[string]int64 {
	t.Helper()
	hits := map[string]int64{}
	cached := map[string]string{}
	for _, query := range binCachedQueries {
		cached[query] = binnedTable(t, env, query)
	}
	env.LogStore.UseBinCache(0)
	uncached := map[string]string{}
	for _, query := range binCachedQueries {
		uncached[query] = binnedTable(t, env, query)
		if cached[query] != uncached[query] {
			t.Errorf("%s, %s\n with the bin cache: %s\n without it:         %s", when, query, cached[query], uncached[query])
		}
	}
	env.LogStore.UseBinCache(config.DefaultLogsBinCacheMB << 20)
	for _, query := range binCachedQueries {
		if got := binnedTable(t, env, query); got != uncached[query] {
			t.Errorf("%s, %s, filling a new bin cache\n got:  %s\n want: %s", when, query, got, uncached[query])
		}
		hitsBefore, missesBefore := binCacheStats(env)
		if got := binnedTable(t, env, query); got != uncached[query] {
			t.Errorf("%s, %s, run again\n got:  %s\n want: %s", when, query, got, uncached[query])
		}
		hitsAfter, missesAfter := binCacheStats(env)
		if missesAfter != missesBefore {
			t.Errorf("%s, %s: expected a second run to miss no bin, got %d misses", when, query, missesAfter-missesBefore)
		}
		hits[query] = hitsAfter - hitsBefore
	}
	return hits
}

func TestTheBinCacheAnswersAsARunWithoutItAcrossExportsLateLogsRetentionAndDeletion(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)

	// At noon, the hours up to 10:00 are exported, and 11:00 and 12:00
	// are hot.
	postChartLogs(t, env)
	env.ExportLogs(t)
	hits := assertSameWithAndWithoutTheBinCache(t, env, "after the export")
	// Of the 29 whole hours of the range, two hold hot Logs; the rest,
	// empty ones after noon too, come from the cache.
	if got := hits[binCachedQueries[0]]; got != 27 {
		t.Errorf("Expected a second run to take 27 hours from the cache, got %d", got)
	}
	// Of the 14 whole 2h bins, those of 10:00 to 12:00 and 12:00 to
	// 14:00 hold hot Logs, as does the day of October 2nd, so each is
	// worked out on every run.
	if got := hits[binCachedQueries[2]]; got != 12 {
		t.Errorf("Expected a second run to take 12 two-hour bins from the cache, got %d", got)
	}
	if got := hits[binCachedQueries[3]]; got != 0 {
		t.Errorf("Expected no whole day from the cache, got %d", got)
	}

	// A late Log of 05:30 goes to logs_late, and one of 03:10 to a late
	// file of its hour at once, so their hours' bins in the cache are
	// stale.
	postAndProcess(t, env, chartLog(t, chartAt(2, 3, 10), "dee", 500))
	env.ExportLogs(t)
	postAndProcess(t, env, chartLog(t, chartAt(2, 5, 30), "dee", 700))
	assertSameWithAndWithoutTheBinCache(t, env, "after late Logs")

	// An hour later, 11:00 is exported, 05:30's Log goes to a late file,
	// and Logs arrive in an hour that was empty.
	env.Clock.Set(testNow.Add(time.Hour))
	postAndProcess(t, env, chartLog(t, chartAt(2, 13, 10), "ada", 30))
	env.ExportLogs(t)
	assertSameWithAndWithoutTheBinCache(t, env, "after the next export")

	// The next day, everything is exported, and a Retention of one day
	// deletes October 1st.
	env.Clock.Set(chartAt(3, 0, 30))
	env.ExportLogs(t)
	assertSameWithAndWithoutTheBinCache(t, env, "after a day's exports")
	consoleExec(t, env, "UPDATE projects SET log_retention_days = 1 WHERE id = 123")
	env.DeleteLogsPastRetention(t)
	assertSameWithAndWithoutTheBinCache(t, env, "after Retention")

	// The Project is deleted.
	consoleExec(t, env, "DELETE FROM projects WHERE id = 123")
	env.DeleteLogsPastRetention(t)
	assertSameWithAndWithoutTheBinCache(t, env, "after the Project's deletion")
	var empty struct {
		Rows []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal([]byte(binnedTable(t, env, binCachedQueries[0])), &empty); err != nil || len(empty.Rows) != 0 {
		t.Errorf("Expected no rows once the Project is deleted, got %v (%v)", empty.Rows, err)
	}
}

func TestTheBinCacheAnswersAsARunWithoutItWithTheFilesInABucket(t *testing.T) {
	env, _ := setupBucketTestEnv(t, 0)
	defer env.Cleanup()

	postChartLogs(t, env)
	env.ExportLogs(t)
	env.UploadLogFiles(t)
	env.EvictLogFiles(t)
	hits := assertSameWithAndWithoutTheBinCache(t, env, "with every exported hour in the bucket")
	if got := hits[binCachedQueries[0]]; got != 27 {
		t.Errorf("Expected a second run to take 27 hours from the cache, got %d", got)
	}

	// Evicting copies changes no bin: what the cache holds is still taken.
	postAndProcess(t, env, chartLog(t, chartAt(2, 4, 50), "dee", 900))
	env.ExportLogs(t)
	hitsBefore, _ := binCacheStats(env)
	binnedTable(t, env, binCachedQueries[0])
	env.UploadLogFiles(t)
	env.EvictLogFiles(t)
	binnedTable(t, env, binCachedQueries[0])
	hitsAfter, _ := binCacheStats(env)
	if got := hitsAfter - hitsBefore; got != 2*27-1 {
		t.Errorf("Expected the hours but 04:00 taken from the cache twice, and 04:00 once after its late file, got %d hits", got)
	}
	assertSameWithAndWithoutTheBinCache(t, env, "after a late file in the bucket")
}

func TestQueriesNotMadeBinByBinNeverUseTheBinCache(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	postChartLogs(t, env)
	env.ExportLogs(t)

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
