package wrapper

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The work-in-progress save on a real clone: what the remote gets, and
// what stays as it was in the agent's checkout.

const saveRunID = "run1"

// commitFiles commits files in the clone at dir.
func commitFiles(t *testing.T, dir string, files map[string]string, message string) {
	t.Helper()
	writeFiles(t, dir, files)
	gitRun(t, "-C", dir, "add", "-A")
	gitRun(t, "-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", message)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// savingExecution is an Execution after its setup: a clone, on the Run's
// branch, of a remote whose main has these files. It gives the Execution
// and the remote.
func savingExecution(t *testing.T, files map[string]string, change func(*dispatcherapi.Task)) (*execution, string) {
	t.Helper()
	dir := t.TempDir()
	remote, seed := seedRemote(t, dir)
	if len(files) > 0 {
		commitFiles(t, seed, files, "The files")
		gitRun(t, "-C", seed, "push", "-q", remote, "main")
	}
	cfg := testConfig(dir)
	task := dispatcherapi.Task{RunID: saveRunID, ExecutionID: "ex1", Repository: "bitbucket.org/example/shop",
		CloneURL: remote, Branch: dispatcherapi.RunBranch(saveRunID), BaseBranch: "main"}
	if change != nil {
		change(&task)
	}
	var log bytes.Buffer
	e := setupExecution(t, cfg, &log, task)
	e.clock, e.environ = realClock{}, os.Environ()
	e.saver = newWIPSaver(e)
	if err := e.setup(context.Background(), os.Environ()); err != nil {
		t.Fatalf("setup: %v\n%s", err, log.String())
	}
	return e, remote
}

// agentState is what the agent sees of its checkout.
func agentState(t *testing.T, checkout string) string {
	t.Helper()
	path := gitRun(t, "-C", checkout, "rev-parse", "--path-format=absolute", "--git-path", "index")
	index, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join([]string{
		"index " + string(index) + info.ModTime().String(),
		"head " + gitRun(t, "-C", checkout, "rev-parse", "--symbolic-full-name", "HEAD") + " " + gitRun(t, "-C", checkout, "rev-parse", "HEAD"),
		"status " + gitRun(t, "--no-optional-locks", "-C", checkout, "status", "--porcelain=v2", "--branch", "--untracked-files=all"),
		"refs " + gitRun(t, "-C", checkout, "for-each-ref"),
		"objects " + gitRun(t, "-C", checkout, "count-objects", "-v"),
	}, "\n")
}

func TestASavePushesTheWorkAndLeavesTheAgentsIndexHeadAndBranchAsTheyAre(t *testing.T) {
	e, remote := savingExecution(t, map[string]string{"staged.txt": "one\n", "changed.txt": "one\n", "gone.txt": "one\n"}, nil)
	checkout := e.checkout
	commitFiles(t, checkout, map[string]string{"committed.txt": "not pushed\n"}, "A commit that is not pushed")
	head := gitRun(t, "-C", checkout, "rev-parse", "HEAD")
	writeFiles(t, checkout, map[string]string{"staged.txt": "two\n", "changed.txt": "two\n", "new/untracked.txt": "new\n"})
	gitRun(t, "-C", checkout, "add", "staged.txt")
	if err := os.Remove(filepath.Join(checkout, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	before := agentState(t, checkout)

	saved, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted)
	if err != nil {
		t.Fatalf("the save: %v", err)
	}

	wip := gitRun(t, "--git-dir", remote, "rev-parse", "refs/agent-wip/"+saveRunID)
	if saved.Commit == "" || wip != saved.Commit {
		t.Fatalf("the remote's refs/agent-wip/%s is %s, the save gives %q", saveRunID, wip, saved.Commit)
	}
	if parent := gitRun(t, "--git-dir", remote, "rev-parse", wip+"^"); parent != head {
		t.Errorf("the WIP commit's parent is %s, not HEAD %s", parent, head)
	}
	for path, want := range map[string]string{"staged.txt": "two", "changed.txt": "two", "new/untracked.txt": "new",
		"committed.txt": "not pushed"} {
		if got := gitRun(t, "--git-dir", remote, "show", wip+":"+path); got != want {
			t.Errorf("%s in the save: %q, want %q", path, got, want)
		}
	}
	if files := gitRun(t, "--git-dir", remote, "ls-tree", "-r", "--name-only", wip); strings.Contains(files, "gone.txt") {
		t.Errorf("the save has the deleted file gone.txt:\n%s", files)
	}
	if after := agentState(t, checkout); after != before {
		t.Errorf("the save changed the agent's checkout:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if len(saved.Skipped) != 0 {
		t.Errorf("the save left out %v", saved.Skipped)
	}
}

var wipMessage = regexp.MustCompile(`^WIP \[skip ci\]: saved by the factory at (\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ), ` +
	`Run run1, Execution ex1, reason (checkpoint|preempted|machine stopped)\. Not reviewed\.$`)

func TestTheWIPCommitMessageHasTheTimeTheRunTheExecutionTheReasonAndSkipCI(t *testing.T) {
	for _, reason := range []string{dispatcherapi.EndPreempted, dispatcherapi.EndMachineStopped, reasonCheckpoint} {
		t.Run(reason, func(t *testing.T) {
			e, remote := savingExecution(t, nil, nil)
			writeFiles(t, e.checkout, map[string]string{"work.txt": "work\n"})

			if _, err := e.saver.save(context.Background(), reason); err != nil {
				t.Fatalf("the save: %v", err)
			}

			message := gitRun(t, "--git-dir", remote, "log", "-1", "--format=%B", "refs/agent-wip/"+saveRunID)
			m := wipMessage.FindStringSubmatch(message)
			if m == nil || m[2] != reason {
				t.Fatalf("the WIP commit message %q, want reason %s", message, reason)
			}
		})
	}
}

// The agent works while a checkpoint runs: an untracked file that goes
// after the save listed it does not stop the save.
func TestAnUntrackedFileThatGoesDuringTheSaveDoesNotStopTheSave(t *testing.T) {
	e, remote := savingExecution(t, nil, nil)
	writeFiles(t, e.checkout, map[string]string{"kept.txt": "kept\n", "gone.txt": "gone\n"})
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// A git that deletes gone.txt right after the save lists the
	// untracked files.
	bin := t.TempDir()
	script := "#!/bin/sh\n" + shellQuote(real) + " \"$@\"; code=$?\n" +
		"case \" $* \" in *\" --others \"*) case \" $* \" in *\" --ignored \"*) ;; *) rm -f " +
		shellQuote(filepath.Join(e.checkout, "gone.txt")) + ";; esac;; esac\nexit $code\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	saved, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted)
	if err != nil {
		t.Fatalf("the save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(e.checkout, "gone.txt")); !os.IsNotExist(err) {
		t.Fatalf("the test's git did not delete gone.txt: %v", err)
	}
	if files := gitRun(t, "--git-dir", remote, "ls-tree", "-r", "--name-only", saved.Commit); files != "kept.txt" {
		t.Errorf("the save has %q, want only kept.txt", files)
	}
}
