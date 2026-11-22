package wrapper_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/factorytest"
)

// Account limits and the switch: the transitions of a Run whose account
// reaches its usage limit, as the harness's result reports it.

// usageLimit are the harness's lines when the account reaches its usage
// limit at the first request of a turn, as the usage-limit stream has
// them: an assistant error and an error result whose text gives the reset
// time. Then the harness exits with code 1.
func usageLimit(reset time.Time) []string {
	return limitLines(fmt.Sprintf("Claude AI usage limit reached|%d", reset.Unix()))
}

// limitLines are the usage-limit lines with text as the error text.
func limitLines(text string) []string {
	assistant, _ := json.Marshal(map[string]any{"type": "assistant", "error": "rate_limit", "parent_tool_use_id": nil,
		"session_id": "", "uuid": "", "message": map[string]any{"id": "msg_01Limit", "type": "message",
			"role": "assistant", "model": "claude-haiku-5-5", "stop_reason": "stop_sequence",
			"content": []any{map[string]any{"type": "text", "text": text}}}})
	result, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": true, "num_turns": 1,
		"result": text, "stop_reason": "stop_sequence", "session_id": "", "uuid": "", "total_cost_usd": 0})
	return []string{string(assistant), string(result), `{"fake":"exit","code":1}`}
}

// rejected is the harness's rate_limit_event when the account is at its
// usage limit until reset.
func rejected(reset time.Time) string {
	b, _ := json.Marshal(map[string]any{"type": "rate_limit_event", "session_id": "", "uuid": "",
		"rate_limit_info": map[string]any{"status": "rejected", "resetsAt": reset.Unix(), "rateLimitType": "five_hour",
			"overageStatus": "rejected", "isUsingOverage": false}})
	return string(b)
}

func script(lines ...any) []string {
	var out []string
	for _, l := range lines {
		switch v := l.(type) {
		case string:
			out = append(out, v)
		case []string:
			out = append(out, v...)
		}
	}
	return out
}

// switchAccount runs 'solidtrace switch-account' for the Run.
func switchAccount(env *factorytest.Env, p *factorytest.Person, id string) factorytest.Result {
	return p.CLI("switch-account", "--dispatcher", env.URL(), id)
}

// limitOf is the reset time that 'dispatcher accounts list' shows for
// the account ("-" is none).
func limitOf(t *testing.T, env *factorytest.Env, account string) string {
	t.Helper()
	got := env.Operator("", "list")
	for _, line := range strings.Split(got.Stdout, "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == account {
			if f[len(f)-1] == "-" {
				return "-"
			}
			return strings.Join(f[len(f)-3:], " ")
		}
	}
	t.Fatalf("no account %s in the list:\n%s%s", account, got.Stdout, got.Stderr)
	return ""
}

