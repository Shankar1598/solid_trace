package fakeforge

// The API follows Bitbucket Cloud's REST API 2.0 reference
// (developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests):
// the same paths, the same JSON names, the same error shape and the same
// status codes, for the parts that the factory uses.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// apiError is Bitbucket's error answer: {"type": "error", "error":
// {"message": ..., "fields": {...}}}.
type apiError struct {
	status  int
	message string
	fields  map[string][]string
}

func (e *apiError) body() any {
	inner := map[string]any{"message": e.message}
	if len(e.fields) > 0 {
		inner["fields"] = e.fields
	}
	return map[string]any{"type": "error", "error": inner}
}

func fieldError(field, message string) *apiError {
	return &apiError{http.StatusBadRequest, field + ": " + message, map[string][]string{field: {message}}}
}

// rawFile is an answer that is a file's content, not JSON (the src
// endpoint).
type rawFile []byte

func writeAnswer(w http.ResponseWriter, status int, body any) {
	if raw, ok := body.(rawFile); ok {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		w.Write(raw)
		return
	}
	data, err := json.Marshal(body)
	if err != nil {
		status, data = http.StatusInternalServerError, []byte(`{"type":"error","error":{"message":"the answer is not JSON"}}`)
	}
	w.WriteHeader(status)
	w.Write(append(data, '\n'))
}

func (f *Forge) api(h func(r *http.Request, repo string) (int, any, *apiError)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+f.Token {
			writeAnswer(w, http.StatusUnauthorized, (&apiError{message: "Token is invalid or not supported for this endpoint."}).body())
			return
		}
		repo := r.PathValue("workspace") + "/" + r.PathValue("repo_slug")
		if _, err := os.Stat(f.bare(repo)); err != nil {
			writeAnswer(w, http.StatusNotFound, (&apiError{message: "Repository " + repo + " not found"}).body())
			return
		}
		f.mu.Lock()
		held := f.holdNext
		f.holdNext = nil
		f.mu.Unlock()
		if held != nil {
			close(held.reached)
			<-held.release
		}
		// The body holds the Forge's state, so encode it while f.mu is
		// held: a test helper can change the same PR.
		f.mu.Lock()
		defer f.mu.Unlock()
		f.apiCalls++
		if e := f.failNext; e != nil {
			f.failNext = nil
			writeAnswer(w, e.status, e.body())
			return
		}
		if e := f.failAll; e != nil {
			writeAnswer(w, e.status, e.body())
			return
		}
		status, body, e := h(r, repo)
		if e != nil {
			status, body = e.status, e.body()
		}
		writeAnswer(w, status, body)
	}
}

// FailNext makes the next API call get this status and Bitbucket's error
// body with this message, as when Bitbucket is down or a token lacks a
// scope.
func (f *Forge) FailNext(status int, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = &apiError{status: status, message: message}
}

// FailAll makes each API call get this status and Bitbucket's error body
// with this message, as when Bitbucket is down for a long time. Status 0
// stops it.
func (f *Forge) FailAll(status int, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAll = nil
	if status != 0 {
		f.failAll = &apiError{status: status, message: message}
	}
}

// FailCommentPosts makes each post of a PR comment get this status, as
// when Bitbucket fails only that call. Status 0 stops it.
func (f *Forge) FailCommentPosts(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failPosts = status
}

// HoldNextAPI makes the next API call wait until release, as a slow
// forge. The channel closes when the call arrives. After the release the
// forge does the call, also when its client went: a slow forge can open
// a PR that its client did not wait for. The end of the test releases it.
func (f *Forge) HoldNextAPI() (reached <-chan struct{}, release func()) {
	h := &hang{reached: make(chan struct{}), release: make(chan struct{})}
	f.mu.Lock()
	f.holdNext = h
	f.mu.Unlock()
	release = func() { h.once.Do(func() { close(h.release) }) }
	f.t.Cleanup(release)
	return h.reached, release
}

// account is Bitbucket's account object of an author who is a person.
func account(name string) map[string]any {
	return map[string]any{"type": "user", "display_name": name, "nickname": name,
		"account_id": "557058:" + name, "uuid": "{" + name + "}"}
}

