package wrapper

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// What the work-in-progress save leaves out, and names.

// sparseFile makes a file of size bytes that takes no disk.
func sparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
}

// savedFiles are the files of the save on the remote.
func savedFiles(t *testing.T, remote string) []string {
	t.Helper()
	return strings.Split(gitRun(t, "--git-dir", remote, "ls-tree", "-r", "--name-only", "refs/agent-wip/"+saveRunID), "\n")
}

func TestASaveLeavesOutIgnoredFilesThePatternsAndBigUntrackedFilesAndNamesThem(t *testing.T) {
	e, remote := savingExecution(t, map[string]string{".gitignore": "build/\n*.tmp\n"}, func(task *dispatcherapi.Task) {
		task.SaveExclude = []string{"*.log", "tmp/"}
	})
	checkout := e.checkout
	// A tracked file that .gitignore matches is still saved.
	writeFiles(t, checkout, map[string]string{"tracked.tmp": "one\n"})
	gitRun(t, "-C", checkout, "add", "-f", "tracked.tmp")
	commitFiles(t, checkout, nil, "A tracked file that .gitignore matches")
	writeFiles(t, checkout, map[string]string{"tracked.tmp": "two\n", "build/out.txt": "built\n", "notes.tmp": "x\n",
		"logs/run.log": "log\n", "tmp/cache.txt": "cache\n", "src/app.log": "log\n", "local.txt": "the agent's own\n",
		"src/main.go": "package main\n"})
	writeFiles(t, filepath.Join(checkout, ".git"), map[string]string{"info/exclude": "local.txt\n"})
	sparseFile(t, filepath.Join(checkout, "exact.bin"), untrackedFileLimit)
	sparseFile(t, filepath.Join(checkout, "data", "big.bin"), untrackedFileLimit+1)

	saved, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted)
	if err != nil {
		t.Fatalf("the save: %v", err)
	}

	files := savedFiles(t, remote)
	for _, want := range []string{"tracked.tmp", "src/main.go", "exact.bin", ".gitignore"} {
		if !slices.Contains(files, want) {
			t.Errorf("the save has no %s: %v", want, files)
		}
	}
	if got := gitRun(t, "--git-dir", remote, "show", "refs/agent-wip/"+saveRunID+":tracked.tmp"); got != "two" {
		t.Errorf("the tracked file that .gitignore matches: %q", got)
	}
	for _, f := range files {
		for _, out := range []string{"build/", "notes.tmp", "logs/", "tmp/", "src/app.log", "local.txt", "data/big.bin"} {
			if f == out || strings.HasSuffix(out, "/") && strings.HasPrefix(f, out) {
				t.Errorf("the save has %s", f)
			}
		}
	}
	slices.Sort(saved.Skipped)
	want := []string{"build/", "data/big.bin", "local.txt", "logs/", "notes.tmp", "src/app.log", "tmp/"}
	if !slices.Equal(saved.Skipped, want) {
		t.Errorf("the save names as left out %q, want %q", saved.Skipped, want)
	}
}

// The untracked files count to 50 MiB in git's order: the file that goes
// over it, and each file after it, are left out.
func TestASaveLeavesOutEachUntrackedFileAfter50MiB(t *testing.T) {
	e, remote := savingExecution(t, nil, nil)
	for _, name := range strings.Split("abcdefghijk", "") {
		sparseFile(t, filepath.Join(e.checkout, name+".bin"), untrackedFileLimit)
	}
	writeFiles(t, e.checkout, map[string]string{"z.txt": "small\n"})

	saved, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted)
	if err != nil {
		t.Fatalf("the save: %v", err)
	}

	files := savedFiles(t, remote)
	for _, name := range strings.Split("abcdefghij", "") {
		if !slices.Contains(files, name+".bin") {
			t.Errorf("the save has no %s.bin, in the first 50 MiB: %v", name, files)
		}
	}
	if want := []string{"k.bin", "z.txt"}; !slices.Equal(saved.Skipped, want) {
		t.Errorf("the save names as left out %q, want %q (files %v)", saved.Skipped, want, files)
	}
}
