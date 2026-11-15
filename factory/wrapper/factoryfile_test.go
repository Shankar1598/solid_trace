package wrapper_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// factoryFile is the path of the factory file in a folder ("" is the
// root).
func factoryFile(folder string) string { return filepath.Join(folder, ".solidtrace/factory.toml") }

// reportsDone is an agent that reports done at once.
func reportsDone() []string {
	return []string{takeTask, reportStep("--outcome", "done", "--summary", "Done"), reply, result, waitForEnd}
}

// runDone starts a Run of the Project shop with flags, waits until it is
// done, and gives its one Execution.
func runDone(t *testing.T, env *factorytest.Env, ana *factorytest.Person, flags ...string) dispatcherapi.Execution {
	t.Helper()
	env.Runner.Agent(reportsDone()...)
	id := startOn(t, env, ana, "Fix the cart", flags...)
	return onlyExecution(t, waitFor(t, env, ana, id, dispatcherapi.StateDone))
}

func TestTheFactoryFileInTheFolderWinsOverTheOneAtTheRoot(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{
		factoryFile("apps/shop"): `machine_kind = "large"`,
		factoryFile(""):          `machine_kind = "standard"`,
		"apps/admin/README.md":   "admin",
	}, "Two factory files")

	saveSettings(t, env, "apps/shop", "")
	if kind := env.Runner.MachineKind(runDone(t, env, ana).ID); kind != "large" {
		t.Errorf("with a file in the folder, the machine kind is %q, want the folder's large", kind)
	}
	saveSettings(t, env, "apps/admin", "")
	if kind := env.Runner.MachineKind(runDone(t, env, ana).ID); kind != "standard" {
		t.Errorf("with no file in the folder, the machine kind is %q, want the root's standard", kind)
	}
}

func TestAChangeOnABranchAppliesOnlyAfterAMergeToTheDefaultBranch(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): `machine_kind = "standard"`}, "A factory file")
	env.Forge.Push("example/shop", "feature", map[string]string{factoryFile(""): `machine_kind = "large"`}, "A bigger machine")

	// Also a Run whose base branch is the branch.
	if kind := env.Runner.MachineKind(runDone(t, env, ana, "--branch", "feature").ID); kind != "standard" {
		t.Errorf("before the merge, the machine kind is %q, want the default branch's standard", kind)
	}
	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): `machine_kind = "large"`}, "Merge feature")
	if kind := env.Runner.MachineKind(runDone(t, env, ana).ID); kind != "large" {
		t.Errorf("after the merge, the machine kind of the next Run is %q, want large", kind)
	}
}

func TestTheFactoryFileReachesTheExecution(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): `
machine_kind = "large"
setup = "echo ready > .setup-done"
save_exclude = ["tmp/", "*.log"]

[limits]
idle = "45m"
total = "12h"

[[checks]]
name = "test"
command = "go test ./..."

[[checks]]
name = "lint"
command = "golangci-lint run"
`}, "A factory file")
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reportStep("--outcome", "done", "--summary", "Done"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	execution := waitFor(t, env, ana, id, dispatcherapi.StateRunning).Executions[0].ID

	// The task call again, as the wrapper: it gives the same task.
	a := env.API(env.Runner.Identity(factorytest.Workload(execution)), "POST", "/api/v1/executions/"+execution+"/task",
		map[string]string{})
	if a.Status != http.StatusOK {
		t.Fatalf("task call: %d %s", a.Status, a.Body)
	}
	var task dispatcherapi.Task
	a.JSON(t, &task)
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if kind := env.Runner.MachineKind(execution); kind != "large" {
		t.Errorf("machine kind %q", kind)
	}
	if task.FactoryFile != ".solidtrace/factory.toml" || task.Setup != "echo ready > .setup-done" {
		t.Errorf("factory file %q, setup %q", task.FactoryFile, task.Setup)
	}
	wantChecks := []dispatcherapi.Check{{Name: "test", Command: "go test ./..."}, {Name: "lint", Command: "golangci-lint run"}}
	if !slices.Equal(task.Checks, wantChecks) {
		t.Errorf("checks %+v", task.Checks)
	}
	if task.Limits != (dispatcherapi.Limits{IdleSeconds: 45 * 60, TotalSeconds: 12 * 3600}) {
		t.Errorf("limits %+v", task.Limits)
	}
	if !slices.Equal(task.SaveExclude, []string{"tmp/", "*.log"}) {
		t.Errorf("save exclusions %q", task.SaveExclude)
	}
}

