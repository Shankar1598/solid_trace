package main_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
)

// The background parse: after the raw layer has a unit of a .jsonl file,
// each of its lines is one Log in the Agent Project, which a Query reads.

// A short Claude Code transcript: a person's message, a reply with a tool
// call, the tool's result, and a line with no timestamp.
var transcript = []string{
	`{"type":"user","uuid":"u1","sessionId":"x","timestamp":"2026-10-02T11:00:00Z","message":{"role":"user","content":"Add a cart page"}}`,
	`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-10-02T11:00:05Z","message":{"id":"msg_1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"cart.go"}}]}}`,
	`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-10-02T11:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"package cart"}]}}`,
	`{"type":"ai-title","aiTitle":"Cart page"}`,
}

func jsonl(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

// sessionLogs is every Log of one Agent session in the Agent Project, by
// the Query on its id, oldest first.
func sessionLogs(t *testing.T, env *TestEnv, projectID int64, sessionID string) []listedLog {
	t.Helper()
	env.ProcessAgentFiles(t)
	logs := runQueryPages(t, env, uint32(projectID), queryRequest{
		Query: fmt.Sprintf(`where agent.session_id == "%s"`, sessionID), From: allTime.from, To: allTime.to, Limit: 1000,
	})
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	return logs
}

// offsetsOfLines is the offset of each line of a unit at offset 0.
func offsetsOfLines(lines []string) []float64 {
	var offsets []float64
	offset := 0
	for _, line := range lines {
		offsets = append(offsets, float64(offset))
		offset += len(line) + 1
	}
	return offsets
}

func TestTheParseMakesOneLogForEachLineOfAnAgentSession(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	body := jsonl(transcript...)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", folder: "web", body: body}), 200)
	// A file that is not .jsonl has no Logs.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "tool-results/r1.txt", body: []byte("package cart\n")}), 200)

	logs := sessionLogs(t, env, o.project, laptopSession)
	if len(logs) != len(transcript) {
		t.Fatalf("Expected %d Logs, one for each line, got %d: %+v", len(transcript), len(logs), logs)
	}
	offsets := offsetsOfLines(transcript)
	want := []struct {
		message, role, tool string
		time                time.Time
	}{
		{"Add a cart page", "user", "", time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)},
		{`Read {"file_path":"cart.go"}`, "assistant", "Read", time.Date(2026, 10, 2, 11, 0, 5, 0, time.UTC)},
		{"package cart", "tool", "Read", time.Date(2026, 10, 2, 11, 0, 6, 0, time.UTC)},
		// A line with no timestamp has the time of the line before it.
		{"Cart page", "", "", time.Date(2026, 10, 2, 11, 0, 6, 0, time.UTC)},
	}
	for i, log := range logs {
		w := want[i]
		a := log.Attributes
		if log.Message != w.message || !log.Time.Equal(w.time) || log.Service != "github.com/acme/shop" || a["file"] != "session.jsonl" ||
			a["agent.session_id"] != laptopSession || a["agent.raw.offset"] != offsets[i] || a["agent.raw.version"] != float64(1) {
			t.Errorf("Log %d: expected %q at %s of github.com/acme/shop, session.jsonl at offset %v, got %+v", i, w.message, w.time, offsets[i], log)
		}
		if role, _ := a["agent.role"].(string); role != w.role {
			t.Errorf("Log %d: expected the role %q, got %q", i, w.role, role)
		}
		if tool, _ := a["agent.tool_name"].(string); tool != w.tool {
			t.Errorf("Log %d: expected the tool %q, got %q", i, w.tool, tool)
		}
		// A laptop's Agent session is not a Run's.
		if _, ok := a["factory.project"]; ok {
			t.Errorf("Log %d: expected no factory.project, got %+v", i, a)
		}
	}
}

// A failed tool call and a system line of level error have the level
// error, so that a Query or a Monitor on level finds them.
func TestAFailedToolCallAndASystemErrorHaveTheLevelError(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	lines := []string{
		`{"type":"user","timestamp":"2026-10-02T11:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"no such file","is_error":true}]}}`,
		`{"type":"user","timestamp":"2026-10-02T11:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"package cart","is_error":false}]}}`,
		`{"type":"system","subtype":"api_error","level":"error","timestamp":"2026-10-02T11:00:03Z","content":"API Error: overloaded"}`,
		`{"type":"system","subtype":"informational","level":"info","timestamp":"2026-10-02T11:00:04Z","content":"Compacted"}`,
	}
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(lines...)}), 200)

	logs := sessionLogs(t, env, o.project, laptopSession)
	if len(logs) != len(lines) {
		t.Fatalf("Expected %d Logs, got %+v", len(lines), logs)
	}
	for i, wantError := range []bool{true, false, true, false} {
		if isError := logs[i].Level == "error"; isError != wantError {
			t.Errorf("Log %d (%q): expected the level error %v, got the level %q", i, logs[i].Message, wantError, logs[i].Level)
		}
	}
}

