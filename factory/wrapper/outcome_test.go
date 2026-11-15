package wrapper_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

func TestNeedsAPersonWaitsOnAPersonWithTheQuestionAndEndsTheExecutionAtOnce(t *testing.T) {
	env, ana := newEnv(t)
	// After its report the agent goes on working: with no end at once, it
	// would wait on stdin here, and then report done.
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "needs-a-person", "--summary", "Two fixes are possible.",
			"--question", "Keep the old cart API?"),
		`{"fake":"await_message","eof_code":0}`,
		reportStep("--outcome", "done", "--summary", "Did not wait"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)

	if run.Question != "Keep the old cart API?" || run.Summary != "Two fixes are possible." {
		t.Errorf("question %q, summary %q", run.Question, run.Summary)
	}
	if e := onlyExecution(t, run); e.EndReason != dispatcherapi.EndReported {
		t.Errorf("end reason %q", e.EndReason)
	}
	got := ana.CLI("runs", "get", id)
	if !strings.Contains(got.Stdout, "waiting_on_person") || !strings.Contains(got.Stdout, "Keep the old cart API?") {
		t.Errorf("runs get:\n%s", got.Stdout)
	}
}

func TestAReportOfFailedFailsTheRunWithTheReason(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "failed", "--summary", "The cart service does not build on main"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	if run.Reason != "The cart service does not build on main" {
		t.Errorf("reason %q", run.Reason)
	}
}

func TestTheLastValidReportIsTheOutcome(t *testing.T) {
	env, ana := newEnv(t)
	// A report that is not valid is refused and does not count; a later
	// report replaces an earlier one.
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "failed", "--summary", "First try failed"),
		reportStep("--outcome", "needs-a-person", "--summary", "no question"),
		reportStep("--outcome", "done", "--summary", "Fixed on the second try"),
		reportStep("--outcome", "done"),
		reportStep("--outcome", "done", "--summary", "Done with a question", "--question", "Is this right?"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if run.Summary != "Fixed on the second try" {
		t.Errorf("summary %q", run.Summary)
	}
}

func TestTheOutcomeNeverComesFromTheExitCode(t *testing.T) {
	cases := []struct {
		name   string
		agent  func(t *testing.T) []string
		state  string
		reason string
		code   int
		end    string
	}{
		{"exit 0 with no report", func(t *testing.T) []string {
			return []string{takeTask, initLine(t), reply, `{"fake":"exit","code":0}`}
		}, dispatcherapi.StateFailed, "the harness exited", 0, dispatcherapi.EndHarnessExited},
		{"exit 0 in the turn after a needs-a-person refusal", func(t *testing.T) []string {
			return []string{takeTask, initLine(t), reportStep("--outcome", "needs-a-person"), `{"fake":"exit","code":0}`}
		}, dispatcherapi.StateFailed, "the harness exited", 0, dispatcherapi.EndHarnessExited},
		{"exit 3 after a report of done", func(t *testing.T) []string {
			return []string{takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"),
				`{"fake":"exit","code":3}`}
		}, dispatcherapi.StateDone, "", 3, dispatcherapi.EndHarnessExited},
		{"a turn that ends with no report", func(t *testing.T) []string {
			return []string{takeTask, initLine(t), reply, result, waitForEnd}
		}, dispatcherapi.StateFailed, "no report", 0, dispatcherapi.EndNoReport},
		{"a turn that ends after a report that is not valid", func(t *testing.T) []string {
			return []string{takeTask, initLine(t), reportStep("--outcome", "done"), reply, result, waitForEnd}
		}, dispatcherapi.StateFailed, "no report", 0, dispatcherapi.EndNoReport},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, ana := newEnv(t)
			env.Runner.Agent(c.agent(t)...)

			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, c.state)

			if run.Reason != c.reason {
				t.Errorf("reason %q, want %q", run.Reason, c.reason)
			}
			e := onlyExecution(t, run)
			if e.ExitCode == nil || *e.ExitCode != c.code {
				t.Errorf("the end has exit code %v, want %d", e.ExitCode, c.code)
			}
			if e.EndReason != c.end {
				t.Errorf("the end reason is %q, want %q", e.EndReason, c.end)
			}
		})
	}
}