func TestARepositoryWithNoFactoryFileGetsTheDefaults(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reportStep("--outcome", "done", "--summary", "Done"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	execution := waitFor(t, env, ana, id, dispatcherapi.StateRunning).Executions[0].ID
	a := env.API(env.Runner.Identity(factorytest.Workload(execution)), "POST", "/api/v1/executions/"+execution+"/task",
		map[string]string{})
	var task dispatcherapi.Task
	a.JSON(t, &task)
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if task.FactoryFile != "" || task.Setup != "" || task.Checks != nil ||
		task.Limits != (dispatcherapi.Limits{IdleSeconds: 30 * 60, TotalSeconds: 8 * 3600}) {
		t.Errorf("task with no factory file: %+v", task)
	}
	if kind := env.Runner.MachineKind(execution); kind != "" {
		t.Errorf("machine kind %q, want the operator's default", kind)
	}
}

func TestAFactoryFileWithAnErrorFailsTheRunWithAClearReason(t *testing.T) {
	env, ana := newEnv(t)
	cases := []struct {
		name, file string
		want       []string
	}{
		{"not TOML", "machine_kind = \"large\nsetup = 1", []string{"line 1"}},
		{"an unknown key", `machine = "large"`, []string{"unknown key machine"}},
		{"a value of the wrong type", `setup = 3`, []string{"setup"}},
		{"a machine kind that the operator has not", `machine_kind = "huge"`,
			[]string{`"huge" is not a machine kind`, "standard, large"}},
		{"a limit that is not a duration", "[limits]\nidle = \"soon\"", []string{"limits.idle", `"soon"`}},
		{"a limit under a minute", "[limits]\ntotal = \"30s\"", []string{"limits.total"}},
		{"a URL in place of a host", `allowed_hosts = ["https://example.com/x"]`, []string{"allowed_hosts", "https://example.com/x"}},
		{"an IP address", `allowed_hosts = ["10.100.0.3"]`, []string{"allowed_hosts", "10.100.0.3"}},
		{"a check with no command", "[[checks]]\nname = \"test\"", []string{"the check test has no command"}},
		{"two checks of one name", "[[checks]]\nname = \"test\"\ncommand = \"a\"\n[[checks]]\nname = \"test\"\ncommand = \"b\"",
			[]string{"two checks have the name test"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): c.file}, "A factory file with an error")
			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
			want := append([]string{"the factory file .solidtrace/factory.toml on the default branch of " +
				"bitbucket.org/example/shop is not valid: "}, c.want...)
			for _, w := range want {
				if !strings.Contains(run.Reason, w) {
					t.Errorf("the reason %q does not say %q", run.Reason, w)
				}
			}
		})
	}
	if got := env.Runner.Dispatched(); len(got) != 0 {
		t.Errorf("the Runner started %d Executions of Runs whose factory file has an error", len(got))
	}
}

func TestAFactoryFileThatTheForgeDoesNotGiveFailsTheRunWithTheReason(t *testing.T) {
	env, ana := newEnv(t)
	// The connection's token is not the Forge's token.
	if a := env.Admin().API("PUT", "/api/v1/connections/"+factorytest.ForgeConnection, map[string]string{
		"kind": "bitbucket", "token": "a-token-that-the-forge-refuses"}); a.Status/100 != 2 {
		t.Fatalf("save the connection: %d %s", a.Status, a.Body)
	}
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if !strings.Contains(run.Reason, "read the factory file") || !strings.Contains(run.Reason, "401") ||
		!strings.Contains(run.Reason, "solidtrace connections set") {
		t.Errorf("reason %q", run.Reason)
	}
	if strings.Contains(run.Reason, "a-token-that-the-forge-refuses") {
		t.Errorf("the reason has the token: %q", run.Reason)
	}
}

func TestTheSetupCommandRunsInTheFolderBeforeTheHarnessStarts(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{
		factoryFile(""):       `setup = "test ! -e .harness-ran && pwd > .setup-ran"`,
		"apps/shop/README.md": "shop",
	}, "A setup command")
	saveSettings(t, env, "apps/shop", "")
	env.Runner.Agent(takeTask, shell(`touch .harness-ran && test "$(cat .setup-ran)" = "$PWD" && `+
		`solidtrace report --outcome done --summary "Set up"`), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
}

