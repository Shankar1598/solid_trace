package wrapper_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/wrapper"
)

const secondToken = "sk-ant-oat01-factorytest-pool-2"

// recordToken is a step that appends the agent's working directory and
// the account token in its environment to file.
func recordToken(file string) string {
	return shell(`echo "$PWD ${CLAUDE_CODE_OAUTH_TOKEN:-none}" >> ` + file)
}

func done(summary string) string {
	return reportStep("--outcome", "done", "--summary", summary)
}

func TestTwoExecutionsAtTheSameTimeGetTwoAccountsWhenTwoAreFree(t *testing.T) {
	env, ana := newEnv(t)
	env.AddAccount("pool-2", secondToken)
	tokens := filepath.Join(t.TempDir(), "tokens")
	wait, open := gate(t)
	for range 2 {
		env.Runner.Agent(takeTask, initLine(t), recordToken(tokens), wait, done("Fixed"), reply, result, waitForEnd)
	}

	first := start(t, env, ana, "Fix the cart")
	second := start(t, env, ana, "Fix the basket")
	runs := []dispatcherapi.Run{
		waitFor(t, env, ana, first, dispatcherapi.StateRunning),
		waitFor(t, env, ana, second, dispatcherapi.StateRunning),
	}
	a, b := onlyExecution(t, runs[0]).Account, onlyExecution(t, runs[1]).Account
	if a == b || a == "" || b == "" {
		t.Errorf("the two live Executions got the accounts %q and %q", a, b)
	}
	open()
	for _, id := range []string{first, second} {
		waitFor(t, env, ana, id, dispatcherapi.StateDone)
	}

	// Each harness got the token of its own Execution's account.
	want := map[string]string{factorytest.DefaultAccount: factorytest.DefaultAccountToken, "pool-2": secondToken}
	recorded, _ := os.ReadFile(tokens)
	for _, run := range runs {
		e := onlyExecution(t, run)
		line := ""
		for _, l := range strings.Split(string(recorded), "\n") {
			if strings.Contains(l, e.ID) {
				line = l
			}
		}
		if !strings.HasSuffix(line, " "+want[e.Account]) {
			t.Errorf("the harness of the Execution on %s has %q, want the token %s", e.Account, line, want[e.Account])
		}
	}
}

func TestAnAccountAtItsLimitIsNotGivenToANewRunBeforeItsResetTime(t *testing.T) {
	env, ana := newEnv(t)
	env.AddAccount("pool-2", secondToken)
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(30*time.Minute))
	wait, open := gate(t)
	for range 3 {
		env.Runner.Agent(takeTask, initLine(t), wait, done("Fixed"), reply, result, waitForEnd)
	}

	// pool-2 has a live Execution and pool-1 has none, but pool-1 is at
	// its limit: both Runs get pool-2.
	first := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, first, dispatcherapi.StateRunning)
	second := start(t, env, ana, "Fix the basket")
	for _, id := range []string{first, second} {
		if got := onlyExecution(t, waitFor(t, env, ana, id, dispatcherapi.StateRunning)).Account; got != "pool-2" {
			t.Errorf("Run %s is on %q, want pool-2", id, got)
		}
	}

	// After the reset time, pool-1 is free and has the fewest live
	// Executions.
	env.Clock.Advance(30 * time.Minute)
	third := start(t, env, ana, "Fix the till")
	if got := onlyExecution(t, waitFor(t, env, ana, third, dispatcherapi.StateRunning)).Account; got != factorytest.DefaultAccount {
		t.Errorf("the Run after the reset time is on %q, want %s", got, factorytest.DefaultAccount)
	}
	open()
	for _, id := range []string{first, second, third} {
		waitFor(t, env, ana, id, dispatcherapi.StateDone)
	}
}

