// Package killboundary is the kill boundary of an Execution (K1 to K6): a
// child cgroup (cgroup v2) of the wrapper's own cgroup. The wrapper starts
// the harness in it, and so each process that the agent starts is in it,
// also a process that changes its session or ignores signals. One write
// of 1 to cgroup.kill stops them all. There is no second method: a
// SIGKILL to the harness's process group leaves its tool processes
// running (ticket 25).
//
// It needs Linux 5.14 or later (cgroup.kill), and a cgroup that the
// process can write to: root (a raw_exec task on an agent VM), or a
// delegated cgroup (a test on a dev box).
package killboundary

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Root is where cgroup v2 is mounted.
const Root = "/sys/fs/cgroup"

// cgroup2Magic is the file system type of cgroup v2 (CGROUP2_SUPER_MAGIC).
const cgroup2Magic = 0x63677270

// Boundary is one child cgroup.
type Boundary struct {
	dir string
}

// Open makes the child cgroup name in this process's own cgroup. It
// refuses, with the reason, when cgroup v2 or cgroup.kill is absent, or
// when the process cannot make the child.
func Open(name string) (*Boundary, error) {
	own, err := readOwnCgroup()
	if err != nil {
		return nil, err
	}
	return openIn(Root, own, name, fsType)
}

// Reopen gives the child cgroup name of this process's own cgroup, which
// an earlier Open made.
func Reopen(name string) (*Boundary, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	own, err := readOwnCgroup()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(Root, own, name)
	if _, err := os.Stat(filepath.Join(dir, "cgroup.kill")); err != nil {
		return nil, fmt.Errorf("no child cgroup %s: %w", dir, err)
	}
	return &Boundary{dir: dir}, nil
}

// Check tells if Open can work here: it makes a child cgroup and removes
// it again.
func Check() error {
	b, err := Open(fmt.Sprintf("killboundary-check-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err != nil {
		return err
	}
	return b.Remove(time.Second)
}

func openIn(root, own, name string, fsType func(string) (int64, error)) (*Boundary, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	if t, err := fsType(root); err != nil || t != cgroup2Magic {
		why := "it is another file system"
		if err != nil {
			why = err.Error()
		}
		return nil, fmt.Errorf("cgroup v2 is necessary at %s for the kill boundary, and it is not there (%s)", root, why)
	}
	parent := filepath.Join(root, own)
	if _, err := os.Stat(filepath.Join(parent, "cgroup.kill")); err != nil {
		return nil, fmt.Errorf("cgroup.kill is necessary for the kill boundary (Linux 5.14 or later), and %s has none", parent)
	}
	dir := filepath.Join(parent, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("make the Execution's child cgroup: %w (the wrapper needs a cgroup v2 that it can write to: run it as root, or in a delegated cgroup)", err)
	}
	return &Boundary{dir: dir}, nil
}

func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00\n") {
		return fmt.Errorf("the child cgroup's name %q is not one directory name", name)
	}
	return nil
}

func fsType(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Type), nil
}

func readOwnCgroup() (string, error) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", fmt.Errorf("cgroup v2 is necessary for the kill boundary: %w", err)
	}
	return ownCgroup(string(b))
}

// ownCgroup gives the cgroup v2 path in the text of /proc/self/cgroup.
func ownCgroup(text string) (string, error) {
	for _, line := range strings.Split(text, "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			return path, nil
		}
	}
	return "", errors.New("cgroup v2 is necessary for the kill boundary, and this process is in no cgroup v2")
}

// Dir is the child cgroup's directory.
func (b *Boundary) Dir() string { return b.dir }

// Start starts cmd in the child cgroup (CLONE_INTO_CGROUP): the process is
// in the cgroup before it runs, so no child of it can start outside.
func (b *Boundary) Start(cmd *exec.Cmd) error {
	f, err := os.Open(b.dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = int(f.Fd())
	return cmd.Start()
}

// Populated tells if a process that has not ended is in the cgroup.
func (b *Boundary) Populated() (bool, error) {
	f, err := os.Open(filepath.Join(b.dir, "cgroup.events"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "populated "); ok {
			return v != "0", nil
		}
	}
	return false, fmt.Errorf("%s/cgroup.events has no populated line", b.dir)
}

// Kill sends SIGKILL to each process in the cgroup (a write of 1 to
// cgroup.kill). A cgroup that is removed has nothing to kill.
func (b *Boundary) Kill() error {
	err := os.WriteFile(filepath.Join(b.dir, "cgroup.kill"), []byte("1"), 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Remove waits up to within until the cgroup is empty, and removes it. It
// does not kill: a cgroup that still has processes after within stays,
// and gives an error.
func (b *Boundary) Remove(within time.Duration) error {
	deadline := time.Now().Add(within)
	for {
		err := os.Remove(b.dir)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if !errors.Is(err, syscall.EBUSY) {
			return fmt.Errorf("remove the child cgroup: %w", err)
		}
		if time.Now().After(deadline) {
			procs, _ := os.ReadFile(filepath.Join(b.dir, "cgroup.procs"))
			return fmt.Errorf("the child cgroup %s is not empty after %s: processes %s", b.dir, within,
				strings.Join(strings.Fields(string(procs)), " "))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
