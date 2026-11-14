package capture

import (
	"context"
	"net/url"
	"os"
	"os/exec"
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
	if cwd == "" {
		return repo, false, nil
	}
	prefix, err := git(ctx, cwd, "rev-parse", "--show-prefix")
	if err != nil {
		// Not a repository, or one that git refuses to read.
		return repo, false, nil
	}
	out, err := git(ctx, cwd, "remote", "-v")
	if err != nil {
		return repo, false, err
	}
	// "origin" comes first, then the other remotes in git's order.
	var names []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, ok := Normalise(fields[1])
		if !ok {
			continue
		}
		if fields[0] == "origin" {
			names = append([]string{name}, names...)
		} else {
			names = append(names, name)
		}
	}
	for _, name := range names {
		for _, entry := range entries {
			if Matches(entry, name) {
				return Repository{Name: name, Folder: strings.Trim(strings.TrimSpace(prefix), "/")}, true, nil
			}
		}
	}
	return repo, false, nil
}

// Normalise gives a git remote URL as host/workspace/name: the ssh, scp
// and HTTPS forms give the same name, with the host in lower case, no
// user, no port and no .git ending. ok is false for a local path or a
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
// repository of the workspace. The entry's host case and a .git ending
// are ignored too.
func Matches(entry, repository string) bool {
	host, rest, _ := strings.Cut(strings.TrimSuffix(strings.TrimSpace(entry), ".git"), "/")
	entry = strings.ToLower(host) + "/" + rest
	if workspace, ok := strings.CutSuffix(entry, "/*"); ok {
		return strings.HasPrefix(repository, workspace+"/")
	}
	return repository == entry
}
