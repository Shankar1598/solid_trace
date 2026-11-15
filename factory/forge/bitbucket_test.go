package forge_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/factory/forge"
	"github.com/solidtrace/factory/internal/fakeforge"
)

// repository is the name that the Factory settings keep, and fakeRepo the
// same repository on the fake Forge.
const (
	repository = "bitbucket.org/team/app"
	fakeRepo   = "team/app"
)

func newForge(t *testing.T) (*fakeforge.Forge, *forge.Bitbucket) {
	t.Helper()
	f := fakeforge.New(t)
	f.AddRepo(fakeRepo, map[string]string{"README.md": "hello\n"})
	return f, &forge.Bitbucket{APIURL: f.APIURL(), GitURL: f.GitURL(), Token: f.Token}
}

// git runs git with no global or system configuration, and with the
// extra arguments first, as the wrapper does.
func git(t *testing.T, dir string, extra []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append(extra, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Agent", "GIT_AUTHOR_EMAIL=agent@example.com", "GIT_COMMITTER_NAME=Agent", "GIT_COMMITTER_EMAIL=agent@example.com")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustGit(t *testing.T, dir string, extra []string, args ...string) string {
	t.Helper()
	out, err := git(t, dir, extra, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

// tokenFile writes the token to a file outside the checkout, as the
// wrapper writes its secret files.
func tokenFile(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "forge-token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// clone clones the repository with the credential helper and gives the
// checkout.
func clone(t *testing.T, f *fakeforge.Forge, b *forge.Bitbucket) string {
	t.Helper()
	url, err := b.CloneURL(repository)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := b.CredentialConfig(tokenFile(t, f.Token))
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "checkout")
	mustGit(t, "", forge.GitArgs(settings), "clone", "-q", url, work)
	return work
}

func TestTheCredentialHelperGivesGitTheTokenAndTheURLHoldsNoToken(t *testing.T) {
	f, b := newForge(t)
	url, err := b.CloneURL(repository)
	if err != nil {
		t.Fatal(err)
	}
	if url != f.CloneURL(fakeRepo) {
		t.Errorf("the clone URL is %s; want %s", url, f.CloneURL(fakeRepo))
	}
	settings, err := b.CredentialConfig(tokenFile(t, f.Token))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, f.Token) || strings.Contains(strings.Join(forge.GitArgs(settings), " "), f.Token) {
		t.Fatalf("the token is in the URL %q or in the git settings %v", url, settings)
	}

	work := clone(t, f, b)
	if remote := mustGit(t, work, nil, "remote", "get-url", "origin"); strings.Contains(remote, f.Token) {
		t.Fatalf("the remote URL %q holds the token", remote)
	}
	if body, _ := os.ReadFile(filepath.Join(work, "README.md")); string(body) != "hello\n" {
		t.Fatalf("README.md in the clone = %q", body)
	}
	if config, _ := os.ReadFile(filepath.Join(work, ".git", "config")); strings.Contains(string(config), f.Token) {
		t.Fatalf("the checkout's .git/config holds the token:\n%s", config)
	}

	// A push of the Run's branch with the same settings.
	os.WriteFile(filepath.Join(work, "fix.txt"), []byte("fixed\n"), 0o644)
	mustGit(t, work, nil, "checkout", "-q", "-b", "agent/r1")
	mustGit(t, work, nil, "add", "fix.txt")
	mustGit(t, work, nil, "commit", "-q", "-m", "Fix it")
	mustGit(t, work, forge.GitArgs(settings), "push", "-q", "origin", "agent/r1")
	if body, ok := f.File(fakeRepo, "agent/r1", "fix.txt"); !ok || body != "fixed\n" {
		t.Fatalf("fix.txt on the remote = %q %v", body, ok)
	}

	// With no credential helper, git has no token and cannot clone.
	if out, err := git(t, "", nil, "clone", "-q", url, filepath.Join(t.TempDir(), "c")); err == nil {
		t.Fatalf("a clone with no credential helper worked:\n%s", out)
	}
}

// The token goes only to the Forge's own host: a remote on another host
// gets no password from the helper.
func TestTheCredentialHelperGivesTheTokenToTheForgesHostOnly(t *testing.T) {
	f, b := newForge(t)
	settings, err := b.CredentialConfig(tokenFile(t, f.Token))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", append(forge.GitArgs(settings), "credential", "fill")...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=example.com\npath=other/app.git\n\n")
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), f.Token) {
		t.Fatalf("git gave the token to example.com:\n%s", out)
	}
}

