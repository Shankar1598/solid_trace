package wrapper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// EnvRunUser is the name of the run user (default agent): the user of the
// harness, of its tools and of git. On an agent machine the wrapper runs
// as root, and changes to the run user for each process that it starts.
// A wrapper that is not root starts its processes as its own user, and
// that user must be the run user (the form of the tests).
const EnvRunUser = "WRAPPER_RUN_USER"

// defaultRunUser is the run user of the image recipe.
const defaultRunUser = "agent"

// runUser is the user of each process that the wrapper starts.
//
// Root only where it must be: the wrapper (root) keeps the Execution's
// child token in memory and reads its workload identity JWT; it makes the
// Execution's cgroup, the secret files and the directories; it never runs
// a program, and never writes a path, that the run user controls after the
// run user's first process starts. The run user gets the secret files of
// the secrets directory (read only), the agent's socket, its home and the
// Claude Code directory. Each other path of the Execution's directory is
// root's.
type runUser struct {
	name     string
	uid, gid int
	home     string
	// credential is nil when the wrapper is the run user: no change.
	credential *syscall.Credential
}

// lookupRunUser finds the run user. As root, the run user must not be
// root. As another user, the run user must be that user.
func lookupRunUser(name string) (runUser, error) {
	if name == "" {
		name = defaultRunUser
	}
	u, err := user.Lookup(name)
	if err != nil {
		return runUser{}, fmt.Errorf("the run user %s is not on this machine (%s): %v", name, EnvRunUser, err)
	}
	r := runUser{name: name, home: u.HomeDir}
	if r.uid, err = strconv.Atoi(u.Uid); err != nil {
		return runUser{}, fmt.Errorf("the run user %s has the user id %q: %v", name, u.Uid, err)
	}
	if r.gid, err = strconv.Atoi(u.Gid); err != nil {
		return runUser{}, fmt.Errorf("the run user %s has the group id %q: %v", name, u.Gid, err)
	}
	self := os.Geteuid()
	switch {
	case self == 0 && r.uid == 0:
		return runUser{}, fmt.Errorf("the run user %s is root: the harness never runs as root (%s)", name, EnvRunUser)
	case self == 0:
		// The groups of the run user (the docker group among them).
		ids, err := u.GroupIds()
		if err != nil {
			return runUser{}, fmt.Errorf("the groups of the run user %s: %v", name, err)
		}
		var groups []uint32
		for _, id := range ids {
			g, err := strconv.ParseUint(id, 10, 32)
			if err != nil {
				return runUser{}, fmt.Errorf("the run user %s has the group id %q: %v", name, id, err)
			}
			groups = append(groups, uint32(g))
		}
		r.credential = &syscall.Credential{Uid: uint32(r.uid), Gid: uint32(r.gid), Groups: groups}
	case self != r.uid:
		return runUser{}, fmt.Errorf("the wrapper runs as the user id %d, so it cannot change to the run user %s (%s): it changes to the run user only as root", self, name, EnvRunUser)
	}
	return r, nil
}

// changes tells if the wrapper changes to the run user (it is root).
func (r runUser) changes() bool { return r.credential != nil }

// start makes cmd start as the run user. It keeps the other SysProcAttr
// fields (the kill boundary sets the cgroup).
func (r runUser) start(cmd *exec.Cmd) {
	if !r.changes() {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = r.credential
}

// env is environ with the run user's names, when the wrapper changes to
// the run user: not root's home.
func (r runUser) env(environ []string) []string {
	if !r.changes() {
		return environ
	}
	return append(environ, "HOME="+r.home, "USER="+r.name, "LOGNAME="+r.name)
}

// give makes path the run user's.
func (r runUser) give(path string) error {
	if !r.changes() {
		return nil
	}
	return os.Lchown(path, r.uid, r.gid)
}

// share makes path root's, with the run user's group: with a mode of
// 0440, 0550 or 0750 the run user reads it and cannot change it.
func (r runUser) share(path string, mode os.FileMode) error {
	if r.changes() {
		if err := os.Lchown(path, 0, r.gid); err != nil {
			return err
		}
	}
	return os.Chmod(path, mode)
}

// mkdirAll makes dir and each parent that is not there, and gives each
// directory that it made to the run user. A directory that was there
// stays as it is.
func (r runUser) mkdirAll(dir string) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	}
	if parent := filepath.Dir(dir); parent != dir {
		if err := r.mkdirAll(parent); err != nil {
			return err
		}
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return r.give(dir)
}

// checkDir checks that the run user can reach dir (the agent's socket and
// the secrets directory are in it) and cannot change it: it cannot write
// dir or a parent of it, so that it cannot put a link where the wrapper
// writes (the upload state), or move the secrets directory. A sticky
// directory (such as /tmp) is not a write when another user owns the
// entry.
func (r runUser) checkDir(dir string) error {
	if !r.changes() {
		return nil
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for path, child := dir, true; ; path, child = filepath.Dir(path), false {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("no owner of %s", path)
		}
		bits := r.bits(st)
		if bits&1 == 0 {
			return fmt.Errorf("the run user %s cannot reach %s, so it cannot reach the agent's socket in %s (%s)", r.name, path, dir, EnvDir)
		}
		// The owner can change the mode; a write in a directory that is
		// not sticky moves each entry; a write in dir itself adds one.
		sticky := info.Mode()&os.ModeSticky != 0
		if int(st.Uid) == r.uid || bits&2 != 0 && (child || !sticky) {
			return fmt.Errorf("the run user %s can change %s, so it could change what the wrapper (root) writes in %s (%s)", r.name, path, dir, EnvDir)
		}
		if filepath.Dir(path) == path {
			return nil
		}
	}
}

// writeSecret writes a secret file for the run user: a new file that root
// owns, in the run user's group, with mode (0440 to read, 0550 to run).
// Each file of the secrets directory is for the run user. A secret that
// only the wrapper holds (the Execution's child token) is never in a file.
func writeSecret(r runUser, path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return r.share(path, mode)
}

// bits are the permission bits (rwx) that apply to the run user on a file
// with st.
func (r runUser) bits(st *syscall.Stat_t) uint32 {
	mode := st.Mode & 0o777
	switch {
	case int(st.Uid) == r.uid:
		return mode >> 6
	case r.inGroup(int(st.Gid)):
		return mode >> 3 & 7
	default:
		return mode & 7
	}
}

func (r runUser) inGroup(gid int) bool {
	if gid == r.gid {
		return true
	}
	if r.credential != nil {
		for _, g := range r.credential.Groups {
			if int(g) == gid {
				return true
			}
		}
	}
	return false
}
