package wrapper_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// saveSettings saves the Factory settings of the Project shop, with the
// fake Forge's connection.
func saveSettings(t *testing.T, env *factorytest.Env, folder, baseBranch string) {
	t.Helper()
	a := env.Admin().API("PUT", "/api/v1/projects/shop/settings", map[string]string{
		"repository": "bitbucket.org/example/shop", "folder": folder, "base_branch": baseBranch,
		"connection": factorytest.ForgeConnection})
	if a.Status != http.StatusOK {
		t.Fatalf("save the Factory settings: %d %s", a.Status, a.Body)
	}
}

// startOn runs 'solidtrace start' for the Project shop with more flags,
// and gives the Run id.
func startOn(t *testing.T, env *factorytest.Env, p *factorytest.Person, prompt string, flags ...string) string {
	t.Helper()
	args := append([]string{"start", "--dispatcher", env.URL(), "--project", "shop"}, flags...)
	got := p.CLI(append(args, prompt)...)
	m := queuedLine.FindStringSubmatch(got.Stdout)
	if got.Code != 0 || m == nil {
		t.Fatalf("solidtrace start: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	return m[1]
}

// commitAndReport is the agent's work: a commit of a file that it does
// not push, and a report of done.
func commitAndReport(file string) []string {
	return []string{takeTask, shell(`echo work > ` + file + ` && git add ` + file + ` && git commit -q -m "Do the work" && ` +
		`solidtrace report --outcome done --summary "Did the work"`), reply, result, waitForEnd}
}

func TestTheFirstExecutionPushesItsCommitsToTheRunsBranchFromTheBaseBranch(t *testing.T) {
	cases := []struct {
		name, defaultBase string
		flags             []string
		base              string
	}{
		{name: "the base branch of the start", defaultBase: "develop", flags: []string{"--branch", "release"}, base: "release"},
		{name: "the Project's default base branch", defaultBase: "develop", base: "develop"},
		{name: "the repository's default branch", base: "main"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, ana := newEnv(t)
			for _, branch := range []string{"release", "develop"} {
				env.Forge.Push("example/shop", branch, map[string]string{branch + ".txt": branch}, "Start "+branch)
			}
			saveSettings(t, env, "", c.defaultBase)
			base, _ := env.Forge.Ref("example/shop", "refs/heads/"+c.base)
			env.Runner.Agent(commitAndReport("work.txt")...)

			id := startOn(t, env, ana, "Do the work", c.flags...)
			waitFor(t, env, ana, id, dispatcherapi.StateDone)

			branch := "refs/heads/" + dispatcherapi.RunBranch(id)
			tip, ok := env.Forge.Ref("example/shop", branch)
			if !ok {
				t.Fatalf("the remote has no %s after the end: %v\n%s", branch, env.Forge.Refs("example/shop"),
					env.Runner.Fake.Output(env.Runner.Dispatched()[0]))
			}
			if got := strings.TrimSpace(env.Forge.Git("example/shop", "rev-parse", tip+"^")); got != base {
				t.Errorf("the Run's branch starts from %s, not from %s (%s)", got, c.base, base)
			}
			if body, _ := env.Forge.File("example/shop", branch, "work.txt"); body != "work\n" {
				t.Errorf("the agent's commit on the remote: work.txt is %q", body)
			}
		})
	}
}

func TestTheAgentWorksInTheProjectsFolderOfTheWholeCheckoutAtAFixedPath(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{"apps/shop/app.rb": "app", "apps/admin/app.rb": "admin"}, "Two apps")
	saveSettings(t, env, "apps/shop", "")
	out := filepath.Join(t.TempDir(), "where")
	env.Runner.Agent(takeTask, shell(`{ pwd; git rev-parse --show-toplevel; test -f ../admin/app.rb && echo whole; } > `+out+
		` && solidtrace report --outcome done --summary "Looked"`), reply, result, waitForEnd)

	id := start(t, env, ana, "Look around")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	checkout := filepath.Join(env.Runner.Workspace(onlyExecution(t, run).ID), "bitbucket.org", "example", "shop")
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(checkout, "apps", "shop") + "\n" + checkout + "\nwhole\n"
	if string(b) != want {
		t.Errorf("the agent's working directory, top level and the other app:\n%s\nwant:\n%s", b, want)
	}
}

