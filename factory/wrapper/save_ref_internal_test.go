package wrapper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The ref of the save follows the Forge's setting, and the checkpoint
// pushes only a change.

func TestTheSaveGoesToTheForgesPlaceOfTheWIPRefs(t *testing.T) {
	for _, ref := range []string{"", "refs/agent-wip/" + saveRunID, "refs/heads/agent-wip/" + saveRunID} {
		t.Run(ref, func(t *testing.T) {
			e, remote := savingExecution(t, nil, func(task *dispatcherapi.Task) { task.WIPRef = ref })
			writeFiles(t, e.checkout, map[string]string{"work.txt": "work\n"})

			saved, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted)
			if err != nil {
				t.Fatalf("the save: %v", err)
			}

			want := ref
			if want == "" {
				want = "refs/agent-wip/" + saveRunID
			}
			refs := gitRun(t, "--git-dir", remote, "for-each-ref", "--format=%(objectname) %(refname)")
			if !strings.Contains(refs, saved.Commit+" "+want) || strings.Count(refs, "agent-wip") != 1 {
				t.Errorf("the remote's refs:\n%s\nwant %s at %s only", refs, want, saved.Commit)
			}
		})
	}
}

// A ref that is not a work-in-progress ref of the Run gets no push: a
// save never moves a branch of the repository.
func TestASaveToAnotherRefIsRefused(t *testing.T) {
	for _, ref := range []string{"refs/heads/main", "refs/heads/agent/" + saveRunID, "refs/agent-wip/other",
		"refs/heads/agent-wip/" + saveRunID + "/x"} {
		t.Run(ref, func(t *testing.T) {
			e, remote := savingExecution(t, nil, func(task *dispatcherapi.Task) { task.WIPRef = ref })
			before := gitRun(t, "--git-dir", remote, "for-each-ref")
			writeFiles(t, e.checkout, map[string]string{"work.txt": "work\n"})

			if _, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted); err == nil ||
				!strings.Contains(err.Error(), "work-in-progress ref") {
				t.Errorf("a save to %s: %v", ref, err)
			}
			if after := gitRun(t, "--git-dir", remote, "for-each-ref"); after != before {
				t.Errorf("the remote's refs changed:\n%s\nwant:\n%s", after, before)
			}
		})
	}
}

func TestTheCheckpointPushesOnlyWhenTheWorkChanged(t *testing.T) {
	e, remote := savingExecution(t, map[string]string{"app.txt": "one\n"}, nil)
	ref := "refs/agent-wip/" + saveRunID
	wip := func() string {
		out := gitRun(t, "--git-dir", remote, "for-each-ref", "--format=%(objectname)", ref)
		return out
	}
	checkpoint := func(wantPushed bool) savedWork {
		t.Helper()
		saved, pushed, err := e.saver.checkpoint(context.Background())
		if err != nil || pushed != wantPushed {
			t.Fatalf("the checkpoint: pushed %v (%v), want %v", pushed, err, wantPushed)
		}
		return saved
	}

	checkpoint(false)
	if got := wip(); got != "" || !e.saver.lastCheckpoint().IsZero() {
		t.Fatalf("a checkpoint with no work pushed %s (last checkpoint %s)", got, e.saver.lastCheckpoint())
	}

	writeFiles(t, e.checkout, map[string]string{"app.txt": "two\n"})
	first := checkpoint(true)
	if got := wip(); got != first.Commit || e.saver.lastCheckpoint().IsZero() {
		t.Fatalf("the checkpoint after a change: remote %q, save %q", got, first.Commit)
	}
	at := e.saver.lastCheckpoint()

	checkpoint(false)
	if got := wip(); got != first.Commit || !e.saver.lastCheckpoint().Equal(at) {
		t.Errorf("a checkpoint with no change moved the ref to %s", got)
	}

	// The agent's git command holds the index lock: the checkpoint does
	// not need it, and leaves it.
	lock := filepath.Join(e.checkout, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, e.checkout, map[string]string{"app.txt": "three\n"})
	locked := checkpoint(true)
	if _, err := os.Stat(lock); err != nil || wip() != locked.Commit {
		t.Fatalf("the checkpoint with the agent's index lock: %v, remote %s, save %s", err, wip(), locked.Commit)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	first = locked

	commitFiles(t, e.checkout, nil, "The agent commits the same tree")
	second := checkpoint(true)
	if got := wip(); got != second.Commit || got == first.Commit {
		t.Errorf("a checkpoint after a commit: remote %s, save %s", got, second.Commit)
	}
}

// A push that fails is an error of the save, and the next checkpoint
// saves the same work again.
func TestACheckpointThatCouldNotPushSavesAgainAtTheNextCheckpoint(t *testing.T) {
	e, remote := savingExecution(t, nil, nil)
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho the forge is down >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, e.checkout, map[string]string{"work.txt": "work\n"})

	if _, pushed, err := e.saver.checkpoint(context.Background()); pushed || err == nil ||
		!strings.Contains(err.Error(), "the forge is down") {
		t.Errorf("a checkpoint whose push fails: pushed %v, %v", pushed, err)
	}
	if !e.saver.lastCheckpoint().IsZero() {
		t.Errorf("a checkpoint that did not push is the last checkpoint")
	}
	if _, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted); err == nil {
		t.Errorf("a preemption save whose push fails gives no error")
	}

	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	saved, pushed, err := e.saver.checkpoint(context.Background())
	if err != nil || !pushed {
		t.Fatalf("the next checkpoint: pushed %v, %v", pushed, err)
	}
	if got := gitRun(t, "--git-dir", remote, "rev-parse", "refs/agent-wip/"+saveRunID); got != saved.Commit {
		t.Errorf("the remote's WIP ref %s, the save %s", got, saved.Commit)
	}
}
