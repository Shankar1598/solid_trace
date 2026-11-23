package main_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// One Agent session's page (spec, The agent ingest API: the read
// endpoints for Console): its header, the totals of agent_sessions, and
// its timeline from the derived Logs, in pages.

type timelineStep struct {
	ID         string   `json:"id"`
	Time       string   `json:"time"`
	Path       string   `json:"path"`
	Version    uint32   `json:"version"`
	Offset     uint64   `json:"offset"`
	Line       string   `json:"line"`
	Role       string   `json:"role"`
	Text       string   `json:"text"`
	ToolName   string   `json:"tool_name"`
	ToolUseID  string   `json:"tool_use_id"`
	SubagentID string   `json:"subagent_id"`
	Model      string   `json:"model"`
	Level      string   `json:"level"`
	CostUSD    *float64 `json:"cost_usd"`
	Tokens     *struct {
		Input      uint64 `json:"input"`
		Output     uint64 `json:"output"`
		CacheRead  uint64 `json:"cache_read"`
		CacheWrite uint64 `json:"cache_write"`
	} `json:"tokens"`
	Cut bool `json:"cut"`
}

type timelineSubagent struct {
	Path      string `json:"path"`
	AgentID   string `json:"agent_id"`
	ToolUseID string `json:"tool_use_id"`
	First     string `json:"first"`
	Last      string `json:"last"`
}

type timelineAnswer struct {
	AgentSession listedAgentSession `json:"agent_session"`
	Subagents    []timelineSubagent `json:"subagents"`
	Steps        []timelineStep     `json:"steps"`
	NextCursor   string             `json:"next_cursor"`
}

func readTimeline(t *testing.T, env *TestEnv, projectID int64, sessionID string, body map[string]any) timelineAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/agent-sessions/%s", projectID, sessionID), body)
	if status != 200 {
		t.Fatalf("Expected 200 for %v, got %d: %s", body, status, raw)
	}
	var answer timelineAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode %s: %v", raw, err)
	}
	return answer
}

func stepTexts(steps []timelineStep) []string {
	texts := []string{}
	for _, s := range steps {
		texts = append(texts, s.Role+": "+s.Text)
	}
	return texts
}

func expectTexts(t *testing.T, what string, got, want []string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s: expected\n  %q\ngot\n  %q", what, want, got)
	}
}

func TestTheAgentSessionPageGivesTheHeaderAndTheTimelineInOrder(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", folder: "apps/web", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"cart.go"}}`),
		`{"type":"user","uuid":"u2","timestamp":"2026-10-02T11:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"package cart"}]}}`,
		`{"type":"user","uuid":"u3","isMeta":true,"timestamp":"2026-10-02T11:00:07Z","message":{"role":"user","content":"Stop hook feedback: run the report"}}`,
		replyAt("a2", "2026-10-02T11:00:09Z", "msg_2", "claude-opus-5-5", `{"type":"text","text":"The cart page is done."}`),
		costState(0.01),
	)}), 200)
	env.ProcessAgentFiles(t)

	answer := readTimeline(t, env, o.project, laptopSession, map[string]any{})
	h := answer.AgentSession
	if h.ID != laptopSession || h.UserID != o.user || h.Repository != "github.com/acme/shop" || h.Folder != "apps/web" ||
		h.Model != "claude-opus-5-5" || h.First != "2026-10-02T11:00:00Z" || h.Last != "2026-10-02T11:00:09Z" {
		t.Errorf("Expected the header of the engineer's Agent session, got %+v", h)
	}
	if h.Input != 20 || h.Output != 100 || h.CacheRead != 2000 || h.CacheWrite != 200 || h.CostUSD == nil || *h.CostUSD != 0.01 {
		t.Errorf("Expected the tokens of two requests and the cost-state cost, got %+v", h)
	}
	expectTexts(t, "the timeline", stepTexts(answer.Steps), []string{
		"person: Add a cart page",
		`assistant: {"file_path":"cart.go"}`,
		"tool: package cart",
		"not from a person: Stop hook feedback: run the report",
		"assistant: The cart page is done.",
	})
	if len(answer.Steps) != 5 {
		t.FailNow()
	}
	call, result := answer.Steps[1], answer.Steps[2]
	if call.ToolName != "Read" || call.ToolUseID != "toolu_1" || result.ToolName != "Read" || result.ToolUseID != "toolu_1" {
		t.Errorf("Expected the tool call and its result to carry the tool and its id, got %+v and %+v", call, result)
	}
	if call.Tokens == nil || call.Tokens.Output != 50 || call.CostUSD == nil || call.Model != "claude-opus-5-5" {
		t.Errorf("Expected the request's tokens, cost and model on the reply, got %+v", call)
	}
	if first := answer.Steps[0]; first.Path != "session.jsonl" || first.Version != 1 || first.Offset != 0 || first.Time != "2026-10-02T11:00:00Z" {
		t.Errorf("Expected the step's raw line and time, got %+v", first)
	}
	if answer.NextCursor != "" || len(answer.Subagents) != 0 {
		t.Errorf("Expected one page and no subagents, got %q and %+v", answer.NextCursor, answer.Subagents)
	}
}