func TestASubmoduleWithAnSSHURLClonesOverHTTPSWithTheCredentialHelper(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.AddRepo("example/lib", map[string]string{"lib.rb": "lib"})
	env.Forge.AddSubmodule("example/shop", "main", "vendor/lib", "git@bitbucket.org:example/lib.git", "example/lib")
	env.Forge.AddRepo("example/gem", map[string]string{"gem.rb": "gem"})
	gem := filepath.Join(t.TempDir(), "gem")
	// The agent's own clone of a private gem over ssh goes over HTTPS too.
	env.Runner.Agent(takeTask, shell(`test -f vendor/lib/lib.rb && git clone -q ssh://git@bitbucket.org/example/gem.git `+gem+
		` && test -f `+gem+`/gem.rb && solidtrace report --outcome done --summary "Both"`), reply, result, waitForEnd)

	id := start(t, env, ana, "Use the library")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Both" {
		t.Errorf("the Run: %+v", run)
	}
}

func TestTheEndDeletesTheWorkspaceTheRunUsersHomeAndTheSecretsAndKeepsTheCaches(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, shell(`mkdir -p "$XDG_CACHE_HOME/gems" && echo kept > "$XDG_CACHE_HOME/gems/rack" && `+
		`echo gone > "$HOME/notes" && test "$CLAUDE_CONFIG_DIR" = "$HOME/.claude" && `+
		`git config --global --get-regexp "^credential\\..*\\.helper$" | grep -q /secrets/git-credential && `+
		`solidtrace report --outcome done --summary "Cached"`), reply, result, waitForEnd)

	id := start(t, env, ana, "Fill the cache")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Cached" {
		t.Fatalf("the Run: %+v", run)
	}
	execution := onlyExecution(t, run).ID
	// The secrets folder (WRAPPER_DIR/secrets, beside the agent's socket)
	// holds the forge token and the credential helper.
	secrets := filepath.Join(filepath.Dir(env.Runner.Socket(execution)), "secrets")
	// The wrapper deletes them after its end call: the end call's answer
	// can be a take-over, which uses the workspace and the home.
	deadline := time.Now().Add(10 * time.Second)
	for _, dir := range []string{env.Runner.Workspace(execution), env.Runner.Home(execution), secrets} {
		for ; ; time.Sleep(20 * time.Millisecond) {
			_, err := os.Stat(dir)
			if os.IsNotExist(err) {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("%s is there 10 s after the end (%v)", dir, err)
				break
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(env.Runner.Cache(), "gems", "rack")); err != nil || string(b) != "kept\n" {
		t.Errorf("the cache after the end: %q, %v", b, err)
	}
}

func TestTheWrapperChangesNoFileOfTheCheckout(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{
		"CLAUDE.md":             "# Shop\nRun the tests.\n",
		".claude/settings.json": `{"permissions":{"deny":["Bash(rm:*)"]}}` + "\n",
	}, "The agent's behaviour")
	env.Runner.Agent(takeTask, shell(`test -z "$(git status --porcelain --ignored)" && git diff --quiet origin/main HEAD && `+
		`test "$(cat CLAUDE.md)" = "# Shop
Run the tests." && solidtrace report --outcome done --summary "Clean"`), reply, result, waitForEnd)

	id := start(t, env, ana, "Look at the checkout")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "Clean" {
		t.Errorf("the Run: %+v\n%s", run, env.Runner.Fake.Output(onlyExecution(t, run).ID))
	}
}

