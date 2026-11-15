package fakeforge_test

import (
	"bytes"
	"encoding/json"
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
	helper := "!f() { echo username=x-token-auth; echo password=" + token + "; }; f"
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

type answer struct {
	status int
	body   map[string]any
}

func call(t *testing.T, f *fakeforge.Forge, token, method, path string, body any) answer {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, f.URL()+path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	a := answer{status: resp.StatusCode}
	json.NewDecoder(resp.Body).Decode(&a.body)
	return a
}

func TestPRAndCommentsThatTheFactoryMakes(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	f.Push(repo, "agent/r1", map[string]string{"fix.txt": "x\n"}, "Fix")
	pulls := "/api/repos/" + repo + "/pulls"

	if a := call(t, f, "wrong", "GET", pulls, nil); a.status != 401 {
		t.Fatalf("wrong token: %d; want 401", a.status)
	}
	if a := call(t, f, f.Token, "POST", pulls, map[string]string{"title": "T", "source": "absent"}); a.status != 422 {
		t.Fatalf("PR from an absent branch: %d; want 422", a.status)
	}
	if a := call(t, f, f.Token, "GET", "/api/repos/team/other/pulls", nil); a.status != 404 {
		t.Fatalf("absent repository: %d; want 404", a.status)
	}

	opened := call(t, f, f.Token, "POST", pulls, map[string]string{"title": "Fix the bug", "description": "Evidence: none", "source": "agent/r1", "destination": "main"})
	if opened.status != 201 || opened.body["state"] != "OPEN" || opened.body["link"] == "" {
		t.Fatalf("open: %d %v", opened.status, opened.body)
	}
	if a := call(t, f, f.Token, "POST", pulls, map[string]string{"title": "Again", "source": "agent/r1"}); a.status != 409 {
		t.Fatalf("a second open PR from the same branch: %d; want 409", a.status)
	}
	pr := "/api/repos/" + repo + "/pulls/1"
	if a := call(t, f, f.Token, "PUT", pr, map[string]string{"description": "Evidence: go test passed"}); a.status != 200 {
		t.Fatalf("update: %d %v", a.status, a.body)
	}
	if a := call(t, f, f.Token, "GET", pr, nil); a.body["title"] != "Fix the bug" || a.body["description"] != "Evidence: go test passed" {
		t.Fatalf("after a PUT of only the description: %v; want the old title", a.body)
	}
	if a := call(t, f, f.Token, "PUT", pr, map[string]string{"title": "Fix the bug in fix.txt"}); a.status != 200 {
		t.Fatalf("update: %d %v", a.status, a.body)
	}
	if a := call(t, f, f.Token, "GET", pr, nil); a.body["title"] != "Fix the bug in fix.txt" || a.body["description"] != "Evidence: go test passed" {
		t.Fatalf("after a PUT of only the title: %v; want the old description", a.body)
	}
	if a := call(t, f, f.Token, "POST", pr+"/comments", map[string]string{"body": "Run r1 is done."}); a.status != 201 {
		t.Fatalf("comment: %d %v", a.status, a.body)
	}

	prs := f.PRs(repo)
	if len(prs) != 1 || prs[0].Title != "Fix the bug in fix.txt" || prs[0].Description != "Evidence: go test passed" || prs[0].Author != fakeforge.FactoryAuthor {
		t.Fatalf("PRs = %+v; want the one PR with the new title and description", prs)
	}
	if got := call(t, f, f.Token, "GET", pulls+"?source=agent/r1&state=OPEN", nil).body["values"].([]any); len(got) != 1 {
		t.Fatalf("PRs from agent/r1: %v", got)
	}

	inline := f.PersonComments(repo, 1, fakeforge.Comment{Author: "Asha", Body: "/agent rename this", File: "fix.txt", Line: 1})
	f.Resolve(repo, 1, inline.ID)
	values := call(t, f, f.Token, "GET", pr+"/comments", nil).body["values"].([]any)
	if len(values) != 2 {
		t.Fatalf("comments %v; want the factory's and the person's", values)
	}
	person := values[1].(map[string]any)
	if person["author"] != "Asha" || person["file"] != "fix.txt" || person["line"] != 1.0 || person["resolved"] != true {
		t.Fatalf("the person's comment through the API = %v", person)
	}
	if c := f.Comments(repo, 1); c[0].Author != fakeforge.FactoryAuthor || c[0].Body != "Run r1 is done." {
		t.Fatalf("the factory's comment = %+v", c[0])
	}

	f.SetState(repo, 1, "MERGED")
	if a := call(t, f, f.Token, "GET", pr, nil); a.body["state"] != "MERGED" {
		t.Fatalf("state after the merge = %v", a.body["state"])
	}
}

// A Dispatcher polls the API while the test acts as a person. Under
// `go test -race`, this finds an answer that is encoded with no lock.
func TestAPIAnswersAndPersonHelpersAtTheSameTime(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	f.Push(repo, "agent/r1", map[string]string{"fix.txt": "x\n"}, "Fix")
	pulls := "/api/repos/" + repo + "/pulls"
	call(t, f, f.Token, "POST", pulls, map[string]string{"title": "T", "source": "agent/r1"})
	c := f.PersonComments(repo, 1, fakeforge.Comment{Author: "Asha", Body: "/agent again"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			for _, p := range []string{pulls, pulls + "/1", pulls + "/1/comments"} {
				req, _ := http.NewRequest("GET", f.URL()+p, nil)
				req.Header.Set("Authorization", "Bearer "+f.Token)
				if resp, err := http.DefaultClient.Do(req); err == nil {
					resp.Body.Close()
				}
			}
		}
	}()
	for i := 0; ; i++ {
		select {
		case <-done:
			return
		default:
		}
		f.SetState(repo, 1, []string{"OPEN", "MERGED"}[i%2])
		f.Resolve(repo, 1, c.ID)
	}
}