func TestAUsageLimitWithAnotherAccountFreeMovesTheRunToItAndResumesTheAgentSession(t *testing.T) {
	env, ana := newEnv(t)
	env.AddAccount("pool-2", secondToken)
	reset := env.Clock.Now().Add(3 * time.Hour)
	wait, open := gate(t)
	env.Runner.Agent(script(takeTask, initLine(t), commitStep, wait, usageLimit(reset))...)
	env.Runner.Agent(takeTask, done("Fixed on the second account"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	first := onlyExecution(t, waitFor(t, env, ana, id, dispatcherapi.StateRunning))
	if first.Account != factorytest.DefaultAccount {
		t.Fatalf("the first Execution is on %q", first.Account)
	}
	env.Clock.Advance(time.Second)
	open()

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[0].ID != first.ID {
		t.Fatalf("want two Executions, the first %s: %+v", first.ID, run.Executions)
	}
	limited, second := run.Executions[0], run.Executions[1]
	if limited.EndReason != dispatcherapi.EndUsageLimit {
		t.Errorf("the first Execution ended with %q, want %q", limited.EndReason, dispatcherapi.EndUsageLimit)
	}
	if second.Account != "pool-2" || run.Summary != "Fixed on the second account" {
		t.Errorf("the second Execution is on %q, and the Run is done with %q", second.Account, run.Summary)
	}

	// At the end of the first Execution, the Run was queued on the free
	// account at once.
	ended := eventsOf(env, id, "execution_ended")
	if len(ended) == 0 || ended[0]["run.state"] != dispatcherapi.StateQueued ||
		ended[0]["execution.account"] != factorytest.DefaultAccount {
		t.Errorf("the first execution_ended event: %v", ended)
	}
	switched := waitForEvent(t, env, id, "account_switched")
	if switched["account.old"] != factorytest.DefaultAccount || switched["account.new"] != "pool-2" ||
		switched["actor.kind"] != "dispatcher" || switched["run.state"] != dispatcherapi.StateQueued {
		t.Errorf("the account_switched event: %v", switched)
	}

	// The new Execution continues the same Agent session on the Run's
	// branch: it restored the Agent session (one download).
	started := eventsOf(env, id, "execution_started")
	if len(started) != 2 || started[0]["agent.session_id"] != started[1]["agent.session_id"] ||
		started[1]["execution.account"] != "pool-2" {
		t.Errorf("the execution_started events: %v", started)
	}
	if env.SolidTrace.Downloads() != 1 {
		t.Errorf("%d downloads of the Agent session, want 1: the second Execution did not resume it",
			env.SolidTrace.Downloads())
	}

	// The account is not free until its reset time: the Credential source
	// has its limit, and a new Run does not get it.
	if got := limitOf(t, env, factorytest.DefaultAccount); got != reset.UTC().Format("2006-01-02 15:04 UTC") {
		t.Errorf("pool-1 is at its limit until %q, want %s", got, reset)
	}
	env.Runner.Agent(takeTask, initLine(t), done("Fixed"), reply, result, waitForEnd)
	next := start(t, env, ana, "Fix the till")
	if got := onlyExecution(t, waitFor(t, env, ana, next, dispatcherapi.StateDone)).Account; got != "pool-2" {
		t.Errorf("a new Run got %q, want pool-2", got)
	}
}

func TestAUsageLimitWithNoAccountFreeWaitsAndContinuesAtTheResetTime(t *testing.T) {
	env, ana := newEnv(t)
	reset := env.Clock.Now().Add(2 * time.Hour)
	env.Runner.Agent(script(takeTask, initLine(t), usageLimit(reset))...)
	env.Runner.Agent(takeTask, done("Fixed after the reset"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
	if !strings.Contains(run.Reason, factorytest.DefaultAccount) || !strings.Contains(run.Reason, "2026-11-14 11:00 UTC") {
		t.Errorf("reason %q, want the account and its reset time", run.Reason)
	}
	if got := ana.CLI("runs", "get", id); !strings.Contains(got.Stdout, "2026-11-14 11:00 UTC") {
		t.Errorf("runs get does not show the reset time:\n%s", got.Stdout)
	}

	// A message waits on the Run (P4), and is delivered when it continues.
	got := ana.CLI("message", "--dispatcher", env.URL(), id, "Also fix the tax")
	m := acceptedLine.FindStringSubmatch(got.Stdout)
	if got.Code != 0 || m == nil || strings.Contains(got.Stdout, continuedLine) {
		t.Fatalf("solidtrace message: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}

	// One minute before the reset time, the Run still waits.
	env.Clock.Advance(2*time.Hour - time.Minute)
	time.Sleep(300 * time.Millisecond)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateWaitingOnAccountLimit || len(run.Executions) != 1 {
		t.Fatalf("before the reset time the Run is %s with %d Executions", run.State, len(run.Executions))
	}

	env.Clock.Advance(time.Minute)
	waitForExecutions(t, env, ana, id, 2)
	run = waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Fixed after the reset" || run.Executions[1].Account != factorytest.DefaultAccount {
		t.Errorf("the Run is done with %q, the second Execution on %q", run.Summary, run.Executions[1].Account)
	}
	if delivered := messageEvents(env, id, m[1])["message_delivered"]; len(delivered) != 1 ||
		delivered[0]["execution.id"] != run.Executions[1].ID {
		t.Errorf("the message's delivered events: %v", delivered)
	}
	// The Dispatcher's queued events (they give the reason for the wait for a
	// VM) carry no "continued_from": the continue has its own queued event.
	var continued []map[string]any
	for _, ev := range eventsOf(env, id, "queued") {
		if ev["run.continued_from"] == dispatcherapi.StateWaitingOnAccountLimit {
			continued = append(continued, ev)
		}
	}
	if len(continued) != 1 {
		t.Errorf("%d queued events continue from the account limit wait, want 1", len(continued))
	}
}

func TestTheResetTimeComesFromTheRateLimitEventOrIsOneHourWhenTheHarnessGivesNone(t *testing.T) {
	for name, c := range map[string]struct {
		lines []string
		want  string
	}{
		// Newer Claude Code versions say the time in words; the
		// rate_limit_event has it.
		"rate limit event": {script(rejected(time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)),
			limitLines("You've hit your limit · resets 1pm (UTC)")), "2026-11-14 13:00 UTC"},
		"no reset time": {limitLines("Claude AI usage limit reached"), "2026-11-14 10:00 UTC"},
		// A reset time that has passed is not a reset time.
		"a past reset time": {usageLimit(time.Date(2025, 11, 14, 20, 0, 0, 0, time.UTC)), "2026-11-14 10:00 UTC"},
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			env.Runner.Agent(script(takeTask, initLine(t), c.lines)...)
			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
			if !strings.Contains(run.Reason, c.want) {
				t.Errorf("reason %q, want the reset time %s", run.Reason, c.want)
			}
			if got := limitOf(t, env, factorytest.DefaultAccount); got != c.want {
				t.Errorf("pool-1 is at its limit until %q, want %s", got, c.want)
			}
		})
	}
}

// The third limit signal: an error result whose text names the limit, with
// no rate_limit_event and no assistant rate_limit error before it.
func TestAnErrorResultWithTheLimitTextAloneIsAUsageLimit(t *testing.T) {
	env, ana := newEnv(t)
	limited, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": true, "num_turns": 1,
		"result": "You've hit your limit · resets 1pm (UTC)", "session_id": "", "uuid": "", "total_cost_usd": 0})
	env.Runner.Agent(takeTask, initLine(t), string(limited), `{"fake":"exit","code":1}`)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
	if !strings.Contains(run.Reason, factorytest.DefaultAccount) {
		t.Errorf("reason %q, want the account", run.Reason)
	}
	if got := limitOf(t, env, factorytest.DefaultAccount); got != "2026-11-14 10:00 UTC" {
		t.Errorf("pool-1 is at its limit until %q, want the one-hour default", got)
	}
}

