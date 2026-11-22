package capture_test

import (
	"testing"

	"github.com/solidtrace/cli/capture"
)

// A remote that is a local path, or has a part that leads out of the
// workspace, has no name: an entry of the list never matches it.
func TestNormaliseGivesNoNameForALocalPathOrAPartThatLeadsAway(t *testing.T) {
	for _, remote := range []string{
		"/srv/git/acme/shop.git",
		"../acme/shop",
		"file:///srv/acme/shop.git",
		"https://bitbucket.org/acme",
		"https://bitbucket.org/acme/../other/shop",
		"https://bitbucket.org/acme/./shop",
		"https://bitbucket.org/~acme/shop",
		"https://bitbucket.org//shop",
		"git@bitbucket.org:~/shop.git",
	} {
		if got, ok := capture.Normalise(remote); ok {
			t.Errorf("Normalise(%q) = %q, want no name", remote, got)
		}
	}
}

// host/workspace/* is the repositories of one workspace: a workspace that
// only starts with the same letters is another one.
func TestAWorkspaceEntryMatchesOnlyTheRepositoriesOfThatWorkspace(t *testing.T) {
	if !capture.Matches("bitbucket.org/acme/*", "bitbucket.org/acme/shop") {
		t.Error("the workspace entry does not match its own repository")
	}
	if capture.Matches("bitbucket.org/acme/*", "bitbucket.org/acme-tools/shop") {
		t.Error("the workspace entry matches the repository of another workspace")
	}
}
