package fakeforge_test

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/factory/internal/fakeforge"
)

const repo = "team/app"

// agentGit runs git as the wrapper does: the token comes from a
// credential helper, never from the URL.
func agentGit(t *testing.T, token, dir string, args ...string) (string, error) {
	t.Helper()
	return agentGitWith(t, "!f() { echo username=x-token-auth; echo password="+token+"; }; f", dir, args...)
}

func agentGitWith(t *testing.T, helper, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "credential.helper=", "-c", "credential.helper=" + helper}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Agent", "GIT_AUTHOR_EMAIL=agent@example.com", "GIT_COMMITTER_NAME=Agent", "GIT_COMMITTER_EMAIL=agent@example.com")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustGit(t *testing.T, token, dir string, args ...string) string {
	t.Helper()
	out, err := agentGit(t, token, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func TestCloneAndPushOverHTTP(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, map[string]string{"README.md": "hello\n"})
	work := filepath.Join(t.TempDir(), "checkout")
	mustGit(t, f.Token, "", "clone", "-q", f.CloneURL(repo), work)
	if b, _ := os.ReadFile(filepath.Join(work, "README.md")); string(b) != "hello\n" {
		t.Fatalf("README.md in the clone = %q", b)
	}

	mustGit(t, f.Token, work, "checkout", "-q", "-b", "agent/r1")
	os.WriteFile(filepath.Join(work, "fix.txt"), []byte("fixed\n"), 0o644)
	mustGit(t, f.Token, work, "add", "fix.txt")
	mustGit(t, f.Token, work, "commit", "-q", "-m", "Fix it")
	mustGit(t, f.Token, work, "push", "-q", "origin", "agent/r1")
	mustGit(t, f.Token, work, "push", "-q", "-f", "origin", "HEAD:refs/agent-wip/r1")
	head := strings.TrimSpace(mustGit(t, f.Token, work, "rev-parse", "HEAD"))

	if sha, ok := f.Ref(repo, "refs/heads/agent/r1"); !ok || sha != head {
		t.Fatalf("refs/heads/agent/r1 = %q %v; want %s", sha, ok, head)
	}
	if got := f.Refs(repo)["refs/agent-wip/r1"]; got != head {
		t.Fatalf("refs/agent-wip/r1 = %q; want %s", got, head)
	}
	if body, ok := f.File(repo, "agent/r1", "fix.txt"); !ok || body != "fixed\n" {
		t.Fatalf("fix.txt on the remote = %q %v", body, ok)
	}

	mustGit(t, f.Token, work, "push", "-q", "origin", ":refs/agent-wip/r1")
	if _, ok := f.Ref(repo, "refs/agent-wip/r1"); ok {
		t.Fatal("refs/agent-wip/r1 is still there after the delete")
	}
}

func TestWrongTokenCannotClone(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	if out, err := agentGit(t, "wrong", "", "clone", "-q", f.CloneURL(repo), filepath.Join(t.TempDir(), "c")); err == nil {
		t.Fatalf("a clone with a wrong token worked:\n%s", out)
	}
}

func TestPersonPushIsSeenByTheAgent(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	sha := f.Push(repo, "agent/r1", map[string]string{"person.txt": "mine\n"}, "A person's change")
	work := filepath.Join(t.TempDir(), "c")
	mustGit(t, f.Token, "", "clone", "-q", "--branch", "agent/r1", f.CloneURL(repo), work)
	if got := strings.TrimSpace(mustGit(t, f.Token, work, "rev-parse", "HEAD")); got != sha {
		t.Fatalf("HEAD of the clone %s; want the person's commit %s", got, sha)
	}
}

// NewAt serves on the listener that it gets, and its links and clone URLs
// name the public URL (the local factory stack's "http://forge").
func TestNewAtServesOnTheListenerWithThePublicURL(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := fakeforge.NewAt(t, l, "http://forge/")
	if f.URL() != "http://forge" || f.CloneURL(repo) != "http://forge/git/"+repo+".git" || f.APIURL() != "http://forge/2.0" {
		t.Fatalf("URL %q, CloneURL %q, APIURL %q", f.URL(), f.CloneURL(repo), f.APIURL())
	}
	f.AddRepo(repo, nil)
	work := filepath.Join(t.TempDir(), "checkout")
	mustGit(t, f.Token, "", "clone", "-q", "http://"+l.Addr().String()+"/git/"+repo+".git", work)

	body := strings.NewReader(`{"title": "T", "source": {"branch": {"name": "main"}}, "destination": {"branch": {"name": "main"}}}`)
	req, _ := http.NewRequest(http.MethodPost, "http://"+l.Addr().String()+"/2.0/repositories/"+repo+"/pullrequests", body)
	req.Header.Set("Authorization", "Bearer "+f.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if prs := f.PRs(repo); len(prs) != 1 || !strings.HasPrefix(prs[0].Link, "http://forge/"+repo+"/pull-requests/") {
		t.Fatalf("status %d, PRs %+v", resp.StatusCode, prs)
	}
}