func TestARunWithNoFreeAccountStaysQueuedAndItsPageSaysWhy(t *testing.T) {
	env, ana := newEnv(t)
	reset := env.Clock.Now().Add(90 * time.Minute)
	env.MarkAtLimit(factorytest.DefaultAccount, reset)
	env.Runner.Agent(takeTask, initLine(t), done("Fixed"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	// The dispatch loop looks at the queue each 50 ms: give it some turns.
	time.Sleep(300 * time.Millisecond)
	run := getRun(t, ana, id)
	if run.State != dispatcherapi.StateQueued || len(run.Executions) != 0 {
		t.Fatalf("the Run is %s with %d Executions, want queued with none", run.State, len(run.Executions))
	}
	if !strings.Contains(run.Reason, "no free account") || !strings.Contains(run.Reason, "2026-11-14 10:30 UTC") {
		t.Errorf("reason %q, want no free account and the reset time", run.Reason)
	}
	if got := ana.CLI("runs", "get", id); !strings.Contains(got.Stdout, "no free account") {
		t.Errorf("runs get does not say why:\n%s", got.Stdout)
	}

	// At the reset time the account is free again, and the Run starts.
	env.Clock.Advance(90 * time.Minute)
	run = waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Reason != "" || onlyExecution(t, run).Account != factorytest.DefaultAccount {
		t.Errorf("reason %q, account %q", run.Reason, onlyExecution(t, run).Account)
	}
}

func TestARunWaitsForAnAccountWhenThePoolHasNone(t *testing.T) {
	env, ana := newEnv(t)
	env.RemoveAccount(factorytest.DefaultAccount)
	env.Runner.Agent(takeTask, initLine(t), done("Fixed"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	time.Sleep(300 * time.Millisecond)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateQueued || !strings.Contains(run.Reason, "dispatcher accounts add") {
		t.Fatalf("the Run is %s (%s), want queued with the operator command in the reason", run.State, run.Reason)
	}

	env.AddAccount("pool-2", secondToken)
	if run := waitFor(t, env, ana, id, dispatcherapi.StateDone); onlyExecution(t, run).Account != "pool-2" {
		t.Errorf("account %q", onlyExecution(t, run).Account)
	}
}

func TestTheAccountTokenReachesTheHarnessAndNoOtherProcess(t *testing.T) {
	// A key of the machine's environment never reaches a process of the
	// Execution: it would win over the account's token.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-of-the-machine")
	env, ana := newEnv(t)
	// git records its environment, then runs the real git.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	gitEnv := filepath.Join(dir, "git-env")
	script := "#!/bin/sh\nenv >> " + gitEnv + "\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env.Runner.PathFirst(dir)
	env.Runner.Agent(takeTask, initLine(t),
		shell(`test "$CLAUDE_CODE_OAUTH_TOKEN" = `+factorytest.DefaultAccountToken+` && test -z "$ANTHROPIC_API_KEY" && `+
			`solidtrace report --outcome done --summary "The token is here"`),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	e := onlyExecution(t, run)
	if b, err := os.ReadFile(gitEnv); err != nil || len(b) == 0 {
		t.Fatalf("the wrapper ran no git from the PATH: %v", err)
	} else if strings.Contains(string(b), factorytest.DefaultAccountToken) || strings.Contains(string(b), "ANTHROPIC_API_KEY") {
		t.Errorf("git's environment has a Claude credential")
	}
	// No file of the Execution holds the token, and no output shows it.
	root := filepath.Dir(env.Runner.Socket(e.ID))
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), factorytest.DefaultAccountToken) {
				t.Errorf("%s holds the account's token", path)
			}
		}
		return nil
	})
	if strings.Contains(env.Runner.Fake.Output(e.ID), factorytest.DefaultAccountToken) {
		t.Errorf("the wrapper's output shows the account's token")
	}
}

func TestARunWhoseAccountIsRemovedBeforeTheTaskCallGoesBackInTheQueueAndGetsAFreeAccount(t *testing.T) {
	env, ana := newEnv(t)
	env.AddAccount("pool-2", secondToken)
	tokens := filepath.Join(t.TempDir(), "tokens")
	// The first Execution's wrapper gets no task, so its script never
	// plays; the second Execution plays the second script.
	for range 2 {
		env.Runner.Agent(takeTask, initLine(t), recordToken(tokens), done("Fixed"), reply, result, waitForEnd)
	}
	// The operator removes pool-1 after the dispatch on it, before the
	// wrapper's task call. The clock moves on, so that the second
	// Execution has a later dispatch time: the Run lists its Executions
	// in that order, and with equal times in the order of their random ids.
	env.Runner.BeforeDispatch(func(string) {
		env.RemoveAccount(factorytest.DefaultAccount)
		env.Clock.Advance(time.Second)
	})

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 {
		t.Fatalf("want two Executions, got %+v", run.Executions)
	}
	first, second := run.Executions[0], run.Executions[1]
	if first.Account != factorytest.DefaultAccount || !strings.Contains(first.EndReason, "no longer in the pool") {
		t.Errorf("the first Execution is on %q and ended with %q", first.Account, first.EndReason)
	}
	if second.Account != "pool-2" {
		t.Errorf("the second Execution is on %q, want pool-2", second.Account)
	}
	if recorded, _ := os.ReadFile(tokens); !strings.Contains(string(recorded), second.ID) ||
		!strings.Contains(string(recorded), secondToken) || strings.Contains(string(recorded), first.ID) {
		t.Errorf("the harness tokens: %q", recorded)
	}
}

