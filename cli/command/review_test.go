package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/solidtrace/cli/command"
)

func runDigest(t *testing.T, env command.Env, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env.Stdout, env.Stderr = &stdout, &stderr
	code := command.Run(context.Background(), append([]string{"review", "digest"}, args...), env)
	return code, stdout.String(), stderr.String()
}

type digest struct {
	Sessions []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"sessions"`
	Reviews int              `json:"reviews"`
	Events  []map[string]any `json:"events"`
}

func decodeDigest(t *testing.T, out string) digest {
	t.Helper()
	var d digest
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return d
}

// transcript writes lines as a transcript file and gives its path.
func transcript(t *testing.T, lines ...map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		data, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func userLine(uuid string, content any) map[string]any {
	return map[string]any{"type": "user", "uuid": uuid, "timestamp": "2026-11-03T08:00:00Z", "sessionId": "s-made",
		"cwd": "/work", "message": map[string]any{"role": "user", "content": content}}
}

func assistantLine(uuid string, blocks ...map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "uuid": uuid, "timestamp": "2026-11-03T08:00:01Z", "sessionId": "s-made",
		"cwd": "/work", "message": map[string]any{"role": "assistant", "content": blocks}}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func toolUse(name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": "t-" + name, "name": name, "input": input}
}

func kinds(d digest) []string {
	var k []string
	for _, e := range d.Events {
		k = append(k, e["kind"].(string))
	}
	return k
}

func TestReviewDigestGivesTheTimelineOfEachReview(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "review", "two-reviews.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "s-two.jsonl")
	if err := os.WriteFile(path, bytes.ReplaceAll(raw, []byte("@REPO@"), []byte(repo)), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "review", "two-reviews.want.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got, wanted any
	json.Unmarshal([]byte(stdout), &got)
	json.Unmarshal(want, &wanted)
	if !reflect.DeepEqual(got, wanted) {
		t.Fatalf("digest:\n%s\nwant\n%s", stdout, want)
	}
	if !strings.Contains(stdout, "<between> tries & no cap") {
		t.Fatalf("the digest escapes the code in a text:\n%s", stdout)
	}
}

func TestReviewDigestOfASessionWithNoReviewKeepsOnlyTheStory(t *testing.T) {
	code, stdout, stderr := runDigest(t, command.Env{}, "--file", filepath.Join("testdata", "review", "no-review.jsonl"))

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if len(d.Sessions) != 1 || d.Sessions[0].ID != "s-none" || d.Sessions[0].Title != "Rename the flag." || d.Reviews != 0 ||
		!reflect.DeepEqual(kinds(d), []string{"task", "commit", "person", "person"}) {
		t.Fatalf("digest:\n%s", stdout)
	}
	if d.Events[0]["text"] != "Rename the flag." || d.Events[1]["command"] != "git commit -m x" ||
		d.Events[2]["text"] != "/model opus" || d.Events[3]["text"] != "Good." || d.Events[3]["session"] != "s-none" {
		t.Fatalf("digest:\n%s", stdout)
	}
}

func TestReviewDigestTakesAReviewInTheFirstMessageAsTheTaskAndTheStart(t *testing.T) {
	path := transcript(t,
		userLine("p1", "<command-name>/code-review</command-name>\n<command-args>since main</command-args>"),
		assistantLine("p2", text("No findings.")),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if !reflect.DeepEqual(kinds(d), []string{"task", "review_start", "agent_text"}) || d.Reviews != 1 {
		t.Fatalf("digest:\n%s", stdout)
	}
	if d.Events[0]["text"] != "/code-review since main" || d.Events[1]["uuid"] != "p1" ||
		d.Events[1]["skill"] != "code-review" || d.Events[1]["trigger"] != "person" || d.Events[2]["text"] != "No findings." {
		t.Fatalf("digest:\n%s", stdout)
	}
}

func TestReviewDigestStartsAReviewOnlyForASkillThatSkillsMatches(t *testing.T) {
	path := transcript(t,
		userLine("p1", "Tidy the parser."),
		assistantLine("p2", toolUse("Skill", map[string]any{"skill": "code-review"})),
		assistantLine("p3", toolUse("Skill", map[string]any{"skill": "acme-review", "args": "the parser"})),
		assistantLine("p4", text("One finding.")),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path, "--skills", "^acme-review$")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if !reflect.DeepEqual(kinds(d), []string{"task", "review_start", "agent_text"}) || d.Reviews != 1 ||
		d.Events[1]["skill"] != "acme-review" || d.Events[1]["trigger"] != "agent" || d.Events[1]["args"] != "the parser" {
		t.Fatalf("digest:\n%s", stdout)
	}
}

func TestReviewDigestCutsLongTexts(t *testing.T) {
	long := func(n int) string { return strings.Repeat("é", n) }
	path := transcript(t,
		userLine("p1", long(5000)),
		userLine("p2", "<command-name>/review</command-name>"),
		assistantLine("p3", text(long(10000))),
		userLine("p4", long(4000)),
		assistantLine("p5", toolUse("Bash", map[string]any{"command": "git commit -m '" + long(400) + "'"})),
		userLine("p6", "<command-name>/review</command-name>"),
		assistantLine("p7", text(long(13000))),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if !reflect.DeepEqual(kinds(d), []string{"task", "review_start", "agent_text", "person", "commit", "review_start", "agent_text"}) {
		t.Fatalf("kinds: %v", kinds(d))
	}
	for i, want := range map[int]string{
		0: long(4000) + "…",
		2: long(4000) + "…",
		3: long(4000),
		6: long(4000) + "…",
	} {
		if d.Events[i]["text"] != want {
			t.Errorf("event %d: %d characters, want %d", i, len([]rune(d.Events[i]["text"].(string))), len([]rune(want)))
		}
	}
	if command := d.Events[4]["command"].(string); command != ("git commit -m '" + long(285) + "…") {
		t.Errorf("commit: %d characters: %s", len([]rune(command)), command)
	}
}

func TestReviewDigestFindsTheSessionInTheClaudeConfigDir(t *testing.T) {
	config := t.TempDir()
	projects := command.ClaudeProjectsDir(func(name string) string {
		if name == "CLAUDE_CONFIG_DIR" {
			return config
		}
		return ""
	}, "/no-home")
	main := filepath.Join(projects, "-work-a", "s-1.jsonl")
	subagent := filepath.Join(projects, "-work-a", "s-1", "subagents", "s-1.jsonl")
	for _, path := range []string{main, subagent} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(userLine("m1", "The main transcript's task."))
	os.WriteFile(main, append(raw, '\n'), 0o644)
	raw, _ = json.Marshal(userLine("x1", "A subagent's prompt."))
	os.WriteFile(subagent, append(raw, '\n'), 0o644)
	env := command.Env{ClaudeProjectsDir: projects}

	code, stdout, stderr := runDigest(t, env, "--session", "s-1")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if d := decodeDigest(t, stdout); len(d.Events) != 1 || d.Events[0]["text"] != "The main transcript's task." {
		t.Fatalf("digest:\n%s", stdout)
	}

	if code, _, stderr := runDigest(t, env, "--session", "s-2"); code != 1 || !strings.Contains(stderr, "no transcript") {
		t.Fatalf("a missing session: exit %d\n%s", code, stderr)
	}

	second := filepath.Join(projects, "-work-b", "s-1.jsonl")
	os.MkdirAll(filepath.Dir(second), 0o755)
	os.WriteFile(second, []byte("{}\n"), 0o644)
	if code, _, stderr := runDigest(t, env, "--session", "s-1"); code != 1 || !strings.Contains(stderr, "2 transcripts") {
		t.Fatalf("two transcripts: exit %d\n%s", code, stderr)
	}
}

func TestReviewDigestRefusesWrongArguments(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--file", "x.jsonl", "--skills", "("},
		{"--file", "x.jsonl", "extra"},
	} {
		if code, _, _ := runDigest(t, command.Env{}, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if code, _, stderr := runDigest(t, command.Env{ClaudeProjectsDir: t.TempDir()}, "--session", "../x"); code != 1 ||
		!strings.Contains(stderr, "not an Agent session id") {
		t.Errorf("a path as the id: exit %d\n%s", code, stderr)
	}
	if code, _, _ := runDigest(t, command.Env{}, "--file", filepath.Join(t.TempDir(), "none.jsonl")); code != 1 {
		t.Errorf("a missing file: exit %d, want 1", code)
	}
}

func TestReviewDigestKeepsTheSubagentsOfAReviewAndSkipsClaudeCodesOwnLines(t *testing.T) {
	synthetic := assistantLine("a-err", text("API Error: Connection lost mid-response."))
	synthetic["message"].(map[string]any)["model"] = "<synthetic>"
	handBack := userLine("u-back", "Another Claude session sent a message:\n<agent-message from=\"a1\">\nF1: the retry has no cap.\n</agent-message>")
	handBack["isMeta"] = true
	notice := "<task-notification>\n<task-id>a2</task-id>\n<result>" + strings.Repeat("F2: the log line is missing. ", 12) + "</result>\n</task-notification>"
	agent := toolUse("Agent", map[string]any{"description": "Fix F1", "prompt": "Add a cap to the retry."})
	agent["id"] = "t-fix"
	path := transcript(t,
		userLine("u-clear", "<command-name>/clear</command-name>"),
		userLine("u-task", "Add a retry to the uploader."),
		assistantLine("a-skill", toolUse("Skill", map[string]any{"skill": "code-review", "args": "the retry"})),
		assistantLine("a-launch", text("The reviews are running.")),
		handBack,
		userLine("u-notice", notice),
		synthetic,
		assistantLine("a-fix", agent),
		userLine("u-launched", []map[string]any{{"type": "tool_result", "tool_use_id": "t-fix", "content": "Async agent launched successfully."}}),
		userLine("u-done", []map[string]any{{"type": "tool_result", "tool_use_id": "t-fix", "content": []map[string]any{{"type": "text", "text": "The cap is in."}}}}),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	want := []string{"task", "review_start", "agent_text", "subagent_report", "subagent_report", "delegate", "subagent_report"}
	if !reflect.DeepEqual(kinds(d), want) {
		t.Fatalf("kinds: %v\n%s", kinds(d), stdout)
	}
	if d.Events[0]["uuid"] != "u-task" {
		t.Errorf("task: %v", d.Events[0])
	}
	if !strings.Contains(d.Events[3]["text"].(string), "F1: the retry has no cap.") ||
		!strings.HasPrefix(d.Events[4]["text"].(string), "F2: the log line is missing.") ||
		d.Events[5]["text"] != "Fix F1\n\nAdd a cap to the retry." ||
		d.Events[6]["text"] != "The cap is in." {
		t.Errorf("digest:\n%s", stdout)
	}
}

func TestReviewDigestTakesAFindingsBlockFromASubagentsReport(t *testing.T) {
	plan := toolUse("Agent", map[string]any{"description": "Plan the parts", "prompt": "List the parts."})
	plan["id"] = "t-plan"
	agent := toolUse("Agent", map[string]any{"description": "Build part 1", "prompt": "Build it, then run code-review."})
	agent["id"] = "t-part"
	report := "Part 1 is built.\n```solidtrace-findings\n{\"version\": 2, \"run\": 1, \"findings\": [{\"id\": \"F1\"}]}\n```"
	path := transcript(t,
		userLine("u-task", "Build the parser in parts."),
		assistantLine("a-early", text("I start part 1.")),
		assistantLine("a-plan", plan),
		userLine("u-plan", []map[string]any{{"type": "tool_result", "tool_use_id": "t-plan", "content": "There are two parts."}}),
		assistantLine("a-part", agent),
		userLine("u-report", []map[string]any{{"type": "tool_result", "tool_use_id": "t-part", "content": []map[string]any{{"type": "text", "text": report}}}}),
		assistantLine("a-after", text("F1 is real; I fix it.")),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if want := []string{"task", "delegate", "delegate", "subagent_report", "findings", "agent_text"}; !reflect.DeepEqual(kinds(d), want) {
		t.Fatalf("kinds: %v, want %v\n%s", kinds(d), want, stdout)
	}
	if got, _ := json.Marshal(d.Events[4]["findings"]); !strings.Contains(string(got), `"F1"`) || d.Events[4]["uuid"] != "u-report" {
		t.Errorf("findings: %v", d.Events[4])
	}
}

func TestReviewDigestTakesAFindingsBlockFromAHandBack(t *testing.T) {
	handBack := userLine("u-back", "Another Claude session sent a message:\n<agent-message from=\"a1\">\nPart 1 is built.\n```solidtrace-findings\n{\"version\": 2, \"findings\": []}\n```\n</agent-message>")
	handBack["isMeta"] = true
	path := transcript(t, userLine("u-task", "Build the parser in parts."), handBack)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if d := decodeDigest(t, stdout); !reflect.DeepEqual(kinds(d), []string{"task", "subagent_report", "findings"}) {
		t.Fatalf("kinds: %v\n%s", kinds(d), stdout)
	}
}

func TestReviewDigestTakesAFindingsBlockWithNoReviewStartAsAReview(t *testing.T) {
	path := transcript(t,
		userLine("u-task", "Add a retry to the uploader."),
		assistantLine("a-early", text("The retry is in.")),
		userLine("u-pr", "<command-name>/solidtrace-skills:open-pr</command-name>"),
		assistantLine("a-deep", text("F1 breaks the retry.\n```solidtrace-findings\n{\"version\": 2, \"skill\": \"deep-review\", \"findings\": []}\n```")),
		assistantLine("a-after", text("I fix F1.")),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if want := []string{"task", "person", "agent_text", "findings", "agent_text"}; !reflect.DeepEqual(kinds(d), want) {
		t.Fatalf("kinds: %v, want %v\n%s", kinds(d), want, stdout)
	}
	// reviews counts review starts, and a findings block alone is not one.
	if d.Reviews != 0 {
		t.Errorf("reviews: %d, want 0", d.Reviews)
	}
}

func TestReviewDigestKeepsTheReviewFindingsTrailersOfACommit(t *testing.T) {
	msg := "git commit -m \"Fix the parser\n\n" + strings.Repeat("A long line about the fix. ", 20) +
		"\n\nReview-Findings: run 1: F2, F5\nReview-Findings: run 2: F1\nCo-Authored-By: x\" -- parser.go"
	path := transcript(t, userLine("u-task", "Build the parser."), assistantLine("a-commit", toolUse("Bash", map[string]any{"command": msg})))

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if !reflect.DeepEqual(kinds(d), []string{"task", "commit"}) ||
		!reflect.DeepEqual(d.Events[1]["review_findings"], []any{"run 1: F2, F5", "run 2: F1"}) {
		t.Fatalf("digest:\n%s", stdout)
	}
}

func TestReviewDigestTakesOnlyACommandThatRunsGitCommitAsACommit(t *testing.T) {
	path := transcript(t, userLine("u-task", "Build the parser."),
		assistantLine("a-1", toolUse("Bash", map[string]any{"command": "cat > notes.md <<'EOF'\nRun `git commit` with explicit paths.\nEOF"})),
		assistantLine("a-2", toolUse("Bash", map[string]any{"command": "cd cli && go test ./... && git -C .. commit -q -m x -- a.go"})),
		assistantLine("a-3", toolUse("Bash", map[string]any{"command": "git merge --no-ff -m y feature"})))

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if !reflect.DeepEqual(kinds(d), []string{"task", "commit", "commit"}) || d.Events[1]["uuid"] != "a-2" {
		t.Fatalf("digest:\n%s", stdout)
	}
}

func TestReviewDigestSkipsACompactSummary(t *testing.T) {
	summary := userLine("u-summary", "This session is being continued from a previous conversation.\n"+
		"The hand-back is an <agent-message line. The block looks like this:\n```solidtrace-findings\n{\"version\": 1, \"findings\": [{\"id\": \"F1\"}]}\n```")
	summary["isCompactSummary"] = true
	path := transcript(t, userLine("u-task", "Build the parser."), summary, assistantLine("a-next", text("I go on with the parser.")))

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if d := decodeDigest(t, stdout); !reflect.DeepEqual(kinds(d), []string{"task"}) {
		t.Fatalf("kinds: %v\n%s", kinds(d), stdout)
	}
}

// in sets the session and the time of a transcript line.
func in(l map[string]any, session, at string) map[string]any {
	l["sessionId"], l["timestamp"] = session, at
	return l
}

func TestReviewDigestOfTwoSessionsPutsTheirEventsInTimeOrder(t *testing.T) {
	plan := transcript(t,
		in(userLine("a1", "Plan the parser."), "s-a", "2026-11-03T08:00:00Z"),
		in(assistantLine("a2", text("A plan."),
			toolUse("Edit", map[string]any{"file_path": "/work/plan.md"}),
			toolUse("Bash", map[string]any{"command": "git commit -m 'Plan the parser'"})), "s-a", "2026-11-03T08:01:00Z"),
		in(userLine("a3", "Ship it."), "s-a", "2026-11-03T08:06:00.000Z"),
	)
	title := map[string]any{"type": "ai-title", "aiTitle": "Parser review", "sessionId": "s-b"}
	build := transcript(t,
		title,
		in(userLine("b1", "Continue the parser."), "s-b", "2026-11-03T08:02:00Z"),
		in(assistantLine("b2", toolUse("Skill", map[string]any{"skill": "code-review"})), "s-b", "2026-11-03T08:03:00Z"),
		in(assistantLine("b3", text("One finding.")), "s-b", "2026-11-03T08:04:00.5Z"),
		in(assistantLine("b4", toolUse("Write", map[string]any{"file_path": "/work/fix.go"})), "s-b", "2026-11-03T08:06:00Z"),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", plan, "--file", build)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if len(d.Sessions) != 2 || d.Sessions[0].ID != "s-a" || d.Sessions[0].Title != "Plan the parser." ||
		d.Sessions[1].ID != "s-b" || d.Sessions[1].Title != "Parser review" || d.Reviews != 1 {
		t.Fatalf("digest:\n%s", stdout)
	}
	type step struct{ seq, session, uuid, kind string }
	var got []step
	for _, e := range d.Events {
		got = append(got, step{fmt.Sprint(e["seq"]), e["session"].(string), e["uuid"].(string), e["kind"].(string)})
	}
	want := []step{
		{"0", "s-a", "a1", "task"},
		{"1", "s-a", "a2", "commit"},
		{"2", "s-b", "b1", "task"},
		{"3", "s-b", "b2", "review_start"},
		{"4", "s-b", "b3", "agent_text"},
		{"5", "s-a", "a3", "person"},
		{"6", "s-b", "b4", "edit"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events: %v\nwant %v", got, want)
	}
}
