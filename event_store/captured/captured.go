// Package captured matches repositories with the Captured repositories of
// an Organization's Agent Project: the repository of an Agent session
// upload, and the vcs.repository.url.full of Claude Code's telemetry.
package captured

import (
	"net/url"
	"strings"
)

// Matches reports whether a repository, host/workspace/name, matches an
// entry of the Captured repositories: host/workspace/name, or
// host/workspace/* for each repository of the workspace. Case is ignored on
// both sides, because the hosts' names do not depend on it and Claude
// Code's telemetry gives them in lower case. Spaces around the entry and a
// .git ending are ignored too.
func Matches(entry, repository string) bool {
	entry = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(entry)), ".git")
	repository = strings.TrimSuffix(strings.ToLower(repository), ".git")
	if workspace, ok := strings.CutSuffix(entry, "/*"); ok {
		return strings.HasPrefix(repository, workspace+"/")
	}
	return repository == entry
}

// URLNames gives the names of the repository at a vcs.repository.url.full
// value, as the CLI names a clone of it: host/path, with no user, password,
// port or .git ending. Claude Code gives a Bitbucket Server repository as
// its web address, projects/<project>/repos/<name>, so that path gives the
// two names that the CLI gives its clones: host/scm/<project>/<name> and
// host/<project>/<name>. A value that is not an http or https URL with a
// workspace and a name gives none.
func URLNames(value string) []string {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	path := strings.Trim(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return nil
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, "~") {
			return nil
		}
	}
	if len(parts) == 4 && parts[0] == "projects" && parts[2] == "repos" {
		return []string{host + "/scm/" + parts[1] + "/" + parts[3], host + "/" + parts[1] + "/" + parts[3]}
	}
	return []string{host + "/" + path}
}

// Filter keeps the OpenTelemetry records of the Captured repositories,
// which are all that the Agent Project's key may store.
type Filter struct {
	entries []string
}

// NewFilter is the filter of a list of Captured repositories. With no
// entries, it keeps no record.
func NewFilter(entries []string) *Filter {
	return &Filter{entries: entries}
}

// Keeps reports whether a record whose own vcs.repository.url.full is value
// ("" for none) is of a repository that an entry matches.
func (f *Filter) Keeps(value string) bool {
	for _, name := range URLNames(value) {
		for _, entry := range f.entries {
			if Matches(entry, name) {
				return true
			}
		}
	}
	return false
}
