// Package forge is the Forge interface of the factory and its MVP
// implementation, Bitbucket Cloud. The Dispatcher opens and reads PRs and
// deletes a Run's work-in-progress ref through it; the wrapper clones and
// pushes with its credential helper and saves the work-in-progress ref.
// The comment calls come in a later ticket.
package forge

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Forge is the factory's forge: clone and push credentials, a PR and its
// state, and a work-in-progress ref that starts no CI. A repository is the
// name that the Factory settings keep: host/workspace/name, such as
// bitbucket.org/team/shop.
type Forge interface {
	// CloneURL is the repository's HTTPS clone URL. It never holds the
	// token: git gets it from the credential helper.
	CloneURL(repository string) (string, error)
	// CredentialConfig gives the git settings that make git read the
	// token from tokenFile, for the forge's host only. The token is not in
	// the settings.
	CredentialConfig(tokenFile string) ([]GitSetting, error)
	// OpenPR opens a PR. It never merges.
	OpenPR(ctx context.Context, repository string, pr NewPR) (PR, error)
	// UpdatePR changes a PR's title and description; an empty field
	// stays as it is.
	UpdatePR(ctx context.Context, repository string, id int, text PRText) (PR, error)
	// PR reads a PR and its state.
	PR(ctx context.Context, repository string, id int) (PR, error)
	// WIPRef is the name of a Run's work-in-progress ref.
	WIPRef(runID string) (string, error)
	// SaveWIP force-pushes commit, from the checkout at dir, to the Run's
	// work-in-progress ref, and gives the ref.
	SaveWIP(ctx context.Context, dir, repository, runID, commit string) (string, error)
	// DeleteWIP deletes the Run's work-in-progress ref. A ref that is not
	// there is not an error.
	DeleteWIP(ctx context.Context, repository, runID string) error
	// DefaultBranchFile reads the first of paths that is on the
	// repository's default branch, all at the branch's one commit. It
	// gives the path and the content, or "" and no error when none of
	// the paths is there.
	DefaultBranchFile(ctx context.Context, repository string, paths ...string) (string, []byte, error)
}

// PRState is the state of a PR.
type PRState string

// The states of a PR. A superseded PR is one that Bitbucket replaced.
const (
	PROpen       PRState = "OPEN"
	PRMerged     PRState = "MERGED"
	PRDeclined   PRState = "DECLINED"
	PRSuperseded PRState = "SUPERSEDED"
)

// PR is a pull request on the forge.
type PR struct {
	ID          int
	Title       string
	Description string
	// Source is the PR's branch, and Destination its base branch.
	Source      string
	Destination string
	State       PRState
	// Link is the PR's page for a person.
	Link string
}

// NewPR is a PR to open. With no Destination, the PR goes to the
// repository's main branch.
type NewPR struct {
	Source      string
	Destination string
	Title       string
	Description string
}

// PRText is a new title and description of a PR. An empty field stays.
type PRText struct {
	Title       string
	Description string
}

// GitSetting is one git configuration value.
type GitSetting struct {
	Key   string
	Value string
}

// GitArgs gives the settings as git's -c arguments, which go before the
// git command.
func GitArgs(settings []GitSetting) []string {
	var args []string
	for _, s := range settings {
		args = append(args, "-c", s.Key+"="+s.Value)
	}
	return args
}

// The two places of the work-in-progress refs. WIPOutsideBranches is the
// default: outside refs/heads, so a push starts no CI and makes no branch.
// WIPInBranches is the fallback for a forge that refuses refs outside
// refs/heads (cluster check 12); there the message's [skip ci] stops CI.
const (
	WIPOutsideBranches = "refs/agent-wip"
	WIPInBranches      = "refs/heads/agent-wip"
)

// runIDPattern is a Run id that can be part of a ref name.
var runIDPattern = regexp.MustCompile(`^[a-z0-9]{1,64}$`)

// wipRef is the work-in-progress ref of a Run under place ("" is the
// default).
func wipRef(place, runID string) (string, error) {
	if place == "" {
		place = WIPOutsideBranches
	}
	if place != WIPOutsideBranches && place != WIPInBranches {
		return "", fmt.Errorf("the place of the work-in-progress refs must be %s or %s, not %q",
			WIPOutsideBranches, WIPInBranches, place)
	}
	if !runIDPattern.MatchString(runID) {
		return "", fmt.Errorf("a Run id has 1 to 64 lower-case letters and digits, not %q", runID)
	}
	return place + "/" + runID, nil
}

// Error is a refusal or a fault of the forge, with a reason that a person
// can act on. Its text is fit for the reason on a Run, and never holds
// the token.
type Error struct {
	// Forge is the forge's name, such as Bitbucket.
	Forge string
	// Action is what the factory tried, such as "open the PR".
	Action     string
	Repository string
	// Status is the HTTP status; 0 when the forge did not answer or git
	// failed.
	Status int
	// Message is the forge's or git's own words.
	Message string
	// Hint says what a person can do.
	Hint string
}

func (e *Error) Error() string {
	var b strings.Builder
	switch {
	case e.Status != 0:
		fmt.Fprintf(&b, "%s refused to %s in %s (HTTP %d: %s)", e.Forge, e.Action, e.Repository, e.Status, e.Message)
	default:
		fmt.Fprintf(&b, "could not %s in %s: %s", e.Action, e.Repository, e.Message)
	}
	if e.Hint != "" {
		b.WriteString(". " + e.Hint)
	}
	return b.String()
}
