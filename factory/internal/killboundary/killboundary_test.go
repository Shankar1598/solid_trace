package killboundary_test

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/killboundary"
)

// The cgroup cases need a cgroup v2 that this process can write to: root,
// or a delegated cgroup. Where there is none, they skip with the reason.
// EnvRequired makes a skip a failure: the run as root under Nomad's
// raw_exec on the local factory stack's agent machine sets it.
const (
	envRequired = "KILLBOUNDARY_REQUIRED"
	// envRunUser names the run user: as root, the process tree then runs
	// as that user, as the wrapper starts the harness (ticket 39).
	envRunUser = "KILLBOUNDARY_RUN_USER"
)

func open(t *testing.T, name string) *killboundary.Boundary {
	t.Helper()
	if err := killboundary.Check(); err != nil {
		if os.Getenv(envRequired) != "" {
			t.Fatalf("the kill boundary is not available: %v", err)
		}
		t.Skipf("no delegated cgroup v2 here, so the cgroup cases skip: %v", err)
	}
	b, err := killboundary.Open(name + "-" + strconv.Itoa(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.Kill()
		b.Remove(2 * time.Second)
	})
	return b
}

// tree is a shell with a child, a child in a session of its own, and a
// child that ignores SIGTERM and SIGHUP, as an agent's tools leave them.
// Each process writes its pid to the pids file.
func tree(t *testing.T, b *killboundary.Boundary) (cmd *exec.Cmd, pids string) {
	t.Helper()
	pids = filepath.Join(t.TempDir(), "pids")
	script := `echo $$ >>"$1"
sleep 300 & echo $! >>"$1"
setsid sh -c 'echo $$ >>"$1"; exec sleep 300' _ "$1" &
sh -c 'trap "" TERM HUP; echo $$ >>"$1"; exec sleep 300' _ "$1" &
wait`
	cmd = exec.Command("sh", "-c", script, "_", pids)
	if name := os.Getenv(envRunUser); name != "" && os.Geteuid() == 0 {
		u, err := user.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
		if err := os.Chmod(filepath.Dir(pids), 0o777); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Start(cmd); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for len(readPIDs(t, pids)) < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("the process tree did not start: pids %v", readPIDs(t, pids))
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cmd, pids
}

func readPIDs(t *testing.T, path string) []int {
	b, _ := os.ReadFile(path)
	var pids []int
	for _, f := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(f)
		if err != nil {
			t.Fatalf("pids file %q", b)
		}
		pids = append(pids, pid)
	}
	return pids
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

func TestKillStopsEachProcessInTheCgroupAlsoOneThatChangedItsSession(t *testing.T) {
	b := open(t, "tree")
	_, pids := tree(t, b)
	if populated, err := b.Populated(); err != nil || !populated {
		t.Fatalf("populated %v, %v, with the tree in the cgroup", populated, err)
	}
	if os.Getenv(envRunUser) != "" && os.Geteuid() == 0 {
		for _, pid := range readPIDs(t, pids) {
			info, err := os.Stat("/proc/" + strconv.Itoa(pid))
			if err != nil || info.Sys().(*syscall.Stat_t).Uid == 0 {
				t.Fatalf("process %d does not run as the run user", pid)
			}
		}
	}

	if err := b.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	for _, pid := range readPIDs(t, pids) {
		if alive(pid) {
			t.Errorf("process %d is left after the kill", pid)
		}
	}
	if _, err := os.Stat(b.Dir()); !os.IsNotExist(err) {
		t.Errorf("the cgroup %s is not removed: %v", b.Dir(), err)
	}
}

func TestRemoveRefusesACgroupThatStillHasProcesses(t *testing.T) {
	b := open(t, "busy")
	_, pids := tree(t, b)

	err := b.Remove(200 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("remove with no kill: %v", err)
	}
	for _, pid := range readPIDs(t, pids) {
		if !alive(pid) {
			t.Errorf("process %d ended with no kill", pid)
		}
	}
	if _, err := os.Stat(b.Dir()); err != nil {
		t.Errorf("the cgroup is gone: %v", err)
	}
}

func TestAnEmptyCgroupIsRemovedAndASecondKillOrRemoveDoesNothing(t *testing.T) {
	b := open(t, "empty")
	if populated, err := b.Populated(); err != nil || populated {
		t.Fatalf("populated %v, %v, with no process", populated, err)
	}
	if err := b.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove(time.Second); err != nil {
		t.Fatal(err)
	}
	if err := b.Kill(); err != nil {
		t.Errorf("a kill after the remove: %v", err)
	}
	if err := b.Remove(time.Second); err != nil {
		t.Errorf("a second remove: %v", err)
	}
}

func TestOpenRefusesANameThatIsNotOneCgroup(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", "../escape"} {
		if _, err := killboundary.Open(name); err == nil || !strings.Contains(err.Error(), "name") {
			t.Errorf("Open(%q): %v", name, err)
		}
	}
}

func TestOpenRefusesAChildThatIsThereAlready(t *testing.T) {
	b := open(t, "twice")
	if _, err := killboundary.Open(filepath.Base(b.Dir())); err == nil {
		t.Fatal("a second Open of the same name made no error")
	}
}

func TestReopenFindsAChildThatAnEndedProcessLeft(t *testing.T) {
	b := open(t, "left")
	_, pids := tree(t, b)

	again, err := killboundary.Reopen(filepath.Base(b.Dir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := again.Remove(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	for _, pid := range readPIDs(t, pids) {
		if alive(pid) {
			t.Errorf("process %d is left", pid)
		}
	}
	if _, err := killboundary.Reopen("no-such-child"); err == nil {
		t.Error("Reopen of a child that is not there made no error")
	}
}
