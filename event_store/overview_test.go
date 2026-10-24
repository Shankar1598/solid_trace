package main_test

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// The Overview: an Organization's Ingest counts and stored Logs, answered by
// the query app's POST /overview for its Projects and a range of UTC days.

// overviewRequest is what Console sends: the Organization's Projects and the
// first and last UTC day.
type overviewRequest struct {
	ProjectIDs     []uint32 `json:"project_ids"`
	From           string   `json:"from"`
	To             string   `json:"to"`
	IncludeStorage bool     `json:"include_storage"`
}

type overviewReceived struct {
	Logs        int64 `json:"logs"`
	Bytes       int64 `json:"bytes"`
	BytesOnWire int64 `json:"bytes_on_wire"`
	Requests    int64 `json:"requests"`
}

type overviewStored struct {
	Logs        int64 `json:"logs"`
	Bytes       int64 `json:"bytes"`
	LocalBytes  int64 `json:"local_bytes"`
	BucketBytes int64 `json:"bucket_bytes"`
	RawBytes    int64 `json:"raw_bytes"`
}

type overviewRate struct {
	Average float64    `json:"average"`
	Peak    int64      `json:"peak"`
	PeakAt  *time.Time `json:"peak_at"`
}

type overviewAnswer struct {
	From              string     `json:"from"`
	To                string     `json:"to"`
	IngestCountsSince *time.Time `json:"ingest_counts_since"`
	HasLogs           bool       `json:"has_logs"`
	Band              struct {
		Received           overviewReceived `json:"received"`
		UnreadableLogs     int64            `json:"unreadable_logs"`
		OverloadedRequests int64            `json:"overloaded_requests"`
		Stored             overviewStored   `json:"stored"`
	} `json:"band"`
	Rates struct {
		Seconds         int64        `json:"seconds"`
		Logs            overviewRate `json:"logs"`
		Bytes           overviewRate `json:"bytes"`
		Requests        overviewRate `json:"requests"`
		LastFiveMinutes *struct {
			LogsPerSecond float64 `json:"logs_per_second"`
		} `json:"last_five_minutes"`
	} `json:"rates"`
	Days []struct {
		Day    string         `json:"day"`
		Stored overviewStored `json:"stored"`
	} `json:"days"`
	Storage json.RawMessage `json:"storage"`
}

// overview asks the query app for the Overview of projects over the UTC
// days from to to.
func overview(t *testing.T, env *TestEnv, from, to time.Time, projects ...uint32) overviewAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, "/overview", overviewRequest{
		ProjectIDs: projects, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly),
	})
	if status != 200 {
		t.Fatalf("Expected the Overview, got %d: %s", status, raw)
	}
	var answer overviewAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode the Overview %s: %v", raw, err)
	}
	return answer
}

// sendLogsAt sends one request to a Project's Log route with its key, with
// the clock at at, and returns the body as sent.
func sendLogsAt(t *testing.T, env *TestEnv, projectID uint32, key string, at time.Time, body string, headers map[string]string) {
	t.Helper()
	env.Clock.Set(at)
	all := map[string]string{"Authorization": "Bearer " + key}
	for name, value := range headers {
		all[name] = value
	}
	resp := sendLogs(t, env, fmt.Sprintf("/api/%d/logs", projectID), "application/json", body, all)
	decodeLogCounts(t, resp)
}

