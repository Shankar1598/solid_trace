package killboundary

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These cases run on each machine: they give the open a cgroup root that
// is not cgroup v2, or one with no cgroup.kill.

func TestOpenRefusesWhenCgroupV2IsAbsent(t *testing.T) {
	root := t.TempDir()
	_, err := openIn(root, "/", "execution-1", func(string) (int64, error) { return 0x01021994, nil }) // tmpfs
	if err == nil || !strings.Contains(err.Error(), "cgroup v2") || !strings.Contains(err.Error(), root) {
		t.Fatalf("open with no cgroup v2: %v", err)
	}
	_, err = openIn(root, "/", "execution-1", func(string) (int64, error) { return 0, errors.New("no such file") })
	if err == nil || !strings.Contains(err.Error(), "cgroup v2") {
		t.Fatalf("open with no file system at the root: %v", err)
	}
}

func TestOpenRefusesWhenCgroupKillIsAbsent(t *testing.T) {
	root := t.TempDir()
	own := filepath.Join(root, "task")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := openIn(root, "/task", "execution-1", func(string) (int64, error) { return cgroup2Magic, nil })
	if err == nil || !strings.Contains(err.Error(), "cgroup.kill") || !strings.Contains(err.Error(), "5.14") {
		t.Fatalf("open with no cgroup.kill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(own, "execution-1")); !os.IsNotExist(err) {
		t.Errorf("the refused open made the child: %v", err)
	}
}

func TestOwnCgroupComesFromTheUnifiedLine(t *testing.T) {
	got, err := ownCgroup("12:pids:/legacy\n0::/nomad.slice/share.slice/a1.check.scope\n")
	if err != nil || got != "/nomad.slice/share.slice/a1.check.scope" {
		t.Errorf("own cgroup %q, %v", got, err)
	}
	if _, err := ownCgroup("12:pids:/legacy\n1:name=systemd:/x\n"); err == nil || !strings.Contains(err.Error(), "cgroup v2") {
		t.Errorf("no unified line: %v", err)
	}
}
