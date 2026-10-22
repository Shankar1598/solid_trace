package main_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// onTestDay is a time on testNow's day.
func onTestDay(hour, minute int) time.Time {
	return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC)
}

// seedHourInEverySource stores Logs of hour 10 on testNow's day in each of
// its sources: file in a Log file, late as late Logs, and hot in the hot
// table. late may also hold Logs before hour 10, and hot Logs of hour 11.
func seedHourInEverySource(t *testing.T, env *TestEnv, file, late, hot []string) {
	t.Helper()
	// At 10:29 the hot window is 10:00 to 11:00; at 12:05 the export takes
	// hour 10 to a file, and the window is 11:45 to 13:00.
	postWhileHot(t, env, file...)
	env.Clock.Set(onTestDay(12, 5))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, onTestDay(10, 0), 1)
	postAndProcess(t, env, late...)
	// The clock steps back, so hour 10 is in the hot window again.
	postWhileHot(t, env, hot...)
	env.Clock.Set(onTestDay(11, 30))
}

// assertPagesOfEverySize checks that a Query over all time, paged with every
// limit from 1 to one past its Logs, lists want, newest first, with every
// page but the last full.
func assertPagesOfEverySize(t *testing.T, env *TestEnv, query string, want []string) {
	t.Helper()
	for limit := 1; limit <= len(want)+1; limit++ {
		pages := pageThrough(t, env, queryRequest{Query: query, From: allTime.from, To: allTime.to, Limit: limit})
		if got := slices.Concat(pages...); !slices.Equal(got, want) {
			t.Errorf("%q in pages of %d: expected %v, got %v", query, limit, want, pages)
			continue
		}
		for i, page := range pages[:len(pages)-1] {
			if len(page) != limit {
				t.Errorf("%q in pages of %d: expected page %d full, got %v", query, limit, i+1, pages)
			}
		}
	}
}

func TestLogsAtAPagesLastTimeAreSplitByIDAcrossTheHotTableLateLogsAndAFile(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	tie := onTestDay(10, 15)
	// Each request's Logs get ids in the order sent, and each request's
	// after the last: so at the tie, the hot table's Logs come first, then
	// the late Logs, then the file's.
	seedHourInEverySource(t, env,
		[]string{
			logJSON(t, "file old", onTestDay(10, 5), nil),
			logJSON(t, "file tie 1", tie, nil),
			logJSON(t, "file tie 2", tie, nil),
			logJSON(t, "file new", onTestDay(10, 40), nil),
		},
		[]string{
			logJSON(t, "hour 9", onTestDay(9, 30), nil),
			logJSON(t, "late old", onTestDay(10, 1), nil),
			logJSON(t, "late tie 1", tie, nil),
			logJSON(t, "late tie 2", tie, nil),
		},
		[]string{
			logJSON(t, "hot tie 1", tie, nil),
			logJSON(t, "hot tie 2", tie, nil),
			logJSON(t, "hot new", onTestDay(10, 50), nil),
			logJSON(t, "hour 11", onTestDay(11, 10), nil),
		},
	)

	want := []string{
		"hour 11", "hot new", "file new",
		"hot tie 2", "hot tie 1", "late tie 2", "late tie 1", "file tie 2", "file tie 1",
		"file old", "late old", "hour 9",
	}
	if got := queryMessages(t, env, ""); !slices.Equal(got, want) {
		t.Fatalf("Expected %v, got %v", want, got)
	}
	assertPagesOfEverySize(t, env, "", want)
}