func TestTheLogsOfARunsAgentSessionCarryItsProjectRepositoryAndFolder(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	runSecret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
		scopes: []string{"sessions:write"}, sessionIDs: []string{runSession}})

	expectStatus(t, uploadUnit(t, env, runSecret, runSession, unitUpload{
		path: "session.jsonl", runID: "run-0193", project: "shop", folder: "services/api", body: jsonl(transcript[:2]...),
	}), 200)

	logs := sessionLogs(t, env, o.project, runSession)
	if len(logs) != 2 {
		t.Fatalf("Expected 2 Logs, got %+v", logs)
	}
	for i, log := range logs {
		a := log.Attributes
		if a["factory.project"] != "shop" || a["repository"] != "github.com/acme/shop" || a["folder"] != "services/api" ||
			log.Service != "github.com/acme/shop" {
			t.Errorf("Log %d: expected the Run's Project, repository and folder, got %+v", i, log)
		}
	}
}

func TestALongStringIsCutAtTheLogLimitsAndTheLogKeepsTheOffsetOfItsRawLine(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	long := strings.Repeat("cart ", 30000) // 150,000 bytes, past the 64 KiB cut
	lines := []string{transcript[0], fmt.Sprintf(`{"type":"user","timestamp":"2026-10-02T11:01:00Z","message":{"role":"user","content":%q}}`, long)}
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(lines...)}), 200)

	logs := sessionLogs(t, env, o.project, laptopSession)
	if len(logs) != 2 {
		t.Fatalf("Expected 2 Logs, got %d", len(logs))
	}
	cut := logs[1]
	if len(cut.Message) >= len(long) || len(cut.Message) < 60000 || !strings.HasPrefix(long, cut.Message) {
		t.Errorf("Expected the message cut at the Log limit, got %d bytes", len(cut.Message))
	}
	if cut.Attributes["solidtrace.truncated"] != true {
		t.Errorf("Expected the cut Log to be marked truncated, got %+v", cut.Attributes)
	}
	if want := float64(len(lines[0]) + 1); cut.Attributes["agent.raw.offset"] != want || cut.Attributes["agent.raw.bytes"] != float64(len(lines[1])) {
		t.Errorf("Expected the raw line at offset %v of %d bytes, got %v and %v", want, len(lines[1]),
			cut.Attributes["agent.raw.offset"], cut.Attributes["agent.raw.bytes"])
	}
	// The raw layer keeps the full line.
	if got := storedBytes(t, env, o.project, laptopSession, "session.jsonl"); got != string(jsonl(lines...)) {
		t.Errorf("Expected the raw layer to keep the full lines, got %d bytes", len(got))
	}
}

func TestTheParseStoppedBetweenTheInsertAndTheCursorMakesEachLogOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	first := jsonl(transcript[:2]...)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-a1.jsonl", body: jsonl(transcript[3])}), 200)
	if err := env.AgentFileProcessor.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}

	// The parse inserts the Logs, and EventStore stops before it moves its
	// cursor: the batches stay in Pebble.
	crashed := make(chan struct{})
	env.FlakyLogs.failAfterInsert(connectionLost, func() {
		env.FlakyLogs.goDown()
		close(crashed)
	})
	done := make(chan error, 1)
	go func() { done <- env.AgentParser.ProcessUntilCaughtUp() }()
	select {
	case <-crashed:
	case <-time.After(10 * time.Second):
		t.Fatalf("Expected the parse to insert the Logs")
	}
	// A line that comes after the range.
	second := jsonl(transcript[2])
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: second}), 200)
	env.Restart(t)
	<-done
	env.FlakyLogs.recover()

	logs := sessionLogs(t, env, o.project, laptopSession)
	got := map[string]int{}
	for _, log := range logs {
		got[fmt.Sprintf("%s %v", log.Attributes["file"], log.Attributes["agent.raw.offset"])]++
	}
	offsets := offsetsOfLines(transcript)
	want := map[string]int{
		fmt.Sprintf("session.jsonl %v", offsets[0]): 1,
		fmt.Sprintf("session.jsonl %v", offsets[1]): 1,
		fmt.Sprintf("session.jsonl %v", offsets[2]): 1,
		"subagents/agent-a1.jsonl 0":                1,
	}
	if len(logs) != len(want) || !maps.Equal(got, want) {
		t.Fatalf("Expected each line once, %v, got %v", want, got)
	}

	// Nothing is parsed again later, either.
	third := jsonl(transcript[3])
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first) + len(second), body: third}), 200)
	if logs := sessionLogs(t, env, o.project, laptopSession); len(logs) != len(want)+1 {
		t.Errorf("Expected %d Logs after the next upload, got %d", len(want)+1, len(logs))
	}
}

