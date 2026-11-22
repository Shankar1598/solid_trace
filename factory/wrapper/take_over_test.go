package wrapper_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/killboundary"
	"github.com/solidtrace/factory/internal/recorder"
	"github.com/solidtrace/factory/wrapper"
)

var takenOverLine = regexp.MustCompile(`(?m)^Run (\S+) is taken over\. Its Agent session continues in a terminal on (\S+)\.\nAttach with:\n  (.+)$`)

// takeOverCLI runs 'solidtrace take-over' as p, and gives the machine and
// the attach command that it printed.
func takeOverCLI(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string) (vm, attach string) {
	t.Helper()
	got := p.CLI("take-over", "--dispatcher", env.URL(), id)
	m := takenOverLine.FindStringSubmatch(got.Stdout)
	if got.Code != 0 || m == nil || m[1] != id {
		t.Fatalf("solidtrace take-over: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	return m[2], m[3]
}

// inToolCallRun starts a Run whose agent works in a long tool call, and
// waits until it is there.
func inToolCallRun(t *testing.T, env *factorytest.Env, p *factorytest.Person) string {
	t.Helper()
	// The marker is where the run user can write: the take-over cases
	// also run as root on the local factory stack (cluster check 6).
	path := filepath.Join(env.Runner.SharedDir(fmt.Sprintf("marker-%d", time.Now().UnixNano())), "here")
	here := shell("touch " + path)
	reached := func() {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(path); err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the agent did not reach the marker in 30 s")
			}
		}
	}
	env.Runner.Agent(takeTask, initLine(t), here, inToolCall, interrupted, thenEOF)
	id := start(t, env, p, "Fix the cart")
	reached()
	return id
}

// takeOverTerminal waits until the take-over Execution runs and the
// harness shows its screen in the terminal, and gives the Run and the
// terminal.
func takeOverTerminal(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string) (dispatcherapi.Run, string) {
	t.Helper()
	run := takeOverRuns(t, env, p, id)
	// One terminal for each take-over of the test.
	terminal, err := env.Runner.TakeOver.WaitForTerminal(max(1, len(env.Runner.TakeOver.Terminals())), 30*time.Second)
	if err == nil {
		_, err = env.Runner.TakeOver.WaitForScreen(terminal, `"subtype":"init"`, 30*time.Second)
	}
	if err != nil {
		t.Fatalf("%v\nwrapper output:\n%s", err, env.Runner.Fake.Output(run.Executions[0].ID))
	}
	return run, terminal
}

// takeOverRuns waits until the take-over Execution runs, and gives the
// Run.
func takeOverRuns(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string) dispatcherapi.Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var run dispatcherapi.Run
	for {
		run = getRun(t, p, id)
		if n := len(run.Executions); n >= 2 {
			env.Runner.InAllocation(run.Executions[n-1].ID)
			if run.Executions[n-1].State == dispatcherapi.ExecutionRunning {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no take-over Execution runs after 30 s: %s (%s) %+v\n%s\n%s", run.State, run.Reason, run.Executions,
				env.Runner.Fake.Output(run.Executions[0].ID), env.Logs())
		}
		time.Sleep(20 * time.Millisecond)
	}
	return run
}

// screenInit is the harness's init event on the terminal's screen.
func screenInit(t *testing.T, screen string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(screen, "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) == nil && ev["subtype"] == "init" {
			return ev
		}
	}
	t.Fatalf("no init event on the screen: %s", screen)
	return nil
}

