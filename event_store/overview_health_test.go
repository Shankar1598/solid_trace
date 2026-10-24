package main_test

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/http"
	"slices"
	"testing"
	"time"
)

// The Overview's Health: how long Log processing keeps each Project's Log
// batches waiting, how fast its Log reads ran over the last day, and the
// Events of the range.

type overviewHealth struct {
	Processing []struct {
		ProjectID     uint32    `json:"project_id"`
		OldestBatchAt time.Time `json:"oldest_batch_at"`
		WaitedSeconds float64   `json:"waited_seconds"`
	} `json:"processing"`
	Reads struct {
		Projects []struct {
			ProjectID uint32  `json:"project_id"`
			Reads     int64   `json:"reads"`
			MedianMs  float64 `json:"median_ms"`
			P95Ms     float64 `json:"p95_ms"`
		} `json:"projects"`
		Slowest []struct {
			ProjectID  uint32    `json:"project_id"`
			Kind       string    `json:"kind"`
			At         time.Time `json:"at"`
			DurationMs float64   `json:"duration_ms"`
		} `json:"slowest"`
	} `json:"reads"`
	Events []struct {
		ProjectID uint32 `json:"project_id"`
		Events    int64  `json:"events"`
	} `json:"events"`
}

// flushQueryLog has ClickHouse write its query_log, which it otherwise does
// every few seconds.
func flushQueryLog(t *testing.T, env *TestEnv) {
	t.Helper()
	if err := env.ClickHouse.Conn().Exec(context.Background(), "SYSTEM FLUSH LOGS"); err != nil {
		t.Fatalf("Flush ClickHouse's logs: %v", err)
	}
}

// freshProjectIDs are Project ids this run of the test alone uses: the
// query_log keeps the reads of earlier runs on the same database.
func freshProjectIDs(n int) []uint32 {
	base := 1_000_000 + rand.Uint32N(3_000_000_000)
	ids := make([]uint32, n)
	for i := range ids {
		ids[i] = base + uint32(i)
	}
	return ids
}

// getQueryApp gets path from the query app, which must answer 200.
func getQueryApp(t *testing.T, env *TestEnv, path string) {
	t.Helper()
	req, _ := http.NewRequest("GET", path, nil)
	resp, err := env.QueryApp.Test(req, 10000)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET %s: %v %v", path, resp, err)
	}
}

func TestTheOverviewAnswersEachProjectsReadTimesAndTheSlowestReadsWithTheirKinds(t *testing.T) {
	// With no bin cache, each histogram is read from ClickHouse.
	env := newTestEnv(t, func(env *TestEnv) { env.BinCacheBytes = 0 })
	defer env.Cleanup()
	setSQLQueries(t, env, true)
	ids := freshProjectIDs(3)
	first, second, other := ids[0], ids[1], ids[2]
	for i, id := range ids {
		seedProject(t, env.SQLitePath, id, 0)
		seedProjectKey(t, env.SQLitePath, id, fmt.Sprintf("key_%d", i))
		postAndProcessTo(t, env, id, fmt.Sprintf("key_%d", i), logJSON(t, "hello", testNow, nil))
	}

	// The first Project's newest page and a table; the second's histogram,
	// Fields, a Log by id and a SQL query; the other Project isn't the
	// Organization's.
	runQuery(t, env, first, queryRequest{Query: "", From: allTime.from, To: allTime.to})
	runQuery(t, env, first, queryRequest{Query: "summarize count()", From: allTime.from, To: allTime.to})
	runQuery(t, env, other, queryRequest{Query: "", From: allTime.from, To: allTime.to})
	if status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/logs/histogram", second), histogramRequest{From: allTime.from, To: allTime.to}); status != 200 {
		t.Fatalf("Histogram: %d %s", status, raw)
	}
	getQueryApp(t, env, fieldsPath(second, allTime.from, allTime.to, ""))
	page := runQuery(t, env, second, queryRequest{Query: "", From: allTime.from, To: allTime.to})
	getQueryApp(t, env, fmt.Sprintf("/api/%d/logs/%s", second, page.Logs[0].ID))
	if status, raw := runSQL(t, env, second, sqlRequest{SQL: "SELECT count() FROM logs", From: allTime.from, To: allTime.to}); status != 200 {
		t.Fatalf("SQL: %d %s", status, raw)
	}
	flushQueryLog(t, env)

	health := overview(t, env, testNow, testNow, first, second).Health
	reads := map[uint32]int64{}
	for _, p := range health.Reads.Projects {
		reads[p.ProjectID] = p.Reads
		if p.MedianMs <= 0 || p.P95Ms < p.MedianMs {
			t.Errorf("Expected Project %d's median and 95th percentile, got %+v", p.ProjectID, p)
		}
	}
	// The second Project's newest page is a read too.
	if want := map[uint32]int64{first: 2, second: 5}; !maps.Equal(reads, want) {
		t.Errorf("Expected reads %v, got %v", want, reads)
	}
	kinds := map[uint32][]string{}
	for _, read := range health.Reads.Slowest {
		kinds[read.ProjectID] = append(kinds[read.ProjectID], read.Kind)
		if read.DurationMs <= 0 || time.Since(read.At) > time.Hour {
			t.Errorf("Expected a read's time and how long it took, got %+v", read)
		}
	}
	for id, want := range map[uint32][]string{
		first:  {"newest page", "table"},
		second: {"Fields", "Log by id", "SQL query", "histogram", "newest page"},
	} {
		got := kinds[id]
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("Expected Project %d's slowest reads to be %v, got %v", id, want, got)
		}
	}
	if _, ok := kinds[other]; ok || len(kinds) != 2 {
		t.Errorf("Expected only the Organization's Projects' reads, got %v", kinds)
	}
	for i := 1; i < len(health.Reads.Slowest); i++ {
		if health.Reads.Slowest[i].DurationMs > health.Reads.Slowest[i-1].DurationMs {
			t.Errorf("Expected the slowest reads slowest first, got %+v", health.Reads.Slowest)
		}
	}
}

