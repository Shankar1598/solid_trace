package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

func TestDuckDBWriter(t *testing.T) {
	tmpDir := t.TempDir()
	logger.Init()
	dbPath := filepath.Join(tmpDir, "test_events.duckdb")
	parquetPath := filepath.Join(tmpDir, "parquet")

	writer, err := NewDuckDBWriter(dbPath, parquetPath, "", "")
	if err != nil {
		t.Fatalf("Failed to create DuckDBWriter: %v", err)
	}
	defer writer.Close()

	timestamp := time.Now().UTC().Truncate(time.Second)
	eventsToInsert := []models.Event{
		{
			EventUUID:          "test-uuid-1",
			ProjectID:          123,
			IssueFingerprintID: 456,
			Timestamp:          timestamp,
			Environment:        "production",
			Tags:               map[string]string{"browser": "chrome"},
		},
		{
			EventUUID:          "test-uuid-2",
			ProjectID:          123,
			IssueFingerprintID: 789,
			Timestamp:          timestamp.Add(time.Minute),
			Environment:        "staging",
			Tags:               map[string]string{"os": "linux"},
		},
	}

	if err := writer.WriteBatch(eventsToInsert); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}

	params := QueryParams{
		ProjectID: 123,
		SortDesc:  false, // Sort by timestamp ASC
	}
	events, err := writer.QueryEvents(params)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}

	// Verify first event
	if events[0].EventUUID != "test-uuid-1" {
		t.Errorf("Expected UUID test-uuid-1, got %s", events[0].EventUUID)
	}
	if events[0].Tags["browser"] != "chrome" {
		t.Errorf("Expected tag browser=chrome, got %v", events[0].Tags["browser"])
	}

	// Verify second event
	if events[1].EventUUID != "test-uuid-2" {
		t.Errorf("Expected UUID test-uuid-2, got %s", events[1].EventUUID)
	}
	if events[1].Environment != "staging" {
		t.Errorf("Expected Environment staging, got %s", events[1].Environment)
	}
	if events[1].Tags["os"] != "linux" {
		t.Errorf("Expected tag os=linux, got %v", events[1].Tags["os"])
	}

	// Test querying by tags
	tagParams := QueryParams{
		ProjectID: 123,
		Tags:      map[string]string{"browser": "chrome"},
	}
	tagEvents, err := writer.QueryEvents(tagParams)
	if err != nil {
		t.Fatalf("QueryEvents (tags) failed: %v", err)
	}
	if len(tagEvents) != 1 {
		t.Fatalf("Expected 1 event for tag browser=chrome, got %d", len(tagEvents))
	}
	if tagEvents[0].EventUUID != "test-uuid-1" {
		t.Errorf("Expected UUID test-uuid-1, got %s", tagEvents[0].EventUUID)
	}

	// Test querying by another tag
	osParams := QueryParams{
		ProjectID: 123,
		Tags:      map[string]string{"os": "linux"},
	}
	osEvents, err := writer.QueryEvents(osParams)
	if err != nil {
		t.Fatalf("QueryEvents (tags) failed: %v", err)
	}
	if len(osEvents) != 1 {
		t.Fatalf("Expected 1 event for tag os=linux, got %d", len(osEvents))
	}
	if osEvents[0].EventUUID != "test-uuid-2" {
		t.Errorf("Expected UUID test-uuid-2, got %s", osEvents[0].EventUUID)
	}

	// Test querying by non-existent tag
	noneParams := QueryParams{
		ProjectID: 123,
		Tags:      map[string]string{"browser": "firefox"},
	}
	noneEvents, err := writer.QueryEvents(noneParams)
	if err != nil {
		t.Fatalf("QueryEvents (tags) failed: %v", err)
	}
	if len(noneEvents) != 0 {
		t.Fatalf("Expected 0 events for tag browser=firefox, got %d", len(noneEvents))
	}

	// Test time filters. Run with a non-UTC TZ to catch the session time zone
	// shifting the comparison against the TIMESTAMP column.
	between := timestamp.Add(30 * time.Second)
	newerEvents, err := writer.QueryEvents(QueryParams{ProjectID: 123, NewerThan: between})
	if err != nil {
		t.Fatalf("QueryEvents (newer than) failed: %v", err)
	}
	if len(newerEvents) != 1 || newerEvents[0].EventUUID != "test-uuid-2" {
		t.Errorf("Expected only test-uuid-2 newer than %v, got %v", between, newerEvents)
	}
	olderEvents, err := writer.QueryEvents(QueryParams{ProjectID: 123, OlderThan: between})
	if err != nil {
		t.Fatalf("QueryEvents (older than) failed: %v", err)
	}
	if len(olderEvents) != 1 || olderEvents[0].EventUUID != "test-uuid-1" {
		t.Errorf("Expected only test-uuid-1 older than %v, got %v", between, olderEvents)
	}

	// Test CountEvents with tags
	count, err := writer.CountEvents(tagParams)
	if err != nil {
		t.Fatalf("CountEvents (tags) failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1 for tag browser=chrome, got %d", count)
	}

	noneCount, err := writer.CountEvents(noneParams)
	if err != nil {
		t.Fatalf("CountEvents (tags) failed: %v", err)
	}
	if noneCount != 0 {
		t.Errorf("Expected count 0 for tag browser=firefox, got %d", noneCount)
	}
}

