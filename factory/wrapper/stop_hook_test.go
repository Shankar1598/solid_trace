package wrapper_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/wrapper"
)

// stopStep ends the agent's turn: the harness runs the wrapper's Stop
// hook, and after the n-th refusal it plays replies[n] (the last one again
// after more refusals).
func stopStep(replies ...[]string) string {
	var rs [][]json.RawMessage
	for _, r := range replies {
		var lines []json.RawMessage
		for _, l := range r {
			lines = append(lines, json.RawMessage(l))
		}
		rs = append(rs, lines)
	}
	b, _ := json.Marshal(map[string]any{"fake": "stop", "replies": rs})
	return string(b)
}

// feedback is the Stop hook feedback that the agent got, from the Run's
// transcript as SolidTrace stores it: one text for each refusal.
func feedback(t *testing.T, env *factorytest.Env) []string {
	t.Helper()
	session := firstToken(t, env).AgentSessionIDs[0]
	var texts []string
	for _, line := range strings.Split(string(env.SolidTrace.Stored(session, session+".jsonl")), "\n") {
		var l struct {
			IsMeta  bool `json:"isMeta"`
			Message struct {
				Content []struct{ Text string } `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &l) == nil && l.IsMeta && len(l.Message.Content) > 0 {
			texts = append(texts, strings.TrimPrefix(l.Message.Content[0].Text, "Stop hook feedback:\n"))
		}
	}
	return texts
}

// kindsOf names each refusal: "background tasks" or "no report".
func kindsOf(t *testing.T, texts []string) []string {
	t.Helper()
	var kinds []string
	for _, text := range texts {
		switch {
		case strings.Contains(text, "background task"):
			kinds = append(kinds, "background tasks")
		case strings.Contains(text, "solidtrace report"):
			kinds = append(kinds, "no report")
		default:
			t.Errorf("a refusal that is not one of the two checks: %q", text)
		}
	}
	return kinds
}

func TestNoReportIsRefusedTwoTimesAndThenTheRunFailsWithTheReasonNoReport(t *testing.T) {
	cases := []struct {
		name    string
		replies [][]string
		state   string
		reason  string
		refused []string
	}{
		{"the agent never reports", [][]string{{reply}},
			dispatcherapi.StateFailed, "no report", []string{"no report", "no report"}},
		{"the agent reports after the first refusal", [][]string{{reportStep("--outcome", "done", "--summary", "Fixed"), reply}},
			dispatcherapi.StateDone, "", []string{"no report"}},
		{"the agent reports after the second refusal", [][]string{{reply}, {reportStep("--outcome", "done", "--summary", "Fixed"), reply}},
			dispatcherapi.StateDone, "", []string{"no report", "no report"}},
		{"the agent reports after the third stop", [][]string{{reply}, {reply}, {reportStep("--outcome", "done", "--summary", "Late")}},
			dispatcherapi.StateFailed, "no report", []string{"no report", "no report"}},
		{"a report that the Dispatcher refuses is no report", [][]string{{reportStep("--outcome", "done"), reply}},
			dispatcherapi.StateFailed, "no report", []string{"no report", "no report"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, ana := newEnv(t)
			env.Runner.Agent(takeTask, initLine(t), reply, stopStep(c.replies...), result, waitForEnd)

			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, c.state)

			if run.Reason != c.reason {
				t.Errorf("reason %q, want %q", run.Reason, c.reason)
			}
			if got := kindsOf(t, feedback(t, env)); strings.Join(got, ", ") != strings.Join(c.refused, ", ") {
				t.Errorf("refusals %q, want %q", got, c.refused)
			}
			if c.state == dispatcherapi.StateFailed {
				if e := onlyExecution(t, run); e.EndReason != dispatcherapi.EndNoReport {
					t.Errorf("end reason %q", e.EndReason)
				}
			}
		})
	}
}

func TestAReportStopsAtOnceWithNoRefusal(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		stopStep(), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if got := feedback(t, env); len(got) != 0 {
		t.Errorf("refusals %q, want none", got)
	}
}

func TestBackgroundTasksThatStillRunRefuseOneTimeInATurnAndThatRefusalIsNotANoReportRefusal(t *testing.T) {
	report := reportStep("--outcome", "done", "--summary", "Fixed")
	cases := []struct {
		name    string
		before  []string
		replies [][]string
		state   string
		refused []string
	}{
		{"with a report: one refusal, then the stop", []string{backgroundTasks(2), report}, nil,
			dispatcherapi.StateDone, []string{"background tasks"}},
		{"with no report: one refusal for the tasks, then two for no report",
			[]string{backgroundTasks(1)}, [][]string{{reply}},
			dispatcherapi.StateFailed, []string{"background tasks", "no report", "no report"}},
		{"the agent reports after the refusal for the tasks", []string{backgroundTasks(1)}, [][]string{{report, reply}},
			dispatcherapi.StateDone, []string{"background tasks"}},
		{"the tasks end before the stop", []string{backgroundTasks(1), backgroundTasks(0), report}, nil,
			dispatcherapi.StateDone, nil},
		{"the tasks end after the refusal, and the agent reports late", []string{backgroundTasks(3)},
			[][]string{{backgroundTasks(0), reply}, {reply}, {report, reply}},
			dispatcherapi.StateDone, []string{"background tasks", "no report", "no report"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, ana := newEnv(t)
			lines := append(append([]string{takeTask, initLine(t)}, c.before...), reply, stopStep(c.replies...), result, waitForEnd)
			env.Runner.Agent(lines...)

			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, c.state)

			texts := feedback(t, env)
			if got := kindsOf(t, texts); strings.Join(got, ", ") != strings.Join(c.refused, ", ") {
				t.Errorf("refusals %q, want %q", got, c.refused)
			}
			if c.state == dispatcherapi.StateFailed && run.Reason != "no report" {
				t.Errorf("reason %q", run.Reason)
			}
		})
	}
}

func TestTheRefusalForBackgroundTasksNamesHowManyRun(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), backgroundTasks(3), reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, stopStep(), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	texts := feedback(t, env)
	if len(texts) != 1 || !strings.HasPrefix(texts[0], "3 background tasks still run.") {
		t.Errorf("refusals %q", texts)
	}
}

// A new turn can refuse again for background tasks: here a turn that the
// harness starts by itself after the first result, while the wrapper ends.
func TestANewTurnRefusesAgainForBackgroundTasks(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), backgroundTasks(1), reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, stopStep(), result,
		`{"fake":"await_eof"}`, initLine(t), reply, stopStep(), result)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if got := kindsOf(t, feedback(t, env)); strings.Join(got, ", ") != "background tasks, background tasks" {
		t.Errorf("refusals %q, want one for each turn", got)
	}
}

// A hook that has no run state allows the stop and refuses nothing: with
// no file, a refusal could not be counted, and so never end. The outcome
// still comes only from the report.
func TestTheStopHookAllowsTheStopWhenItHasNoRunState(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path string
		code       int
	}{
		{"no environment variable", "", 0},
		{"no file", filepath.Join(dir, "absent.json"), 1},
		{"a file that is not a run state", broken, 1},
		{"a folder", dir, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := wrapper.StopHook(strings.NewReader(`{"hook_event_name":"Stop"}`), &stdout, &stderr,
				func(name string) string {
					if name == wrapper.EnvRunState {
						return c.path
					}
					return ""
				})
			if code != c.code || stdout.Len() != 0 || !strings.Contains(stderr.String(), "allowed") {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit %d, no decision and the reason", code, stdout.String(),
					stderr.String(), c.code)
			}
		})
	}
	if b, _ := os.ReadFile(broken); string(b) != "{not json" {
		t.Errorf("the hook changed a file that is not a run state: %q", b)
	}
}

func TestTheRunStateFileIsOutsideTheCheckout(t *testing.T) {
	env, ana := newEnv(t)
	kept := filepath.Join(t.TempDir(), "state-path")
	env.Runner.Agent(takeTask, initLine(t),
		shell(`printf '%s\n%s\n' "$SOLIDTRACE_RUN_STATE" "$(pwd -P)" > `+kept),
		reportStep("--outcome", "done", "--summary", "Fixed"), reply, stopStep(), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	b, err := os.ReadFile(kept)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !filepath.IsAbs(lines[0]) {
		t.Fatalf("the agent got the state file path %q", b)
	}
	state, cwd := lines[0], lines[1]
	workspace, _ := filepath.EvalSymlinks(env.Runner.Workspace(onlyExecution(t, run).ID))
	for _, dir := range []string{cwd, workspace, env.Runner.Workspace(onlyExecution(t, run).ID)} {
		if dir != "" && (state == dir || strings.HasPrefix(state, dir+"/")) {
			t.Errorf("the state file %s is in %s", state, dir)
		}
	}
}
