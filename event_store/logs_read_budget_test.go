package main_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// busySource is one Source of a busy Project: its Logs a minute, and the
// kept bytes of each.
type busySource struct {
	service, file string
	perMinute     int64
	bytesPerLog   int64
}

// tagalysSources are a Project as busy as tagalys on production: about 90M
// Logs a day, about 425 bytes each, over a few files. Together they keep
// 26,770,000 bytes a minute, so the 4 GiB a Query that matches text may
// read is 160 minutes of them. cron.log alone keeps 1,512,000,000 bytes
// over 7 days, which fits.
var tagalysSources = []busySource{
	{"web", "access.log", 25_000, 400},
	{"worker", "sidekiq.log", 20_000, 450},
	{"api", "production.log", 15_000, 500},
	{"worker", "cron.log", 500, 300},
	{"worker", "mailer.log", 200, 300},
	{"web", "error.log", 100, 600},
}

// seedBusyProject writes the counts a Project's Logs of [from, to) would
// have left, from and to whole minutes, as if sources had sent them: Source
// counts by day, and counts by minute. Only the counts: the Logs themselves
// aren't stored, so a run that reads them finds only the Logs a test sends.
func seedBusyProject(t *testing.T, env *TestEnv, projectID uint32, from, to time.Time, sources []busySource) {
	t.Helper()
	ctx := context.Background()
	conn := env.ClickHouse.Conn()
	var perMinute int64
	for _, s := range sources {
		perMinute += s.perMinute
	}
	minutes := int64(to.Sub(from) / time.Minute)
	if err := conn.Exec(ctx, fmt.Sprintf(`INSERT INTO log_minute_counts (project_id, minute, level, logs)
		SELECT %d, toDateTime64('%s', 6, 'UTC') + toIntervalMinute(number), 'info', %d FROM numbers(%d)`,
		projectID, from.UTC().Format(time.DateTime), perMinute, minutes)); err != nil {
		t.Fatalf("Seed the counts by minute: %v", err)
	}
	var rows []string
	for day := from.Truncate(24 * time.Hour); day.Before(to); day = day.Add(24 * time.Hour) {
		start, end := day, day.Add(24*time.Hour)
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		dayMinutes := int64(end.Sub(start) / time.Minute)
		for _, s := range sources {
			logs := s.perMinute * dayMinutes
			rows = append(rows, fmt.Sprintf("(%d, '%s', '%s', '%s', %d, %d, 0)",
				projectID, day.Format(time.DateOnly), s.service, s.file, logs, logs*s.bytesPerLog))
		}
	}
	if err := conn.Exec(ctx, `INSERT INTO log_source_counts (project_id, day, service, file, logs, bytes, logs_dropped_by_transform) VALUES `+
		strings.Join(rows, ", ")); err != nil {
		t.Fatalf("Seed the Source counts: %v", err)
	}
}

// lastWeek is the 7 days up to testNow.
var lastWeek = struct{ from, to time.Time }{from: testNow.AddDate(0, 0, -7), to: testNow}

// budgetRefusal is a run's refusal, as a checked caller reads it.
type budgetRefusal struct {
	Message  string         `json:"message"`
	Estimate budgetEstimate `json:"estimate"`
	// LargestRange is nil when not even a minute fits.
	LargestRange *struct {
		From  time.Time `json:"from"`
		To    time.Time `json:"to"`
		Bytes int64     `json:"bytes"`
	} `json:"largest_range"`
	TopFiles []struct {
		Service string `json:"service"`
		File    string `json:"file"`
		Logs    int64  `json:"logs"`
		Bytes   int64  `json:"bytes"`
	} `json:"top_files"`
	NarrowerQuery *struct {
		Query string    `json:"query"`
		From  time.Time `json:"from"`
		To    time.Time `json:"to"`
	} `json:"narrower_query"`
}