func TestArchiveEventsUpTo(t *testing.T) {
	tmpDir := t.TempDir()
	logger.Init()
	dbPath := filepath.Join(tmpDir, "test_archive.duckdb")
	parquetPath := filepath.Join(tmpDir, "parquet")

	writer, err := NewDuckDBWriter(dbPath, parquetPath, "", "")
	if err != nil {
		t.Fatalf("Failed to create DuckDBWriter: %v", err)
	}
	defer writer.Close()

	// Use a past date for archiving
	yesterday := time.Now().AddDate(0, 0, -1).UTC().Truncate(24 * time.Hour)
	today := time.Now().UTC().Truncate(time.Second)

	eventsToInsert := []models.Event{
		{
			EventUUID:          "old-event-1",
			ProjectID:          123,
			IssueFingerprintID: 100,
			Timestamp:          yesterday.Add(2 * time.Hour),
			Environment:        "production",
			Tags:               map[string]string{"type": "old"},
		},
		{
			EventUUID:          "old-event-2",
			ProjectID:          123,
			IssueFingerprintID: 101,
			Timestamp:          yesterday.Add(5 * time.Hour),
			Environment:        "production",
			Tags:               map[string]string{"type": "old"},
		},
		{
			EventUUID:          "new-event-1",
			ProjectID:          123,
			IssueFingerprintID: 200,
			Timestamp:          today,
			Environment:        "production",
			Tags:               map[string]string{"type": "new"},
		},
	}

	if err := writer.WriteBatch(eventsToInsert); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}

	// Verify all 3 events exist before archive
	params := QueryParams{ProjectID: 123}
	eventsBefore, err := writer.QueryEvents(params)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(eventsBefore) != 3 {
		t.Fatalf("Expected 3 events before archive, got %d", len(eventsBefore))
	}

	// Archive yesterday's events
	if err := writer.ArchiveEventsUpTo(yesterday); err != nil {
		t.Fatalf("ArchiveEventsUpTo failed: %v", err)
	}

	// Verify parquet file was created
	if files := partitionFiles(t, parquetPath, yesterday, "data-*.parquet"); len(files) != 1 {
		t.Fatalf("Expected 1 parquet file for yesterday, got %v", files)
	}

	// View is automatically recreated after archive, so we can query immediately
	// Verify all 3 events still visible via events view (hot + parquet)
	eventsAfter, err := writer.QueryEvents(params)
	if err != nil {
		t.Fatalf("QueryEvents after archive failed: %v", err)
	}
	if len(eventsAfter) != 3 {
		t.Fatalf("Expected 3 events after archive via unified view, got %d", len(eventsAfter))
	}

	// Verify old events are now from parquet (not in hot table)
	// Query hot table directly to verify only 1 event remains
	var hotCount int
	err = writer.db.QueryRow("SELECT COUNT(*) FROM events_hot").Scan(&hotCount)
	if err != nil {
		t.Fatalf("Failed to count hot table: %v", err)
	}
	if hotCount != 1 {
		t.Errorf("Expected 1 event in hot table after archive, got %d", hotCount)
	}
}

func newArchiveTestWriter(t *testing.T) (*DuckDBWriter, string, string) {
	t.Helper()
	logger.Init()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "archive.duckdb")
	parquetPath := filepath.Join(tmpDir, "parquet")
	writer, err := NewDuckDBWriter(dbPath, parquetPath, "", "")
	if err != nil {
		t.Fatalf("Failed to create DuckDBWriter: %v", err)
	}
	t.Cleanup(func() { writer.Close() })
	return writer, dbPath, parquetPath
}

func archiveTestEvent(uuid string, ts time.Time) models.Event {
	return models.Event{EventUUID: uuid, ProjectID: 123, IssueFingerprintID: 1, Timestamp: ts, Environment: "production"}
}

