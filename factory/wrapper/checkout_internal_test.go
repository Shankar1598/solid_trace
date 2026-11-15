package wrapper

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A wrapper that did not get to its clean-up (it was killed, or the
// machine lost its power) leaves its checkout and the run user's home at
// the fixed paths. The next Execution on that machine still sets up.
// gitRun runs git and gives its output.
func gitRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// seedRemote makes a bare remote with one commit on main, and gives its
// path and the folder of the seed clone.
func seedRemote(t *testing.T, dir string) (remote, seed string) {
	t.Helper()
	remote = filepath.Join(dir, "remote.git")
	seed = filepath.Join(dir, "seed")
	gitRun(t, "init", "-q", "--bare", "-b", "main", remote)
	gitRun(t, "init", "-q", "-b", "main", seed)
	gitRun(t, "-C", seed, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "Start")
	gitRun(t, "-C", seed, "push", "-q", remote, "main")
	return remote, seed
}

func testConfig(dir string) config {
	return config{dir: filepath.Join(dir, "wrapper"), workspace: filepath.Join(dir, "workspace"),
		home: filepath.Join(dir, "home"), cache: filepath.Join(dir, "cache"),
		claudeConfig: filepath.Join(dir, "home", ".claude"), projectName: "bitbucket-org-example-shop"}
}

// startBoundary is a kill boundary that only starts each process.
type startBoundary struct{}

func (startBoundary) Start(cmd *exec.Cmd) error  { return cmd.Start() }
func (startBoundary) Populated() (bool, error)   { return false, nil }
func (startBoundary) Kill() error                { return nil }
func (startBoundary) Remove(time.Duration) error { return nil }

// setupExecution is an Execution for a call of setup: the Execution's
// directory is there (Main makes it), and the run user is the wrapper's
// user.
func setupExecution(t *testing.T, cfg config, log *bytes.Buffer, task dispatcherapi.Task) *execution {
	t.Helper()
	if err := os.Mkdir(cfg.dir, 0o711); err != nil {
		t.Fatal(err)
	}
	e := &execution{cfg: cfg, log: log, task: task, boundary: startBoundary{}, uploads: &uploads{projectDir: cfg.projectDir()}}
	t.Cleanup(func() {
		if e.uploads.dir != nil {
			e.uploads.dir.Close()
		}
	})
	return e
}

func TestSetupRemovesTheCheckoutAndHomeThatAnEarlierExecutionLeft(t *testing.T) {
	dir := t.TempDir()
	remote, _ := seedRemote(t, dir)
	cfg := testConfig(dir)
	repository := "bitbucket.org/example/shop"
	stale := filepath.Join(cfg.checkout(repository), "left.txt")
	for _, f := range []string{stale, filepath.Join(cfg.home, "notes")} {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("left"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var log bytes.Buffer
	e := setupExecution(t, cfg, &log, dispatcherapi.Task{RunID: "r-1", Repository: repository,
		CloneURL: remote, Branch: dispatcherapi.RunBranch("r-1")})

	if err := e.setup(context.Background(), os.Environ()); err != nil {
		t.Fatalf("setup over a checkout that an earlier Execution left: %v", err)
	}
	for _, f := range []string{stale, filepath.Join(cfg.home, "notes")} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the setup (%v)", f, err)
		}
	}
}

// A later Execution of a Run finds the Run's branch on the remote: it
// works on that branch, from the commits that an earlier Execution pushed.
func TestALaterExecutionChecksOutTheRunsBranchThatIsOnTheRemote(t *testing.T) {
	dir := t.TempDir()
	remote, seed := seedRemote(t, dir)
	branch := dispatcherapi.RunBranch("r-1")
	if err := os.WriteFile(filepath.Join(seed, "first.txt"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", seed, "add", "first.txt")
	gitRun(t, "-C", seed, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "The first Execution")
	pushed := gitRun(t, "-C", seed, "rev-parse", "HEAD")
	gitRun(t, "-C", seed, "push", "-q", remote, "HEAD:refs/heads/"+branch)
	gitRun(t, "-C", seed, "reset", "-q", "--hard", "HEAD^")
	gitRun(t, "-C", seed, "push", "-q", "--force", remote, "main")

	cfg := testConfig(dir)
	repository := "bitbucket.org/example/shop"
	var log bytes.Buffer
	e := setupExecution(t, cfg, &log, dispatcherapi.Task{RunID: "r-1", Repository: repository,
		CloneURL: remote, Branch: branch, BaseBranch: "main"})
	if err := e.setup(context.Background(), os.Environ()); err != nil {
		t.Fatalf("setup: %v\n%s", err, log.String())
	}
	checkout := cfg.checkout(repository)
	if got := gitRun(t, "-C", checkout, "rev-parse", "--abbrev-ref", "HEAD"); got != branch {
		t.Errorf("the checkout is on %s, not on the Run's branch %s", got, branch)
	}
	if got := gitRun(t, "-C", checkout, "rev-parse", "HEAD"); got != pushed || e.startTip != pushed {
		t.Errorf("the checkout starts at %s (start tip %s), not at the pushed commit %s", got, e.startTip, pushed)
	}
	if _, err := os.Stat(filepath.Join(checkout, "first.txt")); err != nil {
		t.Errorf("the first Execution's file is not in the checkout: %v", err)
	}
}

// A repository or a Project's folder that is not a plain path cannot put
// the checkout or the work directory outside the workspace.
func TestARepositoryOrFolderThatIsNotAPlainPathIsRefused(t *testing.T) {
	for _, r := range []string{"bitbucket.org/example", "bitbucket.org/example/..", "../example/shop",
		"bitbucket.org/./shop", "bitbucket.org//shop", "bitbucket.org/example/sh\\op", "bitbucket.org/example/sh\nop",
		"/bitbucket.org/example/shop"} {
		if _, err := checkRepository(r); err == nil {
			t.Errorf("checkRepository(%q) is accepted", r)
		}
	}
	if _, err := checkRepository("bitbucket.org/example/shop"); err != nil {
		t.Errorf("checkRepository of a plain repository: %v", err)
	}
	for _, f := range []string{"..", "../x", "apps/../..", "./apps", "apps/", "apps//shop", "apps\\shop", "apps\nshop", "/apps"} {
		if _, err := checkFolder(f); err == nil {
			t.Errorf("checkFolder(%q) is accepted", f)
		}
	}
	for _, f := range []string{"", "apps/shop"} {
		if _, err := checkFolder(f); err != nil {
			t.Errorf("checkFolder(%q): %v", f, err)
		}
	}

	dir := t.TempDir()
	remote, _ := seedRemote(t, dir)
	cfg := testConfig(dir)
	var log bytes.Buffer
	e := setupExecution(t, cfg, &log, dispatcherapi.Task{RunID: "r-1", Repository: "bitbucket.org/example/shop",
		Folder: "../x", CloneURL: remote, Branch: dispatcherapi.RunBranch("r-1")})
	if err := e.setup(context.Background(), os.Environ()); err == nil {
		t.Errorf("setup with the Project's folder ../x has no error")
	}
	for _, d := range []string{cfg.workspace, filepath.Join(dir, "x")} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("setup with the folder ../x made %s (%v)", d, err)
		}
	}
}

// The wrapper's push reads no git configuration of the agent: no global
// file, and no GIT_ variable of the agent's environment.
func TestThePushEnvironmentHasNoGitConfigurationOfTheAgent(t *testing.T) {
	e := &execution{}
	env := e.pushEnv([]string{"PATH=/bin", "HOME=/home/agent", "GIT_CONFIG_GLOBAL=/home/agent/.gitconfig",
		"GIT_DIR=/elsewhere", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath"}, "/tmp/push")
	var git []string
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "HOME=") {
			git = append(git, kv)
		}
	}
	want := "HOME=/tmp/push GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0"
	if got := strings.Join(git, " "); got != want {
		t.Errorf("the push's HOME and GIT_ variables: %s\nwant: %s", got, want)
	}
}
