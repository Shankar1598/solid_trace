package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The Overview's Source counts: the Logs and bytes Log processing kept, by
// Project, day, Service and file, and the Logs the Log transform dropped.

type overviewKept struct {
	Logs  int64 `json:"logs"`
	Bytes int64 `json:"bytes"`
}

type overviewSource struct {
	ProjectID          uint32 `json:"project_id"`
	Service            string `json:"service"`
	File               string `json:"file"`
	Logs               int64  `json:"logs"`
	Bytes              int64  `json:"bytes"`
	DroppedByTransform int64  `json:"dropped_by_transform"`
}

// sourcesAnswer is the part of the Overview Source counts answer.
type sourcesAnswer struct {
	Band struct {
		Kept               overviewKept `json:"kept"`
		DroppedByTransform int64        `json:"dropped_by_transform"`
	} `json:"band"`
	Projects []struct {
		ProjectID          uint32       `json:"project_id"`
		Kept               overviewKept `json:"kept"`
		DroppedByTransform int64        `json:"dropped_by_transform"`
	} `json:"projects"`
	Sources struct {
		ByLogs  []overviewSource `json:"by_logs"`
		ByBytes []overviewSource `json:"by_bytes"`
	} `json:"sources"`
}

// sourcesOverview asks the query app for the Overview of projects over the
// UTC days from to to, and reads its Source counts.
func sourcesOverview(t *testing.T, env *TestEnv, from, to time.Time, projects ...uint32) sourcesAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, "/overview", overviewRequest{
		ProjectIDs: projects, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly),
	})
	if status != 200 {
		t.Fatalf("Expected the Overview, got %d: %s", status, raw)
	}
	var answer sourcesAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode the Overview %s: %v", raw, err)
	}
	return answer
}

// sourceLogs is n Logs of a Service and file ("" for none) at testNow.
func sourceLogs(t *testing.T, n int, service, file, message string) []string {
	t.Helper()
	attributes := map[string]any{}
	if service != "" {
		attributes["service"] = service
	}
	if file != "" {
		attributes["file"] = file
	}
	logs := make([]string, n)
	for i := range logs {
		logs[i] = logJSON(t, message, testNow, attributes)
	}
	return logs
}

func TestTheOverviewAnswersWhatWasKeptAndTheBiggestSources(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	seedLogTransform(t, env, 123, `if .message == "drop" { abort }`)

	// A Log's bytes are its message and its Attributes' JSON: "hello" and
	// {"file":"a.log"} are 5 + 16 bytes, "job done" and {} are 8 + 2.
	var web []string
	web = append(web, sourceLogs(t, 3, "web", "a.log", "hello")...)
	web = append(web, sourceLogs(t, 2, "web", "a.log", "drop")...)
	web = append(web, sourceLogs(t, 2, "worker", "", "job done")...)
	postAndProcessTo(t, env, 123, "test_public_key", web...)
	postAndProcessTo(t, env, 456, "other_public_key", sourceLogs(t, 1, "api", "b.log", "big message, much bigger")...)

	got := sourcesOverview(t, env, testNow, testNow, 123, 456)

	if want := (overviewKept{Logs: 6, Bytes: 3*21 + 2*10 + 24 + 16}); got.Band.Kept != want {
		t.Errorf("Expected %+v kept, got %+v", want, got.Band.Kept)
	}
	if got.Band.DroppedByTransform != 2 {
		t.Errorf("Expected 2 Logs dropped by the Log transform, got %d", got.Band.DroppedByTransform)
	}
	if len(got.Projects) != 2 || got.Projects[0].Kept != (overviewKept{5, 83}) || got.Projects[0].DroppedByTransform != 2 ||
		got.Projects[1].Kept != (overviewKept{1, 40}) {
		t.Errorf("Expected each Project's kept Logs, got %+v", got.Projects)
	}

	// web's a.log sent the most, its dropped Logs too; api's b.log is the
	// biggest by bytes. A dropped Log counts under where it came from.
	webFile := overviewSource{ProjectID: 123, Service: "web", File: "a.log", Logs: 3, Bytes: 63, DroppedByTransform: 2}
	worker := overviewSource{ProjectID: 123, Service: "worker", Logs: 2, Bytes: 20}
	api := overviewSource{ProjectID: 456, Service: "api", File: "b.log", Logs: 1, Bytes: 40}
	assertSources(t, "by Logs", got.Sources.ByLogs, webFile, worker, api)
	assertSources(t, "by bytes", got.Sources.ByBytes, webFile, api, worker)

	// They're by the Logs' own day.
	if yesterday := sourcesOverview(t, env, testNow.AddDate(0, 0, -1), testNow.AddDate(0, 0, -1), 123, 456); yesterday.Band.Kept != (overviewKept{}) ||
		len(yesterday.Sources.ByLogs) != 0 {
		t.Errorf("Expected nothing kept yesterday, got %+v", yesterday)
	}
}

func assertSources(t *testing.T, ranked string, got []overviewSource, want ...overviewSource) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("Expected %d sources %s, got %+v", len(want), ranked, got)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Expected source %d %s to be %+v, got %+v", i+1, ranked, want[i], got[i])
		}
	}
}

func TestLogsTheLogTransformDropsAllOfAreStillCounted(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedLogTransform(t, env, 123, `abort`)

	postAndProcessTo(t, env, 123, "test_public_key", sourceLogs(t, 3, "web", "a.log", "hello")...)

	got := sourcesOverview(t, env, testNow, testNow, 123)
	if got.Band.Kept != (overviewKept{}) || got.Band.DroppedByTransform != 3 {
		t.Errorf("Expected nothing kept and 3 Logs dropped, got %+v", got.Band)
	}
	assertSources(t, "by Logs", got.Sources.ByLogs, overviewSource{ProjectID: 123, Service: "web", File: "a.log", DroppedByTransform: 3})
}

