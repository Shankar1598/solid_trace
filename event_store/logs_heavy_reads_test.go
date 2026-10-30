package main_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/solidtrace/event_store/handler"
)

// lastTwoHours is the 2 hours up to testNow. A search of tagalysSources
// over them reads about 3.2 GB: past a quarter of the budget, so heavy, but
// within it.
var lastTwoHours = struct{ from, to time.Time }{from: testNow.Add(-2 * time.Hour), to: testNow}

// busyRefusalOf is a run's refusal, failing the test unless it is a busy
// one: the message to run them one at a time, and no ways to read less.
func busyRefusalOf(t *testing.T, what string, answer map[string]json.RawMessage) {
	t.Helper()
	refusal := refusalOf(t, answer)
	var busy struct {
		Busy bool `json:"busy"`
	}
	if err := json.Unmarshal(answer["refusal"], &busy); err != nil {
		t.Fatal(err)
	}
	if !busy.Busy || refusal.Message != "Another large Query is running. Run them one at a time." {
		t.Errorf("%s: expected a busy refusal, got %s", what, answer["refusal"])
	}
	if refusal.LargestRange != nil || len(refusal.TopFiles) != 0 || refusal.NarrowerQuery != nil {
		t.Errorf("%s: expected no ways to read less, as the read fits, got %+v", what, refusal)
	}
	if !refusal.Estimate.Heavy || refusal.Estimate.OverBudget {
		t.Errorf("%s: expected the estimate of a heavy read within the budget, got %+v", what, refusal.Estimate)
	}
}

// heavyReadsTestEnv is a TestEnv whose Project 123 is as busy as tagalys,
// with one heavy-read slot.
func heavyReadsTestEnv(t *testing.T) *TestEnv {
	t.Helper()
	env := setupTestEnv(t)
	seedProject(t, env.SQLitePath, 123, 0)
	seedBusyProject(t, env, 123, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)
	env.LogsHandler.HeavyReads = handler.NewHeavyReads(1)
	return env
}

func TestAHeavyReadIsRefusedWhileAnotherRunsAndRunsOnceItEnds(t *testing.T) {
	env := heavyReadsTestEnv(t)
	defer env.Cleanup()
	query := `search "timeout" | summarize count() by file`
	// Another large Query is running.
	release, _ := env.LogsHandler.HeavyReads.Take(123, `search "deadlock"`, lastTwoHours.from, lastTwoHours.to)

	busyRefusalOf(t, "a checked run", checkedRun(t, env, query, lastTwoHours.from, lastTwoHours.to, nil))
	busyRefusalOf(t, "a forced run", checkedRun(t, env, query, lastTwoHours.from, lastTwoHours.to, map[string]any{"force": true}))
	busyRefusalOf(t, "its histogram", answerOf(t, env, "/api/123/logs/histogram", map[string]any{
		"query": query, "from": lastTwoHours.from, "to": lastTwoHours.to, "check_budget": true}, 200))

	release()
	ran(t, "the run once the other ended", checkedRun(t, env, query, lastTwoHours.from, lastTwoHours.to, nil))
	if held := env.LogsHandler.HeavyReads.Held(); held != 0 {
		t.Errorf("Expected the run to give its slot back when it ended, got %d held", held)
	}
}

// The Logs explorer runs a Query and its histogram at once.
func TestTheHistogramOfAHeavyQueryRunsBesideItsRun(t *testing.T) {
	env := heavyReadsTestEnv(t)
	defer env.Cleanup()
	query := `search "timeout"`
	// The explorer's run of the Query is reading.
	release, _ := env.LogsHandler.HeavyReads.Take(123, query, lastTwoHours.from, lastTwoHours.to)
	defer release()

	histogram := func(from time.Time) map[string]json.RawMessage {
		return answerOf(t, env, "/api/123/logs/histogram", map[string]any{
			"query": query, "from": from, "to": lastTwoHours.to, "check_budget": true}, 200)
	}
	if answer := histogram(lastTwoHours.from); answer["buckets"] == nil {
		t.Errorf("Expected the histogram of the running Query, got %s", mustMarshal(t, answer))
	}
	busyRefusalOf(t, "the Query over another range", histogram(lastTwoHours.from.Add(30*time.Minute)))
}

// The Logs explorer's user edits a large search while its read runs on:
// Console still waits on it after the browser has gone.
func TestAnExplorerRunReplacesItsUsersOlderHeavyReadAndNoOneElses(t *testing.T) {
	env := heavyReadsTestEnv(t)
	defer env.Cleanup()
	from, to := lastTwoHours.from, lastTwoHours.to
	query := `search "timeout" | summarize count() by file`
	olderStopped := false
	older := func() {
		olderStopped = false
		env.LogsHandler.HeavyReads.Replace("user 7", func() { olderStopped = true }, 123, `search "deadlock"`, from, to)
	}
	older()

	busyRefusalOf(t, "an MCP run, which replaces nothing", checkedRun(t, env, query, from, to, nil))
	busyRefusalOf(t, "another user's explorer run", checkedRun(t, env, query, from, to, map[string]any{"replaces_reads_of": "user 8"}))
	if olderStopped {
		t.Fatal("Expected the older read left running by reads that may not replace it")
	}

	ran(t, "the user's newer explorer run", checkedRun(t, env, query, from, to, map[string]any{"replaces_reads_of": "user 7"}))
	if !olderStopped {
		t.Error("Expected the user's older read stopped by their newer run")
	}
	if held := env.LogsHandler.HeavyReads.Held(); held != 0 {
		t.Errorf("Expected no slot held once the newer run ended, got %d", held)
	}

	older()
	answer := answerOf(t, env, "/api/123/logs/histogram", map[string]any{
		"query": query, "from": from, "to": to, "check_budget": true, "force": true, "replaces_reads_of": "user 7"}, 200)
	if answer["buckets"] == nil || !olderStopped {
		t.Errorf("Expected Run anyway's histogram to replace the user's older read, got stopped %v and %s", olderStopped, mustMarshal(t, answer))
	}
}

func TestReadsThatArentHeavyOrArentCheckedRunWhileAHeavyReadDoes(t *testing.T) {
	env := heavyReadsTestEnv(t)
	defer env.Cleanup()
	release, _ := env.LogsHandler.HeavyReads.Take(123, `search "deadlock"`, lastTwoHours.from, lastTwoHours.to)
	defer release()

	ran(t, "a Query that doesn't match text, over a week",
		checkedRun(t, env, `where level == "error" | summarize count() by file`, lastWeek.from, lastWeek.to, nil))
	ran(t, "a search under a quarter of the budget",
		checkedRun(t, env, `search "timeout" | summarize count() by file`, testNow.Add(-10*time.Minute), testNow, nil))
	ran(t, "a Chart's heavy search, unchecked", answerOf(t, env, "/api/123/logs/query", map[string]any{
		"query": `search "timeout" | summarize count() by bin_auto(time)`, "from": lastTwoHours.from, "to": lastTwoHours.to}, 200))
}
