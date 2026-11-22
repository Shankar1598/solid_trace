package recorder

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

// IssueCommentsPath is Console's API for a comment on an Issue
// (IssueCommentsController).
const IssueCommentsPath = "/api/issue_comments"

// MaxIssueCommentBody is the longest body that Console takes, in
// characters (IssueCommentsController::MAX_BODY).
const MaxIssueCommentBody = 65_536

// IssueComment is one comment that the factory added to an Issue.
type IssueComment struct {
	// Project is the Project's slug, and Issue the Issue's number.
	Project string
	Issue   int
	Body    string
	RunID   string
	// Token is the token that sent it.
	Token string
}

// RefuseIssueComments makes each later comment get status (0 accepts
// again), as Console gives when it is down (503) or has no such Issue
// (404).
func (r *Recorder) RefuseIssueComments(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refuseComments = status
}

// IssueComments are the comments that the recorder took, oldest first.
func (r *Recorder) IssueComments() []IssueComment {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.comments)
}

// serveIssueComments answers POST /api/issue_comments for an active
// token with issues:comment, in the shapes of Console's answer: 401 for
// another token, 403 with no issues:comment, 422 for a body that is not
// the API's or is longer than MaxIssueCommentBody, and 404 for a Project
// slug that the token's Organization does not have now.
func (r *Recorder) serveIssueComments(w http.ResponseWriter, call Call) {
	token, _ := strings.CutPrefix(call.Authorization, "Bearer ")
	r.mu.Lock()
	defer r.mu.Unlock()
	answer, active := r.answers[token]
	switch {
	case call.Method != http.MethodPost:
		answerJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	case token == "" || !active:
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		answerJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	case !slices.Contains(strings.Fields(answer.Scope), "issues:comment"):
		answerJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient_scope", "scope": "issues:comment"})
		return
	case r.refuseComments == http.StatusNotFound:
		answerJSON(w, http.StatusNotFound, map[string]string{"error": "No such Issue."})
		return
	case r.refuseComments != 0:
		answerJSON(w, r.refuseComments, map[string]string{"error": "refused by the test"})
		return
	}
	var body struct {
		Project string `json:"project"`
		Issue   int    `json:"issue"`
		Body    string `json:"body"`
		RunID   string `json:"run_id"`
	}
	if err := json.Unmarshal(call.Body, &body); err != nil || strings.TrimSpace(body.Body) == "" {
		answerJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": map[string][]string{"body": {"Give the comment's body as text."}}})
		return
	}
	if utf8.RuneCountInString(body.Body) > MaxIssueCommentBody {
		answerJSON(w, http.StatusUnprocessableEntity,
			map[string]any{"errors": map[string][]string{"body": {"The body can have at most 65536 characters."}}})
		return
	}
	if !r.hasProject(answer, body.Project) {
		answerJSON(w, http.StatusNotFound, map[string]string{"error": "No such Issue."})
		return
	}
	r.comments = append(r.comments, IssueComment{Project: body.Project, Issue: body.Issue, Body: body.Body,
		RunID: body.RunID, Token: token})
	n := len(r.comments)
	answerJSON(w, http.StatusCreated, map[string]any{"comment": map[string]any{"id": n,
		"author": map[string]any{"name": "Factory", "run": map[string]string{"id": body.RunID}}, "body": body.Body}})
}

// hasProject is true when an Organization of the token's answer has a
// Project with slug now. The caller holds r.mu.
func (r *Recorder) hasProject(answer Answer, slug string) bool {
	for _, o := range answer.Organizations {
		if slices.ContainsFunc(r.projects[o.Slug], func(p Project) bool { return p.Slug == slug }) {
			return true
		}
	}
	return false
}
