package wrapper

import (
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

// setup makes the workspace, the run user's home and the caches folder,
// writes the forge token and the git configuration outside the checkout,
// and clones the repository into its fixed checkout path, on the Run's
// branch: the first Execution makes the branch from the base branch, and
// a later one checks it out.
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
	for _, dir := range []string{secrets, e.cfg.workspace, e.cfg.home, e.cfg.cache} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := e.writeGitConfig(secrets); err != nil {
		return err
	}
	checkout := e.cfg.checkout(repository)
	if err := os.MkdirAll(filepath.Dir(checkout), 0o700); err != nil {
		return err
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
		if err := os.WriteFile(token, []byte(e.task.ForgeToken), 0o600); err != nil {
			return err
		}
		if strings.ContainsAny(e.task.ForgeUsername, "\n\x00") {
			return errors.New("the forge user name has a new line")
		}
		script := "#!/bin/sh\n# The git credential helper of the Execution: the forge connection's token.\n" +
			"test \"$1\" = get || exit 0\n" +
			"printf 'username=%s\\n' " + shellQuote(e.task.ForgeUsername) + "\n" +
			"printf 'password=%s\\n' \"$(cat '" + token + "')\"\n"
		if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
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
	return os.WriteFile(e.cfg.gitConfig(), []byte(b.String()), 0o600)
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
// did not answer), the commits go to refs/agent-wip/<run id>, which
// cannot conflict. The text that push gives then goes on the end call.
func (e *execution) push(environ []string) string {
	if e.checkout == "" {
		return ""
	}
	tip, err := readBranch(filepath.Join(e.checkout, ".git"), e.task.Branch)
	if err != nil {
		return e.pushFailed(fmt.Sprintf("the push before the end: %v", err))
	}
	if tip == e.startTip {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushWait)
	defer cancel()
	dir, err := os.MkdirTemp(e.cfg.dir, "push-")
	if err != nil {
		return e.pushFailed(fmt.Sprintf("the push before the end: %v", err))
	}
	defer os.RemoveAll(dir)
	env := e.pushEnv(environ, dir)
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", args[0], err, lastLines(string(out), 5))
		}
		return nil
	}
	if err := run("init", "--quiet", "--bare", dir); err != nil {
		return e.pushFailed(fmt.Sprintf("the push before the end: %v", err))
	}
	alternates := filepath.Join(e.checkout, ".git", "objects") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "objects", "info", "alternates"), []byte(alternates), 0o600); err != nil {
		return e.pushFailed(fmt.Sprintf("the push before the end: %v", err))
	}
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
	wip := "refs/agent-wip/" + e.task.RunID
	if werr := run("push", "--quiet", "--no-verify", "--force", "--", e.task.CloneURL, tip+":"+wip); werr != nil {
		return e.pushFailed(fmt.Sprintf("the push of the Run's branch before the end failed (%v), and the save of its commits to %s failed too (%v)", err, wip, werr))
	}
	return e.pushFailed(fmt.Sprintf("the push of the Run's branch before the end failed (%v); its commits are in %s", err, wip))
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
	ref := "refs/heads/" + branch
	if info, err := os.Lstat(filepath.Join(gitDir, ref)); err == nil && info.Mode().IsRegular() {
		b, err := os.ReadFile(filepath.Join(gitDir, ref))
		if err != nil {
			return "", err
		}
		if id := strings.TrimSpace(string(b)); objectID(id) {
			return id, nil
		}
		return "", fmt.Errorf("the Run's branch %s is not a commit id", branch)
	}
	if b, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if id, name, ok := strings.Cut(line, " "); ok && name == ref && objectID(id) {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("the checkout has no Run's branch %s", branch)
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

// gitEnv is the environment of git and of the harness: the run user's
// home and git configuration, and the caches folder. The harness also
// gets the fixed Claude Code paths (uploads.claudeEnv).
func (e *execution) gitEnv(environ []string, socket string) []string {
	return append(harnessEnv(environ, socket),
		"HOME="+e.cfg.home,
		"GIT_CONFIG_GLOBAL="+e.cfg.gitConfig(),
		"XDG_CACHE_HOME="+e.cfg.cache)
}

// gitWait is how long a git command's output can stay open after ctx
// ended and its process group was killed.
const gitWait = 2 * time.Second

// git runs git, and gives its output and exit code.
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), cmd.ProcessState.ExitCode(), fmt.Errorf("git %s: %v: %s", args[0], err, lastLines(string(out), 20))
	}
	return string(out), 0, nil
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
