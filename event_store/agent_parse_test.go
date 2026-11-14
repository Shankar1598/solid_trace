package main_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
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
