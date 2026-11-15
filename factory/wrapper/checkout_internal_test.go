package wrapper

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A wrapper that did not get to its clean-up (it was killed, or the
// machine lost its power) leaves its checkout and the run user's home at
// the fixed paths. The next Execution on that machine still sets up.
func TestSetupRemovesTheCheckoutAndHomeThatAnEarlierExecutionLeft(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	seed := filepath.Join(dir, "seed")
	for _, args := range [][]string{
		{"init", "-q", "--bare", "-b", "main", remote},
		{"init", "-q", "-b", "main", seed},
		{"-C", seed, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "Start"},
		{"-C", seed, "push", "-q", remote, "main"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	cfg := config{dir: filepath.Join(dir, "wrapper"), workspace: filepath.Join(dir, "workspace"),
		home: filepath.Join(dir, "home"), cache: filepath.Join(dir, "cache")}
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
	e := &execution{cfg: cfg, log: &log, task: dispatcherapi.Task{RunID: "r-1", Repository: repository,
		CloneURL: remote, Branch: dispatcherapi.RunBranch("r-1")}}

	if err := e.setup(context.Background(), os.Environ()); err != nil {
		t.Fatalf("setup over a checkout that an earlier Execution left: %v", err)
	}
	for _, f := range []string{stale, filepath.Join(cfg.home, "notes")} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the setup (%v)", f, err)
		}
	}
}