func TestTheOverviewAnswersWhatArrivedAndItsRatesBySecondOfArrival(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	seedProject(t, env.SQLitePath, 789, 0)
	seedProjectKey(t, env.SQLitePath, 789, "third_public_key")

	at := func(second int) time.Time { return testNow.Add(time.Duration(second) * time.Second) }
	logs := func(n int, at time.Time) string {
		var each []string
		for i := range n {
			each = append(each, logJSON(t, fmt.Sprintf("log %d", i), at, nil))
		}
		return strings.Join(each, ",")
	}

	// At 12:00:00 Project 123 sends 2 Logs and an element that isn't one,
	// and Project 456 sends 3 Logs gzipped: 6 received in the second, 3
	// from each.
	first := "[" + logs(2, at(0)) + ",5]"
	gzippedBody := string(gzipped(t, []byte("["+logs(3, at(0))+"]")))
	sendLogsAt(t, env, 123, "test_public_key", at(0), first, nil)
	sendLogsAt(t, env, 456, "other_public_key", at(0), gzippedBody, map[string]string{"Content-Encoding": "gzip"})
	// At 12:00:05 Project 123 sends 4 Logs in 3 requests.
	fifth := []string{"[" + logs(2, at(5)) + "]", "[" + logs(1, at(5)) + "]", "[" + logs(1, at(5)) + "]"}
	for _, body := range fifth {
		sendLogsAt(t, env, 123, "test_public_key", at(5), body, nil)
	}
	// Project 789 isn't the Organization's.
	sendLogsAt(t, env, 789, "third_public_key", at(5), "["+logs(9, at(5))+"]", nil)

	today := testNow
	env.Clock.Set(at(60))
	if got := overview(t, env, today, today, 123, 456); got.Band.Received.Requests != 0 || got.HasLogs {
		t.Errorf("Expected nothing counted before the counts are written, got %+v", got.Band.Received)
	}
	env.FlushIngestCounts(t)
	env.ProcessLogs(t)
	got := overview(t, env, today, today, 123, 456)

	decoded := len(first) + len("["+logs(3, at(0))+"]") + len(fifth[0]) + len(fifth[1]) + len(fifth[2])
	wire := len(first) + len(gzippedBody) + len(fifth[0]) + len(fifth[1]) + len(fifth[2])
	want := overviewReceived{Logs: 10, Bytes: int64(decoded), BytesOnWire: int64(wire), Requests: 5}
	if got.Band.Received != want {
		t.Errorf("Expected received %+v, got %+v", want, got.Band.Received)
	}
	if got.Band.UnreadableLogs != 1 || got.Band.OverloadedRequests != 0 {
		t.Errorf("Expected 1 unreadable Log and no overloaded request, got %d and %d", got.Band.UnreadableLogs, got.Band.OverloadedRequests)
	}
	if got.Band.Stored.Logs != 9 || got.Band.Stored.Bytes <= 0 || got.Band.Stored.LocalBytes != got.Band.Stored.Bytes || got.Band.Stored.BucketBytes != 0 {
		t.Errorf("Expected 9 Logs stored on local disk, got %+v", got.Band.Stored)
	}
	if !got.HasLogs {
		t.Error("Expected the Projects to have Logs")
	}
	if got.IngestCountsSince == nil || !got.IngestCountsSince.Equal(at(0)) {
		t.Errorf("Expected Ingest counts since %v, got %v", at(0), got.IngestCountsSince)
	}

	// The day so far is 12:01:00, 43,260 seconds.
	seconds := int64(12*3600 + 60)
	if got.Rates.Seconds != seconds {
		t.Errorf("Expected rates over %d seconds, got %d", seconds, got.Rates.Seconds)
	}
	firstSecond := int64(len(first) + len("["+logs(3, at(0))+"]"))
	fifthSecond := int64(len(fifth[0]) + len(fifth[1]) + len(fifth[2]))
	busiestBytes, busiestBytesAt := firstSecond, at(0)
	if fifthSecond > firstSecond {
		busiestBytes, busiestBytesAt = fifthSecond, at(5)
	}
	for name, rate := range map[string]struct {
		got    overviewRate
		total  int64
		peak   int64
		peakAt time.Time
	}{
		// The Organization's busiest second sums its Projects: 6 at
		// 12:00:00, though neither sent more than 3 then, and 123 sent 4
		// at 12:00:05.
		"Logs":     {got.Rates.Logs, 10, 6, at(0)},
		"bytes":    {got.Rates.Bytes, int64(decoded), busiestBytes, busiestBytesAt},
		"requests": {got.Rates.Requests, 5, 3, at(5)},
	} {
		if average := float64(rate.total) / float64(seconds); math.Abs(rate.got.Average-average) > 1e-9 {
			t.Errorf("Expected an average of %v %s a second, got %v", average, name, rate.got.Average)
		}
		if rate.got.Peak != rate.peak || rate.got.PeakAt == nil || !rate.got.PeakAt.Equal(rate.peakAt) {
			t.Errorf("Expected a peak of %d %s at %v, got %d at %v", rate.peak, name, rate.peakAt, rate.got.Peak, rate.got.PeakAt)
		}
	}
	if five := got.Rates.LastFiveMinutes; five == nil || math.Abs(five.LogsPerSecond-10.0/300) > 1e-9 {
		t.Errorf("Expected 10 Logs over the last five minutes, got %+v", five)
	}
	if len(got.Days) != 1 || got.Days[0].Day != "2026-10-02" || got.Days[0].Stored != got.Band.Stored {
		t.Errorf("Expected the day's stored Logs, got %+v", got.Days)
	}
	if got.Storage != nil {
		t.Errorf("Expected no Storage unless asked, got %s", got.Storage)
	}

	// Yesterday had none of it, and no last five minutes.
	yesterday := overview(t, env, today.AddDate(0, 0, -1), today.AddDate(0, 0, -1), 123, 456)
	if yesterday.Band.Received != (overviewReceived{}) || yesterday.Rates.Logs.PeakAt != nil || yesterday.Rates.LastFiveMinutes != nil {
		t.Errorf("Expected nothing yesterday, got %+v and %+v", yesterday.Band, yesterday.Rates)
	}
}

