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
		Day           string           `json:"day"`
		Stored        overviewStored   `json:"stored"`
		Received      overviewReceived `json:"received"`
		LogsPerSecond overviewRate     `json:"logs_per_second"`
		Projects      []struct {
			ProjectID    uint32 `json:"project_id"`
			ReceivedLogs int64  `json:"received_logs"`
		} `json:"projects"`
	} `json:"days"`
	Projects []overviewProject `json:"projects"`
	Storage  json.RawMessage   `json:"storage"`
}

type overviewProject struct {
	ProjectID     uint32           `json:"project_id"`
	Received      overviewReceived `json:"received"`
	Stored        overviewStored   `json:"stored"`
	LogsPerSecond overviewRate     `json:"logs_per_second"`
	AllDays       struct {
		Logs      int64   `json:"logs"`
		Bytes     int64   `json:"bytes"`
		OldestDay *string `json:"oldest_day"`
	} `json:"all_days"`
	LastLogAt *time.Time `json:"last_log_at"`
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

func TestTheOverviewAnswersEachDayAndEachProject(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	seedProject(t, env.SQLitePath, 789, 0)

	today, yesterday, fiveDaysAgo := testNow, testNow.AddDate(0, 0, -1), testNow.AddDate(0, 0, -5)
	yesterdayAt, todayAt := yesterday.Add(-2*time.Hour), today.Add(-time.Hour)
	logs := func(at time.Time, n int) []string {
		var each []string
		for i := range n {
			each = append(each, logJSON(t, fmt.Sprintf("log %d", i), at, nil))
		}
		return each
	}
	body := func(logs ...[]string) string {
		var all []string
		for _, l := range logs {
			all = append(all, l...)
		}
		return "[" + strings.Join(all, ",") + "]"
	}

	// Yesterday at 10:00:00, 123 sends 2 Logs and 456 sends 1: 3 in the
	// second. Today at 11:00:00 123 sends 2 of today and 1 of five days
	// ago, and at 11:00:01 1 more. 789 sends nothing.
	sent := map[string]string{
		"yesterday 123": body(logs(yesterdayAt, 2)),
		"yesterday 456": body(logs(yesterdayAt, 1)),
		"today":         body(logs(todayAt, 2), logs(fiveDaysAgo, 1)),
		"today later":   body(logs(todayAt.Add(time.Second), 1)),
	}
	sendLogsAt(t, env, 123, "test_public_key", yesterdayAt, sent["yesterday 123"], nil)
	sendLogsAt(t, env, 456, "other_public_key", yesterdayAt, sent["yesterday 456"], nil)
	sendLogsAt(t, env, 123, "test_public_key", todayAt, sent["today"], nil)
	sendLogsAt(t, env, 123, "test_public_key", todayAt.Add(time.Second), sent["today later"], nil)
	env.Clock.Set(today)
	env.FlushIngestCounts(t)
	env.ProcessLogs(t)

	got := overview(t, env, yesterday, today, 123, 456, 789)
	bytes := func(names ...string) (n int64) {
		for _, name := range names {
			n += int64(len(sent[name]))
		}
		return n
	}

	// Each day: what arrived on it, its busiest second of the Projects
	// together, the average over its seconds so far, and each Project's
	// Logs received.
	if len(got.Days) != 2 {
		t.Fatalf("Expected 2 days, got %+v", got.Days)
	}
	for i, want := range []struct {
		day      string
		received overviewReceived
		seconds  float64
		peak     int64
		peakAt   time.Time
		projects map[uint32]int64
	}{
		{"2026-10-01", overviewReceived{3, bytes("yesterday 123", "yesterday 456"), bytes("yesterday 123", "yesterday 456"), 2}, 86400, 3, yesterdayAt, map[uint32]int64{123: 2, 456: 1}},
		{"2026-10-02", overviewReceived{4, bytes("today", "today later"), bytes("today", "today later"), 2}, 12 * 3600, 3, todayAt, map[uint32]int64{123: 4}},
	} {
		day := got.Days[i]
		if day.Day != want.day || day.Received != want.received {
			t.Errorf("Expected %s to have received %+v, got %s with %+v", want.day, want.received, day.Day, day.Received)
		}
		rate := day.LogsPerSecond
		if math.Abs(rate.Average-float64(want.received.Logs)/want.seconds) > 1e-9 || rate.Peak != want.peak || rate.PeakAt == nil || !rate.PeakAt.Equal(want.peakAt) {
			t.Errorf("Expected %s's Logs a second to average %v and peak at %d at %v, got %+v", want.day, float64(want.received.Logs)/want.seconds, want.peak, want.peakAt, rate)
		}
		projects := map[uint32]int64{}
		for _, p := range day.Projects {
			if p.ReceivedLogs > 0 {
				projects[p.ProjectID] = p.ReceivedLogs
			}
		}
		if fmt.Sprint(projects) != fmt.Sprint(want.projects) {
			t.Errorf("Expected %s's Logs by Project %v, got %v", want.day, want.projects, projects)
		}
	}
	if s := got.Days[1].Stored; s.Logs != 3 {
		t.Errorf("Expected today's 3 Logs stored, got %+v", s)
	}

	// Each Project, in the order asked: the range's totals and rate, every
	// day it keeps, and when its last Log arrived.
	if len(got.Projects) != 3 {
		t.Fatalf("Expected 3 Projects, got %+v", got.Projects)
	}
	web, api, quiet := got.Projects[0], got.Projects[1], got.Projects[2]
	if web.ProjectID != 123 || api.ProjectID != 456 || quiet.ProjectID != 789 {
		t.Errorf("Expected Projects 123, 456 and 789 in order, got %d, %d and %d", web.ProjectID, api.ProjectID, quiet.ProjectID)
	}
	rangeSeconds := float64(86400 + 12*3600)
	for _, want := range []struct {
		got        overviewProject
		received   overviewReceived
		storedLogs int64
		peak       int64
		peakAt     time.Time
		allLogs    int64
		oldest     string
		last       time.Time
	}{
		{web, overviewReceived{6, bytes("yesterday 123", "today", "today later"), bytes("yesterday 123", "today", "today later"), 3}, 5, 3, todayAt, 6, "2026-09-27", todayAt.Add(time.Second)},
		{api, overviewReceived{1, bytes("yesterday 456"), bytes("yesterday 456"), 1}, 1, 1, yesterdayAt, 1, "2026-10-01", yesterdayAt},
	} {
		p := want.got
		if p.Received != want.received || p.Stored.Logs != want.storedLogs || p.Stored.Bytes <= 0 {
			t.Errorf("Expected Project %d to have received %+v and stored %d Logs, got %+v and %+v", p.ProjectID, want.received, want.storedLogs, p.Received, p.Stored)
		}
		rate := p.LogsPerSecond
		if math.Abs(rate.Average-float64(want.received.Logs)/rangeSeconds) > 1e-9 || rate.Peak != want.peak || rate.PeakAt == nil || !rate.PeakAt.Equal(want.peakAt) {
			t.Errorf("Expected Project %d to peak at %d Logs a second at %v, got %+v", p.ProjectID, want.peak, want.peakAt, rate)
		}
		if p.AllDays.Logs != want.allLogs || p.AllDays.Bytes < p.Stored.Bytes || p.AllDays.OldestDay == nil || *p.AllDays.OldestDay != want.oldest {
			t.Errorf("Expected Project %d to keep %d Logs since %s, got %+v", p.ProjectID, want.allLogs, want.oldest, p.AllDays)
		}
		if p.LastLogAt == nil || !p.LastLogAt.Equal(want.last) {
			t.Errorf("Expected Project %d's last Log at %v, got %v", p.ProjectID, want.last, p.LastLogAt)
		}
	}
	if web.AllDays.Bytes <= web.Stored.Bytes {
		t.Errorf("Expected 123's every day to hold more than the range's, got %d and %d", web.AllDays.Bytes, web.Stored.Bytes)
	}
	if quiet.Received != (overviewReceived{}) || quiet.AllDays.Logs != 0 || quiet.AllDays.OldestDay != nil || quiet.LastLogAt != nil || quiet.LogsPerSecond.PeakAt != nil {
		t.Errorf("Expected nothing of Project 789, got %+v", quiet)
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