// budgetEstimate is how much a run would read, as the counts put it.
type budgetEstimate struct {
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	TextMatch   bool      `json:"text_match"`
	Logs        int64     `json:"logs"`
	Bytes       int64     `json:"bytes"`
	BudgetBytes int64     `json:"budget_bytes"`
	OverBudget  bool      `json:"over_budget"`
	Heavy       bool      `json:"heavy"`
	Forced      bool      `json:"forced"`
}

// checkedRun runs a Query for Project 123 as a checked caller, the Logs
// explorer or the MCP server, does, with the other fields of body, and
// returns the answer's top-level fields. It must answer 200.
func checkedRun(t *testing.T, env *TestEnv, query string, from, to time.Time, body map[string]any) map[string]json.RawMessage {
	t.Helper()
	request := map[string]any{"query": query, "from": from, "to": to, "check_budget": true}
	for k, v := range body {
		request[k] = v
	}
	return answerOf(t, env, "/api/123/logs/query", request, 200)
}

// refusalOf is a run's refusal, failing the test if the run wasn't refused.
func refusalOf(t *testing.T, answer map[string]json.RawMessage) budgetRefusal {
	t.Helper()
	raw, ok := answer["refusal"]
	if !ok {
		t.Fatalf("Expected a refusal, got %s", mustMarshal(t, answer))
	}
	if _, ran := answer["table"]; ran {
		t.Errorf("Expected no table with a refusal, got %s", mustMarshal(t, answer))
	}
	if _, ran := answer["logs"]; ran {
		t.Errorf("Expected no Logs with a refusal, got %s", mustMarshal(t, answer))
	}
	var refusal budgetRefusal
	if err := json.Unmarshal(raw, &refusal); err != nil {
		t.Fatalf("Decode the refusal %s: %v", raw, err)
	}
	return refusal
}

// ran fails the test unless the answer is a run's: Logs or a table.
func ran(t *testing.T, what string, answer map[string]json.RawMessage) {
	t.Helper()
	_, logs := answer["logs"]
	_, table := answer["table"]
	if _, refused := answer["refusal"]; refused || !logs && !table {
		t.Errorf("%s: expected the Query run, got %s", what, mustMarshal(t, answer))
	}
}

const fourGiB = 4 << 30

func TestASearchOverMoreThanTheBudgetIsRefusedWithANarrowerQueryThatFits(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	// Today's Logs up to now, and the 7 days before.
	seedBusyProject(t, env, 123, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)

	query := `search "timeout" | summarize count() by file`
	refusal := refusalOf(t, checkedRun(t, env, query, lastWeek.from, lastWeek.to, nil))

	// 7 days of 26,770,000 bytes and 60,800 Logs a minute.
	estimate := refusal.Estimate
	if !estimate.TextMatch || estimate.Bytes != 7*1440*26_770_000 || estimate.Logs != 7*1440*60_800 ||
		estimate.BudgetBytes != fourGiB || !estimate.OverBudget || !estimate.Heavy || estimate.Forced {
		t.Errorf("Expected 7 days' %d bytes over the 4 GiB budget, got %+v", 7*1440*26_770_000, estimate)
	}
	// 160 minutes of 26,770,000 bytes fit in 4 GiB; 161 don't.
	if r := refusal.LargestRange; r == nil || !r.From.Equal(testNow.Add(-160*time.Minute)) || !r.To.Equal(testNow) || r.Bytes != 160*26_770_000 {
		t.Errorf("Expected the largest range that fits to be the last 160 minutes, got %+v", r)
	}
	var files []string
	for _, f := range refusal.TopFiles {
		files = append(files, f.Service+"/"+f.File)
	}
	if want := "web/access.log worker/sidekiq.log api/production.log worker/cron.log worker/mailer.log"; strings.Join(files, " ") != want {
		t.Errorf("Expected the 5 busiest files by Logs, %s, got %v", want, files)
	}
	if access := refusal.TopFiles[0]; access.Logs != 7*1440*25_000 || access.Bytes != 7*1440*25_000*400 {
		t.Errorf("Expected access.log's 7 days of Logs and bytes, got %+v", access)
	}
	for _, said := range []string{"4 GiB", "web", "access.log"} {
		if !strings.Contains(refusal.Message, said) {
			t.Errorf("Expected the message to say %q, got %q", said, refusal.Message)
		}
	}
	if strings.Contains(strings.ToLower(refusal.Message), "force") {
		t.Errorf("Expected the message never to mention force, got %q", refusal.Message)
	}

	// The narrower Query runs as a checked caller runs it.
	postAndProcessTo(t, env, 123, "test_public_key",
		logJSON(t, "a timeout in cron", testNow.Add(-time.Hour), map[string]any{"service": "worker", "file": "cron.log"}))
	narrower := refusal.NarrowerQuery
	if narrower == nil {
		t.Fatalf("Expected a narrower Query, got %+v", refusal)
	}
	answer := checkedRun(t, env, narrower.Query, narrower.From, narrower.To, nil)
	ran(t, "the narrower Query "+narrower.Query, answer)
	if !strings.Contains(string(answer["table"]), `["cron.log",1]`) {
		t.Errorf("Expected the narrower Query %q to find the timeout in cron.log, got %s", narrower.Query, answer["table"])
	}
}

