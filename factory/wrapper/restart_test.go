package wrapper_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// The restore after a lost machine (the spec's V6) and the automatic
// restart (V7), through the Dispatcher, the wrappers and the fake Forge.
// The steps keep their marks in folders that the run user can write: the
// local factory stack runs these cases as root, with the run user agent.

// sharedPath is a new path name in a folder that the run user can write.
func sharedPath(t *testing.T, env *factorytest.Env, name string) string {
	t.Helper()
	return filepath.Join(env.Runner.SharedDir(name+"-"+filepath.Base(t.TempDir())), name)
}

// agentShell is a step that runs command in the agent's checkout, then
// makes a mark; the wait waits for the mark.
func agentShell(t *testing.T, env *factorytest.Env, command string) (step string, wait func()) {
	t.Helper()
	path := sharedPath(t, env, "done")
	return shell(command + " && touch " + path), func() {
		waitUntil(t, "the agent's command", func() bool {
			_, err := os.Stat(path)
			return err == nil
		})
	}
}

// agentGate is a step that waits until the test opens the gate.
func agentGate(t *testing.T, env *factorytest.Env) (step string, open func()) {
	t.Helper()
	path := sharedPath(t, env, "open")
	return shell("while [ ! -e " + path + " ]; do sleep 0.02; done"), func() {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// lookStep is a step that writes what the agent sees in its checkout
// to files in a folder that the test reads: HEAD, the branch, the
// status, the staged names, the subjects of the history, and the
// content of names.
func lookStep(t *testing.T, env *factorytest.Env, names ...string) (step string, look func(name string) string) {
	t.Helper()
	out := env.Runner.SharedDir("look-" + filepath.Base(t.TempDir()))
	command := "git rev-parse HEAD >" + out + "/head && git rev-parse --abbrev-ref HEAD >" + out + "/branch && " +
		"git status --porcelain --untracked-files=all >" + out + "/status && git diff --cached --name-only >" + out +
		"/staged && git log --format=%s >" + out + "/log && { git rev-parse -q --verify refs/factory/saved-work >" + out +
		"/saved || true; }"
	for _, name := range names {
		command += " && { cat " + name + " >" + out + "/file-" + strings.ReplaceAll(name, "/", "-") + " 2>/dev/null || true; }"
	}
	path := filepath.Join(env.Runner.SharedDir("looked-"+filepath.Base(t.TempDir())), "looked")
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
func changeStep(t *testing.T, env *factorytest.Env) (step string, wait func()) {
	return agentShell(t, env, "echo kept >committed.txt && echo old >old.txt && git add committed.txt old.txt && "+
		"git commit -q -m 'Add the cart total' && echo changed >>README.md && rm old.txt && echo new >new.txt && "+
		"git add new.txt && echo notes >notes.txt")
}

// waitForRestart waits until the Run is queued again after its machine
// went, with a new Execution that waits for a machine (the test called
// env.Runner.HoldMachines), and gives the Run.
func waitForRestart(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string) dispatcherapi.Run {
	t.Helper()
	var run dispatcherapi.Run
	waitUntil(t, "the restart of the Run", func() bool {
		run = getRun(t, p, id)
		n := len(run.Executions)
		return run.State == dispatcherapi.StateQueued && n >= 2 &&
			run.Executions[n-2].State == dispatcherapi.ExecutionEnded &&
			run.Executions[n-1].State == dispatcherapi.ExecutionDispatched
	})
	return run
}

// endedAs gives the Run's state and reason that the execution_ended
// event of the Execution records: the reason of a queued Run goes when
// its next Execution is dispatched.
func endedAs(t *testing.T, env *factorytest.Env, run, execution string) (state, reason string) {
	t.Helper()
	var ended map[string]any
	waitUntil(t, "the execution_ended event of "+execution, func() bool {
		for _, ev := range env.RunEvents(run) {
			if ev["event.kind"] == "execution_ended" && ev["execution.id"] == execution {
				ended = ev
				return true
			}
		}
		return false
	})
	state, _ = ended["run.state"].(string)
	reason, _ = ended["run.reason"].(string)
	return state, reason
}

// transcriptHas tells if the transcript of the Run's Agent session at
// SolidTrace has text: the messages that the harnesses got are in it.
func transcriptHas(t *testing.T, env *factorytest.Env, text string) bool {
	t.Helper()
	session := firstToken(t, env).AgentSessionIDs[0]
	return strings.Contains(string(env.SolidTrace.Stored(session, session+".jsonl")), text)
}

func TestAPreemptedRunRestartsAndTheNewExecutionRestoresTheSavedWork(t *testing.T) {
	env, ana := newEnv(t)
	change, changed := changeStep(t, env)
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

// A person's push to the Run's branch after the save wins: the new
// Execution starts from the branch, and the first message says where the
// saved work is.
func TestAPersonsPushAfterTheSaveWinsOverTheSavedWork(t *testing.T) {
	env, ana := newEnv(t)
	change, changed := changeStep(t, env)
	env.Runner.Agent(interruptedTurn(t, change)...)
	look, seen := lookStep(t, env, "person.txt", "notes.txt")
	env.Runner.Agent(takeTask, initLine(t), look, reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	changed()
	env.Runner.HoldMachines()
	env.Runner.Preempt(getRun(t, ana, id).Executions[0].ID)
	waitForRestart(t, env, ana, id)
	ref := "refs/agent-wip/" + id
	wip, _ := env.Forge.Ref("example/shop", ref)

	pushed := env.Forge.Push("example/shop", "agent/"+id, map[string]string{"person.txt": "mine\n"}, "A person's fix")
	env.Runner.GiveMachines()

	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if head := strings.TrimSpace(seen("head")); head != pushed {
		t.Errorf("HEAD of the new Execution is %s, want the person's push %s", head, pushed)
	}
	if status := seen("status"); status != "" {
		t.Errorf("the saved work went into the checkout over the person's push:\n%s", status)
	}
	if got := seen("file-person.txt"); got != "mine\n" {
		t.Errorf("person.txt: %q", got)
	}
	if saved := strings.TrimSpace(seen("saved")); saved != wip {
		t.Errorf("the checkout's refs/factory/saved-work is %q, want the save %s", saved, wip)
	}
	for _, text := range []string{"a person pushed", ref, wip} {
		if !transcriptHas(t, env, text) {
			t.Errorf("the first message does not say %q", text)
		}
	}
	if tip, _ := env.Forge.Ref("example/shop", "refs/heads/agent/"+id); tip != pushed {
		t.Errorf("the Run's branch on the Forge is at %s, want the person's push %s", tip, pushed)
	}
	if now, _ := env.Forge.Ref("example/shop", ref); now != wip {
		t.Errorf("%s moved from %s to %s", ref, wip, now)
	}
}

// waitForever is a step that waits until the machine goes.
func waitForever(t *testing.T, env *factorytest.Env) string {
	step, _ := agentGate(t, env)
	return step
}

// runningExecution waits until the Run has n Executions and the last one
// runs, and gives its id.
func runningExecution(t *testing.T, ana *factorytest.Person, id string, n int) string {
	t.Helper()
	var last dispatcherapi.Execution
	waitUntil(t, fmt.Sprintf("Execution %d runs", n), func() bool {
		run := getRun(t, ana, id)
		if len(run.Executions) != n {
			return false
		}
		last = run.Executions[n-1]
		return last.State == dispatcherapi.ExecutionRunning
	})
	return last.ID
}

// lostEvents are the Run's machine_lost events, in order.
func lostEvents(env *factorytest.Env, id string) []map[string]any {
	var lost []map[string]any
	for _, ev := range env.RunEvents(id) {
		if ev["event.kind"] == "machine_lost" {
			lost = append(lost, ev)
		}
	}
	return lost
}

func TestTheFourthLostMachineInARowFailsTheRun(t *testing.T) {
	env, ana := newEnv(t)
	for range 4 {
		env.Runner.Agent(takeTask, initLine(t), waitForever(t, env))
	}
	id := start(t, env, ana, "Fix the cart")
	for i := 1; i <= 4; i++ {
		if err := env.Runner.Fake.LoseMachine(runningExecution(t, ana, id, i)); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, fmt.Sprintf("machine_lost event %d", i), func() bool { return len(lostEvents(env, id)) == i })
	}

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if run.Reason != "machine lost 4 times in a row" {
		t.Errorf("the Run's reason %q", run.Reason)
	}
	time.Sleep(settle)
	if n := len(env.Runner.Dispatched()); n != 4 {
		t.Errorf("%d Executions, want 4: the fourth lost machine must not restart the Run", n)
	}
	run = getRun(t, ana, id)
	for i, ev := range lostEvents(env, id) {
		checkFields(t, ev, map[string]any{"execution.id": run.Executions[i].ID, "machine_lost.reason": "machine lost",
			"machine_lost.in_a_row": float64(i + 1), "machine_lost.restart": i < 3})
	}
}

func TestAnExecutionThatEndsForAnotherReasonStartsTheCountAgain(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), waitForever(t, env))
	env.Runner.Agent(takeTask, initLine(t), waitForever(t, env))
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "needs_a_person", "--question", "Which tax?"),
		reply, result, waitForEnd)
	env.Runner.Agent(takeTask, initLine(t), waitForever(t, env))
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply, result,
		waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	for i := 1; i <= 2; i++ {
		if err := env.Runner.Fake.LoseMachine(runningExecution(t, ana, id, i)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	send(t, env, ana, id, "The state tax")
	if err := env.Runner.Fake.LoseMachine(runningExecution(t, ana, id, 4)); err != nil {
		t.Fatal(err)
	}

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 5 {
		t.Fatalf("want 5 Executions, got %+v", run.Executions)
	}
	lost := lostEvents(env, id)
	if len(lost) != 3 {
		t.Fatalf("want 3 machine_lost events, got %v", lost)
	}
	checkFields(t, lost[1], map[string]any{"execution.id": run.Executions[1].ID, "machine_lost.in_a_row": float64(2)})
	checkFields(t, lost[2], map[string]any{"execution.id": run.Executions[3].ID, "machine_lost.in_a_row": float64(1),
		"machine_lost.restart": true})
}

// The restore uses only a save of a lost Execution: a checkpoint of an
// Execution that ended for another reason is not the Run's last work.
func TestTheRestoreDoesNotUseASaveOfAnExecutionThatEndedForAnotherReason(t *testing.T) {
	env, ana := newEnv(t)
	env.Config.CheckpointInterval = 5 * time.Minute
	env.Restart()
	step, stepped := agentShell(t, env, "echo old >work.txt")
	wait, open := agentGate(t, env)
	env.Runner.Agent(takeTask, initLine(t), step, wait,
		reportStep("--outcome", "needs_a_person", "--question", "Which tax?"), reply, result, waitForEnd)
	env.Runner.Agent(takeTask, initLine(t), waitForever(t, env))
	look, seen := lookStep(t, env, "work.txt")
	env.Runner.Agent(takeTask, initLine(t), look, reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	stepped()
	env.Clock.Advance(5 * time.Minute)
	wipRefOf(t, env, "refs/agent-wip/"+id, "")
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	send(t, env, ana, id, "The state tax")
	if err := env.Runner.Fake.LoseMachine(runningExecution(t, ana, id, 2)); err != nil {
		t.Fatal(err)
	}

	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if status, file := seen("status"), seen("file-work.txt"); status != "" || file != "" {
		t.Errorf("an old save came back: status %q, work.txt %q", status, file)
	}
	if !transcriptHas(t, env, "No saved work is back") {
		t.Error("the first message does not say that no saved work is back")
	}
}

// A machine that goes with no notice restarts the Run on the same
// account, and the new Execution gets the last checkpoint back. The first
// message names the files that the save left out.
func TestALostMachineRestartsTheRunFromTheLastCheckpoint(t *testing.T) {
	env, ana := newEnv(t)
	env.Config.CheckpointInterval = 5 * time.Minute
	env.Restart()
	step, stepped := agentShell(t, env, "echo saved >work.txt && head -c 6000000 /dev/zero >big.bin")
	env.Runner.Agent(takeTask, initLine(t), step, waitForever(t, env))
	look, seen := lookStep(t, env, "work.txt")
	env.Runner.Agent(takeTask, initLine(t), look, reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	stepped()
	env.Clock.Advance(5 * time.Minute)
	wipRefOf(t, env, "refs/agent-wip/"+id, "")
	first := getRun(t, ana, id).Executions[0]

	if err := env.Runner.Fake.LoseMachine(first.ID); err != nil {
		t.Fatal(err)
	}

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[1].Account != first.Account {
		t.Fatalf("want a second Execution on the account %s: %+v", first.Account, run.Executions)
	}
	if got := seen("file-work.txt"); got != "saved\n" {
		t.Errorf("work.txt after the restore: %q", got)
	}
	if status := seen("status"); status != "?? work.txt\n" {
		t.Errorf("the status after the restore:\n%s", status)
	}
	for _, text := range []string{"was lost with no notice", "at the last checkpoint", "big.bin", "not finish"} {
		if !transcriptHas(t, env, text) {
			t.Errorf("the first message does not say %q", text)
		}
	}
	if lost := lostEvents(env, id); len(lost) != 1 {
		t.Errorf("want one machine_lost event, got %v", lost)
	}
}

// A preemption during a take-over gives waiting on a person with the
// reason, and no restart, also with a kept message (V8).
func TestAPreemptionDuringATakeOverWaitsOnAPersonAndDoesNotRestart(t *testing.T) {
	env, ana := newEnv(t)
	id := inToolCallRun(t, env, ana)
	takeOverCLI(t, env, ana, id)
	run, _ := takeOverTerminal(t, env, ana, id)
	send(t, env, ana, id, "Add a test for the cart")

	env.Runner.Preempt(run.Executions[0].ID)

	run = waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if run.Reason != "the take-over ended: preempted" {
		t.Errorf("the reason is %q", run.Reason)
	}
	if run.Executions[1].EndReason != dispatcherapi.EndPreempted {
		t.Errorf("the take-over Execution ended with %q", run.Executions[1].EndReason)
	}
	time.Sleep(settle)
	if n := len(env.Runner.Dispatched()); n != 1 || len(getRun(t, ana, id).Executions) != 2 {
		t.Errorf("a preempted take-over started again by itself: %v", env.Runner.Dispatched())
	}
	preemption := awaitEvent(t, env, id, "preemption")
	checkFields(t, preemption, map[string]any{"execution.id": run.Executions[1].ID, "preemption.restart": false})
}

// The time that a restarted Run waits in the queue for a machine does not
// count to the limits of its new Execution.
func TestTheWaitForAMachineDoesNotCountToTheLimits(t *testing.T) {
	env, ana := newEnv(t)
	step, working := agentShell(t, env, `printf 'the cart total is right' >"$SOLIDTRACE_EVIDENCE_DIR/notes.txt"`)
	env.Runner.Agent(interruptedTurn(t, step)...)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply, result,
		waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	working()
	env.Runner.HoldMachines()
	env.Runner.Preempt(getRun(t, ana, id).Executions[0].ID)
	waitForRestart(t, env, ana, id)

	env.Clock.Advance(dispatcherapi.DefaultTotalLimit + time.Hour)
	env.Runner.GiveMachines()

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[1].EndReason != dispatcherapi.EndReported {
		t.Errorf("the new Execution after the wait: %+v", run.Executions)
	}
}

// A restore that cannot fetch the save fails the setup, so that no
// checkpoint of a new Execution replaces the save; a later Execution
// restores it.
func TestARestoreThatCannotFetchTheSaveFailsTheSetupAndALaterExecutionRestores(t *testing.T) {
	env, ana := newEnv(t)
	step, stepped := agentShell(t, env, "echo saved >work.txt")
	env.Runner.Agent(interruptedTurn(t, step)...)
	// The second Execution fails its setup: the harness does not start.
	env.Runner.Agent(takeTask)
	look, seen := lookStep(t, env, "work.txt")
	env.Runner.Agent(takeTask, initLine(t), look, reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	stepped()
	dir := env.Runner.SharedDir("no-fetch")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$1\" = fetch ] && { echo 'fatal: the forge does not answer' >&2; exit 128; }\nexec " + real +
		" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env.Runner.PathFirst(dir)
	env.Runner.Preempt(getRun(t, ana, id).Executions[0].ID)
	ref := "refs/agent-wip/" + id

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if len(run.Executions) != 2 || !strings.Contains(run.Reason, "restore the work-in-progress save") {
		t.Fatalf("the Run is %s (%s): %+v", run.State, run.Reason, run.Executions)
	}
	saved, _ := env.Forge.Ref("example/shop", ref)
	env.Runner.PathFirst("")
	send(t, env, ana, id, "Go on")

	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if got := seen("file-work.txt"); got != "saved\n" {
		t.Errorf("work.txt after the later restore: %q", got)
	}
	if now, _ := env.Forge.Ref("example/shop", ref); now != saved {
		t.Errorf("%s moved from %s to %s", ref, saved, now)
	}
}

// A restarted Execution that is preempted during its restore saves
// nothing: the WIP ref keeps the lost Execution's save, and the next
// Execution restores it.
func TestAPreemptionDuringTheRestoreKeepsTheLostExecutionsSave(t *testing.T) {
	env, ana := newEnv(t)
	step, stepped := agentShell(t, env, "echo saved >work.txt")
	env.Runner.Agent(interruptedTurn(t, step)...)
	// The second Execution is preempted in its restore's fetch.
	env.Runner.Agent(takeTask)
	look, seen := lookStep(t, env, "work.txt")
	env.Runner.Agent(takeTask, initLine(t), look, reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	stepped()
	dir := env.Runner.SharedDir("slow-fetch")
	fetching, gate := filepath.Join(dir, "fetching"), filepath.Join(dir, "gate")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = fetch ] && [ ! -e " + gate + " ]; then touch " + fetching +
		"; while [ ! -e " + gate + " ]; do sleep 0.05; done; fi\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env.Runner.PathFirst(dir)
	env.Runner.Preempt(getRun(t, ana, id).Executions[0].ID)
	second := runningExecution(t, ana, id, 2)
	waitUntil(t, "the restore's fetch", func() bool {
		_, err := os.Stat(fetching)
		return err == nil
	})
	ref := "refs/agent-wip/" + id
	saved, _ := env.Forge.Ref("example/shop", ref)
	env.Runner.HoldMachines()

	env.Runner.Preempt(second)

	waitForRestart(t, env, ana, id)
	if now, _ := env.Forge.Ref("example/shop", ref); now != saved {
		t.Errorf("%s moved from %s to %s: %s", ref, saved, now,
			env.Forge.Git("example/shop", "log", "-1", "--format=%s", now))
	}
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env.Runner.PathFirst("")
	env.Runner.GiveMachines()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if got := seen("file-work.txt"); got != "saved\n" {
		t.Errorf("work.txt after the third Execution's restore: %q, want the first Execution's save", got)
	}
}

// After a person's push, the save is not in the new checkout: a
// preemption during the setup command, before the agent saw where the
// save is, does not replace it on the forge.
func TestAPreemptionDuringTheSetupCommandAfterAPersonsPushKeepsTheSave(t *testing.T) {
	env, ana := newEnv(t)
	dir := env.Runner.SharedDir("setup-gate")
	armed, inSetup, gate := filepath.Join(dir, "armed"), filepath.Join(dir, "in-setup"), filepath.Join(dir, "gate")
	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): "setup = \"echo setup >setup.txt; if [ -e " +
		armed + " ]; then touch " + inSetup + "; while [ ! -e " + gate + " ]; do sleep 0.05; done; fi\"\n"},
		"A factory file")
	change, changed := changeStep(t, env)
	env.Runner.Agent(interruptedTurn(t, change)...)
	// The second Execution is preempted in its setup command.
	env.Runner.Agent(takeTask)
	look, seen := lookStep(t, env)
	env.Runner.Agent(takeTask, initLine(t), look, reportStep("--outcome", "done", "--summary", "Fixed"), reply,
		result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	changed()
	if err := os.WriteFile(armed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env.Runner.HoldMachines()
	env.Runner.Preempt(getRun(t, ana, id).Executions[0].ID)
	waitForRestart(t, env, ana, id)
	ref := "refs/agent-wip/" + id
	wip, _ := env.Forge.Ref("example/shop", ref)
	env.Forge.Push("example/shop", "agent/"+id, map[string]string{"person.txt": "mine\n"}, "A person's fix")
	env.Runner.GiveMachines()
	second := runningExecution(t, ana, id, 2)
	waitUntil(t, "the second Execution's setup command", func() bool {
		_, err := os.Stat(inSetup)
		return err == nil
	})
	env.Runner.HoldMachines()

	env.Runner.Preempt(second)

	waitForRestart(t, env, ana, id)
	if now, _ := env.Forge.Ref("example/shop", ref); now != wip {
		t.Errorf("%s moved from %s to %s: %s", ref, wip, now,
			env.Forge.Git("example/shop", "log", "-1", "--format=%s", now))
	}
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env.Runner.GiveMachines()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if saved := strings.TrimSpace(seen("saved")); saved != wip {
		t.Errorf("the third Execution's refs/factory/saved-work is %q, want the first Execution's save %s", saved, wip)
	}
}

// A restart takes another free account when the Run's account is at its
// usage limit, and that is no account switch.
func TestARestartTakesAnotherAccountWhenItsAccountIsNotFree(t *testing.T) {
	env, ana := newEnv(t)
	env.AddAccount("pool-2", secondToken)
	step, working := agentShell(t, env, `printf 'the cart total is right' >"$SOLIDTRACE_EVIDENCE_DIR/notes.txt"`)
	env.Runner.Agent(interruptedTurn(t, step)...)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply, result,
		waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	working()
	first := getRun(t, ana, id).Executions[0]
	other := "pool-2"
	if first.Account == other {
		other = factorytest.DefaultAccount
	}
	env.MarkAtLimit(first.Account, env.Clock.Now().Add(time.Hour))

	env.Runner.Preempt(first.ID)

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[1].Account != other {
		t.Errorf("want the restart on %s: %+v", other, run.Executions)
	}
	if slices.Contains(kinds(env.RunEvents(id)), "account_switched") {
		t.Errorf("a restart made an account_switched event: %v", kinds(env.RunEvents(id)))
	}
}

// With two free accounts, a restart stays on the Run's account, though
// the pool's order gives the other one.
func TestARestartStaysOnTheRunsAccountWhenItIsFree(t *testing.T) {
	env, ana := newEnv(t)
	env.AddAccount("pool-2", secondToken)
	hold, release := agentGate(t, env)
	env.Runner.Agent(takeTask, initLine(t), hold, reportStep("--outcome", "done", "--summary", "Other"), reply, result,
		waitForEnd)
	other := start(t, env, ana, "Fix the tax")
	waitFor(t, env, ana, other, dispatcherapi.StateRunning)
	step, working := agentShell(t, env, `printf 'the cart total is right' >"$SOLIDTRACE_EVIDENCE_DIR/notes.txt"`)
	env.Runner.Agent(interruptedTurn(t, step)...)
	id := start(t, env, ana, "Fix the cart")
	working()
	first := getRun(t, ana, id).Executions[0]
	release()
	waitFor(t, env, ana, other, dispatcherapi.StateDone)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply, result,
		waitForEnd)

	env.Runner.Preempt(first.ID)

	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[1].Account != first.Account || first.Account == factorytest.DefaultAccount {
		t.Errorf("want both Executions on pool-2: %+v", run.Executions)
	}
}
