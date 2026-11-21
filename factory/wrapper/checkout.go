package wrapper

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// pushWait is the limit of the push before the end, and pushRetryWait
// the wait before its second try.
const (
	pushWait      = 2 * time.Minute
	pushRetryWait = 2 * time.Second
)

// The git identity of the agent's commits on the run user's new home.
const (
	commitName  = "SolidTrace Factory"
	commitEmail = "factory@solidtrace.invalid"
)

// setup makes the workspace, the run user's home and the caches folder
// for the run user, writes the forge token and the git configuration
// outside the checkout, and clones the repository into its fixed checkout
// path as the run user, on the Run's branch: the first Execution makes
// the branch from the base branch, and a later one checks it out. Root
// writes each file before the run user's first process (the first git
// call), and after it writes in no folder of the run user's.
func (e *execution) setup(ctx context.Context, environ []string) error {
	repository, err := checkRepository(e.task.Repository)
	if err != nil {
		return err
	}
	folder, err := checkFolder(e.task.Folder)
	if err != nil {
		return err
	}
	if e.task.Branch == "" {
		return errors.New("the task names no Run's branch")
	}
	secrets := e.cfg.secrets()
	if strings.ContainsAny(secrets, "'\n") {
		return fmt.Errorf("the secrets directory %q has a quote or a new line", secrets)
	}
	// A wrapper that did not get to its clean-up (it was killed, or the
	// machine lost its power) left its checkout and home at the fixed
	// paths: git cannot clone into a folder that is not empty.
	for _, dir := range []string{secrets, e.cfg.workspace, e.cfg.home} {
		if err := removeAll(dir); err != nil {
			return fmt.Errorf("delete what an earlier Execution left: %w", err)
		}
	}
	// The secrets directory is root's: the run user reads its files and
	// cannot change them.
	if err := os.Mkdir(secrets, 0o700); err != nil {
		return err
	}
	if err := e.runUser.share(secrets, 0o750); err != nil {
		return err
	}
	for _, dir := range []string{e.cfg.workspace, e.cfg.home, e.cfg.cache} {
		if err := e.runUser.mkdirAll(dir); err != nil {
			return err
		}
	}
	if err := e.writeGitConfig(secrets); err != nil {
		return err
	}
	if err := e.writeMCP(); err != nil {
		return err
	}
	checkout := e.cfg.checkout(repository)
	if err := e.runUser.mkdirAll(filepath.Dir(checkout)); err != nil {
		return err
	}
	// Before the run user's first process: the uploads read through the
	// open project directory.
	if err := e.uploads.prepare(e.runUser); err != nil {
		return fmt.Errorf("make the project directory: %w", err)
	}
	branch := e.task.Branch
	_, code, err := e.git(ctx, environ, "", "ls-remote", "--exit-code", "--heads", "--", e.task.CloneURL, "refs/heads/"+branch)
	pushed := err == nil
	if err != nil && code != 2 {
		return fmt.Errorf("clone %s: find the Run's branch %s: %w", e.task.Repository, branch, err)
	}
	clone := []string{"clone", "--quiet", "--recurse-submodules"}
	switch {
	case pushed:
		clone = append(clone, "--branch", branch)
	case e.task.BaseBranch != "":
		clone = append(clone, "--branch", e.task.BaseBranch)
	}
	clone = append(clone, "--", e.task.CloneURL, checkout)
	if _, _, err := e.git(ctx, environ, "", clone...); err != nil {
		return fmt.Errorf("clone %s: %w", e.task.Repository, err)
	}
	e.checkout = checkout
	if !pushed {
		// The Run's branch, with origin as its upstream, so that the
		// agent's own 'git push' goes to it.
		for _, args := range [][]string{
			{"checkout", "--quiet", "--no-track", "-b", branch},
			{"config", "branch." + branch + ".remote", "origin"},
			{"config", "branch." + branch + ".merge", "refs/heads/" + branch},
		} {
			if _, _, err := e.git(ctx, environ, checkout, args...); err != nil {
				return fmt.Errorf("make the Run's branch %s: %w", branch, err)
			}
		}
	}
	tip, _, err := e.git(ctx, environ, checkout, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("find the start of the Run's branch %s: %w", branch, err)
	}
	e.startTip = strings.TrimSpace(tip)
	e.workDir = filepath.Join(checkout, folder)
	if info, err := os.Stat(e.workDir); err != nil || !info.IsDir() {
		return fmt.Errorf("the repository %s has no folder %s, the Project's folder", e.task.Repository, folder)
	}
	// After the clone, before the setup command and the harness: the
	// Agent session directory of the Run, so that a resume works on this
	// machine.
	if e.task.Resume {
		resumed, err := e.uploads.restore(ctx, e.runUser)
		if err != nil {
			return fmt.Errorf("restore the Agent session %s: %w", e.task.SessionID, err)
		}
		e.resumed = resumed
	}
	if e.task.Setup != "" {
		return e.runSetupCommand(ctx, environ)
	}
	return nil
}

