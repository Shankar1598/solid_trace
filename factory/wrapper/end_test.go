package wrapper_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/killboundary"
	"github.com/solidtrace/factory/wrapper"
)

// leaveProcesses is a step that starts three processes that outlive the
// tool call, as an agent's tools leave them: a background child, a child
// in a session of its own, and a child that ignores SIGTERM and SIGHUP.
// It writes their pids to a file, and gives the file.
func leaveProcesses(t *testing.T) (step, pids string) {
	t.Helper()
	pids = filepath.Join(t.TempDir(), "pids")
	return shell(`p=` + pids + `
sleep 300 >/dev/null 2>&1 & echo $! >>"$p"
setsid sh -c 'echo $$ >>"$1"; exec sleep 300' _ "$p" >/dev/null 2>&1 &
sh -c 'trap "" TERM HUP; echo $$ >>"$1"; exec sleep 300' _ "$p" >/dev/null 2>&1 &
while [ "$(wc -l <"$p")" -lt 3 ]; do sleep 0.01; done`), pids
}

// backgroundTasks is the harness's background_tasks_changed event with n
// running tasks.
func backgroundTasks(n int) string {
	tasks := []map[string]any{}
	for i := 0; i < n; i++ {
		tasks = append(tasks, map[string]any{"task_id": "b" + strconv.Itoa(i), "task_type": "local_bash",
			"description": "a long command"})
	}
	b, _ := json.Marshal(map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": tasks,
		"session_id": "", "uuid": ""})
	return string(b)
}

// noProcessLeft fails the test for each pid in the file that has not
// ended.
func noProcessLeft(t *testing.T, pids string) {
	t.Helper()
	b, err := os.ReadFile(pids)
	if err != nil {
		t.Fatalf("the agent's processes wrote no pids: %v", err)
	}
	fields := strings.Fields(string(b))
	if len(fields) != 3 {
		t.Fatalf("pids %q, want 3", b)
	}
	for _, f := range fields {
		pid, _ := strconv.Atoi(f)
		if alive(pid) {
			t.Errorf("process %d of the Execution is left after the end", pid)
		}
	}
}

// alive tells if pid is a process that has not ended (a zombie has).
func alive(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(stat)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return len(fields) > 0 && fields[0] != "Z" && fields[0] != "X"
}

func TestAfterEachEndNoProcessOfTheExecutionIsLeft(t *testing.T) {
	cases := []struct {
		name   string
		script func(t *testing.T, leave string) []string
		state  string
		reason string
	}{
		{"a done report", func(t *testing.T, leave string) []string {
			return []string{takeTask, initLine(t), leave, reportStep("--outcome", "done", "--summary", "Fixed"),
				reply, result, waitForEnd}
		}, dispatcherapi.StateDone, dispatcherapi.EndReported},
		{"needs a person", func(t *testing.T, leave string) []string {
			return []string{takeTask, initLine(t), leave,
				reportStep("--outcome", "needs-a-person", "--summary", "Two fixes", "--question", "Which one?"),
				`{"fake":"await_message","eof_code":0}`, reply, result, waitForEnd}
		}, dispatcherapi.StateWaitingOnPerson, dispatcherapi.EndReported},
		{"no report", func(t *testing.T, leave string) []string {
			return []string{takeTask, initLine(t), leave, reply, result, waitForEnd}
		}, dispatcherapi.StateFailed, dispatcherapi.EndNoReport},
		{"the harness exits by itself", func(t *testing.T, leave string) []string {
			return []string{takeTask, initLine(t), leave, `{"fake":"exit","code":3}`}
		}, dispatcherapi.StateFailed, dispatcherapi.EndHarnessExited},
		{"the harness does not exit when stdin closes", func(t *testing.T, leave string) []string {
			return []string{takeTask, initLine(t), leave, reportStep("--outcome", "done", "--summary", "Fixed"),
				reply, result, `{"fake":"sleep","ms":60000}`}
		}, dispatcherapi.StateDone, dispatcherapi.EndReported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, ana := newEnv(t)
			leave, pids := leaveProcesses(t)
			env.Runner.Agent(c.script(t, leave)...)

			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, c.state)

			if e := onlyExecution(t, run); e.EndReason != c.reason {
				t.Errorf("end reason %q, want %q", e.EndReason, c.reason)
			}
			noProcessLeft(t, pids)
			if _, err := killboundary.Reopen(wrapper.CgroupName(onlyExecution(t, run).ID)); err == nil {
				t.Errorf("the Execution's child cgroup is not removed")
			}
		})
	}
}