func TestPagesOfTheNewestLogsContinueInsideAnHourAndAcrossHours(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Hour 12 has 3 Logs and hour 11 has 5, in the hot table; hour 10 has 4,
	// in a file.
	var logs, want []string
	for _, hour := range []struct{ hour, count int }{{12, 3}, {11, 5}, {10, 4}} {
		for i := range hour.count {
			message := fmt.Sprintf("%d:%02d", hour.hour, 50-10*i)
			logs = append(logs, logJSON(t, message, onTestDay(hour.hour, 50-10*i), nil))
			want = append(want, message)
		}
	}
	postWhileHot(t, env, logs...)
	env.Clock.Set(onTestDay(12, 55))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, onTestDay(10, 0), 1)

	assertPagesOfEverySize(t, env, "", want)

	// A page that needs fewer Logs than an hour has takes the newest.
	page := runQuery(t, env, 123, queryRequest{From: allTime.from, To: allTime.to, Limit: 4})
	if got := messages(page.Logs); !slices.Equal(got, want[:4]) || page.NextCursor == nil {
		t.Errorf("Expected %v and a cursor, got %v", want[:4], got)
	}

	// An hour with fewer Logs than the limit lists them all, with no cursor.
	page = runQuery(t, env, 123, queryRequest{From: onTestDay(11, 0), To: onTestDay(12, 0), Limit: 10})
	if got := messages(page.Logs); !slices.Equal(got, want[3:8]) || page.NextCursor != nil {
		t.Errorf("Expected %v and no cursor, got %v and %v", want[3:8], got, page.NextCursor)
	}
	page = runQuery(t, env, 123, queryRequest{From: allTime.from, To: allTime.to, Limit: 200})
	if got := messages(page.Logs); !slices.Equal(got, want) || page.NextCursor != nil {
		t.Errorf("Expected %v and no cursor, got %v and %v", want, got, page.NextCursor)
	}
}

func TestAConditionInSmallPagesListsTheSameLogsAsFilteringThemAll(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// In each source, the Logs the conditions keep are older than those
	// they drop, so a page of the newest Logs would hold none of them.
	keep := map[string]any{"keep": true}
	drop := map[string]any{"keep": false}
	seedHourInEverySource(t, env,
		[]string{
			logJSON(t, "keep file 1", onTestDay(10, 2), keep),
			logJSON(t, "keep file 2", onTestDay(10, 3), keep),
			logJSON(t, "drop file 1", onTestDay(10, 30), drop),
			logJSON(t, "drop file 2", onTestDay(10, 31), drop),
			logJSON(t, "drop file 3", onTestDay(10, 32), drop),
		},
		[]string{
			logJSON(t, "keep late 1", onTestDay(10, 4), keep),
			logJSON(t, "drop late 1", onTestDay(10, 33), drop),
			logJSON(t, "drop late 2", onTestDay(10, 34), drop),
			logJSON(t, "keep late 2", onTestDay(10, 35), keep),
		},
		[]string{
			logJSON(t, "keep hot 1", onTestDay(10, 1), keep),
			logJSON(t, "drop hot 1", onTestDay(10, 40), drop),
			logJSON(t, "drop hot 2", onTestDay(10, 41), drop),
			logJSON(t, "keep hot 2", onTestDay(11, 1), keep),
			logJSON(t, "drop hot 3", onTestDay(11, 5), drop),
		},
	)

	all := queryMessages(t, env, "")
	if len(all) != 14 {
		t.Fatalf("Expected 14 Logs, got %v", all)
	}
	want := slices.DeleteFunc(slices.Clone(all), func(m string) bool { return !strings.HasPrefix(m, "keep") })
	if len(want) != 6 {
		t.Fatalf("Expected 6 Logs to keep, got %v", want)
	}
	for _, query := range []string{`where keep == true`, `where message contains "KEEP"`, `search "keep"`} {
		assertPagesOfEverySize(t, env, query, want)
	}
}

func TestPagesThatSpendTheirBudgetAnswerWhatTheyFoundAndACursor(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Hour 12 has 1 Log and hour 11 has 3, in the hot table; hour 10 has 2,
	// in a file.
	postWhileHot(t, env,
		logJSON(t, "12:10", onTestDay(12, 10), nil),
		logJSON(t, "11:30", onTestDay(11, 30), nil),
		logJSON(t, "11:20", onTestDay(11, 20), nil),
		logJSON(t, "11:10", onTestDay(11, 10), nil),
		logJSON(t, "10:30", onTestDay(10, 30), nil),
		logJSON(t, "10:20", onTestDay(10, 20), nil),
	)
	env.Clock.Set(onTestDay(12, 30))
	env.ExportLogs(t)

	// Every scan spends its budget on its first hour.
	env.LogsHandler.ScanBudget = time.Nanosecond
	pages := pageThrough(t, env, queryRequest{From: allTime.from, To: allTime.to, Limit: 2})
	want := [][]string{{"12:10"}, {"11:30", "11:20"}, {"11:10"}, {"10:30", "10:20"}, {}}
	if !reflect.DeepEqual(pages, want) {
		t.Errorf("Expected pages %v, got %v", want, pages)
	}
}