func TestAnUnpushedCommitIsPushedWhenTheAgentAsksForAPerson(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, shell(`echo half > half.txt && git add half.txt && git commit -q -m "Half" && `+
		`solidtrace report --outcome needs-a-person --summary "Half" --question "Which color?"`), reply, result, waitForEnd)

	id := start(t, env, ana, "Paint it")
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if body, ok := env.Forge.File("example/shop", "refs/heads/"+dispatcherapi.RunBranch(id), "half.txt"); !ok || body != "half\n" {
		t.Errorf("half.txt on the Run's branch: %q, %v", body, ok)
	}
}

func TestTheForgeTokenGoesOnlyToTheForge(t *testing.T) {
	env, ana := newEnv(t)
	// Another git server that asks for credentials, as a private or
	// missing repository on another host does.
	var mu sync.Mutex
	var auth []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if a := r.Header.Get("Authorization"); a != "" {
			auth = append(auth, a)
		}
		mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(other.Close)
	gem := filepath.Join(t.TempDir(), "gem")
	env.Runner.Agent(takeTask, shell(`git clone -q `+other.URL+`/someone/private-gem.git `+gem+`; `+
		`solidtrace report --outcome done --summary "Tried"`), reply, result, waitForEnd)

	id := start(t, env, ana, "Clone a gem")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	mu.Lock()
	defer mu.Unlock()
	if len(auth) != 0 {
		t.Errorf("the other server got credentials: %q", auth)
	}
}

func TestTheWrappersPushRunsNoHookAndNoRemoteOfTheAgent(t *testing.T) {
	env, ana := newEnv(t)
	marker := filepath.Join(t.TempDir(), "hook-ran")
	// The agent's hooks and configuration in the checkout: a pre-push
	// hook, a hooks path, and an origin that goes nowhere.
	env.Runner.Agent(takeTask, shell(`printf '#!/bin/sh\ntouch `+marker+`\n' > .git/hooks/pre-push && chmod +x .git/hooks/pre-push && `+
		`mkdir -p ../hooks && cp .git/hooks/pre-push ../hooks/ && git config core.hooksPath "$PWD/../hooks" && `+
		`git remote set-url origin /nowhere.git && echo work > work.txt && git add work.txt && git commit -q -m Work && `+
		`solidtrace report --outcome done --summary "Done"`), reply, result, waitForEnd)

	id := start(t, env, ana, "Do the work")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if body, ok := env.Forge.File("example/shop", "refs/heads/"+dispatcherapi.RunBranch(id), "work.txt"); !ok || body != "work\n" {
		t.Errorf("work.txt on the Run's branch: %q, %v\n%s", body, ok, env.Runner.Fake.Output(env.Runner.Dispatched()[0]))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("the agent's pre-push hook ran in the wrapper's push (%v)", err)
	}
}

func TestARefusedPushKeepsTheCommitsInTheWIPRefAndTheEndSaysSo(t *testing.T) {
	env, ana := newEnv(t)
	// A person pushes to the Run's branch while the agent works: the
	// wrapper's push is not a fast-forward.
	env.Runner.Agent(takeTask, shell(`b=$(git rev-parse --abbrev-ref HEAD) && `+
		`p=$(git commit-tree -p origin/main -m "A person's commit" HEAD^{tree}) && git push -q origin "$p:refs/heads/$b" && `+
		`echo work > work.txt && git add work.txt && git commit -q -m Work && `+
		`solidtrace report --outcome done --summary "Done"`), reply, result, waitForEnd)

	id := start(t, env, ana, "Do the work")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if body, ok := env.Forge.File("example/shop", "refs/agent-wip/"+id, "work.txt"); !ok || body != "work\n" {
		t.Errorf("work.txt in refs/agent-wip/%s: %q, %v\n%s", id, body, ok, env.Runner.Fake.Output(onlyExecution(t, run).ID))
	}
	if r := onlyExecution(t, run).EndReason; !strings.Contains(r, "push") || !strings.Contains(r, "refs/agent-wip/"+id) {
		t.Errorf("the Execution's end reason %q does not say that the push failed and where the commits are", r)
	}
}
