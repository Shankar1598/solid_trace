package capture_test

import (
	"testing"

	"github.com/solidtrace/cli/capture"
)

// A host's SSH forms and its HTTPS form give one repository name, so an
// entry of the captured repositories matches a clone of either form.
func TestNormaliseGivesOneNameForTheSSHAndHTTPSFormsOfAHost(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"https://bitbucket.org/ws/repo.git", "bitbucket.org/ws/repo"},
		{"git@bitbucket.org:ws/repo.git", "bitbucket.org/ws/repo"},
		{"git@ssh.bitbucket.org:ws/repo.git", "bitbucket.org/ws/repo"},
		{"ssh://git@ssh.bitbucket.org/ws/repo.git", "bitbucket.org/ws/repo"},
		{"ssh://git@altssh.bitbucket.org:443/ws/repo.git", "bitbucket.org/ws/repo"},
		{"https://github.com/acme/shop", "github.com/acme/shop"},
		{"git@github.com:acme/shop.git", "github.com/acme/shop"},
		{"ssh://git@ssh.github.com:443/acme/shop.git", "github.com/acme/shop"},
		{"https://gitlab.com/acme/shop.git", "gitlab.com/acme/shop"},
		{"git@gitlab.com:acme/shop.git", "gitlab.com/acme/shop"},
		{"ssh://git@altssh.gitlab.com:443/acme/shop.git", "gitlab.com/acme/shop"},
		{"git@Git.Example.com:team/app.git", "git.example.com/team/app"},
	} {
		got, ok := capture.Normalise(tc.name)
		if !ok || got != tc.want {
			t.Errorf("Normalise(%q) = %q, %v; want %q", tc.name, got, ok, tc.want)
		}
	}
}

func TestACapturedRepositoryMatchesACloneOfEitherForm(t *testing.T) {
	name, _ := capture.Normalise("git@ssh.bitbucket.org:ws/repo.git")
	for _, entry := range []string{"bitbucket.org/ws/repo", "bitbucket.org/ws/*", "Bitbucket.org/ws/repo.git"} {
		if !capture.Matches(entry, name) {
			t.Errorf("Expected %q to match %q", entry, name)
		}
	}
}