func TestAFailedExecutionIsNotStartedAgain(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), `{"fake":"exit","code":1}`)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	// A start of a new Run, and a restart of the Dispatcher, each look for
	// queued Runs.
	other := start(t, env, ana, "Fix the search")
	waitFor(t, env, ana, other, dispatcherapi.StateFailed)
	env.Restart()
	time.Sleep(300 * time.Millisecond)

	run := getRun(t, ana, id)
	if run.State != dispatcherapi.StateFailed || len(run.Executions) != 1 {
		t.Errorf("the failed Run is %s with %d Executions", run.State, len(run.Executions))
	}
	if got := len(env.Runner.Dispatched()); got != 2 {
		t.Errorf("the Runner started %d Executions, want 2 (one for each Run)", got)
	}
}

func TestAWrapperThatEndsWithNoEndCallFailsTheRun(t *testing.T) {
	env, ana := newEnv(t)
	// The agent reports done, then the machine goes before the wrapper's
	// end: the report alone must not make the Run done.
	wait, _ := gate(t)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), wait)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(env.Runner.Fake.Output(run.Executions[0].ID), "Reported done") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if err := env.Runner.Fake.LoseMachine(run.Executions[0].ID); err != nil {
		t.Fatal(err)
	}

	run = waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if run.Reason != "the machine was lost" {
		t.Errorf("reason %q", run.Reason)
	}
}

func TestTheReplayEchoWithTheWrappersUUIDIsTheProofOfDelivery(t *testing.T) {
	env, ana := newEnv(t)
	// The harness takes the task but gives no echo of it: only an echo
	// with a uuid that the wrapper did not send.
	env.Runner.Agent(`{"fake":"await_message","echo":false}`, initLine(t),
		`{"type":"user","isReplay":true,"uuid":"","session_id":"","parent_tool_use_id":null,"message":{"role":"user","content":"Fix the cart"}}`,
		reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if e := onlyExecution(t, run); e.TaskDeliveredAt != nil {
		t.Errorf("an echo with another uuid was taken as the delivery: %v", e.TaskDeliveredAt)
	}
}

func TestAskUserQuestionIsNotAvailableToTheAgent(t *testing.T) {
	env, ana := newEnv(t)
	// A harness that drops --disallowedTools from its command line offers
	// AskUserQuestion: the wrapper ends the Execution, and the agent's
	// report does not count.
	program := filepath.Join(t.TempDir(), "claude-with-all-tools")
	script := "#!/bin/sh\nfor a; do shift\n" +
		"  if [ -n \"$skip\" ]; then skip=; continue; fi\n" +
		"  if [ \"$a\" = --disallowedTools ]; then skip=1; continue; fi\n" +
		"  set -- \"$@\" \"$a\"\ndone\nexec " + env.Runner.Harness() + " \"$@\"\n"
	if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env.Runner.UseHarness(program)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	if !strings.Contains(run.Reason, "AskUserQuestion") {
		t.Errorf("reason %q", run.Reason)
	}
	if e := onlyExecution(t, run); !strings.HasPrefix(e.EndReason, dispatcherapi.EndSetupFailed+": ") {
		t.Errorf("end reason %q", e.EndReason)
	}
}

func TestACloneThatFailsFailsTheRunWithTheReason(t *testing.T) {
	env, ana := newEnv(t)
	// The connection's token is not the Forge's token.
	admin := env.PersonIn("Dana", factorytest.Organization, "admin")
	if a := admin.API("PUT", "/api/v1/connections/forge", map[string]string{"kind": "bitbucket",
		"token": "a-token-that-the-forge-refuses"}); a.Status/100 != 2 {
		t.Fatalf("save the connection: %d %s", a.Status, a.Body)
	}

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	if !strings.HasPrefix(run.Reason, "the setup failed: clone bitbucket.org/example/shop") {
		t.Errorf("reason %q", run.Reason)
	}
	if strings.Contains(run.Reason, "a-token-that-the-forge-refuses") {
		t.Errorf("the reason has the token: %q", run.Reason)
	}
	if e := onlyExecution(t, run); !strings.HasPrefix(e.EndReason, dispatcherapi.EndSetupFailed+": ") {
		t.Errorf("end reason %q", e.EndReason)
	}
}

func TestAHarnessThatExitsEndsTheExecutionAlthoughAProcessHoldsItsOutput(t *testing.T) {
	// The harness starts a process that holds the harness's stdout for
	// 60 s, and exits: in its process group, and in a session of its own,
	// out of the reach of the group kill.
	for name, prefix := range map[string]string{"in the group": "", "out of the group": "setsid "} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			dir := t.TempDir()
			pid := filepath.Join(dir, "pid")
			program := filepath.Join(dir, "claude-with-a-child")
			script := "#!/bin/sh\n" + prefix + "sh -c 'echo $$ >" + pid + "; exec sleep 60' </dev/null 2>/dev/null &\n" +
				"while [ ! -s " + pid + " ]; do sleep 0.01; done\nexec " + env.Runner.Harness() + " \"$@\"\n"
			if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if b, err := os.ReadFile(pid); err == nil {
					exec.Command("kill", "-9", strings.TrimSpace(string(b))).Run()
				}
			})
			env.Runner.UseHarness(program)
			env.Runner.Agent(takeTask, initLine(t), reply, `{"fake":"exit","code":1}`)

			began := time.Now()
			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

			if run.Reason != "the harness exited" {
				t.Errorf("reason %q", run.Reason)
			}
			if e := onlyExecution(t, run); e.ExitCode == nil || *e.ExitCode != 1 || e.EndReason != dispatcherapi.EndHarnessExited {
				t.Errorf("the end has exit code %v and reason %q, want 1 and %s", e.ExitCode, e.EndReason,
					dispatcherapi.EndHarnessExited)
			}
			if took := time.Since(began); took > 20*time.Second {
				t.Errorf("the end took %s", took)
			}
		})
	}
}