func TestAMidTurnMessageShowsAtTheTimeTheAgentGotIt(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)
	session := "9d3b0d78-a7ce-4152-ba06-ce61683e5ef7"
	uploadFixture(t, env, o, "mid-turn-message.jsonl", session)

	steps := readTimeline(t, env, o.project, session, map[string]any{}).Steps
	at := -1
	for i, s := range steps {
		if s.Text == "Also reply with the word BANANA." {
			at = i
		}
	}
	// The person wrote it during the Bash call, and the agent got it with
	// the call's result: so it comes after the result, not before.
	if at < 1 || steps[at].Role != "person" || steps[at-1].Role != "tool" || steps[at].Time != "2026-11-14T13:39:39.58Z" {
		t.Fatalf("Expected the mid-turn message after the tool result, at its delivery time, got %q", stepTexts(steps))
	}
	if at+1 >= len(steps) || steps[at+1].Role != "assistant" {
		t.Errorf("Expected the agent's reply after the mid-turn message, got %q", stepTexts(steps))
	}
}

func TestASubagentShowsInsideItsParentAgentSession(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Find the cart code"),
		replyAt("a1", "2026-10-02T11:00:01Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_9","name":"Agent","input":{"description":"Find the cart"}}`),
		`{"type":"user","uuid":"u2","timestamp":"2026-10-02T11:00:09Z","toolUseResult":{"agentId":"a1b2","status":"completed"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_9","content":"cart.go"}]}}`,
	)}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-a1b2.jsonl", body: jsonl(
		`{"type":"user","uuid":"s1","isSidechain":true,"agentId":"a1b2","timestamp":"2026-10-02T11:00:02Z","message":{"role":"user","content":"Find the cart"}}`,
		replyAt("s2", "2026-10-02T11:00:03Z", "msg_s", "claude-sonnet-5-5", `{"type":"text","text":"It is in cart.go."}`),
	)}), 200)
	env.ProcessAgentFiles(t)

	answer := readTimeline(t, env, o.project, laptopSession, map[string]any{})
	expectTexts(t, "the main timeline", stepTexts(answer.Steps), []string{
		"person: Find the cart code", `assistant: {"description":"Find the cart"}`, "tool: cart.go",
	})
	if len(answer.Steps) == 3 && answer.Steps[2].SubagentID != "a1b2" {
		t.Errorf("Expected the Agent call's result to name its subagent, got %+v", answer.Steps[2])
	}
	want := timelineSubagent{Path: "subagents/agent-a1b2.jsonl", AgentID: "a1b2", ToolUseID: "toolu_9",
		First: "2026-10-02T11:00:02Z", Last: "2026-10-02T11:00:03Z"}
	if len(answer.Subagents) != 1 || answer.Subagents[0] != want {
		t.Errorf("Expected the subagent with the tool call that started it, got %+v", answer.Subagents)
	}

	sub := readTimeline(t, env, o.project, laptopSession, map[string]any{"path": "subagents/agent-a1b2.jsonl"})
	expectTexts(t, "the subagent's timeline", stepTexts(sub.Steps), []string{
		"not from a person: Find the cart", "assistant: It is in cart.go.",
	})
	if sub.AgentSession.Model != "claude-opus-5-5" {
		t.Errorf("Expected the header's model from the main transcript, got %q", sub.AgentSession.Model)
	}
}

