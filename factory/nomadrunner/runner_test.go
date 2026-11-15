//go:build factorystack

package nomadrunner_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/nomadrunner"
)

func TestADispatchRunsOnTheNodePoolOfItsMachineKindAndWithNoKindOnTheDefault(t *testing.T) {
	s := theStack(t)
	// A node pool with no node: an Execution of that kind waits for a
	// machine.
	empty := "test-empty-" + random(t)
	if _, err := s.api.NodePools().Register(&api.NodePool{Name: empty}, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.api.NodePools().Delete(empty, nil) })
	r := s.runner(t, `echo "execution $WRAPPER_EXECUTION_ID"; echo "identity $WRAPPER_IDENTITY_FILE"; echo "url $WRAPPER_DISPATCHER_URL"`,
		func(c *nomadrunner.Config) {
			c.Kinds = append(c.Kinds, dispatcher.MachineKind{Name: empty, MemoryMB: 64})
		})
	ev := &events{t: t, r: r}

	workload := dispatch(t, r, "run-1", "exec-default", "")
	ev.until("exec-default", dispatcher.RunnerComplete, 60*time.Second)
	if pool := *s.job(t, workload).NodePool; pool != "default" {
		t.Errorf("with no kind, the job is in node pool %q, want default", pool)
	}
	alloc := s.alloc(t, workload)
	out := s.stdout(t, alloc.ID)
	for _, want := range []string{"execution exec-default\n", "/secrets/nomad_dispatcher.jwt\n", "url http://dispatcher:7600\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("the wrapper's environment has no %q:\n%s", want, out)
		}
	}
	job := s.job(t, workload)
	if job.Meta[nomadrunner.MetaRun] != "run-1" || job.Meta[nomadrunner.MetaExecution] != "exec-default" || len(job.Meta) != 2 {
		t.Errorf("the dispatch meta is %v, want only the Run and the Execution", job.Meta)
	}

	waiting := dispatch(t, r, "run-2", "exec-empty", empty)
	if pool := *s.job(t, waiting).NodePool; pool != empty {
		t.Errorf("the job is in node pool %q, want %q", pool, empty)
	}
	ev.none("exec-empty", 3*time.Second)
	if allocs := s.allocs(t, waiting); len(allocs) != 0 {
		t.Errorf("an Execution of a kind with no machine has %d allocations", len(allocs))
	}

	if _, err := r.Dispatch(context.Background(), dispatcher.RunnerExecution{Run: "run-3", Execution: "exec-3", MachineKind: "huge"}); err == nil ||
		!strings.Contains(err.Error(), `"huge"`) {
		t.Errorf("a dispatch of a kind that the operator did not define: %v", err)
	}
}

func TestTheRunnerGivesEachStateOfTheAllocationAndItsEnd(t *testing.T) {
	s := theStack(t)
	r := s.runner(t, `sleep 2; [ "$WRAPPER_EXECUTION_ID" = exec-ok ] || exit 3`)
	ev := &events{t: t, r: r}

	dispatch(t, r, "run-1", "exec-ok", "")
	var states []string
	for {
		e := ev.next("exec-ok", 60*time.Second)
		states = append(states, e.State)
		if e.Ended {
			if e.State != dispatcher.RunnerComplete || e.ExitCode != 0 || e.Lost {
				t.Errorf("the end is %+v, want complete with exit 0", e)
			}
			break
		}
	}
	if got := strings.Join(states, " "); got != "pending running complete" && got != "running complete" {
		t.Errorf("the states are %q, want pending, running, complete", got)
	}

	dispatch(t, r, "run-2", "exec-fails", "")
	end := ev.until("exec-fails", dispatcher.RunnerFailed, 60*time.Second)
	if !end.Ended || end.ExitCode != 3 || end.Lost {
		t.Errorf("the end is %+v, want failed with exit 3", end)
	}
	ev.none("exec-fails", 2*time.Second)
}

func TestNomadNeverRestartsOrReschedulesTheWrapper(t *testing.T) {
	s := theStack(t)
	r := s.runner(t, `exit 1`)
	ev := &events{t: t, r: r}

	workload := dispatch(t, r, "run-1", "exec-1", "")
	ev.until("exec-1", dispatcher.RunnerFailed, 60*time.Second)
	// Nomad's default would restart the task, and reschedule it later.
	ev.none("exec-1", 5*time.Second)
	allocs := s.allocs(t, workload)
	if len(allocs) != 1 {
		t.Fatalf("the job has %d allocations, want 1", len(allocs))
	}
	if restarts := allocs[0].TaskStates[nomadrunner.TaskName].Restarts; restarts != 0 {
		t.Errorf("the task restarted %d times", restarts)
	}
	group := s.job(t, workload).TaskGroups[0]
	if *group.RestartPolicy.Attempts != 0 || *group.ReschedulePolicy.Attempts != 0 || *group.ReschedulePolicy.Unlimited ||
		*group.Disconnect.Replace {
		t.Errorf("restart %+v, reschedule %+v, disconnect %+v", *group.RestartPolicy, *group.ReschedulePolicy, *group.Disconnect)
	}
}

