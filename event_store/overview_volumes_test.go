package main_test

import (
	"encoding/json"
	"testing"
	"time"
)

// Volumes: each Project's Logs and bytes an hour over the last 24 hours, and
// its Sources, from the counts, for agents to size a range before a Query.

type volumesRequest struct {
	ProjectIDs []uint32 `json:"project_ids"`
}

type projectVolume struct {
	ProjectID    uint32  `json:"project_id"`
	LogsPerHour  float64 `json:"logs_per_hour"`
	BytesPerHour float64 `json:"bytes_per_hour"`
	Files        int64   `json:"files"`
}

// volumes asks the query app for the Volumes of projects.
func volumes(t *testing.T, env *TestEnv, projects ...uint32) []projectVolume {
	t.Helper()
	status, raw := postQueryApp(t, env, "/volumes", volumesRequest{ProjectIDs: projects})
	if status != 200 {
		t.Fatalf("Expected the Volumes, got %d: %s", status, raw)
	}
	var answer struct {
		Projects []projectVolume `json:"projects"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode the Volumes %s: %v", raw, err)
	}
	return answer.Projects
}

func TestVolumesAnswerEachProjectsLogsAndBytesAnHourOverTheLast24Hours(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)

	// Every Log is 20 bytes, its message and its Attributes' JSON: "ping"
	// and {"file":"a.log"}, or "job done, all good" and {}.
	at := func(when time.Time, service, file, message string) string {
		attributes := map[string]any{"service": service}
		if file != "" {
			attributes["file"] = file
		}
		return logJSON(t, message, when, attributes)
	}
	postAndProcess(t, env,
		at(testNow, "web", "a.log", "ping"), at(testNow, "web", "a.log", "ping"), at(testNow, "web", "a.log", "ping"),
		at(testNow, "worker", "", "job done, all good"), at(testNow, "worker", "", "job done, all good"),
		at(testNow.Add(-20*time.Hour), "web", "b.log", "pong"),
		// Before the last 24 hours, and days before.
		at(testNow.Add(-30*time.Hour), "web", "a.log", "ping"),
		at(testNow.AddDate(0, 0, -3), "web", "c.log", "ping"),
	)

	got := volumes(t, env, 123, 456)

	// Six Logs in the last 24 hours, of 20 bytes each, from web's a.log and
	// b.log and worker's Logs without a file. Project 456 has none.
	want := []projectVolume{
		{ProjectID: 123, LogsPerHour: 6.0 / 24, BytesPerHour: 6 * 20.0 / 24, Files: 3},
		{ProjectID: 456},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Expected Volumes %+v, got %+v", want, got)
	}
}

func TestVolumesNeedAProject(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	if status, raw := postQueryApp(t, env, "/volumes", volumesRequest{}); status != 400 {
		t.Errorf("Expected no Projects refused with 400, got %d: %s", status, raw)
	}
}
