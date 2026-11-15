package fakerunner_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/fakerunner"
)

// The test binary is also the wrapper: FAKE_WRAPPER says what it does.
func TestMain(m *testing.M) {
	switch mode := os.Getenv("FAKE_WRAPPER"); {
	case mode == "":
		os.Exit(m.Run())
	case strings.HasPrefix(mode, "exit:"):
		fmt.Println("execution", os.Getenv("EXECUTION_ID"))
		code, _ := strconv.Atoi(strings.TrimPrefix(mode, "exit:"))
		os.Exit(code)
	case mode == "term":
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM)
		fmt.Println("waiting")
		<-c
		fmt.Println("stopping")
		os.Exit(0)
	case mode == "deaf":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Hour)
	case mode == "child", mode == "setsid-child":
		// A wrapper often starts the Harness in a session of its own.
		child := exec.Command("sleep", "300")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: mode == "setsid-child"}
		child.Start()
		os.WriteFile("child.pid", []byte(strconv.Itoa(child.Process.Pid)), 0o600)
		time.Sleep(time.Hour)
	case mode == "term-child":
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM)
		child := exec.Command("sleep", "300")
		child.Start()
		os.WriteFile("child.pid", []byte(strconv.Itoa(child.Process.Pid)), 0o600)
		<-c
		time.Sleep(200 * time.Millisecond)
		// A killed child that is not yet reaped is a zombie (state Z).
		stat, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child.Process.Pid))
		if f := strings.Fields(string(stat)); len(f) > 2 && f[2] != "Z" {
			fmt.Println("child-alive")
		} else {
			fmt.Println("child-gone")
		}
		os.Exit(0)
	}
}

func newRunner(t *testing.T, mode string) *fakerunner.Runner {
	t.Helper()
	r, err := fakerunner.New(fakerunner.Options{
		Command:     []string{os.Args[0]},
		Env:         append(os.Environ(), "FAKE_WRAPPER="+mode),
		Dir:         t.TempDir(),
		KillTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func states(evs []fakerunner.Event) string {
	var s []string
	for _, e := range evs {
		s = append(s, string(e.State))
	}
	return strings.Join(s, " ")
}

func waitFor(t *testing.T, r *fakerunner.Runner, id string, want fakerunner.State) {
	t.Helper()
	for seen := 0; ; {
		evs, err := r.Events(ctx(t), seen)
		if err != nil {
			t.Fatalf("no %s state: %v", want, err)
		}
		seen += len(evs)
		for _, e := range evs {
			if e.Execution == id && e.State == want {
				return
			}
		}
	}
}

func TestExecutionRunsAndEndsWithItsExitCode(t *testing.T) {
	for _, tc := range []struct {
		code int
		want fakerunner.State
	}{{0, fakerunner.Complete}, {3, fakerunner.Failed}} {
		r := newRunner(t, fmt.Sprintf("exit:%d", tc.code))
		if err := r.Dispatch(ctx(t), fakerunner.Execution{ID: "ex1", Env: []string{"EXECUTION_ID=ex1"}}); err != nil {
			t.Fatal(err)
		}
		end, err := r.Wait(ctx(t), "ex1")
		if err != nil {
			t.Fatal(err)
		}
		if end.State != tc.want || end.ExitCode != tc.code || end.Stopped {
			t.Fatalf("end %+v; want %s with code %d", end, tc.want, tc.code)
		}
		evs, _ := r.Events(ctx(t), 0)
		if got := states(evs); got != "pending running "+string(tc.want) {
			t.Fatalf("states %q; want pending, running, %s", got, tc.want)
		}
		if out := r.Output("ex1"); !strings.Contains(out, "execution ex1") {
			t.Fatalf("output %q; want the Execution's environment to reach the wrapper", out)
		}
		if s, _ := r.State("ex1"); s != tc.want {
			t.Fatalf("State = %s; want %s", s, tc.want)
		}
	}
}

func TestStopEndsTheExecution(t *testing.T) {
	r := newRunner(t, "term")
	r.Dispatch(ctx(t), fakerunner.Execution{ID: "ex1"})
	waitFor(t, r, "ex1", fakerunner.Running)
	for !strings.Contains(r.Output("ex1"), "waiting") {
		time.Sleep(10 * time.Millisecond)
	}
	if err := r.Stop(ctx(t), "ex1"); err != nil {
		t.Fatal(err)
	}
	end, _ := r.Wait(ctx(t), "ex1")
	if end.State != fakerunner.Complete || !end.Stopped || !strings.Contains(r.Output("ex1"), "stopping") {
		t.Fatalf("end %+v, output %q; want a stopped, complete Execution that got SIGTERM", end, r.Output("ex1"))
	}
}

func TestStopKillsAWrapperThatDoesNotStop(t *testing.T) {
	r := newRunner(t, "deaf")
	r.Dispatch(ctx(t), fakerunner.Execution{ID: "ex1"})
	waitFor(t, r, "ex1", fakerunner.Running)
	time.Sleep(100 * time.Millisecond)
	if err := r.Stop(ctx(t), "ex1"); err != nil {
		t.Fatal(err)
	}
	end, _ := r.Wait(ctx(t), "ex1")
	if end.State != fakerunner.Failed || !end.Stopped {
		t.Fatalf("end %+v; want a stopped Execution that failed after the kill", end)
	}
}

func childPID(t *testing.T, dir string) int {
	t.Helper()
	var pid int
	for deadline := time.Now().Add(10 * time.Second); pid == 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		b, _ := os.ReadFile(filepath.Join(dir, "ex1", "child.pid"))
		pid, _ = strconv.Atoi(string(b))
	}
	if pid == 0 {
		t.Fatal("the wrapper started no child")
	}
	return pid
}

func waitGone(t *testing.T, pid int, when string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); syscall.Kill(pid, 0) == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the wrapper's child %d still runs %s", pid, when)
		}
	}
}

