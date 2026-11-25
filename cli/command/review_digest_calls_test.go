package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/solidtrace/cli/command"
)

const (
	callRunBlock    = `{"version":2,"run":1,"skill":"code-review","findings":[{"id":"F1"}]}`
	callUpdateBlock = `{"version":2,"update":1,"findings":[{"id":"F1","events":[{"type":"fix"}]}]}`
)

func fenced(block string) string { return "```solidtrace-findings\n" + block + "\n```\n" }

func bash(id, command string) map[string]any {
	return withID(toolUse("Bash", map[string]any{"command": command}), id)
}

// step is one event of a digest: its uuid, its kind and its writer.
type step struct{ uuid, kind, writer string }

func steps(d digest) []step {
	var out []step
	for _, e := range d.Events {
		writer, _ := e["writer"].(string)
		out = append(out, step{e["uuid"].(string), e["kind"].(string), writer})
	}
	return out
}

func TestReviewDigestTakesTheBlocksThatTheReviewCommandsPrinted(t *testing.T) {
	path := transcript(t,
		userLine("u-task", "Build the parser."),
		assistantLine("a-block", bash("t-block", "solidtrace review block <<'EOF'\n"+callRunBlock+"\nEOF")),
		userLine("u-block", []map[string]any{toolResult("t-block", fenced(callRunBlock), false)}),
		assistantLine("a-text", text("The review is recorded.")),
		assistantLine("a-fail", bash("t-fail", "/home/x/bin/solidtrace review fixed")),
		userLine("u-fail", []map[string]any{toolResult("t-fail", "Exit code 1\n"+fenced(callUpdateBlock), true)}),
		assistantLine("a-fixed", bash("t-fixed", "git commit -q -m x && ~/go/bin/solidtrace review fixed HEAD")),
		userLine("u-fixed", []map[string]any{toolResult("t-fixed", []map[string]any{text(fenced(callUpdateBlock))}, false)}),
		assistantLine("a-echo", bash("t-echo", "echo 'solidtrace review block'")),
		userLine("u-echo", []map[string]any{toolResult("t-echo", fenced(callRunBlock), false)}),
		assistantLine("a-copy", text("Recorded:\n"+fenced(callRunBlock))),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	want := []step{
		{"u-task", "task", ""},
		{"u-block", "findings", ""},
		{"a-text", "agent_text", ""},
		{"a-fixed", "commit", ""},
		{"u-fixed", "findings", ""},
		{"a-copy", "agent_text", ""},
	}
	if got := steps(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("events: %v\nwant %v\n%s", got, want, stdout)
	}
	var block any
	json.Unmarshal([]byte(callRunBlock), &block)
	if !reflect.DeepEqual(d.Events[1]["findings"], block) {
		t.Errorf("block: %v", d.Events[1]["findings"])
	}
	if _, ok := d.Events[1]["writer"]; ok {
		t.Errorf("a block of the main transcript has a writer: %v", d.Events[1])
	}
}

// subagentLine makes a line of a subagent's transcript.
func subagentLine(l map[string]any, agentID, at string) map[string]any {
	l = in(l, "s-1", at)
	l["isSidechain"] = true
	if agentID != "" {
		l["agentId"] = agentID
	}
	return l
}

func writeLines(t *testing.T, path string, lines ...map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range lines {
		data, _ := json.Marshal(l)
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReviewDigestOfASessionTakesTheBlocksOfItsSubagents(t *testing.T) {
	projects := t.TempDir()
	folder := filepath.Join(projects, "-work")
	otherBlock := `{"version":2,"run":1,"skill":"code-review","findings":[{"id":"F2"}]}`
	writeLines(t, filepath.Join(folder, "s-1.jsonl"),
		in(userLine("m-task", "Build the parser in parts."), "s-1", "2026-11-03T08:00:00Z"),
		in(assistantLine("m-part", withID(toolUse("Agent", map[string]any{"description": "Part 1", "prompt": "Build and review it."}), "t-a1")), "s-1", "2026-11-03T08:01:00Z"),
		in(assistantLine("m-wait", text("I wait for part 1.")), "s-1", "2026-11-03T08:02:00Z"),
		in(userLine("m-report", []map[string]any{toolResult("t-a1", "Part 1 is built.\n"+fenced(callRunBlock), false)}), "s-1", "2026-11-03T08:06:00Z"),
		in(assistantLine("m-after", text("Both reviews are in.")), "s-1", "2026-11-03T08:07:00Z"),
	)
	writeLines(t, filepath.Join(folder, "s-1", "subagents", "agent-a1.jsonl"),
		subagentLine(userLine("x-prompt", "Build and review it."), "a1", "2026-11-03T08:01:00Z"),
		subagentLine(assistantLine("x-call", bash("t-x", "solidtrace review block <<'EOF'\n"+callRunBlock+"\nEOF")), "a1", "2026-11-03T08:03:00Z"),
		subagentLine(userLine("x-out", []map[string]any{toolResult("t-x", fenced(callRunBlock), false)}), "a1", "2026-11-03T08:03:30Z"),
		subagentLine(assistantLine("x-text", text("My block:\n"+fenced(otherBlock))), "a1", "2026-11-03T08:04:00Z"),
		subagentLine(assistantLine("x-edit", toolUse("Edit", map[string]any{"file_path": "/work/a.go"})), "a1", "2026-11-03T08:04:10Z"),
	)
	writeLines(t, filepath.Join(folder, "s-1", "subagents", "agent-b2.jsonl"),
		subagentLine(assistantLine("y-call", bash("t-y", "solidtrace review block < block.json")), "", "2026-11-03T08:04:00Z"),
		subagentLine(userLine("y-out", []map[string]any{toolResult("t-y", fenced(otherBlock), false)}), "", "2026-11-03T08:05:00Z"),
	)

	code, stdout, stderr := runDigest(t, command.Env{ClaudeProjectsDir: projects}, "--session", "s-1")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	// m-wait is from before the first review, and m-report's block is a copy
	// of a1's block.
	want := []step{
		{"m-task", "task", ""},
		{"m-part", "delegate", ""},
		{"x-out", "findings", "a1"},
		{"y-out", "findings", "b2"},
		{"m-report", "subagent_report", ""},
		{"m-after", "agent_text", ""},
	}
	if got := steps(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("events: %v\nwant %v\n%s", got, want, stdout)
	}
	for i, e := range d.Events {
		if e["session"] != "s-1" || e["seq"] != float64(i) {
			t.Errorf("event %d: %v", i, e)
		}
	}

	// --file reads only the main transcript.
	code, stdout, _ = runDigest(t, command.Env{}, "--file", filepath.Join(folder, "s-1.jsonl"))
	if got := steps(decodeDigest(t, stdout)); code != 0 || len(got) != 5 || got[3] != (step{"m-report", "findings", ""}) {
		t.Fatalf("--file: %v", got)
	}
}

// persisted is the tool result of an output that is too long for Claude
// Code: only its start is in the transcript.
func persisted(start string) string {
	return "<persisted-output>\nOutput too large (31.2KB). Full output saved to: /home/x/tool-results/b1.txt\n\nPreview (first 2KB):\n" +
		start + "\n...\n</persisted-output>"
}

func TestReviewDigestTakesTheBlockOfAReviewBlockCallFromItsStdin(t *testing.T) {
	big := `{"version":2,"run":2,"skill":"code-review","findings":[{"id":"F1","title":"` + strings.Repeat("x", 40) + `"}]}`
	path := transcript(t,
		userLine("u-task", "Build the parser."),
		assistantLine("a-big", bash("t-big", "solidtrace review block <<'EOF'\n"+big+"\nEOF")),
		userLine("u-big", []map[string]any{toolResult("t-big", persisted("```solidtrace-findings\n"+big[:30]), false)}),
		assistantLine("a-string", bash("t-string", "FOO=1 solidtrace review block <<< '"+callUpdateBlock+"'")),
		userLine("u-string", []map[string]any{toolResult("t-string", "", false)}),
		assistantLine("a-file", bash("t-file", "solidtrace review block < block.json")),
		userLine("u-file", []map[string]any{toolResult("t-file", persisted("```solidtrace-findings\n{\"version\":2"), false)}),
		assistantLine("a-none", bash("t-none", "solidtrace review block < block.json")),
		userLine("u-none", []map[string]any{toolResult("t-none", "", false)}),
	)

	code, stdout, stderr := runDigest(t, command.Env{}, "--file", path)

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	want := []step{{"u-task", "task", ""}, {"u-big", "findings", ""}, {"u-string", "findings", ""}, {"u-file", "findings", ""}, {"u-none", "findings", ""}}
	if got := steps(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("events: %v\nwant %v\n%s", got, want, stdout)
	}
	for i, block := range map[int]string{1: big, 2: callUpdateBlock} {
		var v any
		json.Unmarshal([]byte(block), &v)
		if !reflect.DeepEqual(d.Events[i]["findings"], v) || d.Events[i]["error"] != nil {
			t.Errorf("event %d: %v", i, d.Events[i])
		}
	}
	// With no stdin, the block is in the output, and a cut or missing block
	// is an error that the record and the check report.
	for i, want := range map[int]string{
		3: "the solidtrace-findings block has no closing ```",
		4: "the output of the solidtrace review command has no solidtrace-findings block",
	} {
		if e := d.Events[i]; e["findings"] != nil || e["error"] != want {
			t.Errorf("event %d: %v", i, e)
		}
	}
}

func TestReviewDigestTakesOnlyTheBlockCallsOfASubagentThatDidNotFail(t *testing.T) {
	projects := t.TempDir()
	folder := filepath.Join(projects, "-work")
	big := `{"version":2,"run":1,"skill":"code-review","findings":[{"id":"F3"}]}`
	writeLines(t, filepath.Join(folder, "s-1.jsonl"),
		in(userLine("m-task", "Build the parser."), "s-1", "2026-11-03T08:00:00Z"),
	)
	writeLines(t, filepath.Join(folder, "s-1", "subagents", "agent-a1.jsonl"),
		subagentLine(assistantLine("x-cat", bash("t-cat", "cat notes.md")), "a1", "2026-11-03T08:01:00Z"),
		subagentLine(userLine("x-cat-out", []map[string]any{toolResult("t-cat", fenced(callRunBlock), false)}), "a1", "2026-11-03T08:01:10Z"),
		subagentLine(assistantLine("x-fail", bash("t-fail", "solidtrace review block <<'EOF'\n"+callRunBlock+"\nEOF")), "a1", "2026-11-03T08:02:00Z"),
		subagentLine(userLine("x-fail-out", []map[string]any{toolResult("t-fail", "Exit code 1\n"+fenced(callRunBlock), true)}), "a1", "2026-11-03T08:02:10Z"),
		subagentLine(assistantLine("x-big", bash("t-big", "solidtrace review block <<'EOF'\n"+big+"\nEOF")), "a1", "2026-11-03T08:03:00Z"),
		subagentLine(userLine("x-big-out", []map[string]any{toolResult("t-big", persisted("```solidtrace-findings\n{\"ver"), false)}), "a1", "2026-11-03T08:03:10Z"),
	)

	code, stdout, stderr := runDigest(t, command.Env{ClaudeProjectsDir: projects}, "--session", "s-1")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	d := decodeDigest(t, stdout)
	if got, want := steps(d), []step{{"m-task", "task", ""}, {"x-big-out", "findings", "a1"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events: %v\nwant %v\n%s", got, want, stdout)
	}
	var v any
	json.Unmarshal([]byte(big), &v)
	if !reflect.DeepEqual(d.Events[1]["findings"], v) {
		t.Errorf("block: %v", d.Events[1])
	}
}

func TestReviewDigestStartsTheReviewAtTheEarliestFirstBlockOfASubagent(t *testing.T) {
	projects := t.TempDir()
	folder := filepath.Join(projects, "-work")
	writeLines(t, filepath.Join(folder, "s-1.jsonl"),
		in(userLine("m-task", "Build the parser in parts."), "s-1", "2026-11-03T08:00:00Z"),
		in(assistantLine("m-before", text("I start the parts.")), "s-1", "2026-11-03T08:01:00Z"),
		in(assistantLine("m-between", text("Part 2 is in review.")), "s-1", "2026-11-03T08:04:00Z"),
		in(assistantLine("m-review", toolUse("Skill", map[string]any{"skill": "code-review"})), "s-1", "2026-11-03T08:06:00Z"),
	)
	// The files are read in name order: a1's first block is the later one.
	writeLines(t, filepath.Join(folder, "s-1", "subagents", "agent-a1.jsonl"),
		subagentLine(assistantLine("x-call", bash("t-x", "solidtrace review block <<'EOF'\n"+callRunBlock+"\nEOF")), "a1", "2026-11-03T08:05:00Z"),
		subagentLine(userLine("x-out", []map[string]any{toolResult("t-x", fenced(callRunBlock), false)}), "a1", "2026-11-03T08:05:00Z"),
	)
	writeLines(t, filepath.Join(folder, "s-1", "subagents", "agent-b2.jsonl"),
		subagentLine(assistantLine("y-call", bash("t-y", "solidtrace review block <<'EOF'\n"+callUpdateBlock+"\nEOF")), "b2", "2026-11-03T08:03:00Z"),
		subagentLine(userLine("y-out", []map[string]any{toolResult("t-y", fenced(callUpdateBlock), false)}), "b2", "2026-11-03T08:03:00Z"),
	)

	code, stdout, stderr := runDigest(t, command.Env{ClaudeProjectsDir: projects}, "--session", "s-1")

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	want := []step{
		{"m-task", "task", ""},
		{"y-out", "findings", "b2"},
		{"m-between", "agent_text", ""},
		{"x-out", "findings", "a1"},
		{"m-review", "review_start", ""},
	}
	if got := steps(decodeDigest(t, stdout)); !reflect.DeepEqual(got, want) {
		t.Fatalf("events: %v\nwant %v\n%s", got, want, stdout)
	}
}
