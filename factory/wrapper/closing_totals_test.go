package wrapper_test

import (
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// The closing event of a Run carries its totals (story 122): the times
// from the trigger to the first Execution, to the PR and to its outcome,
// the interventions, the Executions and restarts, and the tokens and cost
// of the Run's Agent sessions, which the Dispatcher reads from
// SolidTrace's totals call with a child token for those Agent sessions.

// interventionKinds are the kinds of totals.interventions.<kind>.
var interventionKinds = []string{"message", "interrupt", "stop", "take_over", "account_switch", "continue"}

// runSessionsOf are the Agent session ids of the Execution tokens of the
// Run, oldest first.
func runSessionsOf(env *factorytest.Env, id string) []string {
	var ids []string
	for _, c := range env.SolidTrace.ChildTokens() {
		if !strings.HasSuffix(c.Name, " of Run "+id) || !strings.HasPrefix(c.Name, "Execution ") {
			continue
		}
		for _, s := range c.AgentSessionIDs {
			if !slices.Contains(ids, s) {
				ids = append(ids, s)
			}
		}
	}
	return ids
}

// totalsTokenOf is the child token of the Run's totals call.
func totalsTokenOf(t *testing.T, env *factorytest.Env, id string) recorder.ChildToken {
	t.Helper()
	var got []recorder.ChildToken
	for _, c := range env.SolidTrace.ChildTokens() {
		if c.Name == "Totals of Run "+id {
			got = append(got, c)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one totals token of the Run, got %+v", got)
	}
	return got[0]
}

func number(t *testing.T, ev map[string]any, name string) float64 {
	t.Helper()
	n, ok := ev[name].(float64)
	if !ok {
		t.Fatalf("the closing event has no number %s: %v", name, ev)
	}
	return n
}

// expectNumbers checks the numbers of the closing event.
func expectNumbers(t *testing.T, ev map[string]any, want map[string]float64) {
	t.Helper()
	for name, n := range want {
		if got, ok := ev[name].(float64); !ok || got != n {
			t.Errorf("the closing event has %s %v, want %v", name, ev[name], n)
		}
	}
}

// expectAbsent checks that the closing event has none of names.
func expectAbsent(t *testing.T, ev map[string]any, names ...string) {
	t.Helper()
	for _, name := range names {
		if v, ok := ev[name]; ok {
			t.Errorf("the closing event has %s %v, want none", name, v)
		}
	}
}

// expectInterventions checks totals.interventions.total and each kind
// (the kinds not in want are 0).
func expectInterventions(t *testing.T, ev map[string]any, total int, want map[string]int) {
	t.Helper()
	numbers := map[string]float64{"totals.interventions.total": float64(total)}
	for _, kind := range interventionKinds {
		numbers["totals.interventions."+kind] = float64(want[kind])
	}
	expectNumbers(t, ev, numbers)
}

func cost(usd float64) *float64 { return &usd }

func TestAMergedRunsClosingEventHasTheTimesTheOutcomeTheCountsAndTheTotals(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.HoldMachines()
	reached, wait := agentShell(t, env, "true")
	hold, open := agentGate(t, env)
	out := env.Runner.SharedDir("pr-" + strings.ReplaceAll(t.Name(), "/", "-"))
	env.Runner.Agent(takeTask, initLine(t), reached, hold, commitStep,
		recorded(out, "pr", `solidtrace pr --title "Fix the cart"`),
		reportStep("--outcome", "done", "--summary", "Fixed the cart test"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the flaky cart test")

	// The first Execution waits 90 s for a machine, and the PR opens 2
	// minutes after its start.
	waitUntil(t, "the Execution waits for a machine", func() bool { return len(env.Runner.Waiting()) == 1 })
	env.Clock.Advance(90 * time.Second)
	env.Runner.GiveMachines()
	wait()
	env.Clock.Advance(2 * time.Minute)
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	prs := env.Forge.PRs("example/shop")
	if len(prs) != 1 {
		t.Fatalf("want one PR, got %+v\npr: %s", prs, readRecorded(t, out, "pr"))
	}
	sessions := runSessionsOf(env, id)
	if len(sessions) != 1 {
		t.Fatalf("want one Agent session of the Run, got %v", sessions)
	}
	env.SolidTrace.SetSessionTotals(sessions[0], recorder.SessionTotals{Input: 1200, Output: 3400, CacheRead: 56000,
		CacheWrite: 7800, CostUSD: cost(0.42)})
	// Another Agent session's totals are not the Run's.
	env.SolidTrace.SetSessionTotals("9b2f6c1e-0d3a-4e5b-8c7d-6e5f4a3b2c1d", recorder.SessionTotals{Input: 1, CostUSD: cost(9)})

	// The next PR check is 5 minutes after the Dispatcher's start: 90 s
	// after the PR opened.
	env.Forge.SetState("example/shop", prs[0].ID, "MERGED")
	env.Clock.Advance(90 * time.Second)
	env.CheckClosedRuns()

	closed := closedEvent(t, env, id, dispatcherapi.StateDone, "merged", "pr_merged")
	expectNumbers(t, closed, map[string]float64{
		"totals.seconds_to_first_execution":        90,
		"totals.seconds_to_pr_opened":              210,
		"totals.seconds_from_pr_opened_to_outcome": 90,
		"run.executions":                           1,
		"totals.restarts":                          0,
		"totals.agent_sessions":                    1,
		"totals.tokens.input":                      1200,
		"totals.tokens.output":                     3400,
		"totals.tokens.cache_read":                 56000,
		"totals.tokens.cache_write":                7800,
		"totals.no_cost":                           0,
	})
	if c := number(t, closed, "totals.cost_usd"); math.Abs(c-0.42) > 1e-9 {
		t.Errorf("the closing event's cost is %v, want 0.42", c)
	}
	expectInterventions(t, closed, 0, nil)
	if closed["totals.complete"] != true || closed["run.trigger"] != "person" || closed["factory.project"] != "shop" ||
		closed["run.pr.outcome"] != "merged" {
		t.Errorf("the closing event: complete %v, trigger %v, Project %v, PR outcome %v", closed["totals.complete"],
			closed["run.trigger"], closed["factory.project"], closed["run.pr.outcome"])
	}

	// The totals call's token reads only the Run's Agent sessions, and
	// stops after the call.
	tok := totalsTokenOf(t, env, id)
	if !slices.Equal(tok.Scopes, []string{"sessions:read"}) || !slices.Equal(tok.AgentSessionIDs, sessions) {
		t.Errorf("the totals token has the scopes %v and the Agent sessions %v; want sessions:read for %v",
			tok.Scopes, tok.AgentSessionIDs, sessions)
	}
	waitUntil(t, "the revocation of the totals token", func() bool { return totalsTokenOf(t, env, id).Revoked })
	if !tok.ExpiresAt.After(env.Clock.Now()) || tok.ExpiresAt.After(env.Clock.Now().Add(time.Hour)) {
		t.Errorf("the totals token expires at %s, want a short time after %s", tok.ExpiresAt, env.Clock.Now())
	}
}

func TestARunClosedAfter30DaysHasNoPRTimesAndEachRunGetsItsOwnTotals(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), done("Fixed the cart"), reply, result, waitForEnd)
	cart := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, cart, dispatcherapi.StateDone)
	env.Runner.Agent(takeTask, initLine(t), done("Fixed the tax"), reply, result, waitForEnd)
	tax := start(t, env, ana, "Fix the tax")
	waitFor(t, env, ana, tax, dispatcherapi.StateDone)
	for i, id := range []string{cart, tax} {
		sessions := runSessionsOf(env, id)
		if len(sessions) != 1 {
			t.Fatalf("want one Agent session of the Run, got %v", sessions)
		}
		env.SolidTrace.SetSessionTotals(sessions[0], recorder.SessionTotals{Input: uint64(100 * (i + 1)),
			Output: uint64(10 * (i + 1))})
	}

	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()

	for i, id := range []string{cart, tax} {
		closed := closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
		expectAbsent(t, closed, "totals.seconds_to_pr_opened", "totals.seconds_from_pr_opened_to_outcome",
			"totals.cost_usd")
		expectNumbers(t, closed, map[string]float64{
			"totals.seconds_to_first_execution": 0,
			"totals.agent_sessions":             1,
			"totals.tokens.input":               float64(100 * (i + 1)),
			"totals.tokens.output":              float64(10 * (i + 1)),
			"totals.no_cost":                    1,
			"run.executions":                    1,
		})
		if closed["totals.complete"] != true {
			t.Errorf("the closing event's totals are not complete: %v", closed)
		}
	}
}

func TestWhenTheParseHasNotCaughtUpTheClosingEventSaysThatItsTotalsAreNotComplete(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), done("Fixed the cart"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	env.SolidTrace.SetSessionTotals(runSessionsOf(env, id)[0], recorder.SessionTotals{Input: 5, CostUSD: cost(0.01)})
	env.SolidTrace.TotalsNotComplete(true)

	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()

	closed := closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
	if closed["totals.complete"] != false {
		t.Errorf("the closing event's totals.complete is %v, want false", closed["totals.complete"])
	}
	expectNumbers(t, closed, map[string]float64{"totals.tokens.input": 5, "totals.agent_sessions": 1})
}

func TestWhenSolidTraceGivesNoTotalsTheRunClosesWithTotalsThatAreNotComplete(t *testing.T) {
	for name, refuse := range map[string]func(env *factorytest.Env){
		"the totals call fails": func(env *factorytest.Env) { env.SolidTrace.FailTotals(http.StatusInternalServerError) },
		"no child token":        func(env *factorytest.Env) { env.SolidTrace.RefuseChildTokens(http.StatusServiceUnavailable) },
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			env.Runner.Agent(takeTask, initLine(t), done("Fixed the cart"), reply, result, waitForEnd)
			id := start(t, env, ana, "Fix the cart")
			waitFor(t, env, ana, id, dispatcherapi.StateDone)
			env.SolidTrace.SetSessionTotals(runSessionsOf(env, id)[0], recorder.SessionTotals{Input: 5})
			refuse(env)

			env.Clock.Advance(closeAfter)
			env.CheckClosedRuns()

			closed := closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
			if closed["totals.complete"] != false {
				t.Errorf("the closing event's totals.complete is %v, want false", closed["totals.complete"])
			}
			expectAbsent(t, closed, "totals.agent_sessions", "totals.tokens.input", "totals.cost_usd")
			expectNumbers(t, closed, map[string]float64{"totals.seconds_to_first_execution": 0, "run.executions": 1})
			expectInterventions(t, closed, 0, nil)
		})
	}
}

