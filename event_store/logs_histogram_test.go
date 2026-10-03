package main_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"testing"
	"time"
)

// histogramRequest asks for the histogram and summary of a Query's Logs.
type histogramRequest struct {
	Query         string    `json:"query"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	BucketSeconds int       `json:"bucket_seconds,omitempty"`
}

// histogramAnswer is the histogram and summary: Logs per bucket per Level,
// and in all.
type histogramAnswer struct {
	BucketSeconds int `json:"bucket_seconds"`
	Buckets       []struct {
		Time   time.Time      `json:"time"`
		Count  int            `json:"count"`
		Levels map[string]int `json:"levels"`
	} `json:"buckets"`
	Count  int            `json:"count"`
	Levels map[string]int `json:"levels"`
}

func histogram(t *testing.T, env *TestEnv, req histogramRequest) histogramAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/histogram", req)
	if status != 200 {
		t.Fatalf("Histogram of %q expected 200, got %d: %s", req.Query, status, raw)
	}
	var answer histogramAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode histogram %q: %v", raw, err)
	}
	return answer
}

// levelCounts are counts by Level, every Level and none present.
func levelCounts(counts map[string]int) map[string]int {
	all := map[string]int{"trace": 0, "debug": 0, "info": 0, "warn": 0, "error": 0, "fatal": 0, "none": 0}
	maps.Copy(all, counts)
	return all
}

// bucketsOf writes a histogram's buckets as "time count {levels}", leaving
// out Levels with no Logs.
func bucketsOf(answer histogramAnswer) []string {
	var buckets []string
	for _, b := range answer.Buckets {
		levels := map[string]int{}
		for level, n := range b.Levels {
			if n > 0 {
				levels[level] = n
			}
		}
		buckets = append(buckets, fmt.Sprintf("%s %d %v", b.Time.UTC().Format(time.TimeOnly), b.Count, levels))
	}
	return buckets
}

func TestHistogramCountsLogsPerBucketAndLevelWithEmptyBuckets(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	check := func(when string, query string, want []string, count int, levels map[string]int) {
		t.Helper()
		answer := histogram(t, env, histogramRequest{Query: query, From: languageAt(10, 0), To: languageAt(12, 0), BucketSeconds: 1800})
		if got := bucketsOf(answer); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s: %s buckets\n want %v\n got  %v", when, query, want, got)
		}
		if answer.BucketSeconds != 1800 || answer.Count != count || !maps.Equal(answer.Levels, levelCounts(levels)) {
			t.Errorf("%s: %s: want bucket 1800, %d Logs %v, got bucket %d, %d Logs %v",
				when, query, count, levelCounts(levels), answer.BucketSeconds, answer.Count, answer.Levels)
		}
		for _, b := range answer.Buckets {
			if len(b.Levels) != 7 {
				t.Errorf("%s: %s: bucket %s should count every Level and none, got %v", when, query, b.Time, b.Levels)
			}
		}
	}

	// ada and bob have no Level; di is the day before, outside the range.
	check("in the hot table", "", []string{
		"10:00:00 2 map[none:2]",
		"10:30:00 1 map[error:1]",
		"11:00:00 1 map[error:1]",
		"11:30:00 0 map[]",
	}, 4, map[string]int{"error": 2, "none": 2})

	exportLanguageLogs(t, env) // hour 10 to files, and fy late at 10:45
	check("across files and a late Log", "", []string{
		"10:00:00 2 map[none:2]",
		"10:30:00 2 map[error:1 none:1]",
		"11:00:00 1 map[error:1]",
		"11:30:00 0 map[]",
	}, 5, map[string]int{"error": 2, "none": 3})
	check("filtered", `where service == "api"`, []string{
		"10:00:00 1 map[none:1]",
		"10:30:00 2 map[error:1 none:1]",
		"11:00:00 1 map[error:1]",
		"11:30:00 0 map[]",
	}, 4, map[string]int{"error": 2, "none": 2})
}

func TestHistogramCountsTheLogsARunReturns(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	// Each Query, and the Query whose run returns the Logs it counts.
	queries := map[string]string{
		"":                                   "",
		`where service == "api"`:             `where service == "api"`,
		`search "users"`:                     `search "users"`,
		`where status != 200 | search "api"`: `where status != 200 | search "api"`,
		`where isempty(level)`:               `where isempty(level)`,
		`where time > datetime(2026-10-02T10:15)`:                           `where time > datetime(2026-10-02T10:15)`,
		`where service == "api" | summarize count() by user | sort by user`: `where service == "api"`,
		`where duration > 100 | take 1`:                                     `where duration > 100`,
		`search "a" | project message | where message == "ed"`:              `search "a"`,
	}
	// From the day before, so di counts too.
	from, to := languageAt(0, 0).AddDate(0, 0, -1), languageAt(13, 0)
	check := func(when string) {
		t.Helper()
		for query, filters := range queries {
			logs := runQuery(t, env, 123, queryRequest{Query: filters, From: from, To: to, Limit: 1000}).Logs
			want := map[string]int{}
			for _, log := range logs {
				level := log.Level
				if level == "" {
					level = "none"
				}
				want[level]++
			}
			answer := histogram(t, env, histogramRequest{Query: query, From: from, To: to, BucketSeconds: 3600})
			inBuckets := 0
			for _, b := range answer.Buckets {
				inBuckets += b.Count
			}
			if answer.Count != len(logs) || inBuckets != len(logs) || !maps.Equal(answer.Levels, levelCounts(want)) {
				t.Errorf("%s: %s: the run returns %d Logs %v; the histogram counts %d, %d in buckets, %v",
					when, query, len(logs), want, answer.Count, inBuckets, answer.Levels)
			}
		}
	}
	check("in the hot table")
	exportLanguageLogs(t, env)
	check("across files and a late Log")
}

func TestHistogramPicksABucketSizeAndRefusesMistakes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	answer := histogram(t, env, histogramRequest{From: languageAt(10, 0), To: languageAt(12, 0)})
	if answer.BucketSeconds != 300 || len(answer.Buckets) != 24 || answer.Count != 4 {
		t.Errorf("two hours: want 24 buckets of 300 s and 4 Logs, got %d of %d s and %d Logs",
			len(answer.Buckets), answer.BucketSeconds, answer.Count)
	}

	status, raw := postQueryApp(t, env, "/api/123/logs/histogram",
		histogramRequest{Query: "where status ==", From: languageAt(10, 0), To: languageAt(12, 0)})
	var mistakes struct {
		Errors []queryProblem `json:"errors"`
	}
	if err := json.Unmarshal(raw, &mistakes); status != 400 || err != nil || len(mistakes.Errors) != 1 || mistakes.Errors[0].Line != 1 {
		t.Errorf("a Query with a mistake: want 400 with its error, got %d: %s", status, raw)
	}
	for name, req := range map[string]histogramRequest{
		"no range":         {},
		"reversed range":   {From: languageAt(12, 0), To: languageAt(10, 0)},
		"negative bucket":  {From: languageAt(10, 0), To: languageAt(12, 0), BucketSeconds: -1},
		"too many buckets": {From: allTime.from, To: allTime.to, BucketSeconds: 60},
		"huge bucket":      {From: allTime.from, To: allTime.to, BucketSeconds: 1 << 40},
	} {
		if status, raw := postQueryApp(t, env, "/api/123/logs/histogram", req); status != 400 {
			t.Errorf("%s: want 400, got %d: %s", name, status, raw)
		}
	}
}
