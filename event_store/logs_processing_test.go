package main_test

import (
	"slices"
	"strings"
	"testing"
	"time"

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
	postLogs(t, env, "application/json", `{"message": "after", "time": "2026-10-02T11:00:04Z"}`)
	processLogsWithin(t, env, 10*time.Second)

	want := []string{"after", "same batch, after", "same batch, before", "before"}
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if !strings.Contains(output.String(), "skipped a Log") || !strings.Contains(output.String(), bad.ID.String()) {
		t.Errorf("Expected the skipped Log %s logged, got %q", bad.ID, output.String())
	}

	// Skipped for good: after a restart nothing is stored again.
	env.Reopen(t)
	processLogsWithin(t, env, 10*time.Second)
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, want) {
		t.Errorf("Expected %v after a restart, got %v", want, got)
	}
}
