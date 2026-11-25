package capture

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// Repository is the git repository of an Agent session's working
// directory, as the match found it.
type Repository struct {
	// Name is the matched remote as host/workspace/name.
	Name string
	// Folder is the working directory's path inside the repository, or "".
	Folder string
}

// gitEnvironment is the hook's environment with no variable that points
// git at another repository than the working directory's.
func gitEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
			"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CEILING_DIRECTORIES":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0")
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnvironment()
	out, err := cmd.Output()
	return string(out), err
}

// Match finds the git repository of the working directory and the first
// of its remotes that matches an entry of the captured repositories. ok is
// false outside a git repository, and when no remote matches. A worktree
// has the remotes of its repository, so it matches through them.
func Match(ctx context.Context, cwd string, entries []string) (repo Repository, ok bool, err error) {
	names, folder, ok, err := Remotes(ctx, cwd)
	if !ok || err != nil {
		return repo, false, err
	}
	for _, name := range names {
		for _, entry := range entries {
			if Matches(entry, name) {
				return Repository{Name: name, Folder: folder}, true, nil
			}
		}
	}
	return repo, false, nil
}

// Remotes gives the remotes of the git repository of the working
// directory as host/workspace/name, each one time, and the working
// directory's folder in the repository ("" at the root). The remotes as
// configured come first, then as git rewrites them (url.insteadOf);
// "origin" comes first in each. ok is false outside a git repository.
func Remotes(ctx context.Context, cwd string) (names []string, folder string, ok bool, err error) {
	if cwd == "" {
		return nil, "", false, nil
	}
	prefix, err := git(ctx, cwd, "rev-parse", "--show-prefix")
	if err != nil {
		// Not a repository, or one that git refuses to read.
		return nil, "", false, nil
	}
	folder = strings.Trim(strings.TrimSpace(prefix), "/")
	// The remotes as configured, then as git rewrites them (url.insteadOf).
	// "origin" comes first in each, then the others in git's order.
	configured, err := git(ctx, cwd, "config", "--get-regexp", `^remote\..*\.url$`)
	if err != nil && configured != "" {
		return nil, "", false, err
	}
	rewritten, err := git(ctx, cwd, "remote", "-v")
	if err != nil {
		return nil, "", false, err
	}
	add := func(remote, url string) {
		name, ok := Normalise(url)
		if !ok {
			return
		}
		if remote == "origin" {
			names = append([]string{name}, names...)
		} else {
			names = append(names, name)
		}
	}
	for _, line := range strings.Split(configured, "\n") {
		if key, url, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			add(strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url"), url)
		}
	}
	first := names
	names = nil
	for _, line := range strings.Split(rewritten, "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 {
			add(fields[0], fields[1])
		}
	}
	var unique []string
	for _, name := range append(first, names...) {
		if !slices.Contains(unique, name) {
			unique = append(unique, name)
		}
	}
	return unique, folder, true, nil
}

// sshHosts are the hosts that some services use only for git over SSH,
// with the host of the same service for HTTPS.
var sshHosts = map[string]string{
	"ssh.bitbucket.org":    "bitbucket.org",
	"altssh.bitbucket.org": "bitbucket.org",
	"ssh.github.com":       "github.com",
	"altssh.gitlab.com":    "gitlab.com",
}

// Normalise gives a git remote URL as host/workspace/name: the ssh, scp
// and HTTPS forms give the same name, with the host in lower case, no
// user, no port and no .git ending. The SSH-only host of a service, as
// ssh.bitbucket.org, gives the host of its HTTPS form. ok is false for a local path or a
// remote with no workspace.
func Normalise(remote string) (string, bool) {
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return "", false
		}
		switch u.Scheme {
		case "https", "http", "ssh", "git", "git+ssh", "ssh+git":
		default:
			return "", false
		}
		host, path = u.Hostname(), u.Path
	} else {
		// scp form: [user@]host:path. A colon after a slash is a local path.
		colon := strings.Index(remote, ":")
		if colon <= 0 || strings.Contains(remote[:colon], "/") {
			return "", false
		}
		host, path = remote[:colon], remote[colon+1:]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
	}
	host = strings.ToLower(host)
	if web, ok := sshHosts[host]; ok {
		host = web
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if host == "" || len(parts) < 2 {
		return "", false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, "~") {
			return "", false
		}
	}
	return host + "/" + path, true
}

// Matches reports whether a normalised repository matches a captured
// repositories entry: host/workspace/name, or host/workspace/* for each
// repository of the workspace. Case is ignored on both sides, because the
// hosts' names do not depend on it and Claude Code's telemetry gives them
// in lower case. Spaces around the entry and a .git ending are ignored too.
// EventStore matches with the same rule (event_store/captured), and both
// run the cases in testdata/captured-repository-cases.json.
func Matches(entry, repository string) bool {
	entry = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(entry)), ".git")
	repository = strings.TrimSuffix(strings.ToLower(repository), ".git")
	if workspace, ok := strings.CutSuffix(entry, "/*"); ok {
		return strings.HasPrefix(repository, workspace+"/")
	}
	return repository == entry
}