func TestARepositoryOutsideBitbucketIsRefused(t *testing.T) {
	_, b := newForge(t)
	for _, r := range []string{"github.com/team/app", "bitbucket.org/team", "bitbucket.org/team/app/more", ""} {
		if _, err := b.CloneURL(r); err == nil {
			t.Errorf("CloneURL(%q) worked; want an error", r)
		}
		if _, err := b.OpenPR(context.Background(), r, forge.NewPR{Source: "agent/r1", Title: "T"}); err == nil {
			t.Errorf("OpenPR(%q) worked; want an error", r)
		}
	}
}

func TestOpenUpdateAndReadAPR(t *testing.T) {
	f, b := newForge(t)
	ctx := context.Background()
	f.Push(fakeRepo, "agent/r1", map[string]string{"fix.txt": "x\n"}, "Fix")

	opened, err := b.OpenPR(ctx, repository, forge.NewPR{Source: "agent/r1", Destination: "main",
		Title: "Fix the bug", Description: "The agent's text"})
	if err != nil {
		t.Fatal(err)
	}
	if opened.ID != 1 || opened.State != forge.PROpen || opened.Source != "agent/r1" || opened.Destination != "main" ||
		opened.Title != "Fix the bug" || opened.Description != "The agent's text" ||
		opened.Link != f.URL()+"/team/app/pull-requests/1" {
		t.Fatalf("the opened PR = %+v", opened)
	}
	prs := f.PRs(fakeRepo)
	if len(prs) != 1 || prs[0].Source != "agent/r1" || prs[0].Destination != "main" || prs[0].Title != "Fix the bug" {
		t.Fatalf("the PRs on the forge = %+v", prs)
	}

	updated, err := b.UpdatePR(ctx, repository, opened.ID, forge.PRText{Description: "The agent's text\n\nChecks: passed"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Fix the bug" || updated.Description != "The agent's text\n\nChecks: passed" {
		t.Fatalf("after an update of the description only: %+v; want the old title", updated)
	}
	if _, err := b.UpdatePR(ctx, repository, opened.ID, forge.PRText{Title: "Fix the bug in fix.txt"}); err != nil {
		t.Fatal(err)
	}
	if p := f.PRs(fakeRepo)[0]; p.Title != "Fix the bug in fix.txt" || p.Description != "The agent's text\n\nChecks: passed" {
		t.Fatalf("on the forge after an update of the title only: %+v", p)
	}

	// With no destination, the PR goes to the repository's main branch.
	f.Push(fakeRepo, "agent/r2", map[string]string{"b.txt": "b\n"}, "B")
	if second, err := b.OpenPR(ctx, repository, forge.NewPR{Source: "agent/r2", Title: "B"}); err != nil || second.Destination != "main" {
		t.Fatalf("a PR with no destination: %+v %v; want main", second, err)
	}
}

func TestThePRStateFollowsTheForge(t *testing.T) {
	f, b := newForge(t)
	ctx := context.Background()
	for _, branch := range []string{"agent/r1", "agent/r2"} {
		f.Push(fakeRepo, branch, map[string]string{branch + ".txt": "x\n"}, "Work")
		if _, err := b.OpenPR(ctx, repository, forge.NewPR{Source: branch, Title: branch}); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := b.PR(ctx, repository, 1); err != nil || p.State != forge.PROpen || p.Source != "agent/r1" {
		t.Fatalf("PR 1: %+v %v; want open", p, err)
	}
	f.SetState(fakeRepo, 1, "MERGED")
	f.SetState(fakeRepo, 2, "DECLINED")
	if p, err := b.PR(ctx, repository, 1); err != nil || p.State != forge.PRMerged {
		t.Fatalf("PR 1 after the merge: %+v %v", p, err)
	}
	if p, err := b.PR(ctx, repository, 2); err != nil || p.State != forge.PRDeclined {
		t.Fatalf("PR 2 after the decline: %+v %v", p, err)
	}
}

// commitWork makes a commit of the work in the checkout that is not on a
// branch, as the work-in-progress save does, and gives its id.
func commitWork(t *testing.T, work string) string {
	t.Helper()
	os.WriteFile(filepath.Join(work, "half-done.txt"), []byte("half\n"), 0o644)
	mustGit(t, work, nil, "add", "-A")
	tree := mustGit(t, work, nil, "write-tree")
	return mustGit(t, work, nil, "commit-tree", tree, "-p", "HEAD", "-m", "WIP [skip ci]: saved by the factory")
}

func TestASaveToTheWorkInProgressRefAndItsDelete(t *testing.T) {
	f, b := newForge(t)
	ctx := context.Background()
	work := clone(t, f, b)
	commit := commitWork(t, work)

	if ref, err := b.WIPRef("r1"); err != nil || ref != "refs/agent-wip/r1" {
		t.Fatalf("WIPRef = %q %v; want refs/agent-wip/r1", ref, err)
	}
	ref, err := b.SaveWIP(ctx, work, repository, "r1", commit)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "refs/agent-wip/r1" {
		t.Errorf("the saved ref is %s", ref)
	}
	if sha, ok := f.Ref(fakeRepo, "refs/agent-wip/r1"); !ok || sha != commit {
		t.Fatalf("refs/agent-wip/r1 on the remote = %q %v; want %s", sha, ok, commit)
	}
	if _, ok := f.Refs(fakeRepo)["refs/heads/agent-wip/r1"]; ok {
		t.Fatal("the save made a branch")
	}

	// A second save replaces the first: the push is a force push.
	mustGit(t, work, nil, "reset", "-q", "--hard", "HEAD")
	os.WriteFile(filepath.Join(work, "other.txt"), []byte("other\n"), 0o644)
	mustGit(t, work, nil, "add", "-A")
	again := mustGit(t, work, nil, "commit-tree", mustGit(t, work, nil, "write-tree"), "-p", "HEAD", "-m", "WIP [skip ci]")
	if _, err := b.SaveWIP(ctx, work, repository, "r1", again); err != nil {
		t.Fatal(err)
	}
	if sha, _ := f.Ref(fakeRepo, "refs/agent-wip/r1"); sha != again {
		t.Fatalf("after the second save the ref is %s; want %s", sha, again)
	}

	if err := b.DeleteWIP(ctx, repository, "r1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Ref(fakeRepo, "refs/agent-wip/r1"); ok {
		t.Fatal("refs/agent-wip/r1 is still there after the delete")
	}
	// A Run with no saved work has no ref to delete: that is not an error.
	if err := b.DeleteWIP(ctx, repository, "r1"); err != nil {
		t.Fatalf("a delete of an absent ref: %v", err)
	}
}

// The fallback of cluster check 12: a forge that refuses refs outside
// refs/heads gets the work-in-progress ref as a branch.
func TestTheSettingPutsTheWorkInProgressRefUnderRefsHeads(t *testing.T) {
	f, b := newForge(t)
	ctx := context.Background()
	f.RefuseRefsOutsideBranches(fakeRepo)
	work := clone(t, f, b)
	commit := commitWork(t, work)

	_, err := b.SaveWIP(ctx, work, repository, "r1", commit)
	if err == nil {
		t.Fatal("a save outside refs/heads worked on a forge that refuses it")
	}
	if !strings.Contains(err.Error(), "refs/agent-wip/r1") || !strings.Contains(err.Error(), "only branches and tags") ||
		!strings.Contains(err.Error(), forge.WIPInBranches) {
		t.Errorf("the reason %q must name the ref, give the forge's words and name the fallback setting", err)
	}

	b.WIPRefs = forge.WIPInBranches
	if ref, err := b.WIPRef("r1"); err != nil || ref != "refs/heads/agent-wip/r1" {
		t.Fatalf("WIPRef with the setting = %q %v", ref, err)
	}
	if _, err := b.SaveWIP(ctx, work, repository, "r1", commit); err != nil {
		t.Fatal(err)
	}
	if sha, ok := f.Ref(fakeRepo, "refs/heads/agent-wip/r1"); !ok || sha != commit {
		t.Fatalf("refs/heads/agent-wip/r1 = %q %v; want %s", sha, ok, commit)
	}
	if err := b.DeleteWIP(ctx, repository, "r1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Ref(fakeRepo, "refs/heads/agent-wip/r1"); ok {
		t.Fatal("refs/heads/agent-wip/r1 is still there after the delete")
	}
}

func TestAWorkInProgressRefNeedsAValidRunIDAndSetting(t *testing.T) {
	_, b := newForge(t)
	for _, id := range []string{"", "../main", "r 1", "r1:x", "R1"} {
		if ref, err := b.WIPRef(id); err == nil {
			t.Errorf("WIPRef(%q) = %s; want an error", id, ref)
		}
	}
	b.WIPRefs = "refs/tags/wip"
	if _, err := b.WIPRef("r1"); err == nil {
		t.Error("a WIP setting that is not one of the two worked")
	}
}

// An API error gives a reason that a person can act on, fit to show on
// the Run, and never the token.
func TestAnAPIErrorGivesAClearReason(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		setup func(f *fakeforge.Forge, b *forge.Bitbucket)
		call  func(b *forge.Bitbucket) error
		want  []string
	}{
		{"a token that is not valid", func(f *fakeforge.Forge, b *forge.Bitbucket) { b.Token = "not-the-token" },
			func(b *forge.Bitbucket) error { _, err := b.PR(ctx, repository, 1); return err },
			[]string{"read the PR", repository, "401", "Token is invalid", "solidtrace connections set"}},
		{"an absent branch", nil,
			func(b *forge.Bitbucket) error {
				_, err := b.OpenPR(ctx, repository, forge.NewPR{Source: "agent/absent", Title: "T"})
				return err
			},
			[]string{"open the PR", "400", "source: Branch not found"}},
		{"an absent repository", nil,
			func(b *forge.Bitbucket) error { _, err := b.PR(ctx, "bitbucket.org/team/other", 1); return err },
			[]string{"bitbucket.org/team/other", "404", "not found"}},
		{"an absent PR", nil,
			func(b *forge.Bitbucket) error {
				_, err := b.UpdatePR(ctx, repository, 9, forge.PRText{Title: "T"})
				return err
			},
			[]string{"update the PR", "404", "Pull request 9 not found"}},
		{"Bitbucket is down", func(f *fakeforge.Forge, b *forge.Bitbucket) {
			f.FailNext(http.StatusServiceUnavailable, "Service unavailable")
		},
			func(b *forge.Bitbucket) error { _, err := b.PR(ctx, repository, 1); return err },
			[]string{"503", "Service unavailable", "try again"}},
		{"a scope that the token lacks", func(f *fakeforge.Forge, b *forge.Bitbucket) {
			f.FailNext(http.StatusForbidden, "Your credentials lack one or more required privilege scopes.")
		},
			func(b *forge.Bitbucket) error {
				_, err := b.OpenPR(ctx, repository, forge.NewPR{Source: "main", Title: "T"})
				return err
			},
			[]string{"403", "privilege scopes", "pullrequest:write"}},
		{"no answer", func(f *fakeforge.Forge, b *forge.Bitbucket) { b.APIURL = "http://127.0.0.1:1/2.0" },
			func(b *forge.Bitbucket) error { _, err := b.PR(ctx, repository, 1); return err },
			[]string{"read the PR", "did not answer"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, b := newForge(t)
			if c.setup != nil {
				c.setup(f, b)
			}
			err := c.call(b)
			if err == nil {
				t.Fatal("no error")
			}
			var fe *forge.Error
			if !errors.As(err, &fe) {
				t.Errorf("the error %v is not a *forge.Error", err)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("the reason %q does not have %q", err, w)
				}
			}
			if strings.Contains(err.Error(), f.Token) {
				t.Errorf("the reason %q holds the token", err)
			}
		})
	}
}

