package main_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/models"
)

// problem is an error or a warning the check route reports in a program.
type problem struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Text    string `json:"text"`
}

// checkAnswer is the check route's answer.
type checkAnswer struct {
	Errors   []problem `json:"errors"`
	Warnings []problem `json:"warnings"`
}

// checkProgram checks a program through the check route, as Console does
// while an admin types it.
func checkProgram(t *testing.T, env *TestEnv, program string) checkAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/transform/check", map[string]string{"program": program})
	if status != 200 {
		t.Fatalf("Check expected 200, got %d: %s", status, raw)
	}
	if !strings.Contains(string(raw), `"errors":[`) || !strings.Contains(string(raw), `"warnings":[`) {
		t.Errorf("Expected errors and warnings as lists, got %s", raw)
	}
	var answer checkAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode check %q: %v", raw, err)
	}
	return answer
}

func TestCheckReportsEachErrorAndWarningWithItsSpan(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	program := ".level = \"info\"\n.x = nope(.message)"
	got := checkProgram(t, env, program)
	if len(got.Errors) != 1 || len(got.Warnings) != 0 {
		t.Fatalf("Expected one error, got %+v", got)
	}
	e := got.Errors[0]
	if e.Code != 105 || e.Message != "call to undefined function" || program[e.Start:e.End] != "nope" ||
		!strings.Contains(e.Text, "error[E105]: call to undefined function") {
		t.Errorf("Expected E105 spanning nope, got %+v", e)
	}

	program = ".a = 1\n\"unused\"\n.b = 2"
	got = checkProgram(t, env, program)
	if len(got.Errors) != 0 || len(got.Warnings) != 1 {
		t.Fatalf("Expected one warning, got %+v", got)
	}
	if w := got.Warnings[0]; program[w.Start:w.End] != `"unused"` || w.Code == 0 || !strings.Contains(w.Text, "warning") {
		t.Errorf("Expected a warning spanning the unused string, got %+v", w)
	}

	if got := checkProgram(t, env, jobLine); len(got.Errors) != 0 || len(got.Warnings) != 0 {
		t.Errorf("Expected nothing wrong with the job line program, got %+v", got)
	}
}

func TestCheckRefusesAProgramOver64KB(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	padded := func(size int) string {
		program := ".a = 1\n"
		return program + "#" + strings.Repeat("x", size-len(program)-1)
	}
	if got := checkProgram(t, env, padded(64<<10)); len(got.Errors) != 0 {
		t.Errorf("Expected a program of 64 KB to pass, got %+v", got)
	}
	program := padded(64<<10 + 1)
	got := checkProgram(t, env, program)
	if len(got.Errors) != 1 || !strings.Contains(got.Errors[0].Message, "64 KB") {
		t.Fatalf("Expected one error naming the limit, got %+v", got)
	}
	if e := got.Errors[0]; e.Start != 64<<10 || e.End != len(program) {
		t.Errorf("Expected the error to span what is past 64 KB, got %d to %d", e.Start, e.End)
	}
}

// previewResult is what a preview's program did with one Log.
type previewResult struct {
	Before  listedLog  `json:"before"`
	After   *listedLog `json:"after"`
	Dropped bool       `json:"dropped"`
	Error   string     `json:"error"`
}

// previewAnswer is the preview route's answer: the results, or the errors
// when the program doesn't compile.
type previewAnswer struct {
	Results []previewResult `json:"results"`
	Kept    int             `json:"kept"`
	Dropped int             `json:"dropped"`
	Failed  int             `json:"failed"`
	Errors  []problem       `json:"errors"`
}

// previewProgram previews a program for Project 123 through the preview
// route, on logs or, when there are none, on the recent arrivals.
func previewProgram(t *testing.T, env *TestEnv, program string, logs ...string) previewAnswer {
	t.Helper()
	status, raw := postPreview(t, env, program, logs...)
	if status != 200 {
		t.Fatalf("Preview expected 200, got %d: %s", status, raw)
	}
	var shapes struct {
		Results []map[string]json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &shapes); err != nil {
		t.Fatalf("Decode preview %q: %v", raw, err)
	}
	for _, result := range shapes.Results {
		keys := slices.Sorted(maps.Keys(result))
		if !slices.Equal(keys, []string{"after", "before"}) && !slices.Equal(keys, []string{"before", "dropped"}) && !slices.Equal(keys, []string{"before", "error"}) {
			t.Errorf("Expected a result of before and one of after, dropped or error, got %v", keys)
		}
	}
	var answer previewAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode preview %q: %v", raw, err)
	}
	return answer
}