func countEvents(t *testing.T, writer *DuckDBWriter) int {
	t.Helper()
	events, err := writer.QueryEvents(QueryParams{ProjectID: 123})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range events {
		if seen[e.EventUUID] {
			t.Fatalf("Event %s returned twice", e.EventUUID)
		}
		seen[e.EventUUID] = true
	}
	return len(events)
}

func partitionFiles(t *testing.T, parquetPath string, date time.Time, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(parquetPath, "event_date="+date.Format("2006-01-02"), pattern))
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}
	return files
}

func daysAgo(n int) time.Time {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -n)
}

func TestArchiveAgainKeepsEarlierArchive(t *testing.T) {
	writer, _, parquetPath := newArchiveTestWriter(t)
	d := daysAgo(1)

	if err := writer.WriteBatch([]models.Event{
		archiveTestEvent("d-1", d.Add(time.Hour)),
		archiveTestEvent("d-2", d.Add(2*time.Hour)),
	}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}
	if err := writer.ArchiveEventsUpTo(d); err != nil {
		t.Fatalf("First archive failed: %v", err)
	}

	if err := writer.WriteBatch([]models.Event{archiveTestEvent("d-late", d.Add(3*time.Hour))}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}
	if err := writer.ArchiveEventsUpTo(d); err != nil {
		t.Fatalf("Second archive failed: %v", err)
	}

	if got := countEvents(t, writer); got != 3 {
		t.Errorf("Expected 3 events after re-archive, got %d", got)
	}
	if files := partitionFiles(t, parquetPath, d, "*.parquet"); len(files) != 2 {
		t.Errorf("Expected 2 parquet files for %s, got %v", d.Format("2006-01-02"), files)
	}
}

func TestArchiveMovesEveryDateUpToCutoff(t *testing.T) {
	writer, _, parquetPath := newArchiveTestWriter(t)

	if err := writer.WriteBatch([]models.Event{
		archiveTestEvent("d3", daysAgo(3).Add(time.Hour)),
		archiveTestEvent("d1", daysAgo(1).Add(time.Hour)),
		archiveTestEvent("today", time.Now().UTC()),
	}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}
	if err := writer.ArchiveEventsUpTo(daysAgo(1)); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	for _, d := range []time.Time{daysAgo(3), daysAgo(1)} {
		if files := partitionFiles(t, parquetPath, d, "*.parquet"); len(files) != 1 {
			t.Errorf("Expected 1 parquet file for %s, got %v", d.Format("2006-01-02"), files)
		}
	}
	var hotUUIDs []string
	rows, err := writer.db.Query("SELECT uuid FROM events_hot")
	if err != nil {
		t.Fatalf("Query events_hot failed: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			t.Fatalf("Scan failed: %v", err)
		}
		hotUUIDs = append(hotUUIDs, u)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Rows failed: %v", err)
	}
	if len(hotUUIDs) != 1 || hotUUIDs[0] != "today" {
		t.Errorf("Expected only 'today' in events_hot, got %v", hotUUIDs)
	}
	if got := countEvents(t, writer); got != 3 {
		t.Errorf("Expected 3 events, got %d", got)
	}
}

func TestArchiveWithNothingToMoveWritesNothing(t *testing.T) {
	writer, _, parquetPath := newArchiveTestWriter(t)

	if err := writer.WriteBatch([]models.Event{archiveTestEvent("today", time.Now().UTC())}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}
	if err := writer.ArchiveEventsUpTo(daysAgo(1)); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	entries, err := os.ReadDir(parquetPath)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected empty parquet root, got %d entries", len(entries))
	}
	if got := countEvents(t, writer); got != 1 {
		t.Errorf("Expected 1 event, got %d", got)
	}
}