func TestTheEndKillsTheBackgroundTasksThatStillRunAndCountsThem(t *testing.T) {
	cases := []struct {
		name   string
		script func(t *testing.T) []string
		killed int
	}{
		{"two tasks run at the end", func(t *testing.T) []string {
			leave, _ := leaveProcesses(t)
			return []string{takeTask, initLine(t), leave, backgroundTasks(2),
				reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd}
		}, 2},
		{"the tasks ended before the end", func(t *testing.T) []string {
			leave, _ := leaveProcesses(t)
			return []string{takeTask, initLine(t), leave, backgroundTasks(2), backgroundTasks(0),
				reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd}
		}, 0},
		{"no process is left when the harness exits", func(t *testing.T) []string {
			return []string{takeTask, initLine(t), backgroundTasks(1),
				reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd}
		}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, ana := newEnv(t)
			env.Runner.Agent(c.script(t)...)

			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

			if e := onlyExecution(t, run); e.BackgroundTasksKilled != c.killed {
				t.Errorf("background tasks killed %d, want %d", e.BackgroundTasksKilled, c.killed)
			}
		})
	}
}

// The harness can write its last background_tasks_changed event and exit
// before the wrapper reads that event. The count comes from the whole
// output stream, not from the events that the wrapper read before the
// kill.
func TestTheCountOfKilledTasksIncludesTheEventsBeforeTheHarnessExit(t *testing.T) {
	cases := []struct {
		name   string
		last   string
		killed int
	}{
		{"two tasks run when the harness exits", backgroundTasks(2), 2},
		{"the tasks ended just before the harness exits", backgroundTasks(0), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, ana := newEnv(t)
			leave, _ := leaveProcesses(t)
			// Many events in the pipe at the exit, so that the wrapper
			// has not read the last one when it sees the exit.
			script := []string{takeTask, initLine(t), leave}
			for i := 0; i < 2000; i++ {
				script = append(script, backgroundTasks(1))
			}
			env.Runner.Agent(append(script, c.last, `{"fake":"exit","code":137}`)...)

			id := start(t, env, ana, "Fix the cart")
			run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

			if e := onlyExecution(t, run); e.BackgroundTasksKilled != c.killed {
				t.Errorf("background tasks killed %d, want %d", e.BackgroundTasksKilled, c.killed)
			}
		})
	}
}

func TestTheEndStopsEachRunningContainer(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		shell(`docker run -d postgres:17 >/dev/null && docker run -d redis:8 >/dev/null`),
		reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if running := env.Runner.Docker.Running(); len(running) != 0 {
		t.Errorf("containers still run after the end: %v", running)
	}
	if killed := env.Runner.Docker.Killed(); len(killed) != 2 {
		t.Errorf("the end killed containers %v, want the agent's 2", killed)
	}
	// The end event says what the container step did.
	if got := awaitEvent(t, env, id, "execution_ended")["execution.container_step"]; got != "killed 2 containers" {
		t.Errorf("the end event's container step is %v, want killed 2 containers", got)
	}
}

func TestWithNoDockerTheContainerStepIsSkipped(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.NoDocker()
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	out := env.Runner.Fake.Output(onlyExecution(t, run).ID)
	if !strings.Contains(out, "no Docker") {
		t.Errorf("the wrapper did not say that it skipped the container step:\n%s", out)
	}
	// The end event says so too (the local factory stack has no Docker).
	got, _ := awaitEvent(t, env, id, "execution_ended")["execution.container_step"].(string)
	if !strings.HasPrefix(got, "skipped: no Docker") {
		t.Errorf("the end event's container step is %q, want skipped", got)
	}
}

func TestADockerThatDoesNotAnswerHoldsTheEndForAtMostItsLimit(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	if err := env.Runner.Docker.Hang(true); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	open()
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	// The limit is 3 seconds; the fake Docker waits 60.
	if took := time.Since(began); took > 15*time.Second {
		t.Errorf("the end took %s with a Docker that does not answer", took)
	}
	if out := env.Runner.Fake.Output(onlyExecution(t, run).ID); !strings.Contains(out, "docker") {
		t.Errorf("the wrapper did not say that the container step failed:\n%s", out)
	}
}

// The agent makes a cgroup in the Execution's cgroup, so that the
// Execution's cgroup cannot go. The wrapper still sends the end call, and
// then fails (exit 1), so that the Runner sees the leak.
func TestTheWrapperFailsWhenTheEndProcedureLeavesTheExecutionsCgroup(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		shell(`mkdir "/sys/fs/cgroup$(sed -n 's/^0:://p' /proc/self/cgroup)/held"`),
		reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd)
	// Before the environment removes the Execution's cgroup.
	t.Cleanup(func() {
		for _, execution := range env.Runner.Dispatched() {
			if b, err := killboundary.Reopen(wrapper.CgroupName(execution)); err == nil {
				os.Remove(filepath.Join(b.Dir(), "held"))
			}
		}
	})

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	execution := onlyExecution(t, run).ID
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev, err := env.Runner.Fake.Wait(ctx, execution)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ExitCode != 1 {
		t.Errorf("the wrapper's exit code %d, want 1", ev.ExitCode)
	}
	if out := env.Runner.Fake.Output(execution); !strings.Contains(out, "the end procedure left processes") {
		t.Errorf("the wrapper did not say that the cgroup is left:\n%s", out)
	}
}