// typeLine types a line into the terminal as the person, and waits until
// the harness shows want.
func typeLine(t *testing.T, env *factorytest.Env, terminal, line, want string) string {
	t.Helper()
	if err := env.Runner.TakeOver.Type(terminal, line); err != nil {
		t.Fatal(err)
	}
	screen, err := env.Runner.TakeOver.WaitForScreen(terminal, want, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return screen
}

func leave(t *testing.T, env *factorytest.Env, terminal string) {
	t.Helper()
	if err := env.Runner.TakeOver.Leave(terminal); err != nil {
		t.Fatal(err)
	}
}

func TestATakeOverOfARunningRunContinuesTheAgentSessionInATerminalOnTheSameMachine(t *testing.T) {
	env, ana := newEnv(t)
	ben := env.Member("Ben")
	id := inToolCallRun(t, env, ana)
	first := getRun(t, ana, id).Executions[0]

	vm, attach := takeOverCLI(t, env, ben, id)

	runUser := env.Runner.RunUser().Username
	if want := "docker exec -i -u engineer " + factorytest.VM(first.ID) + " sudo -u " + runUser + " hc connect"; vm != factorytest.VM(first.ID) || attach != want {
		t.Errorf("the take-over printed the machine %q and the attach command %q, want %q and %q", vm, attach,
			factorytest.VM(first.ID), want)
	}
	run, terminal := takeOverTerminal(t, env, ana, id)
	if run.State != dispatcherapi.StateTakenOver || run.Reason != "taken over by ben@example.com" {
		t.Errorf("the Run is %s (%s), want taken over by Ben", run.State, run.Reason)
	}
	headless, takeOver := run.Executions[0], run.Executions[1]
	if headless.State != dispatcherapi.ExecutionEnded || headless.EndReason != dispatcherapi.EndTakenOver {
		t.Errorf("the headless Execution is %s with the end reason %q, want ended, taken over", headless.State, headless.EndReason)
	}
	if headless.ExitCode == nil || *headless.ExitCode != interruptedCode {
		t.Errorf("the headless harness's exit code is %v: the take-over did not interrupt the agent first", headless.ExitCode)
	}
	if takeOver.Form != dispatcherapi.FormInteractive || takeOver.VM != headless.VM || takeOver.VM != vm ||
		takeOver.Attach != attach || takeOver.Account != headless.Account {
		t.Errorf("the take-over Execution %+v is not in the interactive form on the machine and account of %+v", takeOver, headless)
	}
	if takeOver.TakenOverBy == nil || takeOver.TakenOverBy.Name != "ben@example.com" {
		t.Errorf("the take-over Execution names %+v as the person who took over, want Ben", takeOver.TakenOverBy)
	}
	if got := env.Runner.Dispatched(); len(got) != 1 {
		t.Errorf("the take-over is a new dispatch on the Runner (%v): it must be in the same allocation", got)
	}

	// The same Agent session, in bypass mode, with AskUserQuestion: a
	// person is there.
	started := eventsOf(env, id, "execution_started")
	if len(started) != 2 {
		t.Fatalf("want two execution_started events, got %v", started)
	}
	init := screenInit(t, env.Runner.TakeOver.Screen(terminal))
	if init["session_id"] != started[0]["agent.session_id"] || init["resumed"] != true {
		t.Errorf("the interactive harness has the Agent session %v (resumed %v), want %v resumed", init["session_id"],
			init["resumed"], started[0]["agent.session_id"])
	}
	if tools, _ := init["tools"].([]any); !slices.Contains(tools, any("AskUserQuestion")) || init["permissionMode"] != "bypassPermissions" {
		t.Errorf("the interactive harness has the tools %v in %v mode, want AskUserQuestion in bypass mode", init["tools"], init["permissionMode"])
	}
	// The take-over event names the person, and so does the take-over
	// Execution's start: each person message in it is from Ben.
	takeOverEvent := waitForEvent(t, env, id, "take_over")
	if takeOverEvent["actor.name"] != "ben@example.com" || takeOverEvent["run.state"] != dispatcherapi.StateTakenOver ||
		takeOverEvent["execution.id"] != takeOver.ID || takeOverEvent["execution.vm"] != vm || takeOverEvent["execution.attach"] != attach {
		t.Errorf("the take-over event: %v", takeOverEvent)
	}
	if s := started[1]; s["actor.name"] != "ben@example.com" || s["execution.id"] != takeOver.ID ||
		s["execution.form"] != dispatcherapi.FormInteractive || s["agent.session_id"] != started[0]["agent.session_id"] {
		t.Errorf("the take-over Execution's start event: %v", s)
	}
	// The core is the run user's, started by the wrapper: no SUDO_ name.
	coreEnv, uid := env.Runner.TakeOver.Core()
	if want, _ := strconv.Atoi(env.Runner.RunUser().Uid); uid != want {
		t.Errorf("the hc core runs as the user id %d, want the run user's %d", uid, want)
	}
	for _, kv := range coreEnv {
		if strings.HasPrefix(kv, "SUDO_") || strings.HasPrefix(kv, "WRAPPER_") || strings.HasPrefix(kv, "CLAUDE_CODE_OAUTH_TOKEN=") {
			t.Errorf("the hc core's environment has %s", kv)
		}
	}

	// The person talks to the agent: the Stop hook never refuses, though
	// there is no report.
	screen := typeLine(t, env, terminal, "Why does the cart fail?", "Done.")
	time.Sleep(100 * time.Millisecond)
	if screen = env.Runner.TakeOver.Screen(terminal); strings.Contains(screen, "Stop hook feedback") {
		t.Errorf("the Stop hook refused during the take-over: %s", screen)
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateTakenOver {
		t.Errorf("a turn of the person changed the Run to %s (%s)", run.State, run.Reason)
	}

	leave(t, env, terminal)
	run = waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if run.Reason != dispatcherapi.ReasonTakeOverNoReport {
		t.Errorf("the reason is %q, want %q", run.Reason, dispatcherapi.ReasonTakeOverNoReport)
	}
	if e := run.Executions[1]; e.EndReason != dispatcherapi.EndSessionEnded || e.ExitCode == nil || *e.ExitCode != 0 {
		t.Errorf("the take-over Execution ended with %q and exit code %v", e.EndReason, e.ExitCode)
	}
	// The end procedure left no process of the take-over.
	pid, err := os.ReadFile(filepath.Join(env.Runner.TakeOver.Dir(), terminal, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(string(pid))); alive(n) {
		t.Errorf("the harness of the take-over (pid %d) still runs after the end", n)
	}
	// The wrapper deletes the workspace after its end call.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		_, err := os.Stat(env.Runner.Workspace(first.ID))
		if os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("the workspace is there 10 s after the take-over's end: %v", err)
			break
		}
	}
}

// The take-over of an Issue or a Monitor Run keeps SolidTrace's MCP
// server: the harness gets the MCP configuration, and the take-over
// Execution's own MCP token in a file that the run user reads.
func TestATakeOverOfAnIssueRunKeepsSolidTracesMCPServerWithTheTakeOversToken(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	here, reached := agentShell(t, env, "true")
	env.Runner.Agent(takeTask, initLine(t), here, inToolCall, interrupted, thenEOF)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	reached()

	takeOverCLI(t, env, ana, id)
	_, terminal := takeOverTerminal(t, env, ana, id)

	init := screenInit(t, env.Runner.TakeOver.Screen(terminal))
	servers, _ := init["mcp_servers"].([]any)
	if len(servers) != 1 || fmt.Sprint(servers[0]) != "map[name:solidtrace status:connected]" {
		t.Errorf("the interactive harness has the MCP servers %v, want solidtrace", init["mcp_servers"])
	}
	mcp := mcpTokens(env)
	if len(mcp) != 2 {
		t.Fatalf("want an MCP token for the headless and the take-over Execution, got %+v", mcp)
	}
	screen := typeLine(t, env, terminal, `!f="$`+dispatcherapi.EnvMCPTokenFile+`" && test ! -w "$f" && echo "mcp $`+
		dispatcherapi.EnvMCPURL+` $(cat "$f")"`, "mcp http")
	if want := "mcp " + env.SolidTrace.URL() + "/mcp " + mcp[1].Secret; !strings.Contains(screen, want) {
		t.Errorf("the take-over's harness has no MCP token of its own: %q, want %q", screen, want)
	}
	leave(t, env, terminal)
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if tokens := mcpTokens(env); !tokens[1].Revoked {
		t.Error("the end step did not revoke the take-over's MCP token")
	}
}

// A take-over during the headless Execution's setup stops that setup: the
// take-over does the full setup again, with the setup command, and does
// not take the checkout that the stopped setup left.
func TestATakeOverDuringTheSetupDoesTheFullSetupWithTheSetupCommand(t *testing.T) {
	env, ana := newEnv(t)
	shared := env.Runner.SharedDir("setup-" + filepath.Base(t.TempDir()))
	runs, waiting := filepath.Join(shared, "runs"), filepath.Join(shared, "waiting")
	// The first run of the setup command waits until the take-over stops it.
	env.Forge.Push("example/shop", "main", map[string]string{
		factoryFile(""): `setup = "echo run >> ` + runs + ` && test $(wc -l < ` + runs + `) -gt 1 || { touch ` + waiting +
			` && sleep 300; }"`,
	}, "A setup command")
	ran := filepath.Join(shared, "harness-ran")
	env.Runner.Agent(takeTask, shell("touch "+ran), reportStep("--outcome", "done", "--summary", "Done"), reply, result,
		waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitUntil(t, "the setup command", func() bool {
		_, err := os.Stat(waiting)
		return err == nil
	})

	takeOverCLI(t, env, ana, id)
	run, terminal := takeOverTerminal(t, env, ana, id)

	b, err := os.ReadFile(runs)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "run\n"); n != 2 {
		t.Errorf("the setup command ran %d times, want once in each Execution\nwrapper output:\n%s", n,
			env.Runner.Fake.Output(run.Executions[0].ID))
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("the headless harness started after the take-over stopped its setup")
	}
	leave(t, env, terminal)
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
}

func TestAReportDuringTheTakeOverSetsTheRunsStateWhenTheAgentSessionEnds(t *testing.T) {
	env, ana := newEnv(t)
	id := inToolCallRun(t, env, ana)
	takeOverCLI(t, env, ana, id)
	_, terminal := takeOverTerminal(t, env, ana, id)

	typeLine(t, env, terminal, `!solidtrace report --outcome done --summary "The cart is fixed by hand." && echo reported`, "reported")
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateTakenOver {
		t.Errorf("the report changed the Run to %s before the take-over ended", run.State)
	}
	typeLine(t, env, terminal, "Thanks", "Done.")
	leave(t, env, terminal)

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "The cart is fixed by hand." {
		t.Errorf("the summary is %q", run.Summary)
	}
	if report := waitForEvent(t, env, id, "report"); report["execution.id"] != run.Executions[1].ID {
		t.Errorf("the report is of the Execution %v, want the take-over's", report["execution.id"])
	}
}

func TestOnlyTheTotalLimitAppliesDuringTheTakeOver(t *testing.T) {
	env, ana := newEnv(t)
	id := inToolCallRun(t, env, ana)
	takeOverCLI(t, env, ana, id)
	_, terminal := takeOverTerminal(t, env, ana, id)

	// A person reads: no output event for longer than the idle limit.
	env.Clock.Advance(dispatcherapi.DefaultIdleLimit + time.Minute)
	time.Sleep(settle)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateTakenOver || run.Executions[1].State != dispatcherapi.ExecutionRunning {
		t.Fatalf("after the idle limit the Run is %s (%s): %+v", run.State, run.Reason, run.Executions)
	}
	if env.Runner.TakeOver.Finished(terminal) {
		t.Fatal("the idle limit ended the take-over's harness")
	}

	env.Clock.Advance(dispatcherapi.DefaultTotalLimit - dispatcherapi.DefaultIdleLimit)
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if !strings.HasPrefix(run.Reason, dispatcherapi.ReasonTakeOverNoReport) || !strings.Contains(run.Reason, "total limit") {
		t.Errorf("the reason is %q, want the take-over's end with no report at the total limit", run.Reason)
	}
	if e := run.Executions[1]; !strings.HasPrefix(e.EndReason, dispatcherapi.EndTimeout) {
		t.Errorf("the take-over Execution's end reason is %q", e.EndReason)
	}
}

func TestAMessageDuringTheTakeOverIsKeptAndStartsANewExecutionWhenTheTakeOverEnds(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.KeepAgentSessions()
	id := inToolCallRun(t, env, ana)
	takeOverCLI(t, env, ana, id)
	_, terminal := takeOverTerminal(t, env, ana, id)

	// The next Execution takes the message, and reports.
	env.Runner.Agent(takeTask, initLine(t), done("Added the test."), reply, result, waitForEnd)
	message := send(t, env, ana, id, "Add a test for the cart")
	time.Sleep(settle)
	events := messageEvents(env, id, message)
	if len(events["message_accepted"]) != 1 || len(events["message_written"]) != 0 {
		t.Errorf("the message during the take-over has the events %v, want accepted only", events)
	}
	if strings.Contains(env.Runner.TakeOver.Screen(terminal), "Add a test") {
		t.Error("the message was typed into the take-over's terminal")
	}

	leave(t, env, terminal)
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 3 || run.Executions[2].Form != dispatcherapi.FormHeadless {
		t.Fatalf("want a third, headless Execution after the take-over: %+v", run.Executions)
	}
	delivered := waitForMessageEvent(t, env, id, message, "message_delivered")
	if delivered["execution.id"] != run.Executions[2].ID {
		t.Errorf("the message was delivered in %v, want the Execution after the take-over", delivered["execution.id"])
	}
}

func TestStopDuringTheTakeOverEndsTheInteractiveExecutionAndGivesStopped(t *testing.T) {
	env, ana := newEnv(t)
	ben := env.Member("Ben")
	id := inToolCallRun(t, env, ana)
	takeOverCLI(t, env, ana, id)
	_, terminal := takeOverTerminal(t, env, ana, id)

	if got := ben.CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}

	run := waitFor(t, env, ana, id, dispatcherapi.StateStopped)
	if e := run.Executions[1]; e.EndReason != dispatcherapi.EndStopped {
		t.Errorf("the take-over Execution ended with %q, want stopped", e.EndReason)
	}
	if !env.Runner.TakeOver.Finished(terminal) {
		t.Error("the take-over's harness still runs after the stop")
	}
	if stop := waitForEvent(t, env, id, "stop"); stop["actor.name"] != "ben@example.com" {
		t.Errorf("the stop's actor is %v", stop["actor.name"])
	}
}