// stageArchiveFile writes the given hot Events for date to a .tmp archive file,
// as a run does before its commit.
func stageArchiveFile(t *testing.T, writer *DuckDBWriter, parquetPath string, date time.Time, uuids ...string) string {
	t.Helper()
	dir := filepath.Join(parquetPath, "event_date="+date.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	file := filepath.Join(dir, "data-20000101T000000.000000000Z.parquet.tmp")
	quoted := make([]string, len(uuids))
	for i, u := range uuids {
		quoted[i] = "'" + u + "'"
	}
	_, err := writer.db.Exec("COPY (SELECT * FROM events_hot WHERE uuid IN (" + strings.Join(quoted, ",") + ")) TO '" + file + "' (FORMAT PARQUET)")
	if err != nil {
		t.Fatalf("Staging archive file failed: %v", err)
	}
	return file
}

func reopenWriter(t *testing.T, writer *DuckDBWriter, dbPath, parquetPath string) *DuckDBWriter {
	t.Helper()
	writer.Close()
	reopened, err := NewDuckDBWriter(dbPath, parquetPath, "", "")
	if err != nil {
		t.Fatalf("Reopening DuckDBWriter failed: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	return reopened
}

func TestStartupDeletesStagedFileOfUncommittedRun(t *testing.T) {
	writer, dbPath, parquetPath := newArchiveTestWriter(t)
	d := daysAgo(2)
	if err := writer.WriteBatch([]models.Event{
		archiveTestEvent("a", d.Add(time.Hour)),
		archiveTestEvent("b", d.Add(2*time.Hour)),
	}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}
	staged := stageArchiveFile(t, writer, parquetPath, d, "a", "b")

	writer = reopenWriter(t, writer, dbPath, parquetPath)

	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("Expected staged file to be deleted, stat err: %v", err)
	}
	if files := partitionFiles(t, parquetPath, d, "*.parquet"); len(files) != 0 {
		t.Errorf("Expected no parquet files, got %v", files)
	}
	if got := countEvents(t, writer); got != 2 {
		t.Errorf("Expected 2 events, got %d", got)
	}
}

func TestStartupPublishesStagedFileOfCommittedRun(t *testing.T) {
	writer, dbPath, parquetPath := newArchiveTestWriter(t)
	d := daysAgo(2)
	if err := writer.WriteBatch([]models.Event{
		archiveTestEvent("a", d.Add(time.Hour)),
		archiveTestEvent("b", d.Add(2*time.Hour)),
	}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}
	staged := stageArchiveFile(t, writer, parquetPath, d, "a", "b")
	if _, err := writer.db.Exec("DELETE FROM events_hot WHERE uuid IN ('a', 'b')"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	writer = reopenWriter(t, writer, dbPath, parquetPath)

	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("Expected staged file to be renamed, stat err: %v", err)
	}
	if files := partitionFiles(t, parquetPath, d, "*.parquet"); len(files) != 1 {
		t.Errorf("Expected 1 parquet file, got %v", files)
	}
	if got := countEvents(t, writer); got != 2 {
		t.Errorf("Expected 2 events, got %d", got)
	}
}

func TestArchiveRunDeletesStagedFileOfFailedRun(t *testing.T) {
	writer, _, parquetPath := newArchiveTestWriter(t)
	d := daysAgo(2)
	if err := writer.WriteBatch([]models.Event{
		archiveTestEvent("a", d.Add(time.Hour)),
		archiveTestEvent("b", d.Add(2*time.Hour)),
	}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}
	staged := stageArchiveFile(t, writer, parquetPath, d, "a", "b")

	if err := writer.ArchiveEventsUpTo(d); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("Expected staged file to be deleted, stat err: %v", err)
	}
	if files := partitionFiles(t, parquetPath, d, "*"); len(files) != 1 {
		t.Errorf("Expected only this run's parquet file, got %v", files)
	}
	if got := countEvents(t, writer); got != 2 {
		t.Errorf("Expected 2 events, got %d", got)
	}
}

func TestArchiveRunWithNothingToMoveStillPublishesRecoveredFile(t *testing.T) {
	writer, _, parquetPath := newArchiveTestWriter(t)
	d := daysAgo(2)
	if err := writer.WriteBatch([]models.Event{archiveTestEvent("a", d.Add(time.Hour))}); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}
	stageArchiveFile(t, writer, parquetPath, d, "a")
	if _, err := writer.db.Exec("DELETE FROM events_hot WHERE uuid = 'a'"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if err := writer.ArchiveEventsUpTo(d); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	if got := countEvents(t, writer); got != 1 {
		t.Errorf("Expected recovered event to be queryable, got %d events", got)
	}
}

func TestStartupLeavesUnreadableStagedFileInPlace(t *testing.T) {
	writer, dbPath, parquetPath := newArchiveTestWriter(t)
	dir := filepath.Join(parquetPath, "event_date="+daysAgo(2).Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	staged := filepath.Join(dir, "data-20000101T000000.000000000Z.parquet.tmp")
	if err := os.WriteFile(staged, []byte("not parquet"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	writer = reopenWriter(t, writer, dbPath, parquetPath)

	if _, err := os.Stat(staged); err != nil {
		t.Errorf("Expected unreadable staged file to stay, stat err: %v", err)
	}
	if got := countEvents(t, writer); got != 0 {
		t.Errorf("Expected 0 events, got %d", got)
	}
}