func TestATrimmedTranscriptShowsItsNewVersionAndItsFlag(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "One"), personAt("u2", "2026-10-02T11:00:01Z", "Two"),
	)}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", version: 2, body: jsonl(
		personAt("u3", "2026-10-02T11:00:02Z", "Three"),
	)}), 200)
	env.ProcessAgentFiles(t)

	answer := readTimeline(t, env, o.project, laptopSession, map[string]any{})
	expectTexts(t, "the timeline", stepTexts(answer.Steps), []string{"person: Three"})
	if fmt.Sprint(answer.AgentSession.Flags) != "[trimmed]" {
		t.Errorf("Expected the flag trimmed, got %v", answer.AgentSession.Flags)
	}
}

func TestALongAgentSessionLoadsInPages(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	var lines, want []string
	start := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		// Two lines at each second: a page can end between them.
		at := start.Add(time.Duration(i/2) * time.Second).Format(time.RFC3339)
		lines = append(lines, personAt(fmt.Sprintf("u%d", i), at, fmt.Sprintf("Message %02d", i)))
		want = append(want, fmt.Sprintf("person: Message %02d", i))
	}
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(lines...)}), 200)
	env.ProcessAgentFiles(t)

	var got []string
	cursor, pages := "", 0
	for {
		body := map[string]any{"limit": 10}
		if cursor != "" {
			body["cursor"] = cursor
		}
		answer := readTimeline(t, env, o.project, laptopSession, body)
		pages++
		if len(answer.Steps) > 10 {
			t.Fatalf("Expected at most 10 steps in a page, got %d", len(answer.Steps))
		}
		got = append(got, stepTexts(answer.Steps)...)
		if cursor = answer.NextCursor; cursor == "" || pages > 5 {
			break
		}
	}
	if pages != 3 {
		t.Errorf("Expected 3 pages, got %d", pages)
	}
	expectTexts(t, "the pages together", got, want)
}

func TestALongAgentSessionLoadsInPagesFromItsEndWithTheNewestOrder(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	var lines, want []string
	start := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		// Two lines at each second: a page can end between them.
		at := start.Add(time.Duration(i/2) * time.Second).Format(time.RFC3339)
		lines = append(lines, personAt(fmt.Sprintf("u%d", i), at, fmt.Sprintf("Message %02d", i)))
		want = append([]string{fmt.Sprintf("person: Message %02d", i)}, want...)
	}
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(lines...)}), 200)
	env.ProcessAgentFiles(t)

	var got []string
	cursor, pages := "", 0
	for {
		body := map[string]any{"limit": 10, "order": "newest"}
		if cursor != "" {
			body["cursor"] = cursor
		}
		answer := readTimeline(t, env, o.project, laptopSession, body)
		pages++
		if len(answer.Steps) > 10 {
			t.Fatalf("Expected at most 10 steps in a page, got %d", len(answer.Steps))
		}
		got = append(got, stepTexts(answer.Steps)...)
		if cursor = answer.NextCursor; cursor == "" || pages > 5 {
			break
		}
	}
	if pages != 3 {
		t.Errorf("Expected 3 pages, got %d", pages)
	}
	expectTexts(t, "the pages together, the newest step first", got, want)

	oldest := readTimeline(t, env, o.project, laptopSession, map[string]any{"limit": 2, "order": "oldest"})
	expectTexts(t, "the oldest order", stepTexts(oldest.Steps), []string{"person: Message 00", "person: Message 01"})
}

func TestTheAgentSessionPageRefusesABadRequest(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "One"),
	)}), 200)
	env.ProcessAgentFiles(t)

	cases := []struct {
		name, session string
		body          map[string]any
		want          int
	}{
		{"an Agent session that the Project does not have", runSession, map[string]any{}, 404},
		{"an id that is not a UUID", "nope", map[string]any{}, 404},
		{"a path that the Agent session does not have", laptopSession, map[string]any{"path": "subagents/agent-x.jsonl"}, 404},
		{"a bad cursor", laptopSession, map[string]any{"cursor": "nope"}, 400},
		{"a limit over 500", laptopSession, map[string]any{"limit": 501}, 400},
		{"an order that is not oldest or newest", laptopSession, map[string]any{"order": "random"}, 400},
	}
	for _, c := range cases {
		status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/agent-sessions/%s", o.project, c.session), c.body)
		if status != c.want {
			t.Errorf("%s: expected %d, got %d: %s", c.name, c.want, status, raw)
		}
	}
}