func TestLostMachineTakesEachProcess(t *testing.T) {
	for _, mode := range []string{"child", "setsid-child"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			r, _ := fakerunner.New(fakerunner.Options{Command: []string{os.Args[0]}, Env: append(os.Environ(), "FAKE_WRAPPER="+mode), Dir: dir})
			t.Cleanup(r.Close)
			r.Dispatch(ctx(t), fakerunner.Execution{ID: "ex1"})
			pid := childPID(t, dir)
			if err := r.LoseMachine("ex1"); err != nil {
				t.Fatal(err)
			}
			end, _ := r.Wait(ctx(t), "ex1")
			if end.State != fakerunner.Lost {
				t.Fatalf("end %+v; want lost", end)
			}
			waitGone(t, pid, "after the machine was lost")
			if err := r.LoseMachine("ex1"); err == nil {
				t.Fatal("a second LoseMachine of an ended Execution gave no error")
			}
		})
	}
}

// Nomad sends the kill signal to the task process only, so the wrapper can
// interrupt the Harness and wait for its result.
func TestStopSignalsTheWrapperOnlyAndThenEndsItsChildren(t *testing.T) {
	dir := t.TempDir()
	r, _ := fakerunner.New(fakerunner.Options{Command: []string{os.Args[0]}, Env: append(os.Environ(), "FAKE_WRAPPER=term-child"), Dir: dir, KillTimeout: 5 * time.Second})
	t.Cleanup(r.Close)
	r.Dispatch(ctx(t), fakerunner.Execution{ID: "ex1"})
	pid := childPID(t, dir)
	if err := r.Stop(ctx(t), "ex1"); err != nil {
		t.Fatal(err)
	}
	end, _ := r.Wait(ctx(t), "ex1")
	if end.State != fakerunner.Complete || !end.Stopped || !strings.Contains(r.Output("ex1"), "child-alive") {
		t.Fatalf("end %+v, output %q; want the child alive while the wrapper stops", end, r.Output("ex1"))
	}
	waitGone(t, pid, "after the Execution ended")
}

func TestDispatchRefusesARepeatedID(t *testing.T) {
	r := newRunner(t, "exit:0")
	r.Dispatch(ctx(t), fakerunner.Execution{ID: "ex1"})
	if err := r.Dispatch(ctx(t), fakerunner.Execution{ID: "ex1"}); err == nil {
		t.Fatal("a second Dispatch of ex1 gave no error")
	}
}

func TestWrapperThatCannotStartFails(t *testing.T) {
	r, _ := fakerunner.New(fakerunner.Options{Command: []string{filepath.Join(t.TempDir(), "absent")}, Dir: t.TempDir()})
	t.Cleanup(r.Close)
	if err := r.Dispatch(ctx(t), fakerunner.Execution{ID: "ex1"}); err == nil {
		t.Fatal("no error for an absent wrapper")
	}
	end, _ := r.Wait(ctx(t), "ex1")
	if end.State != fakerunner.Failed || end.Err == "" {
		t.Fatalf("end %+v; want failed with the reason", end)
	}
}
