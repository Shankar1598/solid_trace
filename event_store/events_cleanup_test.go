package main_test

import (
	"slices"
	"testing"
)

func TestADeletedProjectsEventsAreGoneAndOtherProjectsKeepTheirs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")
	kept := postEvent(t, env, divideByZero)
	gone := postEventTo(t, env, 456, "other_public_key", divideByZero)
	env.Process(t)

	consoleExec(t, env, "DELETE FROM projects WHERE id = 456")
	if err := env.EventCleanup.DeleteEventsOfDeletedProjects(); err != nil {
		t.Fatalf("Deleting the Events of deleted Projects failed: %v", err)
	}

	assertGone := func(when string) {
		t.Helper()
		if got := listedUUIDs(t, env, "/api/456/events"); len(got) != 0 {
			t.Errorf("Expected the deleted Project's Events gone %s, got %v", when, got)
		}
		if got := countOf(t, env, "/api/456/events/count"); got != 0 {
			t.Errorf("Expected none of the deleted Project's Events counted %s, got %d", when, got)
		}
		if status, _ := getQuery(t, env, "/api/456/events/"+gone); status != 404 {
			t.Errorf("Expected the deleted Project's raw Event gone %s, got %d", when, status)
		}
		var issues int
		sqliteQuery(t, env, &issues, "SELECT count(*) FROM issues WHERE project_id = 456")
		if issues != 0 {
			t.Errorf("Expected the deleted Project's Issues gone %s, got %d", when, issues)
		}
		if got := listedUUIDs(t, env, "/api/123/events"); !slices.Equal(got, []string{kept}) {
			t.Errorf("Expected Project 123 to keep its Event %s, got %v", when, got)
		}
	}
	assertGone("after the pass")
	env.Reopen(t)
	env.Process(t)
	assertGone("after a restart")
}