func TestAnAgentSessionUploadedLateOverTwoStoredDaysShowsInTheOrderOfItsLines(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	// The lines of the day before the stored day get the stored time, and
	// keep their own time in original.time.
	env.Clock.Set(time.Date(2026, 10, 3, 0, 30, 0, 0, time.UTC))
	first := jsonl(
		personAt("u1", "2026-10-02T23:50:00.123Z", "One"),
		personAt("u2", "2026-10-02T23:50:00.124Z", "Two"),
		personAt("u3", "2026-10-03T00:10:00Z", "Three"),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	env.ProcessAgentFiles(t)
	env.Clock.Set(time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: jsonl(
		personAt("u4", "2026-10-03T00:20:00Z", "Four"),
		personAt("u5", "2026-10-04T00:50:00Z", "Five"),
	)}), 200)
	env.ProcessAgentFiles(t)

	want := []string{"person: One", "person: Two", "person: Three", "person: Four", "person: Five"}
	wantTimes := []string{"2026-10-02T23:50:00.123Z", "2026-10-02T23:50:00.124Z", "2026-10-03T00:10:00Z", "2026-10-03T00:20:00Z", "2026-10-04T00:50:00Z"}
	steps := readTimeline(t, env, o.project, laptopSession, map[string]any{}).Steps
	expectTexts(t, "the timeline", stepTexts(steps), want)
	var times []string
	for _, s := range steps {
		times = append(times, s.Time)
	}
	expectTexts(t, "the steps' own times", times, wantTimes)

	var got []string
	cursor, pages := "", 0
	for {
		body := map[string]any{"limit": 2}
		if cursor != "" {
			body["cursor"] = cursor
		}
		answer := readTimeline(t, env, o.project, laptopSession, body)
		pages++
		got = append(got, stepTexts(answer.Steps)...)
		if cursor = answer.NextCursor; cursor == "" || pages > 5 {
			break
		}
	}
	if pages != 3 {
		t.Errorf("Expected 3 pages, got %d", pages)
	}
	expectTexts(t, "the pages together", got, want)
}

func TestWithoutAMainTranscriptTheMainTimelineIsEmpty(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	// The unit of the main transcript did not come yet.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-zz.jsonl", body: jsonl(
		`{"type":"user","uuid":"s1","isSidechain":true,"agentId":"zz","timestamp":"2026-10-02T11:00:02Z","message":{"role":"user","content":"S1"}}`,
	)}), 200)
	env.ProcessAgentFiles(t)

	answer := readTimeline(t, env, o.project, laptopSession, map[string]any{})
	expectTexts(t, "the main timeline", stepTexts(answer.Steps), []string{})
	if len(answer.Subagents) != 1 || answer.Subagents[0].AgentID != "zz" {
		t.Errorf("Expected one subagent, got %+v", answer.Subagents)
	}
	sub := readTimeline(t, env, o.project, laptopSession, map[string]any{"path": "subagents/agent-zz.jsonl"})
	expectTexts(t, "the subagent's timeline", stepTexts(sub.Steps), []string{"not from a person: S1"})
}

func TestAPageThatHoldsTheLastStepGivesNoCursor(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "One"),
		personAt("u2", "2026-10-02T11:00:01Z", "Two"),
	)}), 200)
	env.ProcessAgentFiles(t)

	// Exactly as many steps as the limit: no next page.
	answer := readTimeline(t, env, o.project, laptopSession, map[string]any{"limit": 2})
	expectTexts(t, "a full page", stepTexts(answer.Steps), []string{"person: One", "person: Two"})
	if answer.NextCursor != "" {
		t.Errorf("Expected no next_cursor when the page holds the last step, got %q", answer.NextCursor)
	}

	// One step more than the limit: a next page.
	answer = readTimeline(t, env, o.project, laptopSession, map[string]any{"limit": 1})
	expectTexts(t, "the first page", stepTexts(answer.Steps), []string{"person: One"})
	if answer.NextCursor == "" {
		t.Fatal("Expected a next_cursor when more steps follow the page")
	}
	answer = readTimeline(t, env, o.project, laptopSession, map[string]any{"limit": 1, "cursor": answer.NextCursor})
	expectTexts(t, "the last page", stepTexts(answer.Steps), []string{"person: Two"})
	if answer.NextCursor != "" {
		t.Errorf("Expected no next_cursor on the last page, got %q", answer.NextCursor)
	}
}
