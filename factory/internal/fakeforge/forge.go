// Package fakeforge is the fake Forge of Seam 3: local bare git
// repositories served over HTTP (clone and push, with the token as the
// password, as from a credential helper), and a small part of Bitbucket
// Cloud's REST API 2.0 that keeps the PRs and comments that the factory
// makes. Tests read the refs and act as a person on the PRs.
//
// The API, with "Authorization: Bearer <token>" (a Bitbucket access
// token), under APIURL() (<URL>/2.0):
//
//	POST /repositories/{workspace}/{repo_slug}/pullrequests               {title, description, source.branch.name, destination.branch.name}
//	GET  /repositories/{workspace}/{repo_slug}/pullrequests?q=&state=     q: source.branch.name, destination.branch.name, state with =, AND
//	GET  /repositories/{workspace}/{repo_slug}/pullrequests/{id}
//	PUT  /repositories/{workspace}/{repo_slug}/pullrequests/{id}          {title, description}; absent fields stay
//	GET  /repositories/{workspace}/{repo_slug}/pullrequests/{id}/comments
//	POST /repositories/{workspace}/{repo_slug}/pullrequests/{id}/comments {content.raw, inline.path, inline.to}
//	GET  /repositories/{workspace}/{repo_slug}                            mainbranch.name is the bare repository's HEAD
//	GET  /repositories/{workspace}/{repo_slug}/refs/branches/{name}       target.hash
//	GET  /repositories/{workspace}/{repo_slug}/src/{commit}/{path}        a file's raw content; 404 when absent
//
// Errors have Bitbucket's shape: {"type": "error", "error": {"message",
// "fields"}}. A PR from an absent branch and a second open PR between the
// same branches get 400, as on Bitbucket. With no state parameter, the
// list gives only open PRs.
//
// Git: GitURL()/{workspace}/{repo_slug}.git (<URL>/git/...), with the
// user name x-token-auth and the token as the password, as Bitbucket asks
// for an access token.
package fakeforge

import (
	"bytes"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TokenUser is the git user name that goes with an access token.
const TokenUser = "x-token-auth"

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
	failNext *apiError
	// hanging are the repositories whose git requests get no answer, with
	// the channel that the first request closes.
	hanging map[string]*hang
	// refusedGit are the repositories whose git requests get 401.
	refusedGit map[string]bool
}

type hang struct {
	reached chan struct{}
	once    sync.Once
	release chan struct{}
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
		hanging:  map[string]*hang{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/git/", f.git)
	const prs = "/2.0/repositories/{workspace}/{repo_slug}/pullrequests"
	mux.HandleFunc("POST "+prs, f.api(f.openPR))
	mux.HandleFunc("GET "+prs, f.api(f.listPRs))
	mux.HandleFunc("GET "+prs+"/{id}", f.api(f.getPR))
	mux.HandleFunc("PUT "+prs+"/{id}", f.api(f.updatePR))
	mux.HandleFunc("GET "+prs+"/{id}/comments", f.api(f.listComments))
	mux.HandleFunc("POST "+prs+"/{id}/comments", f.api(f.postComment))
	const repository = "/2.0/repositories/{workspace}/{repo_slug}"
	mux.HandleFunc("GET "+repository, f.api(f.getRepository))
	mux.HandleFunc("GET "+repository+"/refs/branches/{name...}", f.api(f.getBranch))
	mux.HandleFunc("GET "+repository+"/src/{commit}/{path...}", f.api(f.getSource))
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// URL is the Forge's base URL.
func (f *Forge) URL() string { return f.server.URL }

// APIURL is the base of the API, as https://api.bitbucket.org/2.0 is.
func (f *Forge) APIURL() string { return f.server.URL + "/2.0" }

// GitURL is the base of the clone URLs, as https://bitbucket.org is.
func (f *Forge) GitURL() string { return f.server.URL + "/git" }

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

// AddSubmodule is a person's push of a commit on branch that adds the
// submodule at path, with url in .gitmodules, at the tip of the main
// branch of the Forge's repository sub.
func (f *Forge) AddSubmodule(repo, branch, path, url, sub string) {
	f.t.Helper()
	commit := strings.TrimSpace(f.Git(sub, "rev-parse", "refs/heads/main"))
	work := f.t.TempDir()
	f.run("", "clone", "-q", "--branch", branch, f.bare(repo), work)
	modules := "[submodule \"" + path + "\"]\n\tpath = " + path + "\n\turl = " + url + "\n"
	if err := os.WriteFile(filepath.Join(work, ".gitmodules"), []byte(modules), 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.run(work, "add", ".gitmodules")
	f.run(work, "update-index", "--add", "--cacheinfo", "160000,"+commit+","+path)
	f.run(work, "commit", "-q", "-m", "Add the submodule "+path)
	f.run(work, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
}

// RefuseRefsOutsideBranches makes the repository refuse a push to a ref
// outside refs/heads and refs/tags, as a forge that permits only branches
// and tags does (cluster check 12 finds if Bitbucket Cloud is one).
func (f *Forge) RefuseRefsOutsideBranches(repo string) {
	f.t.Helper()
	hook := filepath.Join(f.bare(repo), "hooks", "pre-receive")
	script := "#!/bin/sh\nwhile read old new ref; do\n  case \"$ref\" in\n    refs/heads/*|refs/tags/*) ;;\n" +
		"    *) echo \"refusing $ref: only branches and tags can be pushed\" >&2; exit 1 ;;\n  esac\ndone\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		f.t.Fatal(err)
	}
}

// RefusePushes makes the repository refuse each push, with message in
// git's output, as a forge hook that refuses a push does.
func (f *Forge) RefusePushes(repo, message string) {
	f.t.Helper()
	hook := filepath.Join(f.bare(repo), "hooks", "pre-receive")
	script := "#!/bin/sh\ncat >/dev/null\necho '" + strings.ReplaceAll(message, "'", "") + "' >&2\nexit 1\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		f.t.Fatal(err)
	}
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
// Hang makes each git request of the repository wait with no answer, as
// a forge that accepts the connection and then does not answer. The
// channel that it gives closes at the first such request. A request ends
// when its client goes, or at the end of the test.
func (f *Forge) Hang(repo string) <-chan struct{} {
	h := &hang{reached: make(chan struct{}), release: make(chan struct{})}
	f.mu.Lock()
	f.hanging["/git/"+repo+".git/"] = h
	f.mu.Unlock()
	// This cleanup runs before the server's Close, which waits for each
	// request.
	f.t.Cleanup(func() { close(h.release) })
	return h.reached
}

// RefuseGit makes each git request of the repository get 401, as for a
// token that lacks the access to clone. The API still answers.
func (f *Forge) RefuseGit(repo string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refusedGit == nil {
		f.refusedGit = map[string]bool{}
	}
	f.refusedGit["/git/"+repo+".git/"] = true
}

func (f *Forge) git(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	var hung *hang
	for prefix, x := range f.hanging {
		if strings.HasPrefix(r.URL.Path, prefix) {
			hung = x
		}
	}
	f.mu.Unlock()
	if hung != nil {
		hung.once.Do(func() { close(hung.reached) })
		select {
		case <-r.Context().Done():
		case <-hung.release:
		}
		return
	}
	f.mu.Lock()
	refused := false
	for prefix := range f.refusedGit {
		refused = refused || strings.HasPrefix(r.URL.Path, prefix)
	}
	f.mu.Unlock()
	if user, password, ok := r.BasicAuth(); !ok || user != TokenUser || password != f.Token || refused {
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