func TestInterventionsAreCountedByKindAlsoForARunWithATakeOver(t *testing.T) {
	env, ana := newEnv(t)
	ben := env.Member("Ben")
	tool, pidFile := toolCall(t)
	env.Runner.Agent(takeTask, initLine(t), tool, interrupted, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	toolPid(t, pidFile)

	// An interrupt, then a message that continues the Run.
	interrupt(t, env, ben, id)
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	here, reached := agentShell(t, env, "true")
	env.Runner.Agent(takeTask, initLine(t), here, inToolCall, interrupted, thenEOF)
	send(t, env, ana, id, "The cart total must include the tax")
	reached()

	// A take-over, and a stop during it.
	takeOverCLI(t, env, ana, id)
	takeOverTerminal(t, env, ana, id)
	if got := ben.CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}
	waitFor(t, env, ana, id, dispatcherapi.StateStopped)
	var input uint64
	sessions := runSessionsOf(env, id)
	for i, s := range sessions {
		env.SolidTrace.SetSessionTotals(s, recorder.SessionTotals{Input: uint64(1000 * (i + 1)), CostUSD: cost(0.5)})
		input += uint64(1000 * (i + 1))
	}

	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()

	closed := closedEvent(t, env, id, dispatcherapi.StateStopped, "", "no_activity")
	// The message that continued the Run is a message and a continue: the
	// total counts each event one time.
	expectInterventions(t, closed, 4, map[string]int{"interrupt": 1, "message": 1, "continue": 1, "take_over": 1,
		"stop": 1})
	expectNumbers(t, closed, map[string]float64{"run.executions": 3, "totals.restarts": 0,
		"totals.agent_sessions": float64(len(sessions)), "totals.tokens.input": float64(input)})
	if c := number(t, closed, "totals.cost_usd"); math.Abs(c-0.5*float64(len(sessions))) > 1e-9 {
		t.Errorf("the closing event's cost is %v, want the sum of its Agent sessions", c)
	}
	if tok := totalsTokenOf(t, env, id); !slices.Equal(tok.AgentSessionIDs, sessions) {
		t.Errorf("the totals token is for %v, want each Agent session of the Run %v", tok.AgentSessionIDs, sessions)
	}
}