func TestTheJobReservesAllTheMemoryOfTheMachine(t *testing.T) {
	s := theStack(t)
	nodes, _, err := s.api.Nodes().List(nil)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("want the one agent machine: %d nodes, %v", len(nodes), err)
	}
	node, _, err := s.api.Nodes().Info(nodes[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	memory := int(node.NodeResources.Memory.MemoryMB) - int(node.ReservedResources.Memory.MemoryMB)
	r := s.runner(t, `sleep 4`, func(c *nomadrunner.Config) {
		c.Kinds = []dispatcher.MachineKind{{Name: "default", MemoryMB: memory}}
	})
	ev := &events{t: t, r: r}

	first := dispatch(t, r, "run-1", "exec-1", "")
	ev.until("exec-1", dispatcher.RunnerRunning, 60*time.Second)
	if got := *s.job(t, first).TaskGroups[0].Tasks[0].Resources.MemoryMB; got != memory {
		t.Errorf("the task reserves %d MB, want %d", got, memory)
	}
	second := dispatch(t, r, "run-2", "exec-2", "")
	// One Run on each machine: the second waits for the first to end.
	time.Sleep(2 * time.Second)
	if allocs := s.allocs(t, second); len(allocs) != 0 {
		t.Errorf("the second Execution has an allocation while the first runs on the machine")
	}
	ev.until("exec-1", dispatcher.RunnerComplete, 60*time.Second)
	ev.until("exec-2", dispatcher.RunnerComplete, 60*time.Second)
}

func TestStopGivesTheWrapperSIGTERMAndTheKillTimeout(t *testing.T) {
	s := theStack(t)
	// The wrapper's end takes longer than Nomad's default of 5 seconds.
	r := s.runner(t, `trap 'echo got TERM; sleep 7; echo stopped; exit 0' TERM; echo ready; while :; do sleep 1; done`)
	ev := &events{t: t, r: r}

	workload := dispatch(t, r, "run-1", "exec-1", "")
	ev.until("exec-1", dispatcher.RunnerRunning, 60*time.Second)
	alloc := s.alloc(t, workload)
	s.waitForStdout(t, alloc.ID, "ready")
	if got := *s.job(t, workload).TaskGroups[0].Tasks[0].KillTimeout; got != nomadrunner.KillTimeout {
		t.Errorf("the kill timeout is %s, want %s", got, nomadrunner.KillTimeout)
	}

	start := time.Now()
	if err := r.Stop(context.Background(), workload); err != nil {
		t.Fatal(err)
	}
	end := ev.next("exec-1", 60*time.Second)
	if !end.Ended || end.Lost {
		t.Fatalf("the end is %+v", end)
	}
	if took := time.Since(start); took < 7*time.Second {
		t.Errorf("the task ended %s after the stop, before its own end", took)
	}
	out := s.stdout(t, alloc.ID)
	if !strings.Contains(out, "got TERM") || !strings.Contains(out, "stopped") {
		t.Errorf("the wrapper did not end by itself after SIGTERM:\n%s", out)
	}

	if err := r.Stop(context.Background(), "another-job/dispatch-1-abc"); err == nil {
		t.Error("Stop stopped a job that is not the Runner's")
	}
}

func TestALostMachineEndsItsExecutionAsLost(t *testing.T) {
	s := theStack(t)
	r := s.runner(t, `exec sleep 600`)
	ev := &events{t: t, r: r}

	workload := dispatch(t, r, "run-1", "exec-1", "")
	ev.until("exec-1", dispatcher.RunnerRunning, 60*time.Second)
	resume := s.pause(t)
	end := ev.next("exec-1", 90*time.Second)
	if !end.Ended || !end.Lost || end.State != dispatcher.RunnerLost {
		t.Fatalf("the event is %+v, want lost", end)
	}

	// The Dispatcher restores the Run in a new Execution. When the machine
	// comes back, its old task stops, and Nomad does not place the lost
	// Execution again (on this machine or on another).
	resume()
	settled := time.Now().Add(15 * time.Second)
	for deadline := time.Now().Add(45 * time.Second); ; time.Sleep(time.Second) {
		var live []string
		for _, a := range s.allocs(t, workload) {
			if a.ClientStatus == api.AllocClientStatusPending || a.ClientStatus == api.AllocClientStatusRunning ||
				a.ClientStatus == api.AllocClientStatusUnknown {
				live = append(live, a.ID[:8]+" "+a.ClientStatus)
			}
		}
		if len(live) == 0 && time.Now().After(settled) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the lost Execution's job still has allocations %v after the machine came back", live)
		}
	}
	if out, _ := exec.Command("docker", "exec", s.machine, "pgrep", "-f", "sleep 600").Output(); len(out) != 0 {
		t.Errorf("the lost Execution's task still runs on the machine: pids %s", out)
	}
	ev.none("exec-1", time.Second)
}
