package command_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	Session string           `json:"session"`
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

func TestReviewDigestOfASessionWithNoReviewHasOnlyTheTask(t *testing.T) {
	code, stdout, stderr := runDigest(t, command.Env{}, "--file", filepath.Join("testdata", "review", "no-review.jsonl"))

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if d.Session != "s-none" || d.Reviews != 0 || len(d.Events) != 1 ||
		d.Events[0]["kind"] != "task" || d.Events[0]["text"] != "Rename the flag." || d.Events[0]["uuid"] != "n1" {
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
	if !reflect.DeepEqual(kinds(d), []string{"task", "review_start", "review_report"}) || d.Reviews != 1 {
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
	if !reflect.DeepEqual(kinds(d), []string{"task", "review_start", "review_report"}) || d.Reviews != 1 ||
		d.Events[1]["skill"] != "acme-review" || d.Events[1]["trigger"] != "agent" || d.Events[1]["args"] != "the parser" {
		t.Fatalf("digest:\n%s", stdout)
	}
}

func TestReviewDigestCutsLongTextsAndKeepsMoreOfAReport(t *testing.T) {
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
	if !reflect.DeepEqual(kinds(d), []string{"task", "review_start", "review_report", "person", "commit", "review_start", "review_report"}) {
		t.Fatalf("kinds: %v", kinds(d))
	}
	for i, want := range map[int]string{
		0: long(4000) + "…",
		2: long(10000),
		3: long(4000),
		6: long(12000) + "…",
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
		{"--session", "s-1", "--file", "x.jsonl"},
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