// appAccount is Bitbucket's account object of an app user: the account
// of an access token or of an app, which is not a person.
func appAccount(name string) map[string]any {
	return map[string]any{"type": "app_user", "display_name": name, "kind": "repository_access_token",
		"account_id": "557058:app-" + name, "uuid": "{app-" + name + "}"}
}

func endpoint(repo, branch string) map[string]any {
	return map[string]any{"branch": map[string]any{"name": branch},
		"repository": map[string]any{"type": "repository", "full_name": repo}}
}

// prJSON is a PR in Bitbucket's pullrequest shape. The caller holds f.mu.
func (f *Forge) prJSON(repo string, p *PR) map[string]any {
	self := fmt.Sprintf("%s/repositories/%s/pullrequests/%d", f.APIURL(), repo, p.ID)
	return map[string]any{
		"type": "pullrequest", "id": p.ID, "title": p.Title, "description": p.Description,
		"summary": map[string]any{"raw": p.Description, "markup": "markdown"},
		"state":   p.State, "author": account(p.Author),
		"source": endpoint(repo, p.Source), "destination": endpoint(repo, p.Destination),
		"close_source_branch": false, "comment_count": len(f.comments[repo][p.ID]),
		"created_on": p.Created.Format(time.RFC3339Nano), "updated_on": p.Updated.Format(time.RFC3339Nano),
		"links": map[string]any{"self": map[string]any{"href": self}, "html": map[string]any{"href": p.Link}},
	}
}

// commentJSON is a comment in Bitbucket's pullrequest_comment shape.
func commentJSON(pr int, c *Comment) map[string]any {
	user := account(c.Author)
	if c.Bot {
		user = appAccount(c.Author)
	}
	body := c.Body
	if c.Deleted {
		body = ""
	}
	out := map[string]any{
		"type": "pullrequest_comment", "id": c.ID, "user": user,
		"content":    map[string]any{"raw": body, "markup": "markdown"},
		"created_on": c.Created.Format(time.RFC3339Nano), "updated_on": c.Updated.Format(time.RFC3339Nano),
		"deleted": c.Deleted, "pending": false, "pullrequest": map[string]any{"type": "pullrequest", "id": pr},
	}
	if c.File != "" {
		out["inline"] = map[string]any{"path": c.File, "to": c.Line, "from": nil}
	}
	if c.Parent != 0 {
		out["parent"] = map[string]any{"id": c.Parent}
	}
	if c.Resolved {
		out["resolution"] = map[string]any{"type": "comment_resolution", "user": account("A person")}
	}
	return out
}

// page is Bitbucket's paginated list, with all values on one page.
func page(values []map[string]any) map[string]any {
	if values == nil {
		values = []map[string]any{}
	}
	return map[string]any{"pagelen": 50, "page": 1, "size": len(values), "values": values}
}

func (f *Forge) openPR(r *http.Request, repo string) (int, any, *apiError) {
	var in struct {
		Title       string
		Description string
		Source      struct{ Branch struct{ Name string } }
		Destination struct{ Branch struct{ Name string } }
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return 0, nil, &apiError{status: http.StatusBadRequest, message: "Bad request"}
	}
	source, destination := in.Source.Branch.Name, in.Destination.Branch.Name
	switch {
	case in.Title == "":
		return 0, nil, fieldError("title", "This field is required.")
	case source == "":
		return 0, nil, fieldError("source", "This field is required.")
	}
	if destination == "" {
		destination = "main" // the repository's main branch
	}
	if !f.gitOK(f.bare(repo), "rev-parse", "--verify", "-q", "refs/heads/"+source) {
		return 0, nil, fieldError("source", "Branch not found")
	}
	if !f.gitOK(f.bare(repo), "rev-parse", "--verify", "-q", "refs/heads/"+destination) {
		return 0, nil, fieldError("destination", "Branch not found")
	}
	for _, p := range f.prs[repo] {
		if p.Source == source && p.Destination == destination && p.State == "OPEN" {
			return 0, nil, &apiError{status: http.StatusBadRequest,
				message: fmt.Sprintf("There is already an open pull request from %s to %s (#%d).", source, destination, p.ID)}
		}
	}
	now := time.Now().UTC()
	p := &PR{ID: len(f.prs[repo]) + 1, Title: in.Title, Description: in.Description, Source: source,
		Destination: destination, State: "OPEN", Author: FactoryAuthor, Created: now, Updated: now}
	p.Link = fmt.Sprintf("%s/%s/pull-requests/%d", f.url, repo, p.ID)
	f.prs[repo] = append(f.prs[repo], p)
	return http.StatusCreated, f.prJSON(repo, p), nil
}