// The refusals of a take-over of a Run with no live Execution are in
// take_over_idle_test.go.
func TestATakeOverIsRefusedForNoRunForAnotherOrganizationAndForATakenOverRun(t *testing.T) {
	env, ana := newEnv(t)
	ben := env.Member("Ben")
	if got := ana.CLI("take-over", "--dispatcher", env.URL(), "no-such-run"); got.Code != 1 || !strings.Contains(got.Stderr, "no Run") {
		t.Errorf("a take-over of no Run: exit %d: %s", got.Code, got.Stderr)
	}

	running := inToolCallRun(t, env, ana)
	other := env.PersonIn("Bo", recorder.Organization{ID: 8, Slug: "other", Name: "Other"}, "admin")
	if got := other.CLI("take-over", "--dispatcher", env.URL(), running); got.Code != 1 || !strings.Contains(got.Stderr, "403") {
		t.Errorf("a take-over from another Organization: exit %d: %s", got.Code, got.Stderr)
	}
	if run := getRun(t, ana, running); run.State != dispatcherapi.StateRunning || len(run.Executions) != 1 {
		t.Errorf("the refused take-over from another Organization changed the Run: %s %+v", run.State, run.Executions)
	}
	takeOverCLI(t, env, ana, running)
	got := ben.CLI("take-over", "--dispatcher", env.URL(), running)
	if got.Code != 1 || !strings.Contains(got.Stderr, "taken over already, by ana@example.com") {
		t.Errorf("a second take-over: exit %d: %s", got.Code, got.Stderr)
	}
	run, _ := takeOverTerminal(t, env, ana, running)
	if len(run.Executions) != 2 || len(eventsOf(env, running, "take_over")) != 1 {
		t.Errorf("the refused take-over changed the Run: %+v", run.Executions)
	}
}