func postPreview(t *testing.T, env *TestEnv, program string, logs ...string) (int, []byte) {
	t.Helper()
	body := map[string]any{"program": program}
	if len(logs) > 0 {
		given := make([]json.RawMessage, len(logs))
		for i, log := range logs {
			given[i] = json.RawMessage(log)
		}
		body["logs"] = given
	}
	return postQueryApp(t, env, "/api/123/logs/transform/preview", body)
}

func TestAPreviewOfGivenLogsIsWhatLogProcessingWouldStore(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	program := "if .message == \"noise\" { abort }\n" + jobLine
	logs := []string{
		`{"message": "HardWorker done: 120 ms", "time": "2026-10-02T11:00:00Z", "lvl": "info", "service": "jobs"}`,
		`{"message": "noise", "time": "2026-10-02T11:00:01Z"}`,
		`{"message": "not a job line", "time": "2026-10-02T11:00:02Z", "solidtrace.x": 1}`,
	}
	got := previewProgram(t, env, program, logs...)
	if got.Kept != 1 || got.Dropped != 1 || got.Failed != 1 || len(got.Results) != 3 {
		t.Fatalf("Expected one Log kept, one dropped and one failed, got %+v", got)
	}
	kept, dropped, failed := got.Results[0], got.Results[1], got.Results[2]

	// Each Log is normalised as Log ingest normalises it, before the program.
	if kept.Before.Level != "info" || kept.Before.Service != "jobs" || len(kept.Before.Attributes) != 0 || kept.Before.ID == "" {
		t.Errorf("Expected the Log before as ingest normalises it, got %+v", kept.Before)
	}
	if want := map[string]any{"worker": "HardWorker", "status": "done", "duration_ms": 120.0}; kept.After == nil ||
		!reflect.DeepEqual(kept.After.Attributes, want) || kept.After.ID != kept.Before.ID {
		t.Errorf("Expected the Log after with %v, got %+v", want, kept.After)
	}
	if !dropped.Dropped || dropped.Before.Message != "noise" {
		t.Errorf("Expected noise dropped, got %+v", dropped)
	}
	if failed.Error == "" || failed.Before.Message != "not a job line" || failed.Before.Attributes["original.solidtrace.x"] != 1.0 {
		t.Errorf("Expected the line that isn't a job line failed, got %+v", failed)
	}

	env.ProcessLogs(t)
	if stored := latestLogs(t, env, 123, 10); len(stored) != 0 {
		t.Fatalf("Expected a preview to store nothing, got %+v", stored)
	}

	// Saved, the same program stores what the preview showed.
	seedLogTransform(t, env, 123, program)
	postAndProcess(t, env, logs...)
	stored := latestLogs(t, env, 123, 10)
	if len(stored) != 2 {
		t.Fatalf("Expected two Logs stored, got %+v", stored)
	}
	wantFailed := failed.Before
	wantFailed.Attributes["solidtrace.transform_error"] = failed.Error
	for i, want := range []listedLog{wantFailed, *kept.After} {
		stored[i].ID, want.ID = "", ""
		if !reflect.DeepEqual(stored[i], want) {
			t.Errorf("Expected stored\n%+v\nas previewed\n%+v", stored[i], want)
		}
	}
}

func TestAPreviewTakesUpTo200JSONObjects(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	many := make([]string, 201)
	for i := range many {
		many[i] = fmt.Sprintf(`{"message": "log %d"}`, i)
	}
	if got := previewProgram(t, env, ".n = 1", many[:200]...); got.Kept != 200 {
		t.Errorf("Expected 200 Logs kept, got %d", got.Kept)
	}
	for name, logs := range map[string][]string{
		"201 Logs":     many,
		"a JSON array": {`[{"message": "inside"}]`},
		"a string":     {`"text"`},
	} {
		if status, raw := postPreview(t, env, ".n = 1", logs...); status != 400 {
			t.Errorf("%s: expected 400, got %d: %s", name, status, raw)
		}
	}
}