// A rejected rate_limit_event before a turn that works (the account uses
// its overage) is not a usage limit: the result is not an error.
func TestARejectedRateLimitEventBeforeASuccessfulResultIsNotAUsageLimit(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), rejected(env.Clock.Now().Add(time.Hour)), done("Fixed"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if e := onlyExecution(t, run); e.EndReason == dispatcherapi.EndUsageLimit {
		t.Errorf("the Execution ended with %q", e.EndReason)
	}
	if got := limitOf(t, env, factorytest.DefaultAccount); got != "-" {
		t.Errorf("pool-1 is at its limit until %q after a turn that worked", got)
	}
}

func TestAReportBeforeTheUsageLimitSetsTheOutcomeAndTheAccountIsAtItsLimit(t *testing.T) {
	env, ana := newEnv(t)
	reset := env.Clock.Now().Add(time.Hour)
	env.Runner.Agent(script(takeTask, initLine(t), done("Fixed"), usageLimit(reset))...)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Fixed" || len(run.Executions) != 1 {
		t.Errorf("the Run is done with %q in %+v", run.Summary, run.Executions)
	}
	if got := limitOf(t, env, factorytest.DefaultAccount); got != "2026-11-14 10:00 UTC" {
		t.Errorf("pool-1 is at its limit until %q", got)
	}
}

