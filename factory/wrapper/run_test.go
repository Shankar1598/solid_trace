package wrapper_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakeharness"
)

// The test binary is also the wrapper, the harness and the agent's
// solidtrace CLI (see factorytest.Executions).
func TestMain(m *testing.M) {
	factorytest.MainIfAsked()
	os.Exit(m.Run())
}

// Lines of the agent's scripts (fakeharness streams).
const (
	// takeTask waits for the wrapper's task and gives its replay echo.
	takeTask = `{"fake":"await_message"}`
	// waitForEnd waits between turns until the wrapper closes stdin.
	waitForEnd = `{"fake":"await_message"}`
	reply      = `{"type":"assistant","message":{"id":"msg_01Thin","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn"},"parent_tool_use_id":null,"session_id":"","uuid":""}`
	result     = `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"Done.","stop_reason":"end_turn","session_id":"","uuid":"","total_cost_usd":0}`
)

// initLine is the recorded init event, with AskUserQuestion among the
// tools, as Claude Code offers it with no --disallowedTools.
func initLine(t *testing.T) string {
	t.Helper()
	stream, err := fakeharness.Stream("turn-with-tool-call")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(stream), "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) == nil && ev["type"] == "system" && ev["subtype"] == "init" {
			ev["tools"] = append(ev["tools"].([]any), "AskUserQuestion")
			b, _ := json.Marshal(ev)
			return string(b)
		}
	}
	t.Fatal("the recorded stream has no init event")
	return ""
}

// shell is a step that runs a shell command, as the agent's Bash tool
// does.
func shell(command string) string {
	b, _ := json.Marshal(map[string]any{"fake": "run", "args": []string{"sh", "-c", command}})
	return string(b)
}

// reportStep is a step that runs 'solidtrace report' with args.
func reportStep(args ...string) string {
	b, _ := json.Marshal(map[string]any{"fake": "run", "args": append([]string{"solidtrace", "report"}, args...)})
	return string(b)
}

// gate is a step that waits until the test opens the gate.
func gate(t *testing.T) (step string, open func()) {
	path := filepath.Join(t.TempDir(), "open")
	return shell("while [ ! -e " + path + " ]; do sleep 0.02; done"), func() {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func newEnv(t *testing.T) (*factorytest.Env, *factorytest.Person) {
	t.Helper()
	env := factorytest.New(t, factorytest.Executions)
	env.Project("shop")
	return env, env.Member("Ana")
}

var queuedLine = regexp.MustCompile(`(?m)^Run (\S+) is queued\.$`)

// start runs 'solidtrace start' for the Project shop, and gives the Run id.
func start(t *testing.T, env *factorytest.Env, p *factorytest.Person, prompt string) string {
	t.Helper()
	got := p.CLI("start", "--dispatcher", env.URL(), "--project", "shop", prompt)
	m := queuedLine.FindStringSubmatch(got.Stdout)
	if got.Code != 0 || m == nil {
		t.Fatalf("solidtrace start: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	return m[1]
}

func getRun(t *testing.T, p *factorytest.Person, id string) dispatcherapi.Run {
	t.Helper()
	a := p.API("GET", "/api/v1/runs/"+id, nil)
	if a.Status != http.StatusOK {
		t.Fatalf("get the Run: %d %s", a.Status, a.Body)
	}
	var run dispatcherapi.Run
	a.JSON(t, &run)
	return run
}

// waitFor waits until the Run is in state and each of its Executions has
// ended, or the Run's state is want and ended is false.
func waitFor(t *testing.T, env *factorytest.Env, p *factorytest.Person, id, state string) dispatcherapi.Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		run := getRun(t, p, id)
		settled := run.State == state
		if state != dispatcherapi.StateRunning {
			for _, e := range run.Executions {
				settled = settled && e.State == dispatcherapi.ExecutionEnded
			}
		}
		if settled {
			return run
		}
		if time.Now().After(deadline) {
			var out string
			for _, e := range env.Runner.Dispatched() {
				out += env.Runner.Fake.Output(e)
			}
			t.Fatalf("the Run is %s (%s), not %s, after 30 s: %+v\nwrapper output:\n%s\nDispatcher log:\n%s",
				run.State, run.Reason, state, run.Executions, out, env.Logs())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func onlyExecution(t *testing.T, run dispatcherapi.Run) dispatcherapi.Execution {
	t.Helper()
	if len(run.Executions) != 1 {
		t.Fatalf("want one Execution, got %+v", run.Executions)
	}
	return run.Executions[0]
}

func TestARunIsRunningWhenItsExecutionStartsAndDoneWhenTheAgentReportsDone(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	// The agent checks that it works on the Run's branch, from the base branch, in a clone whose
	// remote URL holds no token, then reports.
	env.Runner.Agent(takeTask, initLine(t), wait,
		shell(`test -f README.md && git rev-parse --abbrev-ref HEAD | grep -q "^agent/" && git merge-base --is-ancestor origin/main HEAD && `+
			`! git remote get-url origin | grep -q @ && `+
			`solidtrace report --outcome done --summary "Fixed the flaky cart test"`),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the flaky cart test")
	running := waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	if e := onlyExecution(t, running); e.State != dispatcherapi.ExecutionRunning || e.StartedAt == nil {
		t.Errorf("the Execution of a running Run: %+v", e)
	}
	open()

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Fixed the flaky cart test" || run.Reason != "" {
		t.Errorf("summary %q, reason %q", run.Summary, run.Reason)
	}
	e := onlyExecution(t, run)
	if e.EndReason != dispatcherapi.EndReported || e.ExitCode == nil || *e.ExitCode != 0 {
		t.Errorf("the end: reason %q, exit code %v", e.EndReason, e.ExitCode)
	}
	if e.TaskDeliveredAt == nil {
		t.Errorf("the task's replay echo was not taken as its delivery")
	}
	got := ana.CLI("runs", "get", id)
	if !strings.Contains(got.Stdout, "done") || !strings.Contains(got.Stdout, "Fixed the flaky cart test") {
		t.Errorf("runs get:\n%s", got.Stdout)
	}
}
