package main_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// nilBatch is the ingest_cursor of a Project Log processing has stored
// nothing for: the nil UUID.
const nilBatch = "00000000-0000-0000-0000-000000000000"

// ingestedPage is the answer to a run with after_batch.
type ingestedPage struct {
	Logs         []listedLog `json:"logs"`
	NextCursor   *string     `json:"next_cursor"`
	IngestCursor string      `json:"ingest_cursor"`
	Total        int         `json:"total"`
}

// runAfterBatch runs a Query of Project 123 for the Logs ingested after the
// Log batch afterBatch, "" to arm a Monitor, with any more of the request in
// extra.
func runAfterBatch(t *testing.T, env *TestEnv, query, afterBatch string, extra map[string]any) ingestedPage {
	t.Helper()
	body := map[string]any{"query": query, "after_batch": afterBatch}
	for name, value := range extra {
		body[name] = value
	}
	answer := answerOf(t, env, "/api/123/logs/query", body, 200)
	if elapsed := string(answer["elapsed_ms"]); !wholeNumber.MatchString(elapsed) {
		t.Errorf("Expected elapsed_ms, a whole number of milliseconds, got %q", elapsed)
	}
	var page ingestedPage
	if err := json.Unmarshal(mustMarshal(t, answer), &page); err != nil {
		t.Fatalf("Decode %s: %v", mustMarshal(t, answer), err)
	}
	if page.NextCursor != nil {
		t.Errorf("Expected no next_cursor after a batch, got %q", *page.NextCursor)
	}
	if page.IngestCursor == "" {
		t.Errorf("Expected an ingest_cursor, got %s", mustMarshal(t, answer))
	}
	return page
}

// matchMonitor runs a Query as a Match monitor does: armed with an empty
// after_batch, then each run reads the Logs ingested after the last run's
// ingest_cursor, and keeps the new one.
type matchMonitor struct {
	t      *testing.T
	env    *TestEnv
	query  string
	cursor string
}

func armMonitor(t *testing.T, env *TestEnv, query string) *matchMonitor {
	t.Helper()
	page := runAfterBatch(t, env, query, "", nil)
	if len(page.Logs) != 0 || page.Total != 0 {
		t.Errorf("Expected arming to read no Logs, got %d (total %d)", len(page.Logs), page.Total)
	}
	return &matchMonitor{t: t, env: env, query: query, cursor: page.IngestCursor}
}

// run runs the Monitor's Query once, with any more of the request in extra,
// and returns the messages of the Logs it read, newest first.
func (m *matchMonitor) run(extra map[string]any) []string {
	m.t.Helper()
	page := runAfterBatch(m.t, m.env, m.query, m.cursor, extra)
	m.cursor = page.IngestCursor
	return messages(page.Logs)
}

func TestAMatchMonitorReadsTheLogsIngestedSinceItsLastRun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(minute int) time.Time { return testNow.Add(time.Duration(minute) * time.Minute) }

	// Armed before Log processing has stored anything for the Project, a
	// Monitor goes from the nil batch, and reads everything stored after.
	first := armMonitor(t, env, "")
	if first.cursor != nilBatch {
		t.Errorf("Expected a Project with nothing stored to arm at the nil batch, got %q", first.cursor)
	}
	postAndProcess(t, env, logJSON(t, "before", at(-30), nil))

	second := armMonitor(t, env, "")
	if second.cursor == nilBatch {
		t.Fatalf("Expected arming after a Log batch to give its id")
	}
	postAndProcess(t, env, logJSON(t, "one", at(-20), nil), logJSON(t, "two", at(-10), nil))
	postAndProcess(t, env, logJSON(t, "three", at(-15), nil))

	if got, want := first.run(nil), []string{"two", "three", "one", "before"}; !slices.Equal(got, want) {
		t.Errorf("Expected the first Monitor to read %v, got %v", want, got)
	}
	armed := second.cursor
	if got, want := second.run(nil), []string{"two", "three", "one"}; !slices.Equal(got, want) {
		t.Errorf("Expected the second Monitor to read %v, got %v", want, got)
	}
	if second.cursor == armed || second.cursor != first.cursor {
		t.Errorf("Expected both Monitors to move to the same new cursor, got %q and %q (armed at %q)", first.cursor, second.cursor, armed)
	}

	// Nothing new: the next run reads none, and keeps the cursor.
	cursor := second.cursor
	if got := second.run(nil); len(got) != 0 {
		t.Errorf("Expected a run with nothing new to read none, got %v", got)
	}
	if second.cursor != cursor {
		t.Errorf("Expected the cursor kept at %q, got %q", cursor, second.cursor)
	}

	postAndProcess(t, env, logJSON(t, "four", at(-5), nil))
	if got := second.run(nil); !slices.Equal(got, []string{"four"}) {
		t.Errorf("Expected only the new Log, got %v", got)
	}
}