func TestALostMachineDuringTheTakeOverMakesTheRunWaitOnAPersonWithTheReason(t *testing.T) {
	env, ana := newEnv(t)
	id := inToolCallRun(t, env, ana)
	takeOverCLI(t, env, ana, id)
	run, _ := takeOverTerminal(t, env, ana, id)

	if err := env.Runner.Fake.LoseMachine(run.Executions[0].ID); err != nil {
		t.Fatal(err)
	}

	run = waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if !strings.Contains(run.Reason, "the take-over ended") || !strings.Contains(run.Reason, "the machine was lost") {
		t.Errorf("the reason is %q", run.Reason)
	}
	if len(env.Runner.Dispatched()) != 1 {
		t.Errorf("a lost take-over started again by itself: %v", env.Runner.Dispatched())
	}
}

// A core that is not in the Execution's cgroup (a person's hc connect
// started it before the wrapper's) would run the take-over's harness
// outside the kill boundary, with the account's token: the wrapper kills
// that core before it starts a terminal, and starts its own core in the
// cgroup. The early connection drops; the take-over goes on, and no
// stray core stays.
func TestAnHcCoreOutsideTheExecutionsCgroupIsKilledAndTheTakeOverStartsItsOwnCore(t *testing.T) {
	env, ana := newEnv(t)
	var attr *syscall.SysProcAttr
	if os.Geteuid() == 0 {
		// The run user's core, as a person's early hc connect starts it.
		uid, _ := strconv.Atoi(env.Runner.RunUser().Uid)
		gid, _ := strconv.Atoi(env.Runner.RunUser().Gid)
		attr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	}
	// The test's process is outside each Execution's cgroup: so is the
	// core, and each terminal that it starts.
	outside, err := env.Runner.TakeOver.StartCore(env.Runner.HCProgram(), attr)
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- outside.Wait() }()
	t.Cleanup(func() { outside.Process.Kill() })
	id := inToolCallRun(t, env, ana)
	takeOverCLI(t, env, ana, id)

	run, terminal := takeOverTerminal(t, env, ana, id)
	if run.State != dispatcherapi.StateTakenOver {
		t.Errorf("the Run is %s (%s), want taken over", run.State, run.Reason)
	}
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Error("the core outside the cgroup still runs")
	}
	// The terminal's holder is a child of the wrapper's core: it is in the
	// take-over Execution's cgroup.
	holder, err := os.ReadFile(filepath.Join(env.Runner.TakeOver.Dir(), terminal, "holder"))
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := killboundary.Reopen(wrapper.CgroupName(run.Executions[1].ID))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(holder)))
	if in, err := boundary.Holds(pid); err != nil || !in {
		t.Errorf("the take-over's terminal (holder %d) is not in the take-over Execution's cgroup (%v)", pid, err)
	}
	leave(t, env, terminal)
	if run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson); run.Reason != dispatcherapi.ReasonTakeOverNoReport {
		t.Errorf("the reason is %q, want %q", run.Reason, dispatcherapi.ReasonTakeOverNoReport)
	}
}

