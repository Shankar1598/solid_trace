// Package fakeforge is the fake Forge of Seam 3: local bare git
// repositories served over HTTP (clone and push, with the token as the
// password, as from a credential helper), and a small PR and comment API
// that keeps the PRs and comments that the factory makes. Tests read the
// refs and act as a person on the PRs.
//
// The PR API, with "Authorization: Bearer <token>":
//
//	POST /api/repos/{owner}/{name}/pulls               {title, description, source, destination}
//	GET  /api/repos/{owner}/{name}/pulls?source=&state=
//	GET  /api/repos/{owner}/{name}/pulls/{id}
//	PUT  /api/repos/{owner}/{name}/pulls/{id}          {title, description}; absent fields stay
//	GET  /api/repos/{owner}/{name}/pulls/{id}/comments
//	POST /api/repos/{owner}/{name}/pulls/{id}/comments {body, file, line}
//
// Git: <URL>/git/{owner}/{name}.git, with any user name and the token as
// the password.
package fakeforge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// FactoryAuthor is the author of the PRs and comments that come through
// the API.
const FactoryAuthor = "factory"

// PR is a pull request on the fake Forge.
type PR struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	State       string    `json:"state"` // OPEN, MERGED or DECLINED
	Author      string    `json:"author"`
	Link        string    `json:"link"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

// Comment is a general comment on a PR, or an inline one with a file and
// a line.
type Comment struct {
	ID       int       `json:"id"`
	Author   string    `json:"author"`
	Body     string    `json:"body"`
	File     string    `json:"file,omitempty"`
	Line     int       `json:"line,omitempty"`
	Resolved bool      `json:"resolved"`
	Created  time.Time `json:"created"`
}

// Forge is the fake Forge.
type Forge struct {
	// Token is the one token that the API and git accept.
	Token string

	t       testing.TB
	root    string
	backend string
	server  *httptest.Server

	mu       sync.Mutex
	prs      map[string][]*PR
	comments map[string]map[int][]*Comment
	nextID   int
}

// New starts a fake Forge with no repositories. The test's cleanup stops it.
func New(t testing.TB) *Forge {
	t.Helper()
	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatalf("fakeforge: git is needed: %v", err)
	}
	f := &Forge{
		Token:    "forge-token-for-tests",
		t:        t,
		root:     t.TempDir(),
		backend:  filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend"),
		prs:      map[string][]*PR{},
		comments: map[string]map[int][]*Comment{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/git/", f.git)
	mux.HandleFunc("POST /api/repos/{owner}/{name}/pulls", f.api(f.openPR))
	mux.HandleFunc("GET /api/repos/{owner}/{name}/pulls", f.api(f.listPRs))
	mux.HandleFunc("GET /api/repos/{owner}/{name}/pulls/{id}", f.api(f.getPR))
	mux.HandleFunc("PUT /api/repos/{owner}/{name}/pulls/{id}", f.api(f.updatePR))
	mux.HandleFunc("GET /api/repos/{owner}/{name}/pulls/{id}/comments", f.api(f.listComments))
	mux.HandleFunc("POST /api/repos/{owner}/{name}/pulls/{id}/comments", f.api(f.postComment))
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// URL is the Forge's base URL.
func (f *Forge) URL() string { return f.server.URL }

// CloneURL is the HTTPS-style clone URL of a repository "owner/name".
func (f *Forge) CloneURL(repo string) string { return f.server.URL + "/git/" + repo + ".git" }

// AddRepo makes a bare repository "owner/name" with one commit on main
// that holds these files (a README when there are none).
func (f *Forge) AddRepo(repo string, files map[string]string) {
	f.t.Helper()
	bare := f.bare(repo)
	f.run("", "init", "--bare", "--initial-branch=main", bare)
	f.run(bare, "config", "http.receivepack", "true")
	if len(files) == 0 {
		files = map[string]string{"README.md": "# " + repo + "\n"}
	}
	f.Push(repo, "main", files, "First commit")
}

// Push is a person's push: a commit on the branch (made from the
// branch's tip, or a new branch from main) with these files changed.
func (f *Forge) Push(repo, branch string, files map[string]string, message string) string {
	f.t.Helper()
	work := f.t.TempDir()
	f.run("", "clone", "-q", f.bare(repo), work)
	if f.gitOK(work, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+branch) {
		f.run(work, "checkout", "-q", "-B", branch, "origin/"+branch)
	} else {
		f.run(work, "checkout", "-q", "-b", branch)
	}
	for name, body := range files {
		p := filepath.Join(work, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	f.run(work, "add", "-A")
	f.run(work, "commit", "-q", "--allow-empty", "-m", message)
	f.run(work, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	return strings.TrimSpace(f.run(work, "rev-parse", "HEAD"))
}

// Ref gives the commit that a ref (for example refs/heads/main or
// refs/agent-wip/<run id>) names, and false when there is no such ref.
func (f *Forge) Ref(repo, ref string) (string, bool) {
	f.t.Helper()
	out, err := f.git2(f.bare(repo), "rev-parse", "--verify", "-q", ref+"^{commit}")
	return strings.TrimSpace(out), err == nil
}

// Refs gives each ref of the repository with its commit.
func (f *Forge) Refs(repo string) map[string]string {
	f.t.Helper()
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(f.run(f.bare(repo), "for-each-ref", "--format=%(refname) %(objectname)")), "\n") {
		if name, sha, ok := strings.Cut(line, " "); ok {
			refs[name] = sha
		}
	}
	return refs
}

// File gives a file's content at a ref, and false when it is not there.
func (f *Forge) File(repo, ref, path string) (string, bool) {
	f.t.Helper()
	out, err := f.git2(f.bare(repo), "show", ref+":"+path)
	return out, err == nil
}

// Git runs a git command in the bare repository, for a check that the
// other methods do not give (for example `log --format=%s main`).
func (f *Forge) Git(repo string, args ...string) string {
	f.t.Helper()
	return f.run(f.bare(repo), args...)
}

// PRs gives the repository's PRs, the first one first.
func (f *Forge) PRs(repo string) []PR {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []PR
	for _, p := range f.prs[repo] {
		out = append(out, *p)
	}
	return out
}

// Comments gives the comments on a PR, the first one first.
func (f *Forge) Comments(repo string, pr int) []Comment {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Comment
	for _, c := range f.comments[repo][pr] {
		out = append(out, *c)
	}
	return out
}

// PersonComments adds a person's comment to a PR (general, or inline when
// it has a file and a line), and gives it with its id.
func (f *Forge) PersonComments(repo string, pr int, c Comment) Comment {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.findPR(repo, pr) == nil {
		f.t.Fatalf("fakeforge: no PR %d in %s", pr, repo)
	}
	return *f.addComment(repo, pr, c)
}

// Resolve marks a comment resolved, as a person does.
func (f *Forge) Resolve(repo string, pr, comment int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.comments[repo][pr] {
		if c.ID == comment {
			c.Resolved = true
			return
		}
	}
	f.t.Fatalf("fakeforge: no comment %d on PR %d", comment, pr)
}

// SetState sets a PR's state (OPEN, MERGED or DECLINED), as a person does.
func (f *Forge) SetState(repo string, pr int, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.findPR(repo, pr)
	if p == nil {
		f.t.Fatalf("fakeforge: no PR %d in %s", pr, repo)
	}
	p.State, p.Updated = state, time.Now().UTC()
}

func (f *Forge) bare(repo string) string { return filepath.Join(f.root, repo+".git") }

func (f *Forge) run(dir string, args ...string) string {
	f.t.Helper()
	out, err := f.git2(dir, args...)
	if err != nil {
		f.t.Fatalf("fakeforge: git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (f *Forge) gitOK(dir string, args ...string) bool {
	_, err := f.git2(dir, args...)
	return err == nil
}

func (f *Forge) git2(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=A person", "GIT_AUTHOR_EMAIL=person@example.com",
		"GIT_COMMITTER_NAME=A person", "GIT_COMMITTER_EMAIL=person@example.com")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// git serves clone and push with git's own smart HTTP backend.
func (f *Forge) git(w http.ResponseWriter, r *http.Request) {
	if _, password, ok := r.BasicAuth(); !ok || password != f.Token {
		w.Header().Set("WWW-Authenticate", `Basic realm="fake forge"`)
		http.Error(w, "the token is not valid", http.StatusUnauthorized)
		return
	}
	h := &cgi.Handler{
		Path: f.backend,
		Root: "/git",
		Env:  []string{"GIT_PROJECT_ROOT=" + f.root, "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=" + FactoryAuthor},
	}
	h.ServeHTTP(w, r)
}

type apiError struct {
	status int
	msg    string
}

func (f *Forge) api(h func(r *http.Request, repo string) (int, any, *apiError)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+f.Token {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "the token is not valid"})
			return
		}
		repo := r.PathValue("owner") + "/" + r.PathValue("name")
		if _, err := os.Stat(f.bare(repo)); err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "no repository " + repo})
			return
		}
		// The body holds pointers into the Forge's state, so encode it
		// while f.mu is held: a test helper can change the same PR.
		f.mu.Lock()
		status, body, e := h(r, repo)
		if e != nil {
			status, body = e.status, map[string]string{"error": e.msg}
		}
		data, err := json.Marshal(body)
		f.mu.Unlock()
		if err != nil {
			status, data = http.StatusInternalServerError, []byte(`{"error":"the answer is not JSON"}`)
		}
		w.WriteHeader(status)
		w.Write(append(data, '\n'))
	}
}

func (f *Forge) openPR(r *http.Request, repo string) (int, any, *apiError) {
	var in struct{ Title, Description, Source, Destination string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Title == "" || in.Source == "" {
		return 0, nil, &apiError{http.StatusBadRequest, "give a title and a source branch"}
	}
	if in.Destination == "" {
		in.Destination = "main"
	}
	for _, b := range []string{in.Source, in.Destination} {
		if !f.gitOK(f.bare(repo), "rev-parse", "--verify", "-q", "refs/heads/"+b) {
			return 0, nil, &apiError{http.StatusUnprocessableEntity, "no branch " + b}
		}
	}
	for _, p := range f.prs[repo] {
		if p.Source == in.Source && p.State == "OPEN" {
			return 0, nil, &apiError{http.StatusConflict, fmt.Sprintf("PR %d is open from %s", p.ID, in.Source)}
		}
	}
	now := time.Now().UTC()
	p := &PR{ID: len(f.prs[repo]) + 1, Title: in.Title, Description: in.Description, Source: in.Source,
		Destination: in.Destination, State: "OPEN", Author: FactoryAuthor, Created: now, Updated: now}
	p.Link = fmt.Sprintf("%s/%s/pull-requests/%d", f.server.URL, repo, p.ID)
	f.prs[repo] = append(f.prs[repo], p)
	return http.StatusCreated, p, nil
}

func (f *Forge) listPRs(r *http.Request, repo string) (int, any, *apiError) {
	values := []*PR{}
	for _, p := range f.prs[repo] {
		if s := r.URL.Query().Get("source"); s != "" && s != p.Source {
			continue
		}
		if s := r.URL.Query().Get("state"); s != "" && s != p.State {
			continue
		}
		values = append(values, p)
	}
	return http.StatusOK, map[string]any{"values": values}, nil
}

func (f *Forge) prFrom(r *http.Request, repo string) (*PR, *apiError) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if p := f.findPR(repo, id); p != nil {
		return p, nil
	}
	return nil, &apiError{http.StatusNotFound, "no PR " + r.PathValue("id")}
}

func (f *Forge) findPR(repo string, id int) *PR {
	for _, p := range f.prs[repo] {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (f *Forge) getPR(r *http.Request, repo string) (int, any, *apiError) {
	p, e := f.prFrom(r, repo)
	return http.StatusOK, p, e
}

func (f *Forge) updatePR(r *http.Request, repo string) (int, any, *apiError) {
	p, e := f.prFrom(r, repo)
	if e != nil {
		return 0, nil, e
	}
	var in struct{ Title, Description *string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return 0, nil, &apiError{http.StatusBadRequest, "the body is not JSON"}
	}
	if in.Title != nil {
		p.Title = *in.Title
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	p.Updated = time.Now().UTC()
	return http.StatusOK, p, nil
}

func (f *Forge) listComments(r *http.Request, repo string) (int, any, *apiError) {
	p, e := f.prFrom(r, repo)
	if e != nil {
		return 0, nil, e
	}
	values := append([]*Comment{}, f.comments[repo][p.ID]...)
	return http.StatusOK, map[string]any{"values": values}, nil
}

func (f *Forge) postComment(r *http.Request, repo string) (int, any, *apiError) {
	p, e := f.prFrom(r, repo)
	if e != nil {
		return 0, nil, e
	}
	var in struct {
		Body, File string
		Line       int
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Body == "" {
		return 0, nil, &apiError{http.StatusBadRequest, "give a body"}
	}
	c := f.addComment(repo, p.ID, Comment{Author: FactoryAuthor, Body: in.Body, File: in.File, Line: in.Line})
	return http.StatusCreated, c, nil
}

// addComment keeps a comment. The caller holds f.mu.
func (f *Forge) addComment(repo string, pr int, c Comment) *Comment {
	f.nextID++
	c.ID, c.Created = f.nextID, time.Now().UTC()
	if f.comments[repo] == nil {
		f.comments[repo] = map[int][]*Comment{}
	}
	f.comments[repo][pr] = append(f.comments[repo][pr], &c)
	return &c
}