// writeGitConfig writes the run user's git configuration: the credential
// helper with the forge token, git over ssh rewritten to HTTPS, and the
// commit identity. The checkout's own .git/config gets none of it.
func (e *execution) writeGitConfig(secrets string) error {
	var b strings.Builder
	gitURL := strings.TrimRight(e.task.GitURL, "/")
	if e.task.GitURL != "" {
		base, err := url.Parse(e.task.GitURL)
		if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" ||
			strings.ContainsAny(e.task.GitURL, "\"\\\n") {
			return fmt.Errorf("the forge's git URL %q is not an HTTPS URL", e.task.GitURL)
		}
	}
	if e.task.ForgeToken != "" {
		if gitURL == "" {
			return errors.New("the task gives a forge token but no git URL of the forge")
		}
		helper := filepath.Join(secrets, "git-credential")
		token := filepath.Join(secrets, "forge-token")
		if err := writeSecret(e.runUser, token, []byte(e.task.ForgeToken), 0o440); err != nil {
			return err
		}
		if strings.ContainsAny(e.task.ForgeUsername, "\n\x00") {
			return errors.New("the forge user name has a new line")
		}
		script := "#!/bin/sh\n# The git credential helper of the Execution: the forge connection's token.\n" +
			"test \"$1\" = get || exit 0\n" +
			"printf 'username=%s\\n' " + shellQuote(e.task.ForgeUsername) + "\n" +
			"printf 'password=%s\\n' \"$(cat '" + token + "')\"\n"
		if err := writeSecret(e.runUser, helper, []byte(script), 0o550); err != nil {
			return err
		}
		// Only for the forge's git URL: another host that asks for
		// credentials gets none. The empty value first: no helper of the
		// system's configuration.
		b.WriteString("[credential \"" + gitURL + "/\"]\n\thelper =\n\thelper = " + gitValue(helper) + "\n")
	}
	if gitURL != "" {
		host, _, _ := strings.Cut(e.task.Repository, "/")
		b.WriteString("[url \"" + gitURL + "/\"]\n" +
			"\tinsteadOf = " + gitValue("git@"+host+":") + "\n" +
			"\tinsteadOf = " + gitValue("ssh://git@"+host+"/") + "\n")
	}
	b.WriteString("[user]\n\tname = " + gitValue(commitName) + "\n\temail = " + gitValue(commitEmail) + "\n")
	// The run user's own file in its home: no secret is in it.
	path := e.cfg.gitConfig()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(b.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return e.runUser.give(path)
}

// gitValue is s as a value in a git configuration file.
func gitValue(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// push pushes each commit of the Run's branch that is not on the remote,
// before the end. It runs after the kill, as the wrapper's user, so it
// reads nothing that the agent can change: no configuration and no hook
// of the checkout or of the run user's home. A bare repository of the
// wrapper borrows the checkout's objects (as data only), and pushes the
// branch's tip to the clone URL of the task.
//
// When the push fails twice (a person pushed to the branch, or the forge
// did not answer), the commits go to the Run's work-in-progress ref
// (e.wipRef), which cannot conflict. The text that push gives then goes
// on the end call. saved is the parent of the work-in-progress save that
// the WIP ref holds now ("": none). When it is the branch's tip, the save
// has these commits already, and the WIP ref keeps the save.
func (e *execution) push(environ []string, saved string) string {
	// A pr call that pushes now ends first.
	e.prMu.Lock()
	defer e.prMu.Unlock()
	if e.checkout == "" {
		return ""
	}
	tip, err := readBranch(filepath.Join(e.checkout, ".git"), e.task.Branch)
	if err != nil {
		return e.pushFailed(fmt.Sprintf("the push before the end: %v", err))
	}
	if tip == e.startTip || tip == e.pushedTip {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushWait)
	defer cancel()
	run, done, err := e.pushRepository(ctx, environ)
	if err != nil {
		return e.pushFailed(fmt.Sprintf("the push before the end: %v", err))
	}
	defer done()
	branch := "refs/heads/" + e.task.Branch
	err = run("push", "--quiet", "--no-verify", "--", e.task.CloneURL, tip+":"+branch)
	if err != nil {
		select {
		case <-time.After(pushRetryWait):
		case <-ctx.Done():
		}
		err = run("push", "--quiet", "--no-verify", "--", e.task.CloneURL, tip+":"+branch)
	}
	if err == nil {
		return ""
	}
	wip, werr := e.wipRef()
	if werr == nil && saved == tip {
		return e.pushFailed(fmt.Sprintf("the push of the Run's branch before the end failed (%v); its commits are in the work-in-progress save in %s", err, wip))
	}
	if werr != nil {
		return e.pushFailed(fmt.Sprintf("the push of the Run's branch before the end failed (%v), and its commits cannot go to a work-in-progress ref: %v", err, werr))
	}
	if werr := run("push", "--quiet", "--no-verify", "--force", "--", e.task.CloneURL, tip+":"+wip); werr != nil {
		return e.pushFailed(fmt.Sprintf("the push of the Run's branch before the end failed (%v), and the save of its commits to %s failed too (%v)", err, wip, werr))
	}
	return e.pushFailed(fmt.Sprintf("the push of the Run's branch before the end failed (%v); its commits are in %s", err, wip))
}

// pushForPR pushes the Run's branch before a pr call, so that the forge
// has the agent's commits: with no force, in the same way as the push
// before the end. The caller holds e.prMu. A failure is for the agent to
// act on, such as a person's push to the branch.
func (e *execution) pushForPR(ctx context.Context, environ []string) error {
	tip, err := readBranch(filepath.Join(e.checkout, ".git"), e.task.Branch)
	if err != nil {
		return fmt.Errorf("read the Run's branch %s: %w", e.task.Branch, err)
	}
	ctx, cancel := context.WithTimeout(ctx, pushWait)
	defer cancel()
	run, done, err := e.pushRepository(ctx, environ)
	if err != nil {
		return err
	}
	defer done()
	if err := run("push", "--quiet", "--no-verify", "--", e.task.CloneURL, tip+":refs/heads/"+e.task.Branch); err != nil {
		return fmt.Errorf("the push of the Run's branch %s failed (pull, merge and try again): %w", e.task.Branch, err)
	}
	e.pushedTip = tip
	return nil
}

// pushRepository makes the wrapper's bare repository for a push, which
// borrows the objects of these object folders (none: the checkout's). run
// runs git in it, and done removes it.
func (e *execution) pushRepository(ctx context.Context, environ []string, objects ...string) (run func(args ...string) error, done func(), err error) {
	dir, err := os.MkdirTemp(e.cfg.dir, "push-")
	if err != nil {
		return nil, nil, err
	}
	done = func() { os.RemoveAll(dir) }
	env := e.pushEnv(environ, dir)
	run = func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir, cmd.Env = dir, env
		// When ctx ends (the preempting marker, or pushWait), the push's
		// process group is killed: git and its remote helper, which holds
		// the output open. After gitWait the wait stops all the same.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
		cmd.WaitDelay = gitWait
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", args[0], err, lastLines(string(out), 5))
		}
		return nil
	}
	if err := run("init", "--quiet", "--bare", dir); err != nil {
		done()
		return nil, nil, err
	}
	if len(objects) == 0 {
		objects = []string{filepath.Join(e.checkout, ".git", "objects")}
	}
	alternates := strings.Join(objects, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "objects", "info", "alternates"), []byte(alternates), 0o600); err != nil {
		done()
		return nil, nil, err
	}
	return run, done, nil
}