// queryTerm is one `field = "value"` term of a BBQL query that the fake
// knows: source.branch.name, destination.branch.name and state, joined
// with AND.
var queryTerm = regexp.MustCompile(`^\s*(source\.branch\.name|destination\.branch\.name|state)\s*=\s*"([^"]*)"\s*$`)

func (f *Forge) listPRs(r *http.Request, repo string) (int, any, *apiError) {
	want := map[string]string{}
	if q := r.URL.Query().Get("q"); q != "" {
		for _, term := range strings.Split(q, " AND ") {
			m := queryTerm.FindStringSubmatch(term)
			if m == nil {
				return 0, nil, &apiError{status: http.StatusBadRequest, message: "Invalid query: the fake knows " +
					"only source.branch.name, destination.branch.name and state with =, joined by AND"}
			}
			want[m[1]] = m[2]
		}
	}
	// With no state parameter, Bitbucket lists only open PRs.
	states := r.URL.Query()["state"]
	if len(states) == 0 {
		states = []string{"OPEN"}
	}
	if s, ok := want["state"]; ok {
		states = []string{s}
	}
	var values []map[string]any
	for _, p := range f.prs[repo] {
		if !slices.Contains(states, p.State) {
			continue
		}
		if s, ok := want["source.branch.name"]; ok && s != p.Source {
			continue
		}
		if s, ok := want["destination.branch.name"]; ok && s != p.Destination {
			continue
		}
		values = append(values, f.prJSON(repo, p))
	}
	return http.StatusOK, page(values), nil
}

func (f *Forge) prFrom(r *http.Request, repo string) (*PR, *apiError) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if p := f.findPR(repo, id); p != nil {
		return p, nil
	}
	return nil, &apiError{status: http.StatusNotFound, message: "Pull request " + r.PathValue("id") + " not found"}
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
	if e != nil {
		return 0, nil, e
	}
	return http.StatusOK, f.prJSON(repo, p), nil
}

// updatePR changes the fields that the body holds; the others stay. Only
// an open PR can change, as on Bitbucket.
func (f *Forge) updatePR(r *http.Request, repo string) (int, any, *apiError) {
	p, e := f.prFrom(r, repo)
	if e != nil {
		return 0, nil, e
	}
	var in struct{ Title, Description *string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return 0, nil, &apiError{status: http.StatusBadRequest, message: "Bad request"}
	}
	if p.State != "OPEN" {
		return 0, nil, &apiError{status: http.StatusBadRequest,
			message: "You can't edit a pull request that is " + strings.ToLower(p.State) + "."}
	}
	if in.Title != nil {
		p.Title = *in.Title
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	p.Updated = time.Now().UTC()
	return http.StatusOK, f.prJSON(repo, p), nil
}

// listComments gives one page of the PR's comments, oldest first, as
// Bitbucket does: pagelen (10 when absent, at most 100) and page (from
// 1), with the absolute URL of the next page in next.
func (f *Forge) listComments(r *http.Request, repo string) (int, any, *apiError) {
	p, e := f.prFrom(r, repo)
	if e != nil {
		return 0, nil, e
	}
	size, number := 10, 1
	if v := r.URL.Query().Get("pagelen"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return 0, nil, &apiError{status: http.StatusBadRequest, message: "Invalid pagelen"}
		}
		size = n
	}
	if v := r.URL.Query().Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 0, nil, &apiError{status: http.StatusBadRequest, message: "Invalid page"}
		}
		number = n
	}
	all := f.comments[repo][p.ID]
	values := []map[string]any{}
	for k := (number - 1) * size; k < len(all) && k < number*size; k++ {
		values = append(values, commentJSON(p.ID, all[k]))
	}
	out := map[string]any{"pagelen": size, "page": number, "size": len(all), "values": values}
	if number*size < len(all) {
		next := *r.URL
		q := next.Query()
		q.Set("page", strconv.Itoa(number+1))
		next.RawQuery = q.Encode()
		out["next"] = f.url + next.RequestURI()
	}
	return http.StatusOK, out, nil
}