func TestAFailedSetupCommandFailsTheRunWithItsReason(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{
		factoryFile(""): `setup = "echo the database is not there >&2; exit 3"`,
	}, "A setup command that fails")
	ran := filepath.Join(t.TempDir(), "harness-ran")
	env.Runner.Agent(takeTask, shell("touch "+ran), reportStep("--outcome", "done", "--summary", "Done"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	for _, want := range []string{dispatcherapi.EndSetupFailed + ": the setup command", "exit status 3",
		"the database is not there"} {
		if !strings.Contains(run.Reason, want) {
			t.Errorf("the reason %q does not say %q", run.Reason, want)
		}
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("the harness started after the setup command failed")
	}
}

func TestTheIdleLimitOfTheFactoryFileReplacesTheDefault(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): "[limits]\nidle = \"45m\""}, "More time")
	here, reached := marker(t)
	env.Runner.Agent(takeTask, initLine(t), here, inToolCall, interrupted, thenEOF)
	id := start(t, env, ana, "Fix the cart")
	reached()
	time.Sleep(settle)

	env.Clock.Advance(45*time.Minute - time.Second)
	stillRunning(t, env, ana, id, "44m59s with no output, past the default of 30m")
	env.Clock.Advance(time.Second)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if !strings.HasPrefix(run.Reason, dispatcherapi.EndTimeout+": ") || !strings.Contains(run.Reason, "45m0s") {
		t.Errorf("the reason is %q, want timeout with the idle limit of the file", run.Reason)
	}
}

func TestTheAllowlistHoldsTheDefaultListAndTheExtraHostsOfEachRepositoryThatAProjectNames(t *testing.T) {
	env, ana := newEnv(t)
	env.Project("admin")
	u, _ := url.Parse(env.SolidTrace.URL())
	defaults := "api.anthropic.com\n.bitbucket.org\nproxy.golang.org\n" + u.Hostname()
	if got := env.Allowlist(); got != defaults {
		t.Errorf("at the start, the allowlist is\n%s\nwant\n%s", got, defaults)
	}
	reloads := env.SquidReloads()
	if reloads != 1 {
		t.Errorf("Squid got %d reloads for the first allowlist", reloads)
	}

	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""):
	// A wildcard, and a host that it matches already.
	`allowed_hosts = ["api.stripe.com", "*.Example.com", "www.example.com"]`}, "Extra hosts")
	env.Forge.Push("example/admin", "main", map[string]string{factoryFile(""): `allowed_hosts = ["api.stripe.com", ".sentry.io"]`},
		"Extra hosts")

	// The agent of the Run has its repository's hosts already.
	env.Runner.Agent(takeTask, shell(`grep -qx api.stripe.com `+env.Config.SquidAllowlist+` && `+
		`solidtrace report --outcome done --summary "Reached it"`), reply, result, waitForEnd)
	waitFor(t, env, ana, start(t, env, ana, "Fix the cart"), dispatcherapi.StateDone)
	shop := defaults + "\n.example.com\napi.stripe.com"
	if got := env.Allowlist(); got != shop {
		t.Errorf("after a Run of shop, the allowlist is\n%s\nwant\n%s", got, shop)
	}
	if got := env.SquidReloads(); got != reloads+1 {
		t.Errorf("Squid got %d reloads after a change, want %d", got, reloads+1)
	}

	// The same hosts again: no change, no reload.
	runDone(t, env, ana)
	env.Restart()
	if got := env.SquidReloads(); got != reloads+1 {
		t.Errorf("Squid got %d reloads with no change, want %d", got, reloads+1)
	}

	env.Runner.Agent(reportsDone()...)
	got := ana.CLI("start", "--dispatcher", env.URL(), "--project", "admin", "Fix the admin")
	m := queuedLine.FindStringSubmatch(got.Stdout)
	if m == nil {
		t.Fatalf("start a Run of admin: %s%s", got.Stdout, got.Stderr)
	}
	waitFor(t, env, ana, m[1], dispatcherapi.StateDone)
	both := defaults + "\n.example.com\n.sentry.io\napi.stripe.com"
	if got := env.Allowlist(); got != both {
		t.Errorf("after a Run of admin, the allowlist is\n%s\nwant\n%s", got, both)
	}

	// A repository that no Project names now gives no host.
	env.Forge.AddRepo("example/other", nil)
	if a := env.Admin().API("PUT", "/api/v1/projects/admin/settings", map[string]string{
		"repository": "bitbucket.org/example/other", "connection": factorytest.ForgeConnection}); a.Status != http.StatusOK {
		t.Fatalf("save the settings of admin: %d %s", a.Status, a.Body)
	}
	if got := env.Allowlist(); got != shop {
		t.Errorf("after admin named another repository, the allowlist is\n%s\nwant\n%s", got, shop)
	}
	if got := env.SquidReloads(); got != reloads+3 {
		t.Errorf("Squid got %d reloads, want %d", got, reloads+3)
	}
}
