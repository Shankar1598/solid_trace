package main_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/storage"
)

// Retention, Project deletion and the disk limit work over families of day
// tables, Logs' the first. standInDays stands in for another family: one
// table partitioned by Project and UTC day as EventStore's are, kept for
// two days by its own Retention.
var standInDays = storage.DayFamily{Name: "stand-ins", Tables: []string{"stand_in_days"}}

// makeStandInDays makes the stand-in family's table, with a row of each
// Project at each time.
func makeStandInDays(t *testing.T, env *TestEnv, rows map[uint32][]time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := env.ClickHouse.Conn().Exec(ctx, `
		CREATE TABLE stand_in_days (project_id UInt32, time DateTime64(6, 'UTC'))
		ENGINE = MergeTree PARTITION BY (project_id, toDate(time)) ORDER BY (project_id, time)`); err != nil {
		t.Fatalf("Making the stand-in table failed: %v", err)
	}
	for projectID, times := range rows {
		for _, at := range times {
			if err := env.ClickHouse.Conn().Exec(ctx, "INSERT INTO stand_in_days VALUES (?, ?)", projectID, at); err != nil {
				t.Fatalf("Inserting a stand-in row failed: %v", err)
			}
		}
	}
}

// standInDaysKept lists the stand-in family's days, as "<Project> <day>".
func standInDaysKept(t *testing.T, env *TestEnv) []string {
	t.Helper()
	rows, err := env.ClickHouse.Conn().Query(context.Background(),
		"SELECT DISTINCT project_id, toDate(time) AS day FROM stand_in_days ORDER BY day, project_id")
	if err != nil {
		t.Fatalf("Reading the stand-in table failed: %v", err)
	}
	defer rows.Close()
	days := []string{}
	for rows.Next() {
		var (
			projectID uint32
			day       time.Time
		)
		if err := rows.Scan(&projectID, &day); err != nil {
			t.Fatal(err)
		}
		days = append(days, fmt.Sprintf("%d %s", projectID, day.Format(time.DateOnly)))
	}
	return days
}

func TestAnotherFamilyOfDayTablesKeepsItsOwnRetentionCountsTowardTheDiskLimitAndGoesWithItsProject(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Project 123 keeps its Logs the default 30 days; Project 456 is
	// deleted.
	seedProject(t, env.SQLitePath, 123, 0)
	seedProject(t, env.SQLitePath, 456, 0)
	at := func(month, day, hour int) time.Time {
		return time.Date(2026, time.Month(month), day, hour, 10, 0, 0, time.UTC)
	}
	postAt(t, env, 123, "test_public_key", "September 28th", at(9, 28, 12))
	postAt(t, env, 123, "test_public_key", "October 2nd", at(10, 2, 11))
	makeStandInDays(t, env, map[uint32][]time.Time{
		123: {at(9, 28, 12), at(10, 1, 9)},
		456: {at(10, 1, 9)},
	})

	settings, err := storage.NewConsoleSettings(env.SQLitePath)
	if err != nil {
		t.Fatalf("Opening Console's settings failed: %v", err)
	}
	defer settings.Close()
	twoDays := func() (map[uint32]int, error) {
		retention, err := settings.ProjectLogRetention()
		for projectID := range retention {
			retention[projectID] = 2
		}
		return retention, err
	}
	cleanup := logprocessing.NewCleanup(env.ClickHouseLogs, env.SQL, storage.LogDiskLimit{}, env.Clock,
		logprocessing.Family{Days: storage.LogDays, Retention: settings.ProjectLogRetention},
		logprocessing.Family{Days: standInDays, Retention: twoDays})

	// At noon on 2 October, two days back is noon on 30 September: the
	// stand-ins' 28 September goes, and the Logs' stays. The deleted
	// Project's stand-ins go with it.
	consoleExec(t, env, "DELETE FROM projects WHERE id = 456")
	env.Clock.Set(testNow)
	if err := cleanup.DeleteDaysPastRetention(env.Clock.Now()); err != nil {
		t.Fatalf("Deleting days past their Retention failed: %v", err)
	}
	if got, want := standInDaysKept(t, env), []string{"123 2026-10-01"}; !slices.Equal(got, want) {
		t.Errorf("Expected the stand-ins' days %v after Retention, got %v", want, got)
	}
	if got, want := messages(latestLogs(t, env, 123, 10)), []string{"October 2nd", "September 28th"}; !slices.Equal(got, want) {
		t.Errorf("Expected the Logs %v after Retention, got %v", want, got)
	}

	// The stand-ins' local disk counts toward the disk limit: at the Logs'
	// own size, the oldest day goes, whichever family's it is.
	use, logsTotal := localDiskUse(t, env)
	enforce := func(bytes int64) {
		t.Helper()
		if err := env.ClickHouseLogs.EnforceDiskLimit(storage.LogDiskLimit{Bytes: bytes}, []storage.DayFamily{storage.LogDays, standInDays}); err != nil {
			t.Fatalf("Enforcing the disk limit failed: %v", err)
		}
	}
	enforce(logsTotal)
	if got, want := messages(latestLogs(t, env, 123, 10)), []string{"October 2nd"}; !slices.Equal(got, want) {
		t.Errorf("Expected the Logs %v past the limit, got %v", want, got)
	}
	enforce(use["123 2026-10-02"])
	if got := standInDaysKept(t, env); len(got) != 0 {
		t.Errorf("Expected the stand-ins' 1 October gone past the limit, got %v", got)
	}
	if got, want := messages(latestLogs(t, env, 123, 10)), []string{"October 2nd"}; !slices.Equal(got, want) {
		t.Errorf("Expected the Logs %v with room for their day, got %v", want, got)
	}
}