func TestAMatchMonitorReadsLateLogsWhateverTheirTimeAndTheRange(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	today := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	yesterday := func(hour, minute int) time.Time { return today(hour, minute).AddDate(0, 0, -1) }
	env.Clock.Set(today(12, 30))
	monitor := armMonitor(t, env, "")
	// The last quarter hour, as a Threshold monitor reads: it holds none
	// of the late Logs, and a run after a batch reads them all the same.
	quarterHour := map[string]any{"from": today(12, 15), "to": today(12, 30)}

	postAndProcess(t, env,
		logJSON(t, "yesterday ten", yesterday(10, 0), nil),
		logJSON(t, "ten past twelve", today(12, 10), nil),
		logJSON(t, "three days ago", today(9, 0).AddDate(0, 0, -3), nil),
	)
	if got, want := monitor.run(quarterHour), []string{"ten past twelve", "yesterday ten", "three days ago"}; !slices.Equal(got, want) {
		t.Errorf("Expected the late Logs read with the new one, %v, got %v", want, got)
	}

	// The export moves the late Logs to files; one more late Log of
	// yesterday's hour 10 goes to its late file at the next export.
	env.ExportLogs(t)
	postAndProcess(t, env, logJSON(t, "late yesterday ten", yesterday(10, 20), nil))
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, yesterday(10, 0), 2)

	if got, want := monitor.run(quarterHour), []string{"late yesterday ten"}; !slices.Equal(got, want) {
		t.Errorf("Expected only the Log of the late file, %v, got %v", want, got)
	}
	if got := monitor.run(quarterHour); len(got) != 0 {
		t.Errorf("Expected no more Logs, got %v", got)
	}
}

func TestTheLogsIngestedAfterABatchAreTheSameBeforeAndAfterTheExport(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(11, 30))
	// A Log of hour 10 stored before the Monitor is armed goes to the same
	// file as those after, and is never read.
	postAndProcess(t, env, logJSON(t, "before", at(10, 5), map[string]any{"user": "ada"}))
	armed := armMonitor(t, env, "").cursor
	postAndProcess(t, env,
		logJSON(t, "ten", at(10, 40), map[string]any{"user": "ada", "status": 500}),
		logJSON(t, "eleven", at(11, 20), map[string]any{"user": "bob"}),
	)
	postAndProcess(t, env, logJSON(t, "late", at(9, 0).AddDate(0, 0, -1), map[string]any{"user": "ada"}))

	queries := map[string][]string{
		``:                    {"eleven", "ten", "late"},
		`where user == "ada"`: {"ten", "late"},
		`where status >= 500`: {"ten"},
	}
	read := func(query string) ingestedPage { return runAfterBatch(t, env, query, armed, nil) }
	want := map[string]ingestedPage{}
	for query, messagesWanted := range queries {
		want[query] = read(query)
		if got := messages(want[query].Logs); !slices.Equal(got, messagesWanted) {
			t.Errorf("%q: expected %v from the hot table and logs_late, got %v", query, messagesWanted, got)
		}
	}

	// At 12:00 the export moves hour 10 and the late Log to files, and hour
	// 11 stays hot; at 13:00 it moves hour 11.
	for _, hour := range []int{12, 13} {
		env.Clock.Set(at(hour, 0))
		env.ExportLogs(t)
		for query := range queries {
			if got := read(query); !reflect.DeepEqual(got, want[query]) {
				t.Errorf("%q after the export at %d:00: expected\n %+v\ngot\n %+v", query, hour, want[query], got)
			}
		}
	}
	assertHourFiles(t, env, 123, at(10, 0), 1)
	assertHourFiles(t, env, 123, at(11, 0), 1)
}