// A git error names the ref and never holds the token.
func TestAGitErrorGivesAClearReason(t *testing.T) {
	f, b := newForge(t)
	work := clone(t, f, b)
	commit := commitWork(t, work)
	b.Token = "not-the-token"
	_, err := b.SaveWIP(context.Background(), work, repository, "r1", commit)
	if err == nil {
		t.Fatal("a save with a wrong token worked")
	}
	if !strings.Contains(err.Error(), "refs/agent-wip/r1") || !strings.Contains(err.Error(), repository) ||
		strings.Contains(err.Error(), "not-the-token") {
		t.Errorf("the reason %q must name the ref and the repository, with no token", err)
	}
	if !strings.Contains(err.Error(), "solidtrace connections set") || strings.Contains(err.Error(), forge.WIPInBranches) {
		t.Errorf("the reason %q must give the token hint, not the hint of the refs setting", err)
	}
	b.Token = "not-the-token"
	err = b.DeleteWIP(context.Background(), repository, "r1")
	if err == nil || !strings.Contains(err.Error(), "solidtrace connections set") {
		t.Errorf("a delete with a wrong token must give the token hint, not %v", err)
	}
}

// Only a refused ref gets the hint of the refs setting: a commit that is
// not in the checkout gets none.
func TestAPushOfAnUnknownCommitGetsNoHintOfTheRefsSetting(t *testing.T) {
	f, b := newForge(t)
	work := clone(t, f, b)
	_, err := b.SaveWIP(context.Background(), work, repository, "r1", "0123456789abcdef0123456789abcdef01234567")
	if err == nil {
		t.Fatal("a save of an unknown commit worked")
	}
	if strings.Contains(err.Error(), forge.WIPInBranches) || strings.Contains(err.Error(), "solidtrace connections set") {
		t.Errorf("the reason %q must give no hint of the refs setting or the token", err)
	}
}