func TestAProjectWhoseLogBatchesWaitAnswersItsOldestWait(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	// Project 123's batches wait: one from 12:00:00 and one from 12:00:30.
	// Project 456's is processed.
	sendLogsAt(t, env, 456, "other_public_key", testNow, "["+logJSON(t, "stored", testNow, nil)+"]", nil)
	env.ProcessLogs(t)
	sendLogsAt(t, env, 123, "test_public_key", testNow, "["+logJSON(t, "waits", testNow, nil)+"]", nil)
	sendLogsAt(t, env, 123, "test_public_key", testNow.Add(30*time.Second), "["+logJSON(t, "waits too", testNow, nil)+"]", nil)
	env.Clock.Set(testNow.Add(90 * time.Second))

	processing := overview(t, env, testNow, testNow, 123, 456).Health.Processing
	if len(processing) != 1 || processing[0].ProjectID != 123 || !processing[0].OldestBatchAt.Equal(testNow) || processing[0].WaitedSeconds != 90 {
		t.Errorf("Expected Project 123's oldest batch, from 12:00:00, to have waited 90 s, got %+v", processing)
	}

	env.ProcessLogs(t)
	if processing := overview(t, env, testNow, testNow, 123, 456).Health.Processing; len(processing) != 0 {
		t.Errorf("Expected nothing waiting once processed, got %+v", processing)
	}
}

func TestTheOverviewCountsEachProjectsEventsOfTheRange(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	yesterday := testNow.AddDate(0, 0, -1)
	postEvent(t, env, eventAt("today", testNow.Format(time.RFC3339)))
	postEvent(t, env, eventAt("today", testNow.Add(-time.Hour).Format(time.RFC3339)))
	postEvent(t, env, eventAt("yesterday", yesterday.Format(time.RFC3339)))
	postEventTo(t, env, 456, "other_public_key", eventAt("elsewhere", testNow.Format(time.RFC3339)))
	env.Process(t)

	events := func(from time.Time) map[uint32]int64 {
		got := map[uint32]int64{}
		for _, e := range overview(t, env, from, testNow, 123).Health.Events {
			got[e.ProjectID] = e.Events
		}
		return got
	}
	if got := events(testNow); !maps.Equal(got, map[uint32]int64{123: 2}) {
		t.Errorf("Expected Today's 2 Events of Project 123, got %v", got)
	}
	if got := events(yesterday); !maps.Equal(got, map[uint32]int64{123: 3}) {
		t.Errorf("Expected 3 Events over two days, got %v", got)
	}
}