func TestAnAddWithTheNameOfAnAccountReplacesItsTokenAndKeepsItsLimit(t *testing.T) {
	env, ana := newEnv(t)
	reset := env.Clock.Now().Add(time.Hour)
	env.MarkAtLimit(factorytest.DefaultAccount, reset)
	const newToken = "sk-ant-oat01-factorytest-pool-1-new"
	got := env.Operator(newToken+"\n", "add", factorytest.DefaultAccount)
	if got.Code != 0 || !strings.Contains(got.Stdout, "replaced") {
		t.Fatalf("add again: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	if list := env.Operator("", "list"); !strings.Contains(list.Stdout, "2026-11-14 10:00 UTC") {
		t.Errorf("the list does not show the limit after the replace:\n%s", list.Stdout)
	}

	tokens := filepath.Join(t.TempDir(), "tokens")
	env.Runner.Agent(takeTask, initLine(t), recordToken(tokens), done("Fixed"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	time.Sleep(300 * time.Millisecond)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateQueued {
		t.Fatalf("the Run is %s, want queued: the account is at its limit", run.State)
	}
	env.Clock.Advance(time.Hour)
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if recorded, _ := os.ReadFile(tokens); !strings.HasSuffix(strings.TrimSpace(string(recorded)), " "+newToken) {
		t.Errorf("the harness has %q, want the new token", recorded)
	}
}

// A Dispatcher that sends no account token must not start the harness: the
// harness would use another login of the machine, or none.
func TestTheWrapperRefusesATaskWithNoAccountToken(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	for _, args := range [][]string{{"init", "--quiet", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "--allow-empty", "-m", "start"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v\n%s", args, err, out)
		}
	}
	started := filepath.Join(dir, "harness-started")
	harness := filepath.Join(dir, "harness")
	if err := os.WriteFile(harness, []byte("#!/bin/sh\ntouch "+started+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(dir, "identity")
	if err := os.WriteFile(identity, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The fake Dispatcher's task has all but the account token.
	ends := make(chan dispatcherapi.ExecutionEnd, 1)
	dispatcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case dispatcherapi.Path + "/executions/x1/task":
			json.NewEncoder(w).Encode(dispatcherapi.Task{RunID: "r1", ExecutionID: "x1", Prompt: "Fix the cart",
				Repository: "example.com/shop/shop", CloneURL: repo, SessionID: "00000000-0000-4000-8000-000000000001"})
		case dispatcherapi.Path + "/executions/x1/end":
			var end dispatcherapi.ExecutionEnd
			json.NewDecoder(r.Body).Decode(&end)
			ends <- end
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer dispatcher.Close()

	values := map[string]string{wrapper.EnvDispatcherURL: dispatcher.URL, wrapper.EnvExecution: "x1",
		wrapper.EnvIdentityFile: identity, wrapper.EnvDir: filepath.Join(dir, "execution"), wrapper.EnvHarness: harness}
	var stderr strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if code := wrapper.Main(ctx, func(name string) string { return values[name] }, os.Environ(), &stderr); code != 0 {
		t.Fatalf("the wrapper exits %d: %s", code, stderr.String())
	}
	if end := <-ends; end.Reason != dispatcherapi.EndSetupFailed || !strings.Contains(end.Error, "the task has no account token") {
		t.Errorf("the end is %+v, want %s with no account token", end, dispatcherapi.EndSetupFailed)
	}
	if _, err := os.Stat(started); err == nil {
		t.Errorf("the harness started with no account token")
	}
}