// waitAtLimit sets the fallback of cluster check 5: a Run stays on its
// account and waits for its reset.
func waitAtLimit(t *testing.T, env *factorytest.Env) {
	t.Helper()
	env.Config.AtAccountLimit = dispatcher.AtAccountLimitWait
	env.Restart()
}

func TestWithTheWaitSettingARunWaitsForItsOwnAccountWhenAnotherIsFree(t *testing.T) {
	env, ana := newEnv(t)
	waitAtLimit(t, env)
	env.AddAccount("pool-2", secondToken)
	// pool-1 is at its limit for a minute, so that the Run gets pool-2.
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(time.Minute))
	reset := env.Clock.Now().Add(time.Hour)
	env.Runner.Agent(script(takeTask, initLine(t), usageLimit(reset))...)
	env.Runner.Agent(takeTask, done("Fixed on its own account"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
	if onlyExecution(t, run).Account != "pool-2" {
		t.Fatalf("the Run is on %q", onlyExecution(t, run).Account)
	}
	// pool-1 is free, but the Run waits for pool-2.
	env.Clock.Advance(time.Minute)
	time.Sleep(300 * time.Millisecond)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateWaitingOnAccountLimit || len(run.Executions) != 1 {
		t.Fatalf("with pool-1 free, the Run is %s with %d Executions", run.State, len(run.Executions))
	}
	if len(eventsOf(env, id, "account_switched")) != 0 {
		t.Errorf("an account_switched event with the wait setting")
	}

	env.Clock.Advance(59 * time.Minute)
	run = waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[1].Account != "pool-2" {
		t.Errorf("the Run continued on %+v, want pool-2", run.Executions)
	}
}

func TestSwitchAccountMovesAWaitingRunToAFreeAccount(t *testing.T) {
	env, ana := newEnv(t)
	waitAtLimit(t, env)
	env.AddAccount("pool-2", secondToken)
	env.Runner.Agent(script(takeTask, initLine(t), usageLimit(env.Clock.Now().Add(time.Hour)))...)
	env.Runner.Agent(takeTask, done("Fixed on pool-2"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
	env.Clock.Advance(time.Second)

	got := switchAccount(env, ana, id)
	if got.Code != 0 || !strings.Contains(got.Stdout, "pool-2") || !strings.Contains(got.Stdout, "queued") {
		t.Fatalf("solidtrace switch-account: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[1].Account != "pool-2" || run.Summary != "Fixed on pool-2" {
		t.Fatalf("the Run is done with %q in %+v", run.Summary, run.Executions)
	}
	switched := waitForEvent(t, env, id, "account_switched")
	if switched["account.old"] != factorytest.DefaultAccount || switched["account.new"] != "pool-2" ||
		switched["actor.kind"] != "person" || switched["actor.name"] != "ana@example.com" {
		t.Errorf("the account_switched event: %v", switched)
	}

	// The Run is done now: only a Run that waits on an account limit
	// switches.
	if got := switchAccount(env, ana, id); got.Code == 0 || !strings.Contains(got.Stderr, "waits on an account limit") {
		t.Errorf("switch-account on a done Run: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
}

// With the wait setting, a queued Run whose own account is at its limit
// stays queued and waits for it; a person can still switch its account.
func TestWithTheWaitSettingSwitchAccountMovesAQueuedRunThatWaitsForItsAccount(t *testing.T) {
	env, ana := newEnv(t)
	waitAtLimit(t, env)
	env.AddAccount("pool-2", secondToken)
	env.Runner.Agent(takeTask, initLine(t), done("Fixed"), reply, result, waitForEnd)
	env.Runner.Agent(takeTask, done("Fixed the tax on pool-2"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	if got := onlyExecution(t, waitFor(t, env, ana, id, dispatcherapi.StateDone)).Account; got != factorytest.DefaultAccount {
		t.Fatalf("the Run is on %q", got)
	}
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(time.Hour))
	if got := ana.CLI("message", "--dispatcher", env.URL(), id, "Also fix the tax"); got.Code != 0 {
		t.Fatalf("solidtrace message: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	time.Sleep(300 * time.Millisecond)
	run := getRun(t, ana, id)
	if run.State != dispatcherapi.StateQueued || len(run.Executions) != 1 || !strings.Contains(run.Reason, "waits for it") {
		t.Fatalf("the Run is %s (%s) with %d Executions, want queued for its account", run.State, run.Reason,
			len(run.Executions))
	}
	env.Clock.Advance(time.Second)

	got := switchAccount(env, ana, id)
	if got.Code != 0 || !strings.Contains(got.Stdout, "pool-2") {
		t.Fatalf("solidtrace switch-account: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	run = waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[1].Account != "pool-2" || run.Summary != "Fixed the tax on pool-2" {
		t.Fatalf("the Run is done with %q in %+v", run.Summary, run.Executions)
	}
	switched := waitForEvent(t, env, id, "account_switched")
	if switched["account.old"] != factorytest.DefaultAccount || switched["account.new"] != "pool-2" {
		t.Errorf("the account_switched event: %v", switched)
	}
}

func TestSwitchAccountWithNoOtherAccountFreeIsRefused(t *testing.T) {
	env, ana := newEnv(t)
	waitAtLimit(t, env)
	env.AddAccount("pool-2", secondToken)
	env.MarkAtLimit("pool-2", env.Clock.Now().Add(2*time.Hour))
	env.Runner.Agent(script(takeTask, initLine(t), usageLimit(env.Clock.Now().Add(time.Hour)))...)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
	got := switchAccount(env, ana, id)
	if got.Code == 0 || !strings.Contains(got.Stderr, "no other account is free") ||
		!strings.Contains(got.Stderr, "2026-11-14 10:00 UTC") {
		t.Errorf("solidtrace switch-account: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateWaitingOnAccountLimit || len(run.Executions) != 1 {
		t.Errorf("the refused switch changed the Run: %s with %d Executions", run.State, len(run.Executions))
	}
	if len(eventsOf(env, id, "account_switched")) != 0 {
		t.Errorf("a refused switch has an account_switched event")
	}
}

func TestStopOnARunThatWaitsOnAnAccountLimitStopsIt(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(script(takeTask, initLine(t), usageLimit(env.Clock.Now().Add(time.Hour)))...)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnAccountLimit)
	if got := ana.CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateStopped {
		t.Fatalf("the Run is %s, want stopped", run.State)
	}
	// After the reset time, the stopped Run does not continue.
	env.Clock.Advance(time.Hour)
	time.Sleep(300 * time.Millisecond)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateStopped || len(run.Executions) != 1 {
		t.Errorf("after the reset time the Run is %s with %d Executions", run.State, len(run.Executions))
	}
}

// A Run that waits on an account limit is active: a delivery with its
// dedupe_key gives that Run and starts none, so two Runs do not work on
// the same Issue when the reset time comes.
func TestARunThatWaitsOnAnAccountLimitKeepsItsDedupeKey(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Alerts")
	env.Runner.Agent(script(takeTask, initLine(t), usageLimit(env.Clock.Now().Add(2*time.Hour)))...)

	waiting := deliver(t, env, path, sender.Secret, "d-1", "issue-1")
	waitFor(t, env, ana, waiting.RunID, dispatcherapi.StateWaitingOnAccountLimit)
	if again := deliver(t, env, path, sender.Secret, "d-2", "issue-1"); again.Started || again.RunID != waiting.RunID {
		t.Errorf("the dedupe_key of a Run that waits on an account limit: %+v, want the Run %s", again, waiting.RunID)
	}
}