func TestNoLogIsMissedOrReadTwiceWhenRequestsStoreTheirBatchesOutOfOrder(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	monitor := armMonitor(t, env, "")

	// Two requests at once: the first is normalised first, so its Log has
	// the lower id, but the second stores its batch first, and Log
	// processing stores it before the first's batch is there.
	first := logAt(t, "first request", "2026-10-02T11:50:00Z")
	second := logAt(t, "second request", "2026-10-02T11:40:00Z")
	storeLogBatch(t, env, 123, second)
	env.ProcessLogs(t)
	if got := monitor.run(nil); !slices.Equal(got, []string{"second request"}) {
		t.Errorf("Expected the second request's Log, got %v", got)
	}
	storeLogBatch(t, env, 123, first)
	env.ProcessLogs(t)
	if got := monitor.run(nil); !slices.Equal(got, []string{"first request"}) {
		t.Errorf("Expected the first request's Log, stored later with a lower id, got %v", got)
	}

	// The other order: both batches are stored before Log processing runs.
	third, fourth := logAt(t, "third request", "2026-10-02T11:30:00Z"), logAt(t, "fourth request", "2026-10-02T11:35:00Z")
	storeLogBatch(t, env, 123, third)
	storeLogBatch(t, env, 123, fourth)
	env.ProcessLogs(t)
	if got := monitor.run(nil); !slices.Equal(got, []string{"fourth request", "third request"}) {
		t.Errorf("Expected both Logs in one run, got %v", got)
	}
	if got := monitor.run(nil); len(got) != 0 {
		t.Errorf("Expected no Log read twice, got %v", got)
	}
}

