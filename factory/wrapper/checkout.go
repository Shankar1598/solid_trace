package wrapper

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// pushWait is the limit of the push before the end.
const pushWait = 2 * time.Minute

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
	e.workDir = filepath.Join(checkout, folder)
	if info, err := os.Stat(e.workDir); err != nil || !info.IsDir() {
		return fmt.Errorf("the repository %s has no folder %s, the Project's folder", e.task.Repository, folder)
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
// before the end. A failure goes to the wrapper's log: the end still
// goes on.
func (e *execution) push(environ []string) {
	if e.checkout == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushWait)
	defer cancel()
	branch := "refs/heads/" + e.task.Branch
	out, _, err := e.git(ctx, environ, e.checkout, "rev-list", "--count", branch, "--not", "--remotes=origin")
	if err != nil {
		fmt.Fprintln(e.log, "wrapper: find the commits that are not pushed:", err)
		return
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(out)); n == 0 {
		return
	}
	if _, _, err := e.git(ctx, environ, e.checkout, "push", "--quiet", "origin", branch+":"+branch); err != nil {
		fmt.Fprintln(e.log, "wrapper: push the Run's branch before the end:", err)
	}
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

// git runs git, and gives its output and exit code.
func (e *execution) git(ctx context.Context, environ []string, dir string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
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