// serviceLog is a Log of a Service with a message.
func serviceLog(t *testing.T, service, message string) models.Log {
	t.Helper()
	log := logAt(t, message, "2026-10-02T11:00:00Z")
	log.Service = service
	return log
}

func TestAPreviewRunsOnTheNewest100LogsOfEachServiceAsTheyArrived(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, ".stored = true")
	storeLogBatch(t, env, 123, serviceLog(t, "sidekiq", "quiet"))
	env.ProcessLogs(t)
	var busy []models.Log
	for i := range 150 {
		busy = append(busy, serviceLog(t, "indexer", fmt.Sprintf("busy %d", i)))
	}
	storeLogBatch(t, env, 123, busy[:70]...)
	storeLogBatch(t, env, 123, busy[70:]...)
	env.ProcessLogs(t)

	got := previewProgram(t, env, ".seen = true")
	var want []string
	for i := 149; i >= 50; i-- {
		want = append(want, fmt.Sprintf("busy %d", i))
	}
	want = append(want, "quiet")
	var befores []string
	for _, result := range got.Results {
		befores = append(befores, result.Before.Message)
		if len(result.Before.Attributes) != 0 || result.After == nil || result.After.Attributes["seen"] != true || result.After.Attributes["stored"] != nil {
			t.Fatalf("Expected each Log as it arrived, before the saved program, and after the draft, got %+v", result)
		}
	}
	if !slices.Equal(befores, want) || got.Kept != 101 {
		t.Errorf("Expected the newest 100 indexer Logs and the sidekiq one, newest first, got %d kept of %v", got.Kept, befores)
	}

	env.ProcessLogs(t)
	if stored := latestLogs(t, env, 123, 1000); len(stored) != 151 {
		t.Errorf("Expected the 151 Logs Log processing stored and none from the preview, got %d", len(stored))
	}
	if other := previewProgram(t, env, ".seen = true"); len(other.Results) != 101 {
		t.Errorf("Expected a second preview on the same Logs, got %d", len(other.Results))
	}
}

func TestTheRecentArrivalsKeepAtMost4MBOfAProjectsLogs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	var big []models.Log
	for i := range 5 {
		big = append(big, serviceLog(t, "web", fmt.Sprintf("%d %s", i, strings.Repeat("x", 900<<10))))
	}
	storeLogBatch(t, env, 123, big...)
	env.ProcessLogs(t)

	got := previewProgram(t, env, "abort")
	var heads []string
	for _, result := range got.Results {
		heads = append(heads, result.Before.Message[:1])
	}
	if !slices.Equal(heads, []string{"4", "3", "2", "1"}) || got.Dropped != 4 {
		t.Errorf("Expected the newest four Logs of about 900 KB, got %v", heads)
	}
}

// transformStats is the stats route's answer.
type transformStats struct {
	VersionID     *int64  `json:"version_id"`
	Processed     int     `json:"processed"`
	Dropped       int     `json:"dropped"`
	Failed        int     `json:"failed"`
	AverageMicros float64 `json:"average_micros"`
	CompileError  *string `json:"compile_error"`
}

