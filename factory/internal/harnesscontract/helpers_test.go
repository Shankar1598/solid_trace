package harnesscontract_test

// Tests of the helpers of the contract checks. They run in the usual
// suites, with no Claude Code.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/solidtrace/factory/internal/harnesscontract"
)

func events(t *testing.T, lines ...string) []harnesscontract.Event {
	t.Helper()
	var out []harnesscontract.Event
	for _, l := range lines {
		var e map[string]any
		must(t, json.Unmarshal([]byte(l), &e))
		out = append(out, harnesscontract.Event{Data: e})
	}
	return out
}

func objects(t *testing.T, lines ...string) []map[string]any {
	var out []map[string]any
	for _, ev := range events(t, lines...) {
		out = append(out, ev.Data)
	}
	return out
}

func TestReplyProblems(t *testing.T) {
	stream := events(t,
		`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"thinking"}]}}`,
		`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"tool_use"}]}}`)
	same := objects(t,
		`{"type":"assistant","message":{"id":"msg_1","usage":{"output_tokens":40}}}`,
		`{"type":"assistant","message":{"id":"msg_1","usage":{"output_tokens":40}}}`)
	if problems, several := replyProblems(stream, same); len(problems) > 0 || !several {
		t.Errorf("one reply of two lines with the same usage: problems %v, several %v; want none, true", problems, several)
	}

	different := objects(t,
		`{"type":"assistant","message":{"id":"msg_1","usage":{"output_tokens":1}}}`,
		`{"type":"assistant","message":{"id":"msg_1","usage":{"output_tokens":40}}}`)
	if problems, _ := replyProblems(stream, different); len(problems) != 1 || !strings.Contains(problems[0], "different usage") {
		t.Errorf("two lines of one reply with different usage: problems %v; want one", problems)
	}

	missing := objects(t, `{"type":"assistant","message":{"id":"msg_2","usage":{}}}`)
	if problems, several := replyProblems(stream, missing); len(problems) != 1 || several {
		t.Errorf("a reply of the stream not in the .jsonl: problems %v, several %v; want one, false", problems, several)
	}
}

func TestAnsweredLooksAfterTheEcho(t *testing.T) {
	evs := events(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"APPLE"}]}}`,
		`{"type":"user","isReplay":true,"message":{"content":"Also reply with the word BANANA."}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"APPLE"}]}}`,
		`{"type":"result","result":"APPLE"}`)
	if answered(evs, 1, "BANANA") {
		t.Errorf("the echo alone counts as an answer")
	}
	evs[3].Data["result"] = "APPLE BANANA"
	if !answered(evs, 1, "BANANA") {
		t.Errorf("the result after the echo does not count as an answer")
	}
	if answered(evs, -1, "BANANA") {
		t.Errorf("an answer with no echo")
	}
}

func TestRunningTasks(t *testing.T) {
	evs := events(t,
		`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash"}]}`,
		`{"type":"system","subtype":"task_notification","task_id":"b1"}`,
		`{"type":"system","subtype":"background_tasks_changed","tasks":[]}`)
	if !runningTasks(evs[:1], "b1", 1) || runningTasks(evs[:1], "b2", 1) || runningTasks(evs[:1], "", 0) {
		t.Errorf("one running task b1: wrong count")
	}
	if !runningTasks(evs[1:], "", 0) {
		t.Errorf("no running task after the notification: not found")
	}
	evs[2].Data["tasks"] = nil
	if runningTasks(evs[1:], "", 0) {
		t.Errorf("an event with no tasks list counts as zero running tasks")
	}
}

func TestSessionFreeKeepsTheLogin(t *testing.T) {
	got := sessionFree([]string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_PID=7", "CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDE_CODE_SESSION_ID=s", "CLAUDE_CODE_SSE_PORT=1", "CLAUDE_CONFIG_DIR=/c",
		"CLAUDE_CODE_OAUTH_TOKEN=tok", "CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_API_KEY=k"})
	want := []string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=tok", "CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_API_KEY=k"}
	if !slices.Equal(got, want) {
		t.Errorf("sessionFree gives %v; want %v", got, want)
	}
}

func TestProcessCostIsTheLastTotal(t *testing.T) {
	evs := events(t, `{"type":"result","total_cost_usd":0.01}`, `{"type":"assistant"}`, `{"type":"result","total_cost_usd":0.03}`)
	if got := processCost(evs); got != 0.03 {
		t.Errorf("cost %v; want 0.03", got)
	}
}

func TestRemoveMadeGlobalKeepsOtherBackups(t *testing.T) {
	config := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(config, "backups"), 0o700))
	ours := `{"firstStartTime":"2026-11-15T01:40:08.137Z"}`
	theirs := `{"firstStartTime":"2026-09-03T19:06:57.243Z","oauthAccount":{}}`
	old := filepath.Join(config, "backups", ".claude.json.backup.1")
	other := filepath.Join(config, "backups", ".claude.json.backup.3")
	must(t, os.WriteFile(old, []byte(ours), 0o600))
	defer func(m string, b map[string]bool) { madeGlobal, oldBackups = m, b }(madeGlobal, oldBackups)
	madeGlobal = filepath.Join(config, ".claude.json")
	oldBackups = map[string]bool{old: true}

	made := []string{madeGlobal, filepath.Join(config, "backups", ".claude.json.backup.2"), madeGlobal + ".backup"}
	for _, f := range made {
		must(t, os.WriteFile(f, []byte(ours), 0o600))
	}
	// A backup of the person's own settings, written during the run.
	must(t, os.WriteFile(other, []byte(theirs), 0o600))
	removeMadeGlobal()
	for _, f := range made {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s is still there", f)
		}
	}
	for _, f := range []string{old, other} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("a backup that the run did not make is gone: %v", err)
		}
	}
}
