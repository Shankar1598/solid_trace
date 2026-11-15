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
	// The run user owns each directory (the test's user owns them): with
	// no write bit, only the owner check refuses it.
	owner := stranger
	owner.uid = os.Geteuid()
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
		{"the run user owns the directory and its parent", owner, 0o511, 0o511, "can change"},
	} {
		t.Run(c.name, func(t *testing.T) {
			parent, err := os.MkdirTemp("", "checkdir-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(parent, 0o700); os.RemoveAll(parent) })
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
	if err := owner.checkDir(dir); err == nil || !strings.Contains(err.Error(), "can change") {
		t.Errorf("a directory of the run user's: %v", err)
	}
	// A wrapper that is the run user has no other user to keep out.
	if err := (runUser{name: "me", uid: os.Geteuid()}).checkDir(dir); err != nil {
		t.Errorf("no change of user: %v", err)
	}
}

// mkdirAll follows no link: a link in place of a directory, which the run
// user can put there, stops it, so root makes and gives nothing in the
// link's target.
func TestMkdirAllRefusesALinkInPlaceOfADirectory(t *testing.T) {
	dir, target := t.TempDir(), t.TempDir()
	if err := os.Symlink(target, filepath.Join(dir, "claude")); err != nil {
		t.Fatal(err)
	}
	self := runUser{name: "me", uid: os.Geteuid()}
	err := self.mkdirAll(filepath.Join(dir, "claude", "projects", "run", "evidence"))
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("mkdirAll through a link: %v; want an error", err)
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Errorf("mkdirAll made %v in the link's target", entries)
	}
	if err := self.mkdirAll(filepath.Join(dir, "real", "projects")); err != nil {
		t.Errorf("mkdirAll with no link: %v", err)
	}
}

// writeSecret makes a new file: a file or a link that is at the path (the
// run user can put one where it can write) stops it, so root writes no
// secret to a path that the run user chose.
func TestWriteSecretRefusesAFileOrALinkThatIsThere(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("the run user's"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o666); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "forge-token")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	self := runUser{name: "me", uid: os.Geteuid(), gid: os.Getegid()}
	for _, path := range []string{link, target} {
		if err := writeSecret(self, path, []byte("secret"), 0o440); err == nil {
			t.Errorf("writeSecret to %s that is there: no error", filepath.Base(path))
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "the run user's" {
		t.Errorf("the file that was there has %q", got)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o666 {
		t.Errorf("the file that was there: %v, %v", info.Mode(), err)
	}
	fresh := filepath.Join(dir, "new")
	if err := writeSecret(self, fresh, []byte("secret"), 0o440); err != nil {
		t.Fatalf("writeSecret to a new path: %v", err)
	}
	if got, _ := os.ReadFile(fresh); string(got) != "secret" {
		t.Errorf("the new secret file has %q", got)
	}
}