func TestEveryLogOfConcurrentRequestsIsReadByExactlyOneRun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	monitor := armMonitor(t, env, "")

	// Two senders post at once, while Log processing runs in the
	// background and the Monitor runs again and again.
	const senders, requests = 2, 25
	var sending sync.WaitGroup
	for sender := range senders {
		sending.Add(1)
		go func() {
			defer sending.Done()
			for request := range requests {
				body := logJSON(t, fmt.Sprintf("sender %d request %d", sender, request), testNow.Add(-time.Minute), nil)
				req, _ := http.NewRequest("POST", "/api/123/logs?sentry_key=test_public_key", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp, err := env.App.Test(req, 5000)
				if err != nil || resp.StatusCode != 200 {
					t.Errorf("Sender %d request %d: expected 200, got %v %v", sender, request, resp, err)
					return
				}
				resp.Body.Close()
			}
		}()
	}
	sent := make(chan struct{})
	go func() { sending.Wait(); close(sent) }()
	processed := make(chan error, 1)
	go func() {
		for {
			if err := env.LogProcessor.ProcessUntilCaughtUp(); err != nil {
				processed <- err
				return
			}
			select {
			case <-sent:
				processed <- env.LogProcessor.ProcessUntilCaughtUp()
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()

	read := map[string]int{}
	for done := false; !done; {
		select {
		case err := <-processed:
			if err != nil {
				t.Fatalf("Log processing failed: %v", err)
			}
			done = true
		default:
		}
		for _, message := range monitor.run(map[string]any{"limit": 1000}) {
			read[message]++
		}
	}
	if len(read) != senders*requests {
		t.Errorf("Expected all %d Logs read, got %d", senders*requests, len(read))
	}
	for message, times := range read {
		if times != 1 {
			t.Errorf("Expected %q read once, got %d times", message, times)
		}
	}
}

func TestLogsNotYetProcessedAreReadByTheNextRun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	monitor := armMonitor(t, env, "")

	postAndProcess(t, env, logJSON(t, "processed", testNow.Add(-2*time.Minute), nil))
	if counts := postLogs(t, env, "application/json", logJSON(t, "waiting", testNow.Add(-time.Minute), nil)); counts.Accepted != 1 {
		t.Fatalf("Expected 1 Log accepted, got %+v", counts)
	}
	if got := monitor.run(nil); !slices.Equal(got, []string{"processed"}) {
		t.Errorf("Expected only the processed Log, got %v", got)
	}
	env.ProcessLogs(t)
	if got := monitor.run(nil); !slices.Equal(got, []string{"waiting"}) {
		t.Errorf("Expected the Log processed since, got %v", got)
	}
}

func TestARunAfterABatchFiltersCountsPastItsLimitAndTakesOnlyLogRows(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	errors := `where level == "error"`
	armed := armMonitor(t, env, errors).cursor
	at := func(minute int) time.Time { return testNow.Add(time.Duration(-minute) * time.Minute) }
	log := func(message, level string, minute int) string {
		return logJSON(t, message, at(minute), map[string]any{"level": level, "batch": "the sender's"})
	}
	postAndProcess(t, env, log("error 1", "error", 9), log("info 1", "info", 8), log("error 2", "error", 7))
	postAndProcess(t, env, log("error 3", "error", 6), log("error 4", "error", 5), log("info 2", "info", 4), log("error 5", "error", 3))

	page := runAfterBatch(t, env, errors, armed, map[string]any{"limit": 2})
	if got := messages(page.Logs); !slices.Equal(got, []string{"error 5", "error 4"}) || page.Total != 5 {
		t.Errorf("Expected the 2 newest of 5 errors, got %v of %d", got, page.Total)
	}
	page = runAfterBatch(t, env, errors+" | take 3", armed, nil)
	if got := messages(page.Logs); !slices.Equal(got, []string{"error 5", "error 4", "error 3"}) || page.Total != 5 {
		t.Errorf("Expected take's 3 newest of 5 errors, got %v of %d", got, page.Total)
	}
	page = runAfterBatch(t, env, `where level == "warn"`, armed, nil)
	if len(page.Logs) != 0 || page.Total != 0 {
		t.Errorf("Expected no warnings, got %v of %d", messages(page.Logs), page.Total)
	}

	// The Logs read are as any run lists them: the batch they came in isn't
	// a Field, and a sender's own batch Attribute is kept.
	page = runAfterBatch(t, env, errors, armed, map[string]any{"limit": 1})
	if len(page.Logs) != 1 || !reflect.DeepEqual(page.Logs[0], latestLogs(t, env, 123, 1)[0]) {
		t.Errorf("Expected the Log as a run lists it, got %+v", page.Logs)
	}
	if got := page.Logs[0].Attributes; !reflect.DeepEqual(got, map[string]any{"batch": "the sender's"}) {
		t.Errorf("Expected only the sender's batch Attribute, got %v", got)
	}
	status, raw := postQueryApp(t, env, "/api/123/logs/query", map[string]any{"query": errors, "after_batch": armed, "limit": 1})
	var answer struct {
		Logs []map[string]any `json:"logs"`
	}
	if err := json.Unmarshal(raw, &answer); status != 200 || err != nil || len(answer.Logs) != 1 {
		t.Fatalf("Expected one Log, got %d %s", status, raw)
	}
	for field := range answer.Logs[0] {
		if !slices.Contains([]string{"id", "time", "level", "message", "service", "environment", "trace_id", "span_id", "attributes"}, field) {
			t.Errorf("Expected only a Log's fields, got %q in %s", field, raw)
		}
	}
	if _, rows := runTable(t, env, `summarize count() by batch`); !slices.Equal(rows, []string{`["the sender's",7]`}) {
		t.Errorf("Expected batch in a Query to be the sender's Attribute, got %v", rows)
	}

	// Only a Query of Log rows runs after a batch, armed or not; a Query
	// with mistakes, and an after_batch that isn't a batch id, answer 400.
	for _, after := range []string{"", armed} {
		for _, query := range []string{`summarize count()`, `where level == "error" | project message`} {
			answer := answerOf(t, env, "/api/123/logs/query", map[string]any{"query": query, "after_batch": after}, 400)
			if got := onlyErrorMessage(t, answer); !strings.Contains(got, "only a Query that returns Logs") {
				t.Errorf("%q after %q: expected a message saying only Log rows can be read, got %q", query, after, got)
			}
		}
		answer := answerOf(t, env, "/api/123/logs/query", map[string]any{"query": `where level ==`, "after_batch": after}, 400)
		if _, ok := answer["errors"]; !ok {
			t.Errorf("Expected a Query with mistakes after %q to list them, got %s", after, mustMarshal(t, answer))
		}
	}
	for _, after := range []any{"not a batch", "1234", 5} {
		if status, raw := postQueryApp(t, env, "/api/123/logs/query", map[string]any{"query": ``, "after_batch": after}); status != 400 {
			t.Errorf("after_batch %v: expected 400, got %d: %s", after, status, raw)
		}
	}
}

// forgetBatches makes the closed environment's Logs those of an install from
// before the batch column: no hot table or logs_late has the column, no Log
// file has it, and the manifest has no batch_max.
func forgetBatches(t *testing.T, env *TestEnv) {
	t.Helper()
	db, err := sql.Open("duckdb", filepath.Join(env.dir, "logs.duckdb"))
	if err != nil {
		t.Fatalf("Open logs.duckdb: %v", err)
	}
	defer db.Close()
	tables := []string{"logs_late"}
	rows, err := db.Query("SELECT table_name FROM duckdb_tables() WHERE table_name LIKE 'logs_hot_%'")
	if err != nil {
		t.Fatalf("List hot tables: %v", err)
	}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		tables = append(tables, table)
	}
	rows.Close()
	statements := []string{"UPDATE log_files SET batch_max = NULL"}
	for _, table := range tables {
		statements = append(statements, "ALTER TABLE "+table+" DROP COLUMN batch")
	}
	files, err := filepath.Glob(filepath.Join(env.dir, "logs_parquet", "*", "*", "*", "*.parquet"))
	if err != nil || len(files) == 0 {
		t.Fatalf("Expected Log files, got %v %v", files, err)
	}
	for _, file := range files {
		statements = append(statements, fmt.Sprintf(
			"COPY (SELECT * EXCLUDE (batch) FROM read_parquet('%[1]s')) TO '%[1]s.old' (FORMAT parquet)", file))
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	for _, file := range files {
		if err := os.Rename(file+".old", file); err != nil {
			t.Fatalf("Replace %s: %v", file, err)
		}
	}
}

func TestAnInstallFromBeforeTheBatchColumnKeepsItsLogsAndArmsMonitors(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }
	env.Clock.Set(at(12, 30))
	postAndProcess(t, env,
		logJSON(t, "ten", at(10, 10), map[string]any{"user": "ada", "status": 200}),
		logJSON(t, "yesterday", testNow.AddDate(0, 0, -1), map[string]any{"user": "old"}),
	)
	env.ExportLogs(t) // hour 10's data.parquet, and yesterday's
	// A late Log of hour 10 waits in logs_late, and one of 12:10 is hot.
	postAndProcess(t, env,
		logJSON(t, "late ten", at(10, 40), map[string]any{"user": "bob", "status": 500}),
		logJSON(t, "twelve", at(12, 10), map[string]any{"user": "cy"}),
	)
	want := answersOf(t, env)
	if got := messages(want.logs); !slices.Equal(got, []string{"twelve", "late ten", "ten", "yesterday"}) {
		t.Fatalf("Unexpected Logs: %v", got)
	}

	env.close()
	forgetBatches(t, env)
	env.open(t)
	assertAnswersUnchanged(t, env, want, "after an upgrade from before the batch column")

	// A Monitor armed now reads only Logs stored from now on.
	monitor := armMonitor(t, env, "")
	armed := monitor.cursor
	postAndProcess(t, env,
		logJSON(t, "new twelve", at(12, 20), map[string]any{"user": "di"}),
		logJSON(t, "new late ten", at(10, 50), map[string]any{"user": "ed"}),
	)
	newLogs := []string{"new twelve", "new late ten"}
	if got := monitor.run(nil); !slices.Equal(got, newLogs) {
		t.Errorf("Expected the new Logs, got %v", got)
	}

	// The next export puts old and new late Logs of hour 10 in one late
	// file, beside its data.parquet from before the batch column.
	env.ExportLogs(t)
	assertHourFiles(t, env, 123, at(10, 0), 2)
	if got := messages(runAfterBatch(t, env, "", armed, nil).Logs); !slices.Equal(got, newLogs) {
		t.Errorf("Expected the same new Logs after the export, got %v", got)
	}
	if got := monitor.run(nil); len(got) != 0 {
		t.Errorf("Expected no more Logs, got %v", got)
	}
	if got := messages(latestLogs(t, env, 123, 100)); !slices.Equal(got, []string{"new twelve", "twelve", "new late ten", "late ten", "ten", "yesterday"}) {
		t.Errorf("Expected every Log listed after the export, got %v", got)
	}
	// The late file has the batch column and data.parquet hasn't.
	if got := queryMessages(t, env, `where status >= 200`); !slices.Equal(got, []string{"late ten", "ten"}) {
		t.Errorf("Expected a where to read the files from before and after the batch column, got %v", got)
	}

	// A restart keeps it all.
	env.Reopen(t)
	if got := messages(runAfterBatch(t, env, "", armed, nil).Logs); !slices.Equal(got, newLogs) {
		t.Errorf("Expected the same new Logs after a restart, got %v", got)
	}
}
