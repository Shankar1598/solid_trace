package wrapper_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// The restore after a lost machine (the spec's V6) and the automatic
// restart (V7), through the Dispatcher, the wrappers and the fake Forge.

// lookStep is a step that writes what the agent sees in its checkout
// to files in a folder that the test reads: HEAD, the branch, the
// status, the staged names, the subjects of the history, and the
// content of names.
func lookStep(t *testing.T, env *factorytest.Env, names ...string) (step string, look func(name string) string) {
	t.Helper()
	out := env.Runner.SharedDir("look-" + filepath.Base(t.TempDir()))
	command := "git rev-parse HEAD >" + out + "/head && git rev-parse --abbrev-ref HEAD >" + out + "/branch && " +
		"git status --porcelain --untracked-files=all >" + out + "/status && git diff --cached --name-only >" + out +
		"/staged && git log --format=%s >" + out + "/log"
	for _, name := range names {
		command += " && { cat " + name + " >" + out + "/file-" + strings.ReplaceAll(name, "/", "-") + " 2>/dev/null || true; }"
	}
	path := filepath.Join(t.TempDir(), "looked")
	return shell(command + " && touch " + path), func(name string) string {
		t.Helper()
		waitUntil(t, "the agent's look at the checkout", func() bool {
			_, err := os.Stat(path)
			return err == nil
		})
		b, _ := os.ReadFile(filepath.Join(out, name))
		return string(b)
	}
}

// changeStep is a step of the first Execution: a commit that is not
// pushed (committed.txt), then changes that are not committed: README.md
// changed, old.txt deleted, new.txt new and staged, and notes.txt new
// and not staged.
func changeStep(t *testing.T) (step string, wait func()) {
	return shellStep(t, "echo kept >committed.txt && echo old >old.txt && git add committed.txt old.txt && "+
		"git commit -q -m 'Add the cart total' && echo changed >>README.md && rm old.txt && echo new >new.txt && "+
		"git add new.txt && echo notes >notes.txt")
}

// firstMessage is the text of the first person message of the Run's
// Agent session that the Execution's harness got, from the transcript
// at SolidTrace.
func transcriptHas(t *testing.T, env *factorytest.Env, text string) bool {
	t.Helper()
	session := firstToken(t, env).AgentSessionIDs[0]
	return strings.Contains(string(env.SolidTrace.Stored(session, session+".jsonl")), text)
}

func TestAPreemptedRunRestartsAndTheNewExecutionRestoresTheSavedWork(t *testing.T) {
	env, ana := newEnv(t)
	change, changed := changeStep(t)
	env.Runner.Agent(interruptedTurn(t, change)...)
	look, seen := lookStep(t, env, "README.md", "new.txt", "notes.txt", "old.txt", "committed.txt")
	env.Runner.Agent(takeTask, initLine(t), look, reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	changed()
	first := getRun(t, ana, id).Executions[0]

	env.Runner.Preempt(first.ID)

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 {
		t.Fatalf("want 2 Executions, got %+v", run.Executions)
	}
	if run.Executions[0].EndReason != dispatcherapi.EndPreempted {
		t.Errorf("the first Execution ended with %q", run.Executions[0].EndReason)
	}
	if run.Executions[1].Account != first.Account {
		t.Errorf("the new Execution has the account %q, want the same account %q", run.Executions[1].Account, first.Account)
	}
	wip, _ := env.Forge.Ref("example/shop", "refs/agent-wip/"+id)
	saved := strings.TrimSpace(env.Forge.Git("example/shop", "rev-parse", wip+"^"))
	if head := strings.TrimSpace(seen("head")); head != saved {
		t.Errorf("HEAD after the restore is %s, want the HEAD at the save %s", head, saved)
	}
	if branch := strings.TrimSpace(seen("branch")); branch != "agent/"+id {
		t.Errorf("the checkout is on %q, want the Run's branch", branch)
	}
	if log := seen("log"); strings.Contains(log, "WIP") || !strings.Contains(log, "Add the cart total") {
		t.Errorf("the branch's history:\n%s", log)
	}
	if got, want := seen("status"), " M README.md\n D old.txt\n?? new.txt\n?? notes.txt\n"; got != want {
		t.Errorf("the status after the restore:\n%s\nwant:\n%s", got, want)
	}
	if staged := seen("staged"); staged != "" {
		t.Errorf("staged after the restore: %q", staged)
	}
	for name, want := range map[string]string{"README.md": "# example/shop\nchanged\n", "new.txt": "new\n",
		"notes.txt": "notes\n", "old.txt": "", "committed.txt": "kept\n"} {
		if got := seen("file-" + name); got != want {
			t.Errorf("%s after the restore: %q, want %q", name, got, want)
		}
	}
	for _, text := range []string{"preempted", "is back", "not finish"} {
		if !transcriptHas(t, env, text) {
			t.Errorf("the first message of the new Execution does not say %q", text)
		}
	}
	// The commit that was not pushed is on the Forge after the new
	// Execution's end.
	if tip, _ := env.Forge.Ref("example/shop", "refs/heads/agent/"+id); tip != saved {
		t.Errorf("the Run's branch on the Forge is at %s, want %s", tip, saved)
	}
}