// pushFailed writes the push's failure to the wrapper's log, and gives it.
func (e *execution) pushFailed(text string) string {
	fmt.Fprintln(e.log, "wrapper:", text)
	return text
}

// pushEnv is the environment of the wrapper's push: no git configuration
// but the system's, and the forge token in an HTTP header, not in a
// helper program.
func (e *execution) pushEnv(environ []string, dir string) []string {
	var env []string
	for _, kv := range harnessEnv(environ, "") {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	if e.task.ForgeToken != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(e.task.ForgeUsername + ":" + e.task.ForgeToken))
		env = append(env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+auth)
	}
	return env
}

// readBranch gives the commit of a branch in a git directory, from its
// loose ref or its packed-refs line. It runs no git, so it reads no
// configuration of the agent.
func readBranch(gitDir, branch string) (string, error) {
	if info, err := os.Lstat(gitDir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("the checkout has no .git folder")
	}
	id, err := readRef(gitDir, "refs/heads/"+branch)
	if errors.Is(err, errNoRef) {
		return "", fmt.Errorf("the checkout has no Run's branch %s", branch)
	}
	if err != nil {
		return "", fmt.Errorf("the Run's branch %s: %w", branch, err)
	}
	return id, nil
}

// errNoRef is a ref that a git directory does not have.
var errNoRef = errors.New("no such ref")

