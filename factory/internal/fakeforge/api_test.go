package fakeforge_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/factory/internal/fakeforge"
)

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
	req, _ := http.NewRequest(method, f.APIURL()+path, &buf)
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

// get gives the value at a dotted path of a JSON object.
func get(v any, path string) any {
	for _, k := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func open(title, source, destination string) map[string]any {
	body := map[string]any{"title": title, "source": map[string]any{"branch": map[string]any{"name": source}}}
	if destination != "" {
		body["destination"] = map[string]any{"branch": map[string]any{"name": destination}}
	}
	return body
}

func errorMessage(a answer) string { s, _ := get(a.body, "error.message").(string); return s }

func TestPRAndCommentsInBitbucketShapes(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	f.Push(repo, "agent/r1", map[string]string{"fix.txt": "x\n"}, "Fix")
	pulls := "/repositories/" + repo + "/pullrequests"

	if a := call(t, f, "wrong", "GET", pulls, nil); a.status != 401 || get(a.body, "type") != "error" || errorMessage(a) == "" {
		t.Fatalf("wrong token: %d %v; want 401 with Bitbucket's error body", a.status, a.body)
	}
	if a := call(t, f, f.Token, "POST", pulls, open("T", "absent", "")); a.status != 400 || errorMessage(a) != "source: Branch not found" {
		t.Fatalf("PR from an absent branch: %d %v; want 400 source: Branch not found", a.status, a.body)
	}
	if a := call(t, f, f.Token, "GET", "/repositories/team/other/pullrequests", nil); a.status != 404 {
		t.Fatalf("absent repository: %d; want 404", a.status)
	}

	body := open("Fix the bug", "agent/r1", "main")
	body["description"] = "Evidence: none"
	opened := call(t, f, f.Token, "POST", pulls, body)
	if opened.status != 201 || opened.body["state"] != "OPEN" || get(opened.body, "links.html.href") == nil ||
		get(opened.body, "source.branch.name") != "agent/r1" || get(opened.body, "destination.branch.name") != "main" ||
		get(opened.body, "author.display_name") != fakeforge.FactoryAuthor {
		t.Fatalf("open: %d %v", opened.status, opened.body)
	}
	if a := call(t, f, f.Token, "POST", pulls, open("Again", "agent/r1", "")); a.status != 400 || !strings.Contains(errorMessage(a), "already an open pull request") {
		t.Fatalf("a second open PR between the same branches: %d %v; want 400", a.status, a.body)
	}
	pr := pulls + "/1"
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
	if a := call(t, f, f.Token, "POST", pr+"/comments", map[string]any{"content": map[string]string{"raw": "Run r1 is done."}}); a.status != 201 {
		t.Fatalf("comment: %d %v", a.status, a.body)
	}

	prs := f.PRs(repo)
	if len(prs) != 1 || prs[0].Title != "Fix the bug in fix.txt" || prs[0].Description != "Evidence: go test passed" || prs[0].Author != fakeforge.FactoryAuthor {
		t.Fatalf("PRs = %+v; want the one PR with the new title and description", prs)
	}

	inline := f.PersonComments(repo, 1, fakeforge.Comment{Author: "Asha", Body: "/agent rename this", File: "fix.txt", Line: 1})
	f.Resolve(repo, 1, inline.ID)
	values := call(t, f, f.Token, "GET", pr+"/comments", nil).body["values"].([]any)
	if len(values) != 2 {
		t.Fatalf("comments %v; want the factory's and the person's", values)
	}
	person := values[1]
	if get(person, "user.display_name") != "Asha" || get(person, "content.raw") != "/agent rename this" ||
		get(person, "inline.path") != "fix.txt" || get(person, "inline.to") != 1.0 || get(person, "resolution.type") != "comment_resolution" {
		t.Fatalf("the person's comment through the API = %v", person)
	}
	if get(values[0], "resolution") != nil || get(values[0], "inline") != nil {
		t.Fatalf("the factory's general comment = %v; want no inline and no resolution", values[0])
	}
	if c := f.Comments(repo, 1); c[0].Author != fakeforge.FactoryAuthor || c[0].Body != "Run r1 is done." {
		t.Fatalf("the factory's comment = %+v", c[0])
	}

	f.SetState(repo, 1, "MERGED")
	if a := call(t, f, f.Token, "GET", pr, nil); a.body["state"] != "MERGED" {
		t.Fatalf("state after the merge = %v", a.body["state"])
	}
	if a := call(t, f, f.Token, "PUT", pr, map[string]string{"title": "Late"}); a.status != 400 {
		t.Fatalf("update of a merged PR: %d %v; want 400", a.status, a.body)
	}
}

// The list gives only open PRs with no state parameter, as Bitbucket
// does, and applies each BBQL term that the fake knows.
func TestPRListFiltersAsBitbucketDoes(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	f.Push(repo, "agent/r1", map[string]string{"a.txt": "a\n"}, "A")
	f.Push(repo, "agent/r2", map[string]string{"b.txt": "b\n"}, "B")
	pulls := "/repositories/" + repo + "/pullrequests"
	for _, b := range []string{"agent/r1", "agent/r2"} {
		if a := call(t, f, f.Token, "POST", pulls, open(b, b, "")); a.status != 201 {
			t.Fatalf("open from %s: %d %v", b, a.status, a.body)
		}
	}
	f.SetState(repo, 2, "MERGED")

	q := func(s string) string { return "q=" + url.QueryEscape(s) }
	for query, want := range map[string][]string{
		"":                         {"agent/r1"},
		"?state=OPEN&state=MERGED": {"agent/r1", "agent/r2"},
		"?" + q(`source.branch.name = "agent/r1"`):                      {"agent/r1"},
		"?" + q(`source.branch.name = "agent/r2"`):                      {},
		"?" + q(`source.branch.name = "agent/r2" AND state = "MERGED"`): {"agent/r2"},
		"?state=MERGED": {"agent/r2"},
	} {
		a := call(t, f, f.Token, "GET", pulls+query, nil)
		got := []string{}
		for _, v := range a.body["values"].([]any) {
			got = append(got, get(v, "source.branch.name").(string))
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("GET pullrequests%s: sources %v; want %v", query, got, want)
		}
	}
	if a := call(t, f, f.Token, "GET", pulls+"?"+q(`title ~ "x"`), nil); a.status != 400 {
		t.Errorf("a query term that the fake does not know: %d; want 400", a.status)
	}
}

func TestFailNextGivesOneErrorAnswer(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	pulls := "/repositories/" + repo + "/pullrequests"
	f.FailNext(http.StatusServiceUnavailable, "Bitbucket is down for maintenance")
	if a := call(t, f, f.Token, "GET", pulls, nil); a.status != 503 || errorMessage(a) != "Bitbucket is down for maintenance" {
		t.Fatalf("after FailNext: %d %v", a.status, a.body)
	}
	if a := call(t, f, f.Token, "GET", pulls, nil); a.status != 200 {
		t.Fatalf("the call after: %d; want 200", a.status)
	}
}

func TestRefuseRefsOutsideBranches(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	f.RefuseRefsOutsideBranches(repo)
	work := filepath.Join(t.TempDir(), "c")
	mustGit(t, f.Token, "", "clone", "-q", f.CloneURL(repo), work)
	if out, err := agentGit(t, f.Token, work, "push", "-q", "origin", "HEAD:refs/agent-wip/r1"); err == nil || !strings.Contains(out, "only branches and tags") {
		t.Fatalf("push outside refs/heads: %v\n%s; want a refusal", err, out)
	}
	mustGit(t, f.Token, work, "push", "-q", "origin", "HEAD:refs/heads/agent-wip/r1")
	if _, ok := f.Ref(repo, "refs/heads/agent-wip/r1"); !ok {
		t.Fatal("refs/heads/agent-wip/r1 is not on the remote")
	}
}

func TestGitNeedsTheTokenUser(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	helper := "!f() { echo username=someone; echo password=" + f.Token + "; }; f"
	if out, err := agentGitWith(t, helper, "", "clone", "-q", f.CloneURL(repo), filepath.Join(t.TempDir(), "c")); err == nil {
		t.Fatalf("a clone with the user name someone worked:\n%s", out)
	}
}

// A Dispatcher polls the API while the test acts as a person. Under
// `go test -race`, this finds an answer that is encoded with no lock.
func TestAPIAnswersAndPersonHelpersAtTheSameTime(t *testing.T) {
	f := fakeforge.New(t)
	f.AddRepo(repo, nil)
	f.Push(repo, "agent/r1", map[string]string{"fix.txt": "x\n"}, "Fix")
	pulls := "/repositories/" + repo + "/pullrequests"
	call(t, f, f.Token, "POST", pulls, open("T", "agent/r1", ""))
	c := f.PersonComments(repo, 1, fakeforge.Comment{Author: "Asha", Body: "/agent again"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			for _, p := range []string{pulls, pulls + "/1", pulls + "/1/comments"} {
				req, _ := http.NewRequest("GET", f.APIURL()+p, nil)
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
