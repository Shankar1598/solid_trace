package wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The check of the Execution's directory, for a run user that is not the
// user of the test: the test's user owns each directory, as root does on
// an agent machine.
func TestTheExecutionsDirectoryMustBeReachableAndNotChangeableByTheRunUser(t *testing.T) {
	other := 4242
	if os.Geteuid() == other {
		other = 4243
	}
	stranger := runUser{name: "agent", uid: other, gid: other,
		credential: &syscall.Credential{Uid: uint32(other), Gid: uint32(other), Groups: []uint32{uint32(other)}}}
	self := os.Getegid()
	member := stranger
	member.credential = &syscall.Credential{Uid: uint32(other), Gid: uint32(other), Groups: []uint32{uint32(other), uint32(self)}}
	for _, c := range []struct {
		name         string
		user         runUser
		parent, mode os.FileMode
		want         string
	}{
		{"reachable", stranger, 0o711, 0o711, ""},
		{"reachable through a sticky parent that others write", stranger, 0o777 | os.ModeSticky, 0o711, ""},
		{"no search in the directory", stranger, 0o711, 0o700, "cannot reach"},
		{"no search in a parent", stranger, 0o700, 0o711, "cannot reach"},
		{"others write the directory", stranger, 0o711, 0o777 | os.ModeSticky, "can change"},
		{"others write a parent that is not sticky", stranger, 0o777, 0o711, "can change"},
		{"the run user's group writes the directory", member, 0o711, 0o771, "can change"},
		{"another group writes the directory", stranger, 0o711, 0o771, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			parent, err := os.MkdirTemp("", "checkdir-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(parent) })
			dir := filepath.Join(parent, "execution")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			for path, mode := range map[string]os.FileMode{parent: c.parent, dir: c.mode} {
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			if info, _ := os.Stat(filepath.Dir(parent)); info.Mode()&os.ModeSticky == 0 || info.Mode().Perm()&0o001 == 0 {
				t.Skipf("the temporary directory %s is not a sticky directory that others reach", filepath.Dir(parent))
			}
			err = c.user.checkDir(dir)
			if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
				t.Errorf("checkDir: %v; want %q", err, c.want)
			}
		})
	}

	// The run user owns the directory: it can change its mode.
	dir := t.TempDir()
	owner := stranger
	owner.uid = os.Geteuid()
	if err := owner.checkDir(dir); err == nil || !strings.Contains(err.Error(), "can change") {
		t.Errorf("a directory of the run user's: %v", err)
	}
	// A wrapper that is the run user has no other user to keep out.
	if err := (runUser{name: "me", uid: os.Geteuid()}).checkDir(dir); err != nil {
		t.Errorf("no change of user: %v", err)
	}
}
