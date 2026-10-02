package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"
)

// logCounts is the JSON route's answer.
type logCounts struct {
	Accepted int `json:"accepted"`
	Dropped  int `json:"dropped"`
}

// listedLog is one Log as the query app lists it.
type listedLog struct {
	ID          string         `json:"id"`
	Time        time.Time      `json:"time"`
	Level       string         `json:"level"`
	Message     string         `json:"message"`
	Service     string         `json:"service"`
	Environment string         `json:"environment"`
	TraceID     string         `json:"trace_id"`
	SpanID      string         `json:"span_id"`
	Attributes  map[string]any `json:"attributes"`
}

// sendLogs posts a body to a Project's JSON Log route.
func sendLogs(t *testing.T, env *TestEnv, path, contentType, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", contentType)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := env.App.Test(req, 2000)
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	return resp
}

// postLogs sends Logs to Project 123 with its key and returns the counts.
func postLogs(t *testing.T, env *TestEnv, contentType, body string) logCounts {
	t.Helper()
	resp := sendLogs(t, env, "/api/123/logs?sentry_key=test_public_key", contentType, body, nil)
	return decodeLogCounts(t, resp)
}

func decodeLogCounts(t *testing.T, resp *http.Response) logCounts {
	t.Helper()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, raw)
	}
	var counts logCounts
	if err := json.Unmarshal(raw, &counts); err != nil {
		t.Fatalf("Decode counts %q: %v", raw, err)
	}
	return counts
}

// latestLogs returns a Project's latest Logs from the query app, newest first.
func latestLogs(t *testing.T, env *TestEnv, projectID uint32, limit int) []listedLog {
	t.Helper()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/%d/logs/latest?limit=%d", projectID, limit), nil)
	resp, err := env.QueryApp.Test(req, 2000)
	if err != nil {
		t.Fatalf("Latest Logs request failed: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("Latest Logs expected 200, got %d: %s", resp.StatusCode, raw)
	}
	var body struct {
		Logs []listedLog `json:"logs"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("Decode latest Logs %q: %v", raw, err)
	}
	return body.Logs
}

func TestJSONLogIsListedWithEveryFieldAfterProcessing(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	counts := postLogs(t, env, "application/json", `{
		"message": "user signed in",
		"time": "2026-10-02T11:59:00.25Z",
		"level": "info",
		"service": "web",
		"environment": "production",
		"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
		"span_id": "00f067aa0ba902b7",
		"user": "ada",
		"status": 200,
		"duration_ms": 12.5,
		"cached": true,
		"roles": ["admin", "dev"],
		"http": {"method": "GET"}
	}`)
	if counts != (logCounts{Accepted: 1}) {
		t.Fatalf("Expected 1 accepted, got %+v", counts)
	}

	if logs := latestLogs(t, env, 123, 10); len(logs) != 0 {
		t.Fatalf("Expected no Logs listed before processing, got %d", len(logs))
	}
	env.ProcessLogs(t)

	logs := latestLogs(t, env, 123, 10)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 Log, got %d", len(logs))
	}
	got := logs[0]
	if got.ID == "" {
		t.Errorf("Expected the Log to have an id")
	}
	got.ID = ""
	want := listedLog{
		Time:        time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC),
		Level:       "info",
		Message:     "user signed in",
		Service:     "web",
		Environment: "production",
		TraceID:     "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:      "00f067aa0ba902b7",
		Attributes: map[string]any{
			"user":        "ada",
			"status":      200.0,
			"duration_ms": 12.5,
			"cached":      true,
			"roles":       []any{"admin", "dev"},
			"http":        map[string]any{"method": "GET"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
}
