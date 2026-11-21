package wrapper_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
	"github.com/solidtrace/factory/internal/factorytest"
)

// The checkpoint and the preemption handler's save, through the
// Dispatcher, a wrapper and the fake Forge.

// shellStep is a step that runs command in the agent's checkout, then
// touches a file; the wait waits for that file.
func shellStep(t *testing.T, command string) (step string, wait func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "done")
	return shell(command + " && touch " + path), func() {
		waitUntil(t, "the agent's command", func() bool {
			_, err := os.Stat(path)
			return err == nil
		})
	}
}

// wipRefOf waits until the fake Forge has the ref at a commit that is not
// old, and gives the commit.
func wipRefOf(t *testing.T, env *factorytest.Env, ref, old string) string {
	t.Helper()
	var commit string
	waitUntil(t, ref+" on the Forge", func() bool {
		c, ok := env.Forge.Ref("example/shop", ref)
		commit = c
		return ok && c != old
	})
	return commit
}

var reasonOf = regexp.MustCompile(`^WIP \[skip ci\]: saved by the factory at \d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ, ` +
	`Run (\S+), Execution (\S+), reason ([a-z ]+)\. Not reviewed\.$`)

// wipReason checks the WIP commit's message and gives its reason.
func wipReason(t *testing.T, env *factorytest.Env, commit, run, execution string) string {
	t.Helper()
	message := strings.TrimSpace(env.Forge.Git("example/shop", "log", "-1", "--format=%s", commit))
	m := reasonOf.FindStringSubmatch(message)
	if m == nil || m[1] != run || m[2] != execution {
		t.Fatalf("the WIP commit message %q, want Run %s, Execution %s", message, run, execution)
	}
	return m[3]
}

func TestTheCheckpointSavesEach5MinutesOnlyWhenTheWorkChanged(t *testing.T) {
	env, ana := newEnv(t)
	env.Config.CheckpointInterval = 5 * time.Minute
	env.Restart()
	first, firstDone := shellStep(t, "echo one > work.txt")
	wait, open := gate(t)
	second, secondDone := shellStep(t, "echo two > work.txt")
	env.Runner.Agent(takeTask, initLine(t), first, wait, second, reply, awaitInterrupt, interruptLine, errorResult,
		exitWhenStdinCloses)
	id := start(t, env, ana, "Fix the cart")
	firstDone()
	execution := getRun(t, ana, id).Executions[0].ID
	ref := "refs/agent-wip/" + id
	if _, ok := env.Forge.Ref("example/shop", ref); ok {
		t.Fatalf("%s is on the Forge before the first checkpoint", ref)
	}

	env.Clock.Advance(5 * time.Minute)
	saved := wipRefOf(t, env, ref, "")
	if reason := wipReason(t, env, saved, id, execution); reason != "checkpoint" {
		t.Errorf("the first save's reason %q", reason)
	}
	if body, _ := env.Forge.File("example/shop", saved, "work.txt"); body != "one\n" {
		t.Errorf("work.txt in the checkpoint: %q", body)
	}
	if main, _ := env.Forge.Ref("example/shop", "refs/heads/main"); strings.TrimSpace(env.Forge.Git("example/shop",
		"rev-parse", saved+"^")) != main {
		t.Errorf("the checkpoint's parent is not the Run's HEAD (main, %s)", main)
	}

	env.Clock.Advance(5 * time.Minute)
	waitUntil(t, "the second checkpoint", func() bool {
		return strings.Contains(env.Runner.Fake.Output(execution), "the checkpoint: no change since the last save")
	})
	if now, _ := env.Forge.Ref("example/shop", ref); now != saved {
		t.Errorf("a checkpoint with no change moved %s to %s", ref, now)
	}

	open()
	secondDone()
	env.Clock.Advance(5 * time.Minute)
	changed := wipRefOf(t, env, ref, saved)
	if body, _ := env.Forge.File("example/shop", changed, "work.txt"); body != "two\n" {
		t.Errorf("work.txt in the third checkpoint: %q", body)
	}

	env.Runner.HoldMachines()
	env.Runner.Preempt(execution)
	run := waitForRestart(t, env, ana, id)
	preemption := eventOf(t, env.WaitForRunEvents(run.ID, 5), "preemption")
	last, _ := env.Forge.Ref("example/shop", ref)
	if preemption["preemption.wip_commit"] != last || last == changed {
		t.Errorf("the preemption's WIP commit %v, the Forge's %s (the last checkpoint %s)",
			preemption["preemption.wip_commit"], last, changed)
	}
	if reason := wipReason(t, env, last, id, execution); reason != dispatcherapi.EndPreempted {
		t.Errorf("the preemption save's reason %q", reason)
	}
	if age, ok := preemption["preemption.checkpoint_age_seconds"].(float64); !ok || age < 0 {
		t.Errorf("the age of the last checkpoint: %v", preemption["preemption.checkpoint_age_seconds"])
	}
	if problems, _ := preemption["preemption.problems"].([]any); len(problems) != 0 {
		t.Errorf("the preemption's problems: %v", problems)
	}
}