func TestARangeOfLogsInsertedAgainIsCountedOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)

	// The insert lands, but Log processing hears it failed, and inserts the
	// range again.
	env.FlakyLogs.failAfterInsert(connectionLost, nil)
	body := "[" + strings.Join(sourceLogs(t, 2, "web", "a.log", "hello"), ",") + "]"
	if counts := postLogs(t, env, "application/json", body); counts.Accepted != 2 {
		t.Fatalf("Expected 2 Logs accepted, got %+v", counts)
	}
	processLogsWithin(t, env, 10*time.Second)

	if got := sourcesOverview(t, env, testNow, testNow, 123); got.Band.Kept != (overviewKept{2, 42}) {
		t.Errorf("Expected 2 Logs kept, each counted once, got %+v", got.Band.Kept)
	}
}

func TestRetentionAndProjectDeletionDeleteSourceCounts(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 7)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	// Source counts are by the Logs' own day, as Retention deletes them.
	tenDaysAgo := testNow.AddDate(0, 0, -10)
	postAt(t, env, 123, "test_public_key", "old", tenDaysAgo)
	postAt(t, env, 123, "test_public_key", "new", testNow)
	postAt(t, env, 456, "other_public_key", "old", tenDaysAgo)
	env.Clock.Set(testNow)

	kept := func(projectID uint32) int64 {
		return sourcesOverview(t, env, tenDaysAgo, testNow, projectID).Band.Kept.Logs
	}
	env.DeleteLogsPastRetention(t)
	if got := kept(123); got != 1 {
		t.Errorf("Expected Retention of 7 days to keep only today's counts, got %d Logs", got)
	}
	if got := kept(456); got != 1 {
		t.Errorf("Expected Retention of 30 days to keep the counts, got %d Logs", got)
	}

	consoleExec(t, env, "DELETE FROM projects WHERE id = 456")
	env.DeleteLogsPastRetention(t)
	if got := kept(456); got != 0 {
		t.Errorf("Expected a deleted Project's counts deleted, got %d Logs", got)
	}
}

// countedAnswer is the part of the Overview that the kept-over-stored ratio
// reads: the stored bytes of the days with Source counts, and when they
// begin.
type countedAnswer struct {
	SourceCountsSince *string `json:"source_counts_since"`
	Band              struct {
		Stored             overviewStored `json:"stored"`
		StoredCountedBytes int64          `json:"stored_counted_bytes"`
	} `json:"band"`
	Days []struct {
		Day    string         `json:"day"`
		Stored overviewStored `json:"stored"`
	} `json:"days"`
	Projects []struct {
		ProjectID          uint32 `json:"project_id"`
		StoredCountedBytes int64  `json:"stored_counted_bytes"`
	} `json:"projects"`
}

func countedOverview(t *testing.T, env *TestEnv, from, to time.Time, projects ...uint32) countedAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, "/overview", overviewRequest{
		ProjectIDs: projects, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly),
	})
	if status != 200 {
		t.Fatalf("Expected the Overview, got %d: %s", status, raw)
	}
	var answer countedAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode the Overview %s: %v", raw, err)
	}
	return answer
}

func TestTheOverviewAnswersTheStoredBytesOfOnlyTheDaysWithSourceCounts(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	yesterday := testNow.AddDate(0, 0, -1)
	postAndProcessTo(t, env, 123, "test_public_key",
		logJSON(t, "today", testNow, nil), logJSON(t, "yesterday", yesterday, map[string]any{"service": "web"}))
	// Yesterday's Logs as if stored before Source counts began.
	dropSourceCounts := func(day time.Time) {
		t.Helper()
		if err := env.ClickHouse.Conn().Exec(context.Background(),
			fmt.Sprintf("ALTER TABLE log_source_counts DROP PARTITION ID '123-%s'", day.Format("20060102"))); err != nil {
			t.Fatalf("Drop the Source counts of %s: %v", day.Format(time.DateOnly), err)
		}
	}
	dropSourceCounts(yesterday)

	got := countedOverview(t, env, testNow.AddDate(0, 0, -6), testNow, 123)

	var today int64
	for _, d := range got.Days {
		if d.Day == testNow.Format(time.DateOnly) {
			today = d.Stored.Bytes
		}
	}
	if today <= 0 || got.Band.Stored.Bytes <= today {
		t.Fatalf("Expected both days stored, today's %d bytes of %d, got %+v", today, got.Band.Stored.Bytes, got)
	}
	if got.Band.StoredCountedBytes != today || len(got.Projects) != 1 || got.Projects[0].StoredCountedBytes != today {
		t.Errorf("Expected only today's %d stored bytes counted, in the band and the Project, got %+v", today, got)
	}
	if got.SourceCountsSince == nil || *got.SourceCountsSince != testNow.Format(time.DateOnly) {
		t.Errorf("Expected Source counts since today, got %v", got.SourceCountsSince)
	}

	dropSourceCounts(testNow)
	if none := countedOverview(t, env, testNow.AddDate(0, 0, -6), testNow, 123); none.Band.StoredCountedBytes != 0 || none.SourceCountsSince != nil {
		t.Errorf("Expected no day counted and no Source counts, got %+v", none)
	}
}
