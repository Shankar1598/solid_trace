// Package forge is the Forge interface of the factory and its MVP
// implementation, Bitbucket Cloud. The Dispatcher opens and reads PRs and
// deletes a Run's work-in-progress ref through it; the wrapper clones and
// pushes with its credential helper and saves the work-in-progress ref.
// The Dispatcher reads a PR's comments and posts its own through it.
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
	// DeleteWIP deletes the Run's work-in-progress ref, and the ref
	// <WIP ref>-kept that keeps a save after a person's push. A ref that
	// is not there is not an error.
	DeleteWIP(ctx context.Context, repository, runID string) error
	// DefaultBranchFile reads the first of paths that is on the
	// repository's default branch, all at the branch's one commit. It
	// gives the path and the content, or "" and no error when none of
	// the paths is there.
	DefaultBranchFile(ctx context.Context, repository string, paths ...string) (string, []byte, error)
	// Private is true when only the people that the repository lets in
	// can see it, and false for a public repository.
	Private(ctx context.Context, repository string) (bool, error)
	// PRComments gives each comment on a PR, the oldest first: general
	// and inline comments, replies, and resolved and deleted ones.
	PRComments(ctx context.Context, repository string, id int) ([]Comment, error)
	// PostPRComment posts a general comment on a PR, or a reply to the
	// comment Parent.
	PostPRComment(ctx context.Context, repository string, id int, comment NewComment) (Comment, error)
}

// Account is an account on the forge: the author of a PR or a comment.
type Account struct {
	// ID is the forge's account id, and UUID its other fixed id.
	ID, UUID string
	// Name is the name that the forge shows.
	Name string
	// Kind is the forge's kind of account: AccountPerson for a person.
	// Bitbucket gives app_user for an access token or an app.
	Kind string
}

// AccountPerson is the Kind of a person's account.
const AccountPerson = "user"

// Person is true for a person's account, and false for a bot, an app or
// an access token.
func (a Account) Person() bool { return a.Kind == AccountPerson }

// Same is true when a and b are one account. An account with no id is
// never the same as another.
func (a Account) Same(b Account) bool {
	return (a.UUID != "" && a.UUID == b.UUID) || (a.ID != "" && a.ID == b.ID)
}

// Comment is a comment on a PR.
type Comment struct {
	ID     int
	Author Account
	Text   string
	// File and Line are the place of an inline comment ("" and 0 for a
	// general comment). A reply has the place of its thread.
	File string
	Line int
	// Parent is the comment that this one replies to (0 is none).
	Parent int
	// Resolved is true when a person resolved the comment's thread on
	// this comment.
	Resolved bool
	// Deleted is a comment that its author deleted. Pending is a comment
	// of a review that its author did not submit: only the author sees
	// it.
	Deleted, Pending bool
	// Edited is the forge's time of the last change: an edit changes it.
	Edited string
}

// NewComment is a comment to post: a general comment, or a reply to the
// comment Parent.
type NewComment struct {
	Text   string
	Parent int
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
	// Author is the account that opened the PR: for a Run's PR, the
	// factory's own identity on the forge.
	Author Account
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
