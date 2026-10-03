package main_test

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestLateAndFutureLogsAreListedAndNeverPromoted(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// The hot window is 11:00 to 13:00.
	now := testNow.Add(30 * time.Minute)
	env.Clock.Set(now)
	yesterday := testNow.AddDate(0, 0, -1)
	postAndProcess(t, env,
		logJSON(t, "yesterday", yesterday, map[string]any{"user": "ada", "status": 200}),
		logJSON(t, "before the window", time.Date(2026, 10, 2, 10, 59, 59, 0, time.UTC), map[string]any{"user": "bob"}),
		logJSON(t, "first in the window", time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC), map[string]any{"user": "cy"}),
		logJSON(t, "next hour", time.Date(2026, 10, 2, 13, 10, 0, 0, time.UTC), map[string]any{"user": "di"}),
	)

	logs := latestLogs(t, env, 123, 10)
	if got, want := messages(logs), []string{"next hour", "first in the window", "before the window", "yesterday"}; !slices.Equal(got, want) {
		t.Fatalf("Expected %v, got %v", want, got)
	}
	if got, want := attributesByMessage(logs)["yesterday"], map[string]any{"user": "ada", "status": 200.0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected a late Log's Attributes %v, got %v", want, got)
	}

	// Only the Log in the hot window promotes its keys.
	if got := env.LogStore.PromotedSet(123, yesterday); len(got.Keys) != 0 {
		t.Errorf("Expected a late Log to promote nothing, got %v", promotedNames(got))
	}
	if got, want := promotedNames(env.LogStore.PromotedSet(123, now)), []string{"string user"}; !slices.Equal(got, want) {
		t.Errorf("Expected today's set %v, got %v", want, got)
	}

	// A Project whose only Log is late lists it too.
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	resp := sendLogs(t, env, "/api/456/logs", "application/json",
		logJSON(t, "only late", yesterday, map[string]any{"k": "v"}),
		map[string]string{"Authorization": "Bearer other_public_key"})
	decodeLogCounts(t, resp)
	env.ProcessLogs(t)
	if got, want := messages(latestLogs(t, env, 456, 10)), []string{"only late"}; !slices.Equal(got, want) {
		t.Errorf("Expected Project 456 to list %v, got %v", want, got)
	}
}