// When the task call fails, the wrapper removes the child cgroup that it
// made: a retry of the Execution can make it again.
func TestWhenTheTaskCallFailsTheWrapperRemovesTheChildCgroup(t *testing.T) {
	env, _ := newEnv(t)
	// The wrapper runs in a cgroup of the test's own, to look into.
	b, err := killboundary.Open("task-fails-" + strconv.Itoa(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(b.Dir(), wrapper.CgroupName("ex_unknown"))
	t.Cleanup(func() {
		os.Remove(child)
		b.Kill()
		b.Remove(2 * time.Second)
	})
	// The Dispatcher has no Execution ex_unknown, so it refuses the task
	// call.
	identity := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(identity, []byte(env.Runner.Identity(factorytest.Workload("ex_unknown"))), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(env.Runner.Wrapper())
	cmd.Dir = t.TempDir()
	cmd.Env = []string{wrapper.EnvDispatcherURL + "=" + env.URL(), wrapper.EnvExecution + "=ex_unknown",
		wrapper.EnvIdentityFile + "=" + identity, wrapper.EnvDocker + "=/no/docker", "PATH=" + os.Getenv("PATH"),
		wrapper.EnvRunUser + "=" + env.Runner.RunUser().Username}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := b.Start(cmd); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()

	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Errorf("exit %d, want 1; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "task call") {
		t.Errorf("stderr %q does not say that the task call failed", stderr.String())
	}
	if _, err := os.Stat(child); !os.IsNotExist(err) {
		t.Errorf("the child cgroup %s is left after the task call failed (%v)", child, err)
	}
}

func TestTheWrapperRefusesToStartWhenItCannotMakeTheChildCgroup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to each cgroup")
	}
	env, _ := newEnv(t)
	// The wrapper runs in a cgroup that it cannot write to.
	b, err := killboundary.Open("refuse-" + strconv.Itoa(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Chmod(b.Dir(), 0o755)
		b.Kill()
		b.Remove(2 * time.Second)
	})
	if err := os.Chmod(b.Dir(), 0o555); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(identity, []byte(env.Runner.Identity(factorytest.Workload("ex_refused"))), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(env.Runner.Wrapper())
	cmd.Dir = t.TempDir()
	cmd.Env = []string{wrapper.EnvDispatcherURL + "=" + env.URL(), wrapper.EnvExecution + "=ex_refused",
		wrapper.EnvIdentityFile + "=" + identity, wrapper.EnvDocker + "=/no/docker", "PATH=" + os.Getenv("PATH"),
		wrapper.EnvRunUser + "=" + env.Runner.RunUser().Username}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := b.Start(cmd); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()

	if cmd.ProcessState.ExitCode() != 2 {
		t.Errorf("exit %v, want 2", err)
	}
	if got := stderr.String(); !strings.Contains(got, "child cgroup") || !strings.Contains(got, "delegated cgroup") {
		t.Errorf("stderr %q does not say why", got)
	}
	if _, err := os.Stat(filepath.Join(cmd.Dir, "checkout")); !os.IsNotExist(err) {
		t.Errorf("the wrapper cloned with no kill boundary")
	}
}

// A release run sets killboundary.EnvRequired: with no kill boundary, the
// wrapper's tests then fail, and a skip cannot look like a pass.
func TestWithNoKillBoundaryTheTestsFailWhenTheBoundaryIsRequired(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to each cgroup")
	}
	if err := killboundary.Check(); err != nil {
		t.Skipf("no kill boundary here to make a read-only cgroup in: %v", err)
	}
	// This test binary runs one wrapper test in a cgroup that it cannot
	// write to.
	b, err := killboundary.Open("required-" + strconv.Itoa(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Chmod(b.Dir(), 0o755)
		b.Kill()
		b.Remove(2 * time.Second)
	})
	if err := os.Chmod(b.Dir(), 0o555); err != nil {
		t.Fatal(err)
	}
	inner := func(required bool) (int, string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWithNoDockerTheContainerStepIsSkipped$", "-test.v")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
		if required {
			cmd.Env = append(cmd.Env, killboundary.EnvRequired+"=1")
		}
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := b.Start(cmd); err != nil {
			t.Fatal(err)
		}
		cmd.Wait()
		return cmd.ProcessState.ExitCode(), out.String()
	}

	if code, out := inner(false); code != 0 || !strings.Contains(out, "--- SKIP") {
		t.Errorf("with no kill boundary and no %s: exit %d, want a skip:\n%s", killboundary.EnvRequired, code, out)
	}
	if code, out := inner(true); code == 0 || !strings.Contains(out, "--- FAIL") ||
		!strings.Contains(out, "kill boundary is not available") {
		t.Errorf("with no kill boundary and %s: exit %d, want a failure:\n%s", killboundary.EnvRequired, code, out)
	}
}