func TestTheParseStoppedBetweenTheCursorAndTheDeleteDeletesTheParsedBatchesAtStartup(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	project := uint32(o.project)

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(transcript[:2]...)}), 200)
	if err := env.AgentFileProcessor.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}
	batches, err := env.PebbleWriter.AgentFileBatchesAfter(project, uuid.Nil, 1<<20)
	if err != nil || len(batches) != 1 {
		t.Fatalf("Expected 1 Agent file batch, got %d: %v", len(batches), err)
	}

	// The parse inserts the Logs and moves its cursor, and EventStore stops
	// before it deletes the batches.
	crashed := make(chan struct{})
	env.FlakyLogs.failAfterInsert(connectionLost, func() {
		if err := env.PebbleWriter.SetAgentParseCursor(project, batches[0].ID); err != nil {
			t.Error(err)
		}
		env.FlakyLogs.goDown()
		close(crashed)
	})
	done := make(chan error, 1)
	go func() { done <- env.AgentParser.ProcessUntilCaughtUp() }()
	select {
	case <-crashed:
	case <-time.After(10 * time.Second):
		t.Fatalf("Expected the parse to insert the Logs")
	}
	env.Restart(t)
	<-done
	env.FlakyLogs.recover()

	if left, err := env.PebbleWriter.AgentFileBatchesAfter(project, uuid.Nil, 1<<20); err != nil || len(left) != 0 {
		t.Errorf("Expected startup to delete the parsed batches, got %d left: %v", len(left), err)
	}
	if logs := sessionLogs(t, env, o.project, laptopSession); len(logs) != 2 {
		t.Errorf("Expected each line once, 2 Logs, got %d", len(logs))
	}
}

func TestANewVersionAfterATrimIsParsedFromItsStartAndTheOldLogsStay(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(transcript[:3]...)}), 200)
	env.ProcessAgentFiles(t)
	// The file is trimmed to its last line, which is stored as version 2.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", version: 2, body: jsonl(transcript[2])}), 200)

	var got []string
	for _, log := range sessionLogs(t, env, o.project, laptopSession) {
		got = append(got, fmt.Sprintf("%v %v %s", log.Attributes["agent.raw.version"], log.Attributes["agent.raw.offset"], log.Message))
	}
	offsets := offsetsOfLines(transcript)
	want := []string{
		fmt.Sprintf("1 %v Add a cart page", offsets[0]),
		fmt.Sprintf(`1 %v Read {"file_path":"cart.go"}`, offsets[1]),
		fmt.Sprintf("1 %v package cart", offsets[2]),
		"2 0 package cart",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Expected the Logs of both versions %v, got %v", want, got)
	}
}

// A line written on an earlier day than the day the server stored it, as in
// an old Agent session that uploads late, has its Log on the day of its raw
// row, so that the disk limit and Retention drop the two together.
func TestALineOfAnEarlierDayHasItsLogOnTheDayOfItsRawRow(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	old := `{"type":"user","timestamp":"2026-10-01T09:00:00Z","message":{"role":"user","content":"October 1"}}`
	env.Clock.Set(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(old)}), 200)
	env.ProcessAgentFiles(t)
	env.Clock.Set(time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC))
	body := lines("October 21")
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(old) + 1, body: body}), 200)

	logs := sessionLogs(t, env, o.project, laptopSession)
	if len(logs) != 2 {
		t.Fatalf("Expected 2 Logs, got %+v", logs)
	}
	if want := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC); !logs[0].Time.Equal(want) || logs[0].Attributes["original.time"] != "2026-10-01T09:00:00Z" {
		t.Errorf("Expected the Log at the stored time %s with its own time in original.time, got %+v", want, logs[0])
	}

	expectKept := func(days []string, messages []string, when string) {
		t.Helper()
		if got := agentDaysKept(t, env, o.project); !slices.Equal(got, days) {
			t.Errorf("Expected the raw days %v %s, got %v", days, when, got)
		}
		if got := agentLogMessages(t, env, o.project); !slices.Equal(got, messages) {
			t.Errorf("Expected the Logs %v %s, got %v", messages, when, got)
		}
	}
	_, total := agentDiskUse(t, env)
	if err := env.ClickHouseLogs.EnforceDiskLimit(storage.LogDiskLimit{Bytes: total - 1}, []storage.DayFamily{storage.LogDays, storage.AgentDays}); err != nil {
		t.Fatalf("Enforcing the disk limit failed: %v", err)
	}
	expectKept([]string{"2026-10-21"}, []string{"October 21"}, "after the disk limit")
}

