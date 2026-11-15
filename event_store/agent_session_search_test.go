package main_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Search inside one Agent session, and 'Show full' on a cut value (spec,
// Storage: Search; Console: the Agent session page).

func TestASearchInsideAnAgentSessionFindsItsMatchingStepsOnly(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a Cart page"),
		replyAt("a1", "2026-10-02T11:00:01Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_9","name":"Agent","input":{"description":"Find the code"}}`),
		`{"type":"user","uuid":"u2","timestamp":"2026-10-02T11:00:09Z","toolUseResult":{"agentId":"a1b2","status":"completed"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_9","content":"done"}]}}`,
		replyAt("a2", "2026-10-02T11:00:10Z", "msg_2", "claude-opus-5-5", `{"type":"text","text":"The cart page is done."}`),
	)}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-a1b2.jsonl", body: jsonl(
		replyAt("s1", "2026-10-02T11:00:03Z", "msg_s", "claude-sonnet-5-5", `{"type":"text","text":"It is in cart.go."}`),
	)}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, runSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Another cart"),
	)}), 200)
	env.ProcessAgentFiles(t)

	answer := readTimeline(t, env, o.project, laptopSession, map[string]any{"search": "CART"})
	expectTexts(t, "the matching steps of the Agent session and its subagents", stepTexts(answer.Steps), []string{
		"person: Add a Cart page", "assistant: It is in cart.go.", "assistant: The cart page is done.",
	})
	if len(answer.Steps) == 3 && answer.Steps[1].Path != "subagents/agent-a1b2.jsonl" {
		t.Errorf("Expected the subagent's step to name its transcript, got %+v", answer.Steps[1])
	}

	sub := readTimeline(t, env, o.project, laptopSession, map[string]any{"search": "cart", "path": "subagents/agent-a1b2.jsonl"})
	expectTexts(t, "the matching steps of one transcript", stepTexts(sub.Steps), []string{"assistant: It is in cart.go."})

	none := readTimeline(t, env, o.project, laptopSession, map[string]any{"search": "checkout"})
	if len(none.Steps) != 0 || none.NextCursor != "" {
		t.Errorf("Expected no steps for a text that no step has, got %q", stepTexts(none.Steps))
	}

	var got []string
	cursor := ""
	for pages := 0; pages < 4; pages++ {
		body := map[string]any{"search": "cart", "limit": 2}
		if cursor != "" {
			body["cursor"] = cursor
		}
		page := readTimeline(t, env, o.project, laptopSession, body)
		got = append(got, stepTexts(page.Steps)...)
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	expectTexts(t, "the search in pages", got, stepTexts(answer.Steps))
}

func TestASearchOfMoreThan1000CharactersIsRefused(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "One"),
	)}), 200)
	env.ProcessAgentFiles(t)

	// "é" has 2 bytes: the limit counts characters, not bytes.
	for _, tc := range []struct {
		runes int
		want  int
	}{{1000, 200}, {1001, 400}} {
		status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/agent-sessions/%s", o.project, laptopSession),
			map[string]any{"search": strings.Repeat("é", tc.runes)})
		if status != tc.want {
			t.Errorf("A search of %d characters: expected %d, got %d: %s", tc.runes, tc.want, status, raw)
		}
	}
}

type rawLineAnswer struct {
	Line string `json:"line"`
	Cut  bool   `json:"cut"`
}

func readRawLine(t *testing.T, env *TestEnv, projectID int64, sessionID string, body map[string]any) (int, rawLineAnswer) {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/agent-sessions/%s/line", projectID, sessionID), body)
	var answer rawLineAnswer
	if status == 200 {
		if err := json.Unmarshal(raw, &answer); err != nil {
			t.Fatalf("Decode %s: %v", raw, err)
		}
	}
	return status, answer
}