func TestAPersonsAccountSwitchIsAnInterventionAndTheDispatchersIsNot(t *testing.T) {
	t.Run("a person", func(t *testing.T) {
		env, ana := newEnv(t)
		waitAtLimit(t, env)
		env.AddAccount("pool-2", secondToken)
		env.Runner.Agent(script(takeTask, initLine(t), usageLimit(env.Clock.Now().Add(time.Hour)))...)
		env.Runner.Agent(takeTask, done("Fixed on pool-2"), reply, result, waitForEnd)
		id := start(t, env, ana, "Fix the cart")
		waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
		env.Clock.Advance(time.Second)
		if got := switchAccount(env, ana, id); got.Code != 0 {
			t.Fatalf("solidtrace switch-account: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
		}
		waitFor(t, env, ana, id, dispatcherapi.StateDone)

		env.Clock.Advance(closeAfter)
		env.CheckClosedRuns()

		closed := closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
		expectInterventions(t, closed, 1, map[string]int{"account_switch": 1})
		expectNumbers(t, closed, map[string]float64{"run.executions": 2, "totals.restarts": 0})
	})
	t.Run("the Dispatcher", func(t *testing.T) {
		env, ana := newEnv(t)
		env.AddAccount("pool-2", secondToken)
		env.Runner.Agent(script(takeTask, initLine(t), usageLimit(env.Clock.Now().Add(3*time.Hour)))...)
		env.Runner.Agent(takeTask, done("Fixed on pool-2"), reply, result, waitForEnd)
		id := start(t, env, ana, "Fix the cart")
		waitFor(t, env, ana, id, dispatcherapi.StateDone)
		waitForEvent(t, env, id, "account_switched")

		env.Clock.Advance(closeAfter)
		env.CheckClosedRuns()

		closed := closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
		expectInterventions(t, closed, 0, nil)
		expectNumbers(t, closed, map[string]float64{"run.executions": 2})
	})
}

func TestAnAutomaticRestartIsCountedApartFromTheExecutions(t *testing.T) {
	env, ana := newEnv(t)
	step, working := agentShell(t, env, "true")
	env.Runner.Agent(interruptedTurn(t, step)...)
	env.Runner.Agent(takeTask, initLine(t), done("Fixed"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	working()
	env.Runner.Preempt(getRun(t, ana, id).Executions[0].ID)
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()

	closed := closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
	expectNumbers(t, closed, map[string]float64{"run.executions": 2, "totals.restarts": 1})
	expectInterventions(t, closed, 0, nil)
}

// A machine that went with no notice to the wrapper sends the machine_lost
// event with restart true: it counts as an automatic restart too.
func TestARestartAfterALostMachineIsCountedApartFromTheExecutions(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), waitForever(t, env))
	env.Runner.Agent(takeTask, initLine(t), done("Fixed"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	if err := env.Runner.Fake.LoseMachine(runningExecution(t, ana, id, 1)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()

	closed := closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
	expectNumbers(t, closed, map[string]float64{"run.executions": 2, "totals.restarts": 1})
	expectInterventions(t, closed, 0, nil)
}
