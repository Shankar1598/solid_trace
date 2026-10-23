package storage

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

// The deletes the Event cleanup runs for a deleted Project: each takes that
// Project's rows and leaves every other Project's.

func TestDeleteProjectEventsDeletesOnlyThatProjectsEventsCursorAndRange(t *testing.T) {
	logger.Init()
	writer, err := NewPebbleWriter(&config.Config{PebblePath: filepath.Join(t.TempDir(), "pebble")})
	if err != nil {
		t.Fatalf("NewPebbleWriter: %v", err)
	}
	defer writer.Close()

	for _, projectID := range []uint32{4, 5, 5, 6} {
		event := models.Event{ProjectID: projectID, RawJSON: []byte(`{"event":"data"}`)}
		if err := writer.WriteEvent(&event); err != nil {
			t.Fatalf("WriteEvent: %v", err)
		}
		if err := writer.SetProcessingCursor(projectID, event.EventUUID); err != nil {
			t.Fatalf("SetProcessingCursor: %v", err)
		}
		if err := writer.RecordEventRange(EventRange{ProjectID: projectID, After: event.EventUUID, Through: event.EventUUID}); err != nil {
			t.Fatalf("RecordEventRange: %v", err)
		}
	}

	if err := writer.DeleteProjectEvents(5); err != nil {
		t.Fatalf("DeleteProjectEvents: %v", err)
	}

	projects, err := writer.ProjectsWithEvents()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projects, []uint32{4, 6}) {
		t.Errorf("projects with events = %v, want [4 6]", projects)
	}
	if events, _ := writer.EventsAfter(5, "", 10); len(events) != 0 {
		t.Errorf("project 5 still has %d events", len(events))
	}
	if cursor, _ := writer.ProcessingCursor(5); cursor != "" {
		t.Errorf("project 5 still has cursor %q", cursor)
	}
	if r, ok, _ := writer.RecordedEventRange(5); ok {
		t.Errorf("project 5 still has range %+v", r)
	}
	for _, projectID := range []uint32{4, 6} {
		if events, _ := writer.EventsAfter(projectID, "", 10); len(events) != 1 {
			t.Errorf("project %d has %d events, want 1", projectID, len(events))
		}
		if cursor, _ := writer.ProcessingCursor(projectID); cursor == "" {
			t.Errorf("project %d lost its cursor", projectID)
		}
		if _, ok, _ := writer.RecordedEventRange(projectID); !ok {
			t.Errorf("project %d lost its range", projectID)
		}
	}

	if err := writer.DeleteProjectEvents(0); err == nil {
		t.Error("DeleteProjectEvents(0) deleted the processing cursors' prefix")
	}
}

func TestDeleteProjectIssuesDeletesOnlyThatProjectsRows(t *testing.T) {
	writer := newTestSQLiteWriter(t)
	for _, projectID := range []uint32{5, 6} {
		issueID, _, _, _, err := writer.FindOrCreateIssue(projectID, "fp", "RuntimeError: boom", "app.rb", "error")
		if err != nil {
			t.Fatalf("FindOrCreateIssue: %v", err)
		}
		seen := []models.Event{{EventUUID: "a", IssueID: issueID, Timestamp: time.Now()}}
		if err := writer.RecordSeenEvents(projectID, seen, "a"); err != nil {
			t.Fatalf("RecordSeenEvents: %v", err)
		}
	}

	if err := writer.DeleteProjectIssues(5); err != nil {
		t.Fatalf("DeleteProjectIssues: %v", err)
	}

	for _, table := range []string{"issue_fingerprints", "issues", "project_issue_counters", "project_seen_cursors"} {
		for projectID, want := range map[uint32]int{5: 0, 6: 1} {
			var n int
			if err := writer.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE project_id = ?", projectID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != want {
				t.Errorf("%s has %d rows of project %d, want %d", table, n, projectID, want)
			}
		}
	}
}
