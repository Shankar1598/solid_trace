package main_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedProject adds a Project's row to the Console database, with its own
// Retention in days, or the instance default if retentionDays is 0.
func seedProject(t *testing.T, sqlitePath string, projectID uint32, retentionDays int) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqlitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite for seeding: %v", err)
	}
	defer db.Close()
	retention := sql.NullInt64{Int64: int64(retentionDays), Valid: retentionDays != 0}
	if _, err := db.Exec(
		"INSERT INTO projects (id, name, organization_id, log_retention_days, created_at, updated_at) VALUES (?, ?, 1, ?, ?, ?)",
		projectID, fmt.Sprintf("Project %d", projectID), retention, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to seed project %d: %v", projectID, err)
	}
}

// postAndProcessTo sends one request of Logs to a Project with its key and
// processes it.
func postAndProcessTo(t *testing.T, env *TestEnv, projectID uint32, key string, logs ...string) {
	t.Helper()
	resp := sendLogs(t, env, fmt.Sprintf("/api/%d/logs", projectID), "application/json",
		"["+strings.Join(logs, ",")+"]", map[string]string{"Authorization": "Bearer " + key})
	if counts := decodeLogCounts(t, resp); counts.Accepted != len(logs) {
		t.Fatalf("Expected %d Logs accepted by Project %d, got %+v", len(logs), projectID, counts)
	}
	env.ProcessLogs(t)
}

func TestALogOlderThanItsProjectsRetentionGetsItsReceiptTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 7)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	eightDaysAgo := testNow.AddDate(0, 0, -8)
	postAndProcess(t, env, logJSON(t, "eight days old", eightDaysAgo, nil))
	postAndProcessTo(t, env, 456, "other_public_key", logJSON(t, "eight days old", eightDaysAgo, nil))

	if got := latestLogs(t, env, 123, 10); len(got) != 1 || !got[0].Time.Equal(eightDaysAgo) {
		t.Errorf("Expected the default Retention of 30 days to keep the time %v, got %+v", eightDaysAgo, got)
	}
	got := latestLogs(t, env, 456, 10)
	if len(got) != 1 || !got[0].Time.Equal(testNow) || got[0].Attributes["original.time"] != eightDaysAgo.Format(time.RFC3339Nano) {
		t.Errorf("Expected a Retention of 7 days to give the receipt time %v and keep original.time, got %+v", testNow, got)
	}
}