func TestTheDispatcherRefusesAReportThatIsNotValid(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	execution := waitFor(t, env, ana, id, dispatcherapi.StateRunning).Executions[0].ID
	identity := env.Runner.Identity(factorytest.Workload(execution))
	limit := strings.Repeat("a", 64<<10)

	cases := []struct {
		name   string
		report dispatcherapi.Report
		status int
	}{
		{"a report of done with a question",
			dispatcherapi.Report{Outcome: dispatcherapi.OutcomeDone, Summary: "x", Question: "q"}, http.StatusBadRequest},
		{"a report of failed with a question",
			dispatcherapi.Report{Outcome: dispatcherapi.OutcomeFailed, Summary: "x", Question: "q"}, http.StatusBadRequest},
		{"a summary of 64 KiB and 1 byte",
			dispatcherapi.Report{Outcome: dispatcherapi.OutcomeDone, Summary: limit + "a"}, http.StatusBadRequest},
		{"a question of 64 KiB and 1 byte",
			dispatcherapi.Report{Outcome: dispatcherapi.OutcomeNeedsAPerson, Summary: "x", Question: limit + "a"},
			http.StatusBadRequest},
		{"a summary and a question of 64 KiB",
			dispatcherapi.Report{Outcome: dispatcherapi.OutcomeNeedsAPerson, Summary: limit, Question: limit}, http.StatusOK},
		{"a summary of 64 KiB",
			dispatcherapi.Report{Outcome: dispatcherapi.OutcomeDone, Summary: limit}, http.StatusOK},
	}
	for _, c := range cases {
		got := env.API(identity, "POST", "/api/v1/executions/"+execution+"/report", c.report)
		if got.Status != c.status {
			t.Errorf("%s: %d %s, want %d", c.name, got.Status, got.Body, c.status)
		}
	}
	open()

	// The agent sends no report: the last valid report is the Dispatcher's
	// own report of 64 KiB.
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != limit {
		t.Errorf("the summary has %d bytes, want %d", len(run.Summary), len(limit))
	}
}