// connectAsPerson runs the attach command's sudo part as the user
// engineer (a member of the access group on the local factory stack),
// sends one request of the hc API, and gives its result.
func connectAsPerson(t *testing.T, connect, method string, params map[string]any) (json.RawMessage, error) {
	t.Helper()
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	cmd := exec.Command("runuser", "-u", "engineer", "--", "sh", "-c", connect)
	cmd.Stdin = strings.NewReader(string(request) + "\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %v: %s", connect, err, out)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(strings.NewReader(string(out))).Decode(&reply); err != nil || reply.Result == nil {
		return nil, fmt.Errorf("%s %s: %v: %s", connect, method, err, out)
	}
	return reply.Result, nil
}

// Cluster check 6: on the agent machine (the local factory stack), the
// wrapper starts the real hc core as the run user in the take-over
// Execution's cgroup, with no SUDO_ variable, and a person attaches with
// the printed command's 'sudo -u <run user> hc connect' to that core.
func TestATakeOverOnTheAgentMachineRunsTheRealHcCoreThatAPersonAttachesTo(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.UseRealHC()
	id := inToolCallRun(t, env, ana)
	vm, attach := takeOverCLI(t, env, ana, id)
	runUser := env.Runner.RunUser()
	connect, ok := strings.CutPrefix(attach, "docker exec -i -u engineer "+vm+" ")
	if !ok || connect != "sudo -u "+runUser.Username+" hc connect" {
		t.Fatalf("the attach command is %q", attach)
	}
	run := takeOverRuns(t, env, ana, id)
	takeOver := run.Executions[1]

	// The person finds the take-over's terminal.
	var terminal string
	deadline := time.Now().Add(30 * time.Second)
	for terminal == "" {
		result, err := connectAsPerson(t, connect, "terminal.list", map[string]any{})
		var list struct {
			Terminals []struct {
				Address string  `json:"address"`
				Name    *string `json:"name"`
			} `json:"terminals"`
		}
		if err == nil {
			if json.Unmarshal(result, &list) != nil {
				json.Unmarshal(result, &list.Terminals)
			}
			for _, x := range list.Terminals {
				if x.Name != nil && *x.Name == "take-over" {
					terminal = x.Address
				}
			}
		}
		if terminal == "" && time.Now().After(deadline) {
			t.Fatalf("no take-over terminal after 30 s: %v %s\n%s", err, result, env.Runner.Fake.Output(run.Executions[0].ID))
		}
		time.Sleep(200 * time.Millisecond)
	}

	result, err := connectAsPerson(t, connect, "server.status", map[string]any{})
	var status struct {
		PID     int `json:"pid"`
		Holders []struct {
			PID int `json:"pid"`
		} `json:"holders"`
	}
	if err != nil || json.Unmarshal(result, &status) != nil || status.PID <= 0 {
		t.Fatalf("server.status: %v %s", err, result)
	}
	boundary, err := killboundary.Reopen(wrapper.CgroupName(takeOver.ID))
	if err != nil {
		t.Fatal(err)
	}
	pids := []int{status.PID}
	for _, h := range status.Holders {
		pids = append(pids, h.PID)
	}
	for _, pid := range pids {
		if in, err := boundary.Holds(pid); err != nil || !in {
			t.Errorf("the hc process %d is not in the take-over Execution's cgroup (%v)", pid, err)
		}
		environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
		if err != nil {
			t.Fatal(err)
		}
		for _, kv := range strings.Split(string(environ), "\x00") {
			if strings.HasPrefix(kv, "SUDO_") {
				t.Errorf("the hc process %d has %s", pid, kv)
			}
		}
		var st syscall.Stat_t
		if err := syscall.Stat(fmt.Sprintf("/proc/%d", pid), &st); err != nil || strconv.Itoa(int(st.Uid)) != runUser.Uid {
			t.Errorf("the hc process %d runs as the user id %d, want the run user's %s", pid, st.Uid, runUser.Uid)
		}
	}

	// The person sees the resumed Agent session, and leaves it.
	var screen string
	deadline = time.Now().Add(30 * time.Second)
	for !strings.Contains(screen, `"subtype":"init"`) {
		result, err := connectAsPerson(t, connect, "terminal.read", map[string]any{"terminal": terminal})
		var read struct {
			Text string `json:"text"`
		}
		if err == nil && json.Unmarshal(result, &read) == nil {
			screen = read.Text
		}
		if time.Now().After(deadline) {
			t.Fatalf("the take-over's screen has no init event after 30 s: %v %s", err, screen)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := connectAsPerson(t, connect, "terminal.send", map[string]any{"terminal": terminal, "text": "/exit"}); err != nil {
		t.Fatal(err)
	}

	run = waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if run.Reason != dispatcherapi.ReasonTakeOverNoReport {
		t.Errorf("the reason is %q, want %q", run.Reason, dispatcherapi.ReasonTakeOverNoReport)
	}
	if e := run.Executions[1]; e.EndReason != dispatcherapi.EndSessionEnded || e.ExitCode == nil || *e.ExitCode != 0 {
		t.Errorf("the take-over Execution ended with %q and exit code %v", e.EndReason, e.ExitCode)
	}
	for _, pid := range pids {
		if alive(pid) {
			t.Errorf("the hc process %d still runs after the end", pid)
		}
	}
}