func TestQueriesThatFitTheBudgetOrDontMatchTextRunOverABusyWeek(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedBusyProject(t, env, 123, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)
	postAndProcessTo(t, env, 123, "test_public_key",
		logJSON(t, "a timeout in cron", testNow.Add(-time.Hour), map[string]any{"service": "worker", "file": "cron.log", "level": "error"}))

	for _, query := range []string{
		// Files whose week fits the budget.
		`where file == "cron.log" | search "timeout"`,
		`where service == "worker" and file in ("cron.log", "mailer.log") and message contains "timeout"`,
		// A plain Log list reads only the newest page, however busy.
		``,
		`where file == "access.log" and level == "error"`,
		// A table that matches no text.
		`summarize count() by service`,
	} {
		ran(t, query, checkedRun(t, env, query, lastWeek.from, lastWeek.to, nil))
	}
}

func TestForceRunsARefusedQueryAtALowerPriority(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	// The query_log keeps the reads of earlier runs of the test.
	project := freshProjectIDs(1)[0]
	seedProject(t, env.SQLitePath, project, 0)
	seedBusyProject(t, env, project, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)
	path := fmt.Sprintf("/api/%d/logs/query", project)
	query := `search "timeout" | summarize count() by file`
	run := func(force bool) map[string]json.RawMessage {
		return answerOf(t, env, path, map[string]any{
			"query": query, "from": lastWeek.from, "to": lastWeek.to, "check_budget": true, "force": force}, 200)
	}
	refusalOf(t, run(false))
	ran(t, "the forced Query", run(true))

	flushQueryLog(t, env)
	var priorities []string
	rows, err := env.ClickHouse.Conn().Query(context.Background(), `
		SELECT DISTINCT Settings['priority'] FROM system.query_log
		WHERE current_database = currentDatabase() AND type = 'QueryFinish'
			AND JSONExtractUInt(log_comment, 'project_id') = ? AND JSONExtractBool(log_comment, 'forced')`, project)
	if err != nil {
		t.Fatalf("Read the forced read's statements: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var priority string
		if err := rows.Scan(&priority); err != nil {
			t.Fatal(err)
		}
		priorities = append(priorities, priority)
	}
	if len(priorities) != 1 || priorities[0] != "10" {
		t.Errorf("Expected the forced read's statements at priority 10, below other Log reads' 1, got %v", priorities)
	}
}

func TestAnEstimateOverARangeThatCrossesDaysAddsUpTheDays(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	// Yesterday had twice the Logs of the day before.
	yesterday := testNow.Truncate(24*time.Hour).AddDate(0, 0, -1)
	seedBusyProject(t, env, 123, yesterday.AddDate(0, 0, -1), yesterday, []busySource{{"web", "access.log", 7_000, 400}})
	seedBusyProject(t, env, 123, yesterday, yesterday.AddDate(0, 0, 1), []busySource{{"web", "access.log", 14_000, 400}})

	// The last 6 hours of the one day and the first 6 of the next.
	from, to := yesterday.Add(-6*time.Hour), yesterday.Add(6*time.Hour)
	answer := checkedRun(t, env, `search "timeout"`, from, to, map[string]any{"estimate": true})
	if _, ran := answer["logs"]; ran {
		t.Errorf("Expected the estimate without the Query run, got %s", mustMarshal(t, answer))
	}
	var estimate budgetEstimate
	if err := json.Unmarshal(answer["estimate"], &estimate); err != nil {
		t.Fatalf("Decode the estimate %s: %v", answer["estimate"], err)
	}
	// 360 minutes of 7,000 Logs, and 360 of 14,000, 400 bytes each.
	if estimate.Logs != 7_560_000 || estimate.Bytes != 3_024_000_000 ||
		!estimate.TextMatch || estimate.OverBudget || !estimate.From.Equal(from) || !estimate.To.Equal(to) {
		t.Errorf("Expected 7,560,000 Logs and 3,024,000,000 bytes, within the budget, got %+v", estimate)
	}
	if string(answer["refusal"]) != "null" {
		t.Errorf("Expected no refusal within the budget, got %s", answer["refusal"])
	}
	// A heavy read is one over a quarter of the budget.
	if !estimate.Heavy {
		t.Errorf("Expected 3,024,000,000 bytes, over a quarter of 4 GiB, heavy, got %+v", estimate)
	}
}

// The Logs explorer's histogram counts the Logs that pass the Query's
// filters, so a search reads as much for it as for the run.
func TestTheHistogramOfASearchOverTheBudgetIsRefusedToo(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedBusyProject(t, env, 123, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)
	histogram := func(query string, force bool) map[string]json.RawMessage {
		return answerOf(t, env, "/api/123/logs/histogram", map[string]any{
			"query": query, "from": lastWeek.from, "to": lastWeek.to, "check_budget": true, "force": force}, 200)
	}

	refused := histogram(`search "timeout"`, false)
	if _, counted := refused["buckets"]; counted {
		t.Errorf("Expected no buckets with a refusal, got %s", mustMarshal(t, refused))
	}
	if refusal := refusalOf(t, refused); refusal.NarrowerQuery == nil || !refusal.Estimate.OverBudget {
		t.Errorf("Expected a refusal over the budget with a narrower Query, got %+v", refusal)
	}
	for _, run := range []struct {
		query string
		force bool
	}{{`search "timeout"`, true}, {``, false}, {`where level == "error"`, false}} {
		if answer := histogram(run.query, run.force); answer["buckets"] == nil {
			t.Errorf("Expected the histogram of %q (force %v), got %s", run.query, run.force, mustMarshal(t, answer))
		}
	}
}

// Dashboard Charts and Monitors don't ask to be checked: their runs log the
// estimate, refused or not, so the budget can be checked against them.
func TestARunThatDoesntAskToBeCheckedRunsAndLogsItsEstimate(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedBusyProject(t, env, 123, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)
	logs := captureLogs(t)

	answer := answerOf(t, env, "/api/123/logs/query", map[string]any{
		"query": `search "timeout" | summarize count() by bin_auto(time)`, "from": lastWeek.from, "to": lastWeek.to}, 200)
	ran(t, "a Chart's Query", answer)
	var line string
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, `message="Log read estimate"`) {
			line = l
		}
	}
	for _, field := range []string{
		"kind=log_read_estimate", "project_id=123", "text_match=true", fmt.Sprintf("bytes=%d", 7*1440*26_770_000),
		"over_budget=true", "checked=false", "refused=false",
	} {
		if !strings.Contains(line, field) {
			t.Errorf("Expected %s in the estimate's log line, got %q", field, line)
		}
	}
}

