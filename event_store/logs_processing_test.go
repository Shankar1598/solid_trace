package main_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// processLogsWithin is ProcessLogs with a timeout, for processing that may
// hang.
func processLogsWithin(t *testing.T, env *TestEnv, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- env.LogProcessor.ProcessUntilCaughtUp() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Log processing failed: %v", err)
		}
	case <-time.After(timeout):
		env.LogProcessor.Stop()
		<-done
		t.Fatalf("Log processing didn't return within %v", timeout)
	}
}

// logAt is a Log with a message at an RFC 3339 time.
func logAt(t *testing.T, message, at string) models.Log {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatalf("Parse %q: %v", at, err)
	}
	return models.Log{ID: uuid.Must(uuid.NewV7()), Time: ts.UTC(), Message: message}
}

// unstorableLog is a Log whose time is past DuckDB's TIMESTAMP range, so its
// row can't be appended. Log ingest never stores one: a test writes it with
// storeLogBatch.
func unstorableLog(message string) models.Log {
	return models.Log{
		ID:      uuid.Must(uuid.NewV7()),
		Time:    time.Date(300000, 1, 1, 0, 0, 0, 0, time.UTC),
		Message: message,
	}
}

// storeLogBatch stores Logs for a Project as one Log batch, as Log ingest
// does after normalising a request.
func storeLogBatch(t *testing.T, env *TestEnv, projectID uint32, logs ...models.Log) {
	t.Helper()
	if err := env.PebbleWriter.WriteLogBatch(projectID, logs); err != nil {
		t.Fatalf("Store Log batch: %v", err)
	}
	env.LogProcessor.Notify(projectID)
}

func TestALogDuckDBRejectsIsSkippedAndTheRestOfItsBatchIsStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	output := captureLogs(t)

	postLogs(t, env, "application/json", `{"message": "before", "time": "2026-10-02T11:00:01Z"}`)
	bad := unstorableLog("unstorable")
	storeLogBatch(t, env, 123,
		logAt(t, "same batch, before", "2026-10-02T11:00:02Z"),
		bad,
		logAt(t, "same batch, after", "2026-10-02T11:00:03Z"),
	)
	// A batch of nothing but a bad Log, among the same Logs to store.
	alsoBad := unstorableLog("also unstorable")
	storeLogBatch(t, env, 123, alsoBad)
	postLogs(t, env, "application/json", `{"message": "after", "time": "2026-10-02T11:00:04Z"}`)
	processLogsWithin(t, env, 10*time.Second)

	want := []string{"after", "same batch, after", "same batch, before", "before"}
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	for _, skipped := range []models.Log{bad, alsoBad} {
		if !strings.Contains(output.String(), "skipped a Log") || !strings.Contains(output.String(), skipped.ID.String()) {
			t.Errorf("Expected the skipped Log %s logged, got %q", skipped.ID, output.String())
		}
	}

	// Skipped for good: after a restart nothing is stored again.
	env.Reopen(t)
	processLogsWithin(t, env, 10*time.Second)
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, want) {
		t.Errorf("Expected %v after a restart, got %v", want, got)
	}
}

func TestALoneLogDuckDBRejectsIsSkippedAndLaterLogsAreStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	output := captureLogs(t)

	bad := unstorableLog("unstorable")
	storeLogBatch(t, env, 123, bad)
	processLogsWithin(t, env, 10*time.Second)

	if got := latestLogs(t, env, 123, 10); len(got) != 0 {
		t.Errorf("Expected no Logs stored, got %v", messages(got))
	}
	if !strings.Contains(output.String(), "skipped a Log") || !strings.Contains(output.String(), bad.ID.String()) {
		t.Errorf("Expected the skipped Log %s logged, got %q", bad.ID, output.String())
	}

	postLogs(t, env, "application/json", `{"message": "later", "time": "2026-10-02T11:00:01Z"}`)
	processLogsWithin(t, env, 10*time.Second)

	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"later"}) {
		t.Errorf("Expected only [later], got %v", got)
	}
}

// startLogProcessing runs Log processing until it is caught up, in the
// background. Wait for it with its result.
func startLogProcessing(env *TestEnv) <-chan error {
	done := make(chan error, 1)
	go func() { done <- env.LogProcessor.ProcessUntilCaughtUp() }()
	return done
}

// caughtUpWithin waits for background Log processing to return.
func caughtUpWithin(t *testing.T, env *TestEnv, done <-chan error, timeout time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Log processing failed: %v", err)
		}
	case <-time.After(timeout):
		env.LogProcessor.Stop()
		<-done
		t.Fatalf("Log processing didn't catch up within %v", timeout)
	}
}