func (f *Forge) postComment(r *http.Request, repo string) (int, any, *apiError) {
	if f.failPosts != 0 {
		return 0, nil, &apiError{status: f.failPosts, message: "Bitbucket refuses new comments"}
	}
	p, e := f.prFrom(r, repo)
	if e != nil {
		return 0, nil, e
	}
	var in struct {
		Content struct{ Raw string }
		Inline  *struct {
			Path string
			To   int
		}
		Parent *struct{ ID int }
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Content.Raw == "" {
		return 0, nil, fieldError("content", "This field is required.")
	}
	c := Comment{Author: FactoryAuthor, Body: in.Content.Raw}
	if in.Inline != nil {
		c.File, c.Line = in.Inline.Path, in.Inline.To
	}
	if in.Parent != nil {
		if f.findComment(repo, p.ID, in.Parent.ID) == nil {
			return 0, nil, fieldError("parent", "Comment not found")
		}
		c.Parent = in.Parent.ID
	}
	return http.StatusCreated, commentJSON(p.ID, f.addComment(repo, p.ID, c)), nil
}

// getRepository is GET /repositories/{workspace}/{repo_slug}: the
// repository's main branch is the bare repository's HEAD.
func (f *Forge) getRepository(r *http.Request, repo string) (int, any, *apiError) {
	head, err := f.git2(f.bare(repo), "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return 0, nil, &apiError{status: http.StatusInternalServerError, message: head}
	}
	return http.StatusOK, map[string]any{"type": "repository", "full_name": repo, "is_private": !f.public[repo],
		"mainbranch": map[string]any{"type": "branch", "name": strings.TrimSpace(head)}}, nil
}

// getBranch is GET /repositories/{workspace}/{repo_slug}/refs/branches/{name}.
func (f *Forge) getBranch(r *http.Request, repo string) (int, any, *apiError) {
	name := r.PathValue("name")
	hash, err := f.git2(f.bare(repo), "rev-parse", "--verify", "-q", "refs/heads/"+name+"^{commit}")
	if err != nil {
		return 0, nil, &apiError{status: http.StatusNotFound, message: "Branch \"" + name + "\" not found"}
	}
	return http.StatusOK, map[string]any{"type": "branch", "name": name,
		"target": map[string]any{"type": "commit", "hash": strings.TrimSpace(hash)}}, nil
}

// getSource is GET /repositories/{workspace}/{repo_slug}/src/{commit}/{path}:
// a file's raw content at the commit, and 404 when it is not there.
func (f *Forge) getSource(r *http.Request, repo string) (int, any, *apiError) {
	commit, path := r.PathValue("commit"), r.PathValue("path")
	out, err := f.git2(f.bare(repo), "cat-file", "blob", commit+":"+path)
	if err != nil {
		return 0, nil, &apiError{status: http.StatusNotFound, message: "No such file or directory: " + path}
	}
	return http.StatusOK, rawFile(out), nil
}

// findComment is a comment of the PR, or nil. The caller holds f.mu.
func (f *Forge) findComment(repo string, pr, id int) *Comment {
	for _, c := range f.comments[repo][pr] {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// addComment keeps a comment. A reply gets the file and line of its
// parent. The caller holds f.mu.
func (f *Forge) addComment(repo string, pr int, c Comment) *Comment {
	f.nextID++
	c.ID, c.Created = f.nextID, time.Now().UTC()
	c.Updated = c.Created
	if parent := f.findComment(repo, pr, c.Parent); parent != nil && c.File == "" {
		c.File, c.Line = parent.File, parent.Line
	}
	if f.comments[repo] == nil {
		f.comments[repo] = map[int][]*Comment{}
	}
	f.comments[repo][pr] = append(f.comments[repo][pr], &c)
	return &c
}