func TestShowFullGivesTheWholeRedactedRawLineOfACutStep(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	secret := defaultSecrets()["Anthropic API key"]
	// 150,000 bytes, past the 64 KiB cut, in the middle of the unit.
	long := strings.Repeat("cart ", 30000) + secret
	lines := []string{
		personAt("u1", "2026-10-02T11:00:00Z", "Read the log"),
		fmt.Sprintf(`{"type":"user","uuid":"u2","timestamp":"2026-10-02T11:01:00Z","message":{"role":"user","content":%q}}`, long),
		personAt("u3", "2026-10-02T11:02:00Z", "Thanks"),
	}
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(lines...)}), 200)
	env.ProcessAgentFiles(t)

	steps := readTimeline(t, env, o.project, laptopSession, map[string]any{}).Steps
	if len(steps) != 3 || !steps[1].Cut {
		t.Fatalf("Expected the second step to be cut, got %d steps", len(steps))
	}
	cut := steps[1]
	status, answer := readRawLine(t, env, o.project, laptopSession, map[string]any{"path": cut.Path, "version": cut.Version, "offset": cut.Offset})
	if status != 200 {
		t.Fatalf("Expected 200, got %d", status)
	}
	want := strings.Replace(lines[1], secret, stars(secret), 1)
	if answer.Line != want || answer.Cut {
		t.Errorf("Expected the whole redacted line of %d bytes, got %d bytes (cut %v)", len(want), len(answer.Line), answer.Cut)
	}
	if strings.Contains(answer.Line, secret) {
		t.Errorf("Expected no secret in the raw line")
	}

	_, first := readRawLine(t, env, o.project, laptopSession, map[string]any{"path": "session.jsonl", "version": 1, "offset": 0})
	if first.Line != lines[0] {
		t.Errorf("Expected the first line, got %q", first.Line)
	}

	otherProject := o.rows.agentProject(o.rows.organization("Other"), "github.com/acme/shop")
	cases := []struct {
		name    string
		project int64
		session string
		body    map[string]any
		want    int
	}{
		{"another Organization's Agent Project", otherProject, laptopSession, map[string]any{"path": "session.jsonl", "version": 1, "offset": 0}, 404},
		{"an Agent session that the Project does not have", o.project, runSession, map[string]any{"path": "session.jsonl", "version": 1, "offset": 0}, 404},
		{"a path that the Agent session does not have", o.project, laptopSession, map[string]any{"path": "other.jsonl", "version": 1, "offset": 0}, 404},
		{"a version that the path does not have", o.project, laptopSession, map[string]any{"path": "session.jsonl", "version": 2, "offset": 0}, 404},
		{"an offset past the file", o.project, laptopSession, map[string]any{"path": "session.jsonl", "version": 1, "offset": 10_000_000}, 404},
		{"no path", o.project, laptopSession, map[string]any{"version": 1, "offset": 0}, 400},
	}
	for _, c := range cases {
		if status, _ := readRawLine(t, env, c.project, c.session, c.body); status != c.want {
			t.Errorf("%s: expected %d, got %d", c.name, c.want, status)
		}
	}
}

func TestShowFullMarksALineCutWhenTheRawLayerDoesNotHoldAllOfIt(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	// One unit of two 20 MiB lines, over 32 MiB, so the ingest API keeps
	// only its first 32 MiB: the unit is cut, and it is the last unit of
	// the file.
	const mib = 1 << 20
	first := strings.Repeat("a", 20*mib) + "\n"
	second := strings.Repeat("b", 20*mib) + "\n"
	unit := []byte(first + second)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", size: len(unit), body: unit[:32*mib]}), 200)
	env.ProcessAgentFiles(t)

	status, answer := readRawLine(t, env, o.project, laptopSession, map[string]any{"path": "session.jsonl", "version": 1, "offset": 0})
	if status != 200 {
		t.Fatalf("Expected 200, got %d", status)
	}
	if len(answer.Line) != 16*mib || !answer.Cut || strings.Trim(answer.Line, "a") != "" {
		t.Errorf("Expected a line over 16 MiB cut at 16 MiB, got %d bytes (cut %v)", len(answer.Line), answer.Cut)
	}

	status, answer = readRawLine(t, env, o.project, laptopSession, map[string]any{"path": "session.jsonl", "version": 1, "offset": len(first)})
	if status != 200 {
		t.Fatalf("Expected 200, got %d", status)
	}
	if want := 32*mib - len(first); len(answer.Line) != want || !answer.Cut {
		t.Errorf("Expected the %d stored bytes of a line in the missing part of a cut unit, marked cut, got %d bytes (cut %v)", want, len(answer.Line), answer.Cut)
	}
}