// errorTransport fails each request with the text of err.
type errorTransport struct{ err error }

func (e errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

// A reason never holds the token, also when Bitbucket's answer, the
// network error or git's output holds it.
func TestAReasonNeverHoldsTheTokenThatAnAnswerEchoes(t *testing.T) {
	ctx := context.Background()
	check := func(t *testing.T, token string, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("no error")
		}
		if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[token]") {
			t.Errorf("the reason %q must show [token] in place of the token", err)
		}
	}
	t.Run("an API error body", func(t *testing.T) {
		f, b := newForge(t)
		f.FailNext(http.StatusBadRequest, "the token "+f.Token+" is not valid here")
		_, err := b.PR(ctx, repository, 1)
		check(t, f.Token, err)
	})
	t.Run("a network error", func(t *testing.T) {
		f, b := newForge(t)
		b.HTTP = &http.Client{Transport: errorTransport{errors.New("proxy refused Bearer " + f.Token)}}
		_, err := b.PR(ctx, repository, 1)
		check(t, f.Token, err)
	})
	t.Run("git's output", func(t *testing.T) {
		f, b := newForge(t)
		work := clone(t, f, b)
		commit := commitWork(t, work)
		f.RefusePushes(fakeRepo, "refused for the token "+f.Token)
		_, err := b.SaveWIP(ctx, work, repository, "r1", commit)
		check(t, f.Token, err)
	})
}

// A credential helper of the machine for the forge's URL does not answer
// in place of the connection's token.
func TestTheCredentialHelperReplacesAHelperOfTheMachine(t *testing.T) {
	f, b := newForge(t)
	global := filepath.Join(t.TempDir(), "gitconfig")
	machine := "[credential \"" + f.GitURL() + "\"]\n\thelper = \"!f() { echo username=x-token-auth; echo password=not-the-token; }; f\"\n"
	if err := os.WriteFile(global, []byte(machine), 0o644); err != nil {
		t.Fatal(err)
	}
	url, err := b.CloneURL(repository)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := b.CredentialConfig(tokenFile(t, f.Token))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", append(forge.GitArgs(settings), "clone", "-q", url, filepath.Join(t.TempDir(), "checkout"))...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+global, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the clone used the machine's helper: %v\n%s", err, out)
	}
}