// readRef gives the commit of a ref (such as refs/heads/main) in a git
// directory, from its loose ref or its packed-refs line. It runs no git.
func readRef(gitDir, ref string) (string, error) {
	if info, err := os.Lstat(filepath.Join(gitDir, ref)); err == nil && info.Mode().IsRegular() {
		b, err := os.ReadFile(filepath.Join(gitDir, ref))
		if err != nil {
			return "", err
		}
		if id := strings.TrimSpace(string(b)); objectID(id) {
			return id, nil
		}
		return "", fmt.Errorf("%s is not a commit id", ref)
	}
	if b, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if id, name, ok := strings.Cut(line, " "); ok && name == ref && objectID(id) {
				return id, nil
			}
		}
	}
	return "", errNoRef
}

// readHead gives the commit of HEAD in a git directory: the commit of the
// branch that it names, or its own commit id when it is detached.
func readHead(gitDir string) (string, error) {
	path := filepath.Join(gitDir, "HEAD")
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s has no HEAD file", gitDir)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(string(b))
	if objectID(head) {
		return head, nil
	}
	ref, ok := strings.CutPrefix(head, "ref: ")
	if !ok || !strings.HasPrefix(ref, "refs/heads/") || !plainParts(strings.Split(ref, "/")) {
		return "", fmt.Errorf("the HEAD of %s is not a branch or a commit", gitDir)
	}
	id, err := readRef(gitDir, ref)
	if errors.Is(err, errNoRef) {
		return "", fmt.Errorf("the branch %s of %s has no commit", strings.TrimPrefix(ref, "refs/heads/"), gitDir)
	}
	return id, err
}