func TestRequestsTurnedAwayAsOverloadedAreCounted(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) { env.IngestMaxWaiting = 0 })
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)

	for range 3 {
		resp := sendLogs(t, env, "/api/123/logs?sentry_key=test_public_key", "application/json", logJSON(t, "turned away", testNow, nil), nil)
		if resp.StatusCode != 503 {
			t.Fatalf("Expected 503, got %d", resp.StatusCode)
		}
	}
	env.Clock.Advance(time.Second)
	env.FlushIngestCounts(t)

	got := overview(t, env, testNow, testNow, 123)
	if got.Band.OverloadedRequests != 3 || got.Band.Received != (overviewReceived{}) {
		t.Errorf("Expected 3 overloaded requests and nothing received, got %+v", got.Band)
	}
}

func TestAFailedWriteOfIngestCountsRetriedCountsOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)

	postLogs(t, env, "application/json", logJSON(t, "one", testNow, nil))
	env.Clock.Advance(time.Second)
	// The write lands, but ingest is told it failed.
	env.FlakyIngestCounts.failAfterWrite()
	if err := env.IngestService.FlushIngestCounts(); err == nil {
		t.Fatal("Expected the write to fail")
	}
	// A Log arrives before the retry.
	postLogs(t, env, "application/json", logJSON(t, "two", testNow, nil))
	env.Clock.Advance(time.Second)
	env.FlushIngestCounts(t)

	if got := overview(t, env, testNow, testNow, 123); got.Band.Received.Logs != 2 || got.Band.Received.Requests != 2 {
		t.Errorf("Expected 2 Logs in 2 requests, each counted once, got %+v", got.Band.Received)
	}
}

func TestRetentionAndProjectDeletionDeleteIngestCounts(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 7)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	// Ingest counts are by the day of arrival, so Retention deletes a
	// day's counts by it, whatever the Logs' own days.
	tenDaysAgo := testNow.AddDate(0, 0, -10)
	sendLogsAt(t, env, 123, "test_public_key", tenDaysAgo, "["+logJSON(t, "old", tenDaysAgo, nil)+"]", nil)
	sendLogsAt(t, env, 123, "test_public_key", testNow, "["+logJSON(t, "new", tenDaysAgo, nil)+"]", nil)
	sendLogsAt(t, env, 456, "other_public_key", tenDaysAgo, "["+logJSON(t, "old", tenDaysAgo, nil)+"]", nil)
	env.Clock.Set(testNow.Add(time.Second))
	env.FlushIngestCounts(t)
	env.ProcessLogs(t)

	received := func(projectID uint32) int64 {
		return overview(t, env, tenDaysAgo, testNow, projectID).Band.Received.Logs
	}
	env.DeleteLogsPastRetention(t)
	if got := received(123); got != 1 {
		t.Errorf("Expected Retention of 7 days to keep only today's counts, got %d Logs", got)
	}
	if got := received(456); got != 1 {
		t.Errorf("Expected Retention of 30 days to keep the counts, got %d Logs", got)
	}

	consoleExec(t, env, "DELETE FROM projects WHERE id = 456")
	env.DeleteLogsPastRetention(t)
	if got := received(456); got != 0 {
		t.Errorf("Expected a deleted Project's counts deleted, got %d Logs", got)
	}
}

func TestTheOverviewAnswersStoredBytesOnLocalDiskAndInTheBucket(t *testing.T) {
	env, now := setupBucketTestEnv(t, 24)
	defer env.Cleanup()

	recent, old := now.Add(-time.Hour), now.AddDate(0, 0, -3)
	postAndProcess(t, env, logJSON(t, "recent", recent, nil), logJSON(t, "old", old, nil))

	got := overview(t, env, now.AddDate(0, 0, -6), now, 123)
	byDay := map[string]overviewStored{}
	for _, day := range got.Days {
		byDay[day.Day] = day.Stored
	}
	if s := byDay[recent.Format(time.DateOnly)]; s.Logs != 1 || s.LocalBytes <= 0 || s.BucketBytes != 0 {
		t.Errorf("Expected the recent day on local disk, got %+v", s)
	}
	if s := byDay[old.Format(time.DateOnly)]; s.Logs != 1 || s.BucketBytes <= 0 || s.LocalBytes != 0 {
		t.Errorf("Expected the old day in the bucket, got %+v", s)
	}
	if s := got.Band.Stored; s.Logs != 2 || s.Bytes != s.LocalBytes+s.BucketBytes {
		t.Errorf("Expected both days stored, got %+v", s)
	}
}

func TestTheOverviewRefusesARequestThatIsntOne(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	for name, req := range map[string]overviewRequest{
		"no Projects":      {From: "2026-10-01", To: "2026-10-02"},
		"a day that isn't": {ProjectIDs: []uint32{123}, From: "yesterday", To: "2026-10-02"},
		"to before from":   {ProjectIDs: []uint32{123}, From: "2026-10-02", To: "2026-10-01"},
		"over 31 days":     {ProjectIDs: []uint32{123}, From: "2026-08-01", To: "2026-10-02"},
	} {
		if status, raw := postQueryApp(t, env, "/overview", req); status != 400 {
			t.Errorf("Expected 400 for %s, got %d: %s", name, status, raw)
		}
	}
}