// A Match monitor on the Agent Project reads each Log that the parse makes
// exactly once, also when Log batches of the same Project, such as run
// events, are stored before the parse has caught up.
func TestAMatchMonitorOnTheAgentProjectReadsEachParsedLogOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	project := uint32(o.project)

	run := func(cursor string) ingestedPage {
		t.Helper()
		answer := answerOf(t, env, fmt.Sprintf("/api/%d/logs/query", o.project), map[string]any{"query": "", "after_batch": cursor}, 200)
		var page ingestedPage
		if err := json.Unmarshal(mustMarshal(t, answer), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	runEvent := func(message string) {
		t.Helper()
		storeRunEvent(t, env, project, message)
		env.ProcessLogs(t)
	}
	cursor := run("").IngestCursor
	got := map[string]int{}
	read := func() {
		t.Helper()
		page := run(cursor)
		for _, m := range messages(page.Logs) {
			got[m]++
		}
		cursor = page.IngestCursor
	}

	// A unit is stored, and a run event comes after it before the parse.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: lines("line 1")}), 200)
	if err := env.AgentFileProcessor.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}
	runEvent("event 1")
	read()
	if err := env.AgentParser.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}
	read()
	// A run event waits for Log processing while the parse goes on.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(lines("line 1")), body: lines("line 2")}), 200)
	storeRunEvent(t, env, project, "event 2")
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(lines("line 1", "line 2")), body: lines("line 3")}), 200)
	env.ProcessAgentFiles(t)
	read()
	env.ProcessLogs(t)
	read()
	read()

	want := map[string]int{"line 1": 1, "event 1": 1, "line 2": 1, "event 2": 1, "line 3": 1}
	if !maps.Equal(got, want) {
		t.Errorf("Expected the Monitor to read each Log once, %v, got %v", want, got)
	}
}

// The ingest cursor that a read gives is never lower than the cursor that
// the read started after. When Log processing has gone past a unit that
// waits for the parse, the parse cursor can be lower than the cursor that
// an earlier read gave.
func TestTheIngestCursorOfAReadIsNotLowerThanTheCursorItStartedAfter(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	project := uint32(o.project)

	run := func(cursor string) ingestedPage {
		t.Helper()
		answer := answerOf(t, env, fmt.Sprintf("/api/%d/logs/query", o.project), map[string]any{"query": "", "after_batch": cursor}, 200)
		var page ingestedPage
		if err := json.Unmarshal(mustMarshal(t, answer), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	cursor := run("").IngestCursor
	got := map[string]int{}
	read := func() {
		t.Helper()
		page := run(cursor)
		for _, m := range messages(page.Logs) {
			got[m]++
		}
		if page.IngestCursor < cursor {
			t.Errorf("Expected an ingest cursor not lower than %s, got %s", cursor, page.IngestCursor)
		}
		cursor = page.IngestCursor
	}

	// The parse moves its cursor, then Log processing moves past it.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: lines("line 1")}), 200)
	env.ProcessAgentFiles(t)
	storeRunEvent(t, env, project, "event 1")
	env.ProcessLogs(t)
	read()
	// A unit waits for the parse, and a later run event is stored.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(lines("line 1")), body: lines("line 2")}), 200)
	storeRunEvent(t, env, project, "event 2")
	env.ProcessLogs(t)
	read()
	env.ProcessAgentFiles(t)
	read()

	want := map[string]int{"line 1": 1, "event 1": 1, "line 2": 1, "event 2": 1}
	if !maps.Equal(got, want) {
		t.Errorf("Expected the reads to give each Log once, %v, got %v", want, got)
	}
}

// storeRunEvent stores a Log batch of one Log in a Project, as Log ingest
// does, for Log processing.
func storeRunEvent(t *testing.T, env *TestEnv, projectID uint32, message string) {
	t.Helper()
	log := models.Log{ID: uuid.Must(uuid.NewV7()), Time: env.Clock.Now(), Message: message}
	if err := env.PebbleWriter.WriteLogBatch(projectID, []models.Log{log}, env.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	env.LogProcessor.Notify(projectID)
}