// setTextMatchBudget sets the budget of a Query that matches text, in MiB,
// as an Instance admin does on Settings → Instance.
func setTextMatchBudget(t *testing.T, env *TestEnv, mb int) {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		INSERT INTO instance_settings (id, logs_text_match_budget_mb, created_at, updated_at) VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET logs_text_match_budget_mb = excluded.logs_text_match_budget_mb`,
		mb, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to set the text-match budget: %v", err)
	}
}

// estimateOf is the estimate a checked run of a Query for Project 123
// answers, without running it.
func estimateOf(t *testing.T, env *TestEnv, query string, from, to time.Time) budgetEstimate {
	t.Helper()
	answer := checkedRun(t, env, query, from, to, map[string]any{"estimate": true})
	var estimate budgetEstimate
	if err := json.Unmarshal(answer["estimate"], &estimate); err != nil {
		t.Fatalf("Decode the estimate %s: %v", answer["estimate"], err)
	}
	return estimate
}

func TestTheBudgetSetInConsoleAppliesFromTheNextQueryWithoutARestart(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedBusyProject(t, env, 123, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)
	query := `search "timeout" | summarize count() by file`

	// With no instance_settings row, the week's 251.3 GiB is past the default
	// 4 GiB.
	if refusal := refusalOf(t, checkedRun(t, env, query, lastWeek.from, lastWeek.to, nil)); refusal.Estimate.BudgetBytes != fourGiB {
		t.Errorf("Expected the default budget of 4 GiB with no instance settings, got %+v", refusal.Estimate)
	}

	setTextMatchBudget(t, env, 256<<10)
	ran(t, "the week's search under a 256 GiB budget", checkedRun(t, env, query, lastWeek.from, lastWeek.to, nil))
	if e := estimateOf(t, env, query, lastWeek.from, lastWeek.to); e.BudgetBytes != 256<<30 || e.OverBudget || !e.Heavy {
		t.Errorf("Expected the week within 256 GiB, and heavy, past a quarter of it, got %+v", e)
	}

	// Two hours' 3.0 GiB are heavy under the default budget, past its
	// quarter, but not under 16 GiB, whose quarter is 4 GiB.
	setTextMatchBudget(t, env, 16<<10)
	if e := estimateOf(t, env, query, lastTwoHours.from, lastTwoHours.to); e.BudgetBytes != 16<<30 || e.OverBudget || e.Heavy {
		t.Errorf("Expected two hours within 16 GiB and not heavy, got %+v", e)
	}

	// 0 refuses no Query.
	setTextMatchBudget(t, env, 0)
	ran(t, "the week's search with no budget", checkedRun(t, env, query, lastWeek.from, lastWeek.to, nil))
}

// A Console that hasn't run its migration yet has no budget to read: the
// default applies, and the failure is logged.
func TestABudgetThatCantBeReadIsTheDefault(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)
	seedBusyProject(t, env, 123, lastWeek.from.Truncate(24*time.Hour), testNow, tagalysSources)
	setTextMatchBudget(t, env, 256<<10)
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`ALTER TABLE instance_settings DROP COLUMN logs_text_match_budget_mb`); err != nil {
		t.Fatalf("Drop the budget column: %v", err)
	}
	logs := captureLogs(t)

	query := `search "timeout" | summarize count() by file`
	if refusal := refusalOf(t, checkedRun(t, env, query, lastWeek.from, lastWeek.to, nil)); refusal.Estimate.BudgetBytes != fourGiB {
		t.Errorf("Expected the default budget of 4 GiB, got %+v", refusal.Estimate)
	}
	ran(t, "the narrower Query", checkedRun(t, env, `where file == "cron.log" | `+query, lastWeek.from, lastWeek.to, nil))
	if !strings.Contains(logs.String(), `message="Reading the text-match budget failed, so the default applies"`) {
		t.Errorf("Expected the failed read logged, got %s", logs)
	}
}