func TestACheckpointIntervalOf0SavesNothing(t *testing.T) {
	env, ana := newEnv(t)
	env.Config.CheckpointInterval = 0
	env.Restart()
	step, done := shellStep(t, "echo one > work.txt")
	env.Runner.Agent(takeTask, initLine(t), step, reply, awaitInterrupt, interruptLine, errorResult, exitWhenStdinCloses)
	id := start(t, env, ana, "Fix the cart")
	done()
	execution := getRun(t, ana, id).Executions[0].ID
	waitUntil(t, "the checkpoint is off", func() bool {
		return strings.Contains(env.Runner.Fake.Output(execution), "the checkpoint is off")
	})

	for range 3 {
		env.Clock.Advance(5 * time.Minute)
	}
	time.Sleep(200 * time.Millisecond)
	if refs := env.Forge.Refs("example/shop"); refs["refs/agent-wip/"+id] != "" {
		t.Errorf("a checkpoint with interval 0 saved: %v", refs)
	}

	env.Runner.HoldMachines()
	env.Runner.Preempt(execution)
	waitForRestart(t, env, ana, id)
	preemption := eventOf(t, env.WaitForRunEvents(id, 5), "preemption")
	if _, ok := preemption["preemption.checkpoint_age_seconds"]; ok {
		t.Errorf("a checkpoint age with no checkpoint: %v", preemption["preemption.checkpoint_age_seconds"])
	}
	saved, _ := env.Forge.Ref("example/shop", "refs/agent-wip/"+id)
	if body, _ := env.Forge.File("example/shop", saved, "work.txt"); body != "one\n" || preemption["preemption.wip_commit"] != saved {
		t.Errorf("the preemption's save %v, the Forge's %s with work.txt %q", preemption["preemption.wip_commit"], saved, body)
	}
}

// A forge that refuses refs outside refs/heads gets the save in
// refs/heads/agent-wip/<run id>.
func TestTheSaveFollowsTheForgesPlaceOfTheWIPRefs(t *testing.T) {
	env, ana := newEnv(t)
	env.Config.WIPRefs = forge.WIPInBranches
	env.Restart()
	env.Forge.RefuseRefsOutsideBranches("example/shop")
	step, done := shellStep(t, "echo one > work.txt")
	env.Runner.Agent(takeTask, initLine(t), step, reply, awaitInterrupt, interruptLine, errorResult, exitWhenStdinCloses)
	id := start(t, env, ana, "Fix the cart")
	done()

	env.Runner.HoldMachines()
	env.Runner.Preempt(getRun(t, ana, id).Executions[0].ID)
	waitForRestart(t, env, ana, id)

	refs := env.Forge.Refs("example/shop")
	saved := refs["refs/heads/agent-wip/"+id]
	if body, _ := env.Forge.File("example/shop", saved, "work.txt"); saved == "" || body != "one\n" {
		t.Errorf("refs/heads/agent-wip/%s is %q with work.txt %q: %v", id, saved, body, refs)
	}
	if refs["refs/agent-wip/"+id] != "" {
		t.Errorf("the save went to refs/agent-wip too: %v", refs)
	}
	preemption := eventOf(t, env.WaitForRunEvents(id, 5), "preemption")
	if preemption["preemption.wip_commit"] != saved {
		t.Errorf("the preemption's WIP commit %v, want %s", preemption["preemption.wip_commit"], saved)
	}
}