// waitFor polls until ok holds, failing the test after timeout.
func waitFor(t *testing.T, timeout time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("Timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// diskFull is how DuckDB fails a commit when the disk is full.
var diskFull = &duckdb.Error{Type: duckdb.ErrorTypeIO, Msg: `IO Error: Could not write file "logs.duckdb.wal": No space left on device`}

func TestLogsWaitOutAStoreFailureAndNoneIsLost(t *testing.T) {
	failures := map[string]func(*flakyLogStore){
		"DuckDB unavailable": func(s *flakyLogStore) { s.goDown() },
		"disk full":          func(s *flakyLogStore) { s.fail(diskFull) },
	}
	for name, breakStore := range failures {
		t.Run(name, func(t *testing.T) {
			env := setupTestEnv(t)
			defer env.Cleanup()
			output := captureLogs(t)

			postLogs(t, env, "application/json", `{"message": "before", "time": "2026-10-02T11:00:01Z"}`)
			env.ProcessLogs(t)

			breakStore(env.FlakyLogs)
			postLogs(t, env, "application/json", `[{"message": "one", "time": "2026-10-02T11:00:02Z"}, {"message": "two", "time": "2026-10-02T11:00:03Z"}]`)
			postLogs(t, env, "application/json", `{"message": "three", "time": "2026-10-02T11:00:04Z"}`)
			tries := env.FlakyLogs.attempts.Load()
			done := startLogProcessing(env)

			// Retried with backoff, and nothing is stored meanwhile.
			waitFor(t, 5*time.Second, "Log processing to retry", func() bool { return env.FlakyLogs.attempts.Load() >= tries+4 })
			if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"before"}) {
				t.Errorf("Expected only [before] while the store fails, got %v", got)
			}

			env.FlakyLogs.recover()
			caughtUpWithin(t, env, done, 10*time.Second)

			want := []string{"three", "two", "one", "before"}
			if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, want) {
				t.Errorf("Expected each Log once, %v, got %v", want, got)
			}
			if !strings.Contains(output.String(), "store error") || strings.Contains(output.String(), "content error") {
				t.Errorf("Expected the failure reported as a store error, got %q", output.String())
			}
			if strings.Contains(output.String(), "skipped a Log") {
				t.Errorf("Expected no Log skipped, got %q", output.String())
			}
		})
	}
}

func TestAFailingLogProjectDoesNotHoldUpOtherProjectsOrEvents(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	env.FlakyLogs.fail(diskFull, 123)
	postLogs(t, env, "application/json", `{"message": "for 123"}`)
	tries := env.FlakyLogs.attempts.Load()
	done := startLogProcessing(env)
	waitFor(t, 5*time.Second, "Project 123 to be retried", func() bool { return env.FlakyLogs.attempts.Load() >= tries+3 })

	// While Project 123 keeps failing, another Project's Logs are stored...
	resp := sendLogs(t, env, "/api/456/logs", "application/json", `{"message": "for 456"}`, map[string]string{"Authorization": "Bearer other_public_key"})
	decodeLogCounts(t, resp)
	waitFor(t, 5*time.Second, "Project 456's Log to be stored", func() bool {
		return slices.Equal(messages(latestLogs(t, env, 456, 10)), []string{"for 456"})
	})
	// ...and Events are processed.
	eventUUID := postEvent(t, env, divideByZero)
	env.Process(t)
	if got := eventUUIDs(listEvents(t, env)); !slices.Equal(got, []string{eventUUID}) {
		t.Errorf("Expected the Event [%s] processed, got %v", eventUUID, got)
	}
	if got := latestLogs(t, env, 123, 10); len(got) != 0 {
		t.Errorf("Expected no Logs stored for Project 123 yet, got %v", messages(got))
	}

	env.FlakyLogs.recover()
	caughtUpWithin(t, env, done, 10*time.Second)
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"for 123"}) {
		t.Errorf("Expected Project 123's Log stored once it recovers, got %v", got)
	}
}

// backlogLines returns the lines of output that say Log processing is behind.
func backlogLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Log processing is behind") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestTheBacklogLineNamesAProjectWhoseLogsWaitTooLong(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	output := captureLogs(t)

	// Project 123's Logs can't be stored; Project 456's can.
	env.FlakyLogs.fail(diskFull, 123)
	postLogs(t, env, "application/json", `{"message": "waiting"}`)
	resp := sendLogs(t, env, "/api/456/logs", "application/json", `{"message": "stored"}`, map[string]string{"Authorization": "Bearer other_public_key"})
	decodeLogCounts(t, resp)
	// A Log batch is as old as the real time it was written at.
	written := time.Now()

	env.Clock.Set(written.Add(30 * time.Second))
	done := startLogProcessing(env)
	waitFor(t, 5*time.Second, "Project 456's Log to be stored", func() bool { return len(latestLogs(t, env, 456, 10)) == 1 })
	if lines := backlogLines(output.String()); len(lines) != 0 {
		t.Errorf("Expected no backlog line for Logs 30 s old, got %q", lines)
	}

	env.Clock.Set(written.Add(2 * time.Minute))
	waitFor(t, 10*time.Second, "the backlog line", func() bool { return len(backlogLines(output.String())) > 0 })
	lines := backlogLines(output.String())
	if !strings.Contains(lines[0], "project_id=123") {
		t.Errorf("Expected the backlog line to name Project 123, got %q", lines)
	}
	for _, line := range lines {
		if strings.Contains(line, "project_id=456") {
			t.Errorf("Expected caught-up Project 456 not named, got %q", line)
		}
	}

	env.FlakyLogs.recover()
	caughtUpWithin(t, env, done, 10*time.Second)
}