// objectID tells if s is a SHA-1 or SHA-256 object id.
func objectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// cleanUp deletes the workspace, the run user's home and the secrets after
// the Execution. The caches stay.
func (e *execution) cleanUp() {
	for _, dir := range []string{e.cfg.workspace, e.cfg.home, e.cfg.secrets()} {
		if err := removeAll(dir); err != nil {
			fmt.Fprintln(e.log, "wrapper: delete after the Execution:", err)
		}
	}
}

// removeSecrets deletes the secret files of the Execution, before its
// end call.
func (e *execution) removeSecrets() {
	if err := removeAll(e.cfg.secrets()); err != nil {
		fmt.Fprintln(e.log, "wrapper: delete after the Execution:", err)
	}
}

// removeAll is os.RemoveAll, also of a folder that the agent made read
// only.
func removeAll(dir string) error {
	if os.RemoveAll(dir) == nil {
		return nil
	}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}

// gitEnv is the environment of the run user's processes (git, the setup
// command, the checks and the harness): the run user's names, its home
// and git configuration, and the caches folder. The harness also gets the
// fixed Claude Code paths (uploads.claudeEnv). The fixed home comes after
// the run user's home of the machine, so it is the HOME that counts.
func (e *execution) gitEnv(environ []string, socket string) []string {
	return append(e.runUser.env(harnessEnv(environ, socket)),
		"HOME="+e.cfg.home,
		"GIT_CONFIG_GLOBAL="+e.cfg.gitConfig(),
		"XDG_CACHE_HOME="+e.cfg.cache)
}

// gitWait is how long a git command's output can stay open after ctx
// ended and its process group was killed.
const gitWait = 2 * time.Second

// git runs git as the run user in the Execution's kill boundary, as each
// process of the run user: a hook that git starts is in the child cgroup,
// and the end kills what it leaves. It gives git's output and exit code.
// Root never runs git in the checkout: its hooks and its .git/config are
// the agent's.
func (e *execution) git(ctx context.Context, environ []string, dir string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// When ctx ends (a stop or a limit), the command's process group is
	// killed: git and its remote helper, which holds the output open.
	// After gitWait the wait stops all the same.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = gitWait
	cmd.Env = append(e.gitEnv(environ, ""), "GIT_TERMINAL_PROMPT=0")
	e.runUser.start(cmd)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := e.boundary.Start(cmd)
	if err == nil {
		// A process that git started and that holds the output open does
		// not hold the setup.
		if err = cmd.Wait(); errors.Is(err, exec.ErrWaitDelay) {
			err = nil
		}
	}
	if err != nil {
		return out.String(), cmd.ProcessState.ExitCode(), fmt.Errorf("git %s: %v: %s", args[0], err, lastLines(out.String(), 20))
	}
	return out.String(), 0, nil
}

// projectDirName is the Claude Code project directory name of a
// repository: the same on each machine, so that a resume finds the Agent
// session.
func projectDirName(repository string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, repository)
}

// checkRepository gives repository (host/workspace/name) when each part
// is a plain name, so that the checkout path stays in the workspace.
func checkRepository(repository string) (string, error) {
	parts := strings.Split(repository, "/")
	if len(parts) < 3 || !plainParts(parts) {
		return "", fmt.Errorf("the repository %q is not host/workspace/name", repository)
	}
	return repository, nil
}

// checkFolder gives the Project's folder, "" for the root.
func checkFolder(folder string) (string, error) {
	if folder == "" {
		return "", nil
	}
	if !plainParts(strings.Split(folder, "/")) {
		return "", fmt.Errorf("the Project's folder %q is not a path in the repository", folder)
	}
	return folder, nil
}

func plainParts(parts []string) bool {
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, "\\\x00\n") {
			return false
		}
	}
	return true
}