// statsOf reads the running counts of Project 123's Log transform through
// the stats route.
func statsOf(t *testing.T, env *TestEnv) transformStats {
	t.Helper()
	req, _ := http.NewRequest("GET", "/api/123/logs/transform/stats", nil)
	resp, err := env.QueryApp.Test(req, 10000)
	if err != nil {
		t.Fatalf("GET stats failed: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("Stats expected 200, got %d: %s", resp.StatusCode, raw)
	}
	var stats transformStats
	if err := json.Unmarshal(raw, &stats); err != nil {
		t.Fatalf("Decode stats %q: %v", raw, err)
	}
	return stats
}

// currentVersion is the id of Project 123's current Log transform version.
func currentVersion(t *testing.T, env *TestEnv) int64 {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()
	var id int64
	if err := db.QueryRow("SELECT max(id) FROM log_transform_versions WHERE project_id = 123").Scan(&id); err != nil {
		t.Fatalf("Read current version: %v", err)
	}
	return id
}

func TestStatsCountTheCurrentVersionsLogsAndResetOnANewVersion(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	if got := statsOf(t, env); got != (transformStats{}) {
		t.Errorf("Expected no version and no counts without a Log transform, got %+v", got)
	}

	seedLogTransform(t, env, 123, "if .message == \"noise\" { abort }\n"+jobLine)
	first := currentVersion(t, env)
	storeLogBatch(t, env, 123, logAt(t, "HardWorker done: 120 ms", "2026-10-02T11:00:00Z"), logAt(t, "noise", "2026-10-02T11:00:01Z"))
	env.ProcessLogs(t)
	storeLogBatch(t, env, 123, logAt(t, "noise", "2026-10-02T11:00:02Z"), logAt(t, "not a job line", "2026-10-02T11:00:03Z"),
		logAt(t, "SlowWorker fail: 900 ms", "2026-10-02T11:00:04Z"))
	env.ProcessLogs(t)
	got := statsOf(t, env)
	if got.VersionID == nil || *got.VersionID != first || got.Processed != 5 || got.Dropped != 2 || got.Failed != 1 || got.CompileError != nil {
		t.Errorf("Expected version %d with 5 processed, 2 dropped and 1 failed, got %+v", first, got)
	}
	if got.AverageMicros <= 0 {
		t.Errorf("Expected the average time the program took, got %v", got.AverageMicros)
	}

	seedLogTransform(t, env, 123, ".v = 2")
	second := currentVersion(t, env)
	if got := statsOf(t, env); got.VersionID == nil || *got.VersionID != second || got.Processed != 0 || got.AverageMicros != 0 {
		t.Errorf("Expected the counts reset for version %d before it runs, got %+v", second, got)
	}
	storeLogBatch(t, env, 123, logAt(t, "after", "2026-10-02T11:00:05Z"))
	env.ProcessLogs(t)
	if got := statsOf(t, env); got.VersionID == nil || *got.VersionID != second || got.Processed != 1 || got.Dropped != 0 || got.Failed != 0 {
		t.Errorf("Expected version %d with 1 processed, got %+v", second, got)
	}

	seedLogTransform(t, env, 123, "")
	if got := statsOf(t, env); got != (transformStats{}) {
		t.Errorf("Expected no version and no counts with an empty program, got %+v", got)
	}
}

func TestStatsReportAVersionThatDoesNotCompile(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, ".x = ")
	got := statsOf(t, env)
	if got.CompileError == nil || !strings.Contains(*got.CompileError, "syntax error") || got.Processed != 0 {
		t.Errorf("Expected the compile error before any Log, got %+v", got)
	}
	storeLogBatch(t, env, 123, logAt(t, "one", "2026-10-02T11:00:00Z"), logAt(t, "two", "2026-10-02T11:00:01Z"))
	env.ProcessLogs(t)
	got = statsOf(t, env)
	if got.CompileError == nil || !strings.Contains(*got.CompileError, "syntax error") || got.Processed != 2 || got.Failed != 2 {
		t.Errorf("Expected the compile error with both Logs failed, got %+v", got)
	}
}

func TestALogBatchStoredOnASecondTryIsCountedAndArrivesOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedLogTransform(t, env, 123, jobLine)
	env.FlakyLogs.fail(diskFull, 123)
	storeLogBatch(t, env, 123, logAt(t, "HardWorker done: 120 ms", "2026-10-02T11:00:00Z"))
	tries := env.FlakyLogs.attempts.Load()
	done := startLogProcessing(env)
	waitFor(t, 5*time.Second, "Log processing to retry", func() bool { return env.FlakyLogs.attempts.Load() >= tries+3 })
	env.FlakyLogs.recover()
	caughtUpWithin(t, env, done, 10*time.Second)

	if got := statsOf(t, env); got.Processed != 1 {
		t.Errorf("Expected the Log counted once, got %+v", got)
	}
	if got := previewProgram(t, env, ".n = 1"); len(got.Results) != 1 {
		t.Errorf("Expected the Log among the recent arrivals once, got %+v", got.Results)
	}
}
