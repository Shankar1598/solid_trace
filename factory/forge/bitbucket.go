package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The defaults of Bitbucket Cloud.
const (
	BitbucketAPIURL = "https://api.bitbucket.org/2.0"
	BitbucketGitURL = "https://bitbucket.org"
	// BitbucketHost is the host of the repositories in the Factory
	// settings.
	BitbucketHost = "bitbucket.org"
	// bitbucketTokenUser is the git user name that goes with an access
	// token (a workspace, project or repository access token).
	bitbucketTokenUser = "x-token-auth"
)

// Bitbucket is the Forge on Bitbucket Cloud, with one access token for
// each Organization. The API calls send the token as a bearer token; git
// gets it from a credential helper with the user name x-token-auth.
//
// The token needs the scopes repository:write (clone, push, the
// work-in-progress ref) and pullrequest:write (open and update a PR).
type Bitbucket struct {
	// APIURL is the base of the REST API 2.0 ("" is BitbucketAPIURL).
	APIURL string
	// GitURL is the base of the clone URLs ("" is BitbucketGitURL).
	GitURL string
	// Token is the connection's access token.
	Token string
	// WIPRefs is the place of the work-in-progress refs:
	// WIPOutsideBranches ("" is the same) or WIPInBranches.
	WIPRefs string
	// HTTP is the client of the API calls (nil is a client with a
	// 30-second timeout).
	HTTP *http.Client
}

var _ Forge = (*Bitbucket)(nil)

func (b *Bitbucket) apiURL() string {
	if b.APIURL == "" {
		return BitbucketAPIURL
	}
	return strings.TrimRight(b.APIURL, "/")
}

func (b *Bitbucket) gitURL() string {
	if b.GitURL == "" {
		return BitbucketGitURL
	}
	return strings.TrimRight(b.GitURL, "/")
}

// split gives the workspace and the repository slug of host/workspace/name.
func (b *Bitbucket) split(repository string) (string, string, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 3 || !strings.EqualFold(parts[0], BitbucketHost) || parts[1] == "" || parts[2] == "" {
		return "", "", &Error{Forge: "Bitbucket", Action: "use the repository", Repository: quote(repository),
			Message: "a Bitbucket Cloud repository is " + BitbucketHost + "/<workspace>/<name>",
			Hint:    "Change the repository in the Project's Factory settings"}
	}
	return parts[1], parts[2], nil
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	return s
}

// CloneURL is GitURL/<workspace>/<name>.git.
func (b *Bitbucket) CloneURL(repository string) (string, error) {
	workspace, name, err := b.split(repository)
	if err != nil {
		return "", err
	}
	return b.gitURL() + "/" + url.PathEscape(workspace) + "/" + url.PathEscape(name) + ".git", nil
}

// CredentialConfig gives a credential helper for GitURL only. It first
// clears each other helper for that URL, so a helper of the machine
// cannot answer in its place. The helper reads the file each time, so a
// new token in the file applies at the next git command.
func (b *Bitbucket) CredentialConfig(tokenFile string) ([]GitSetting, error) {
	if !filepath.IsAbs(tokenFile) || strings.ContainsAny(tokenFile, "'\n\r") {
		return nil, fmt.Errorf("the token file must be an absolute path with no ' and no line break, not %q", tokenFile)
	}
	key := "credential." + b.gitURL() + ".helper"
	helper := `!f() { test "$1" = get || exit 0; echo username=` + bitbucketTokenUser +
		`; printf 'password=%s\n' "$(cat '` + tokenFile + `')"; }; f`
	return []GitSetting{{Key: key, Value: ""}, {Key: key, Value: helper}}, nil
}

// prJSON is the part of Bitbucket's pullrequest object that the factory
// reads.
type prJSON struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"`
	Source      struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"source"`
	Destination struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"destination"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

func (p prJSON) pr() PR {
	return PR{ID: p.ID, Title: p.Title, Description: p.Description, State: PRState(p.State),
		Source: p.Source.Branch.Name, Destination: p.Destination.Branch.Name, Link: p.Links.HTML.Href}
}

type branchJSON struct {
	Branch struct {
		Name string `json:"name"`
	} `json:"branch"`
}

func branch(name string) *branchJSON {
	var b branchJSON
	b.Branch.Name = name
	return &b
}

// OpenPR is POST /repositories/{workspace}/{repo_slug}/pullrequests.
func (b *Bitbucket) OpenPR(ctx context.Context, repository string, pr NewPR) (PR, error) {
	body := struct {
		Title             string      `json:"title"`
		Description       string      `json:"description,omitempty"`
		Source            *branchJSON `json:"source"`
		Destination       *branchJSON `json:"destination,omitempty"`
		CloseSourceBranch bool        `json:"close_source_branch"`
	}{Title: pr.Title, Description: pr.Description, Source: branch(pr.Source)}
	if pr.Destination != "" {
		body.Destination = branch(pr.Destination)
	}
	var out prJSON
	err := b.call(ctx, "open the PR", repository, http.MethodPost, "/pullrequests", body, &out)
	return out.pr(), err
}

// UpdatePR is PUT /repositories/{workspace}/{repo_slug}/pullrequests/{id}
// with only the fields that change.
func (b *Bitbucket) UpdatePR(ctx context.Context, repository string, id int, text PRText) (PR, error) {
	body := map[string]string{}
	if text.Title != "" {
		body["title"] = text.Title
	}
	if text.Description != "" {
		body["description"] = text.Description
	}
	var out prJSON
	err := b.call(ctx, "update the PR", repository, http.MethodPut, fmt.Sprintf("/pullrequests/%d", id), body, &out)
	return out.pr(), err
}

// PR is GET /repositories/{workspace}/{repo_slug}/pullrequests/{id}.
func (b *Bitbucket) PR(ctx context.Context, repository string, id int) (PR, error) {
	var out prJSON
	err := b.call(ctx, "read the PR", repository, http.MethodGet, fmt.Sprintf("/pullrequests/%d", id), nil, &out)
	return out.pr(), err
}

// maxAnswer is the most of an answer that the Forge reads.
const maxAnswer = 4 << 20

// call sends one API request about the repository and decodes the
// answer into out. Each failure is an *Error.
func (b *Bitbucket) call(ctx context.Context, action, repository, method, path string, body, out any) error {
	workspace, name, err := b.split(repository)
	if err != nil {
		return err
	}
	fail := func(status int, message, hint string) error {
		return &Error{Forge: "Bitbucket", Action: action, Repository: repository, Status: status, Message: message, Hint: hint}
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fail(0, "the request is not JSON: "+err.Error(), "")
		}
		reader = bytes.NewReader(data)
	}
	target := b.apiURL() + "/repositories/" + url.PathEscape(workspace) + "/" + url.PathEscape(name) + path
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fail(0, err.Error(), "")
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := b.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fail(0, "Bitbucket did not answer: "+b.scrub(err.Error()), "Try again later; if it stays, check the network to Bitbucket")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return fail(0, "Bitbucket's answer stopped: "+b.scrub(err.Error()), "Try again later")
	}
	if resp.StatusCode/100 != 2 {
		return fail(resp.StatusCode, b.scrub(errorMessage(resp.StatusCode, data)), hint(resp.StatusCode))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fail(0, "Bitbucket's answer is not the JSON that the factory knows: "+err.Error(), "")
	}
	return nil
}

// scrub removes the token from a text, so that no reason holds it.
func (b *Bitbucket) scrub(s string) string {
	if b.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, b.Token, "[token]")
}

// errorMessage reads Bitbucket's error body {"type": "error", "error":
// {"message", "fields"}}. A body of another shape gives its first line.
func errorMessage(status int, data []byte) string {
	var e struct {
		Error struct {
			Message string              `json:"message"`
			Fields  map[string][]string `json:"fields"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil {
		if e.Error.Message != "" {
			return e.Error.Message
		}
		var parts []string
		for field, messages := range e.Error.Fields {
			parts = append(parts, field+": "+strings.Join(messages, " "))
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	if len(line) > 200 {
		line = line[:200] + "..."
	}
	if line == "" {
		return http.StatusText(status)
	}
	return line
}

// hint says what a person can do about an HTTP status.
func hint(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "The connection's token is not valid or has expired: an Admin sets a new one with 'solidtrace connections set'"
	case status == http.StatusForbidden:
		return "The connection's token lacks a scope or an access: it needs repository:write and pullrequest:write " +
			"on the repository; an Admin sets a new one with 'solidtrace connections set'"
	case status == http.StatusNotFound:
		return "Check the repository in the Project's Factory settings, and that the connection's token can see it"
	case status == http.StatusTooManyRequests:
		return "Bitbucket limits the rate of requests: try again later"
	case status >= 500:
		return "Bitbucket has a fault: try again later"
	}
	return ""
}

// WIPRef is WIPRefs/<run id>.
func (b *Bitbucket) WIPRef(runID string) (string, error) { return wipRef(b.WIPRefs, runID) }

// SaveWIP force-pushes commit to the Run's work-in-progress ref. The
// checkout's pre-push hook does not run.
func (b *Bitbucket) SaveWIP(ctx context.Context, dir, repository, runID, commit string) (string, error) {
	ref, err := b.WIPRef(runID)
	if err != nil {
		return "", err
	}
	if commit == "" || strings.HasPrefix(commit, "-") {
		return "", fmt.Errorf("a save needs a commit id, not %q", commit)
	}
	out, err := b.push(ctx, dir, repository, "--force", commit+":"+ref)
	if err != nil {
		hint := gitHint(out)
		if hint == "" && b.WIPRefs != WIPInBranches && strings.Contains(out, "[remote rejected]") {
			hint = "If the forge permits only branches, the operator sets the work-in-progress refs to " + WIPInBranches
		}
		return "", b.gitError("save the work-in-progress ref "+ref, repository, out, err, hint)
	}
	return ref, nil
}

// DeleteWIP pushes a delete of the Run's work-in-progress ref, from an
// empty repository in a temp directory.
func (b *Bitbucket) DeleteWIP(ctx context.Context, repository, runID string) error {
	ref, err := b.WIPRef(runID)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "forge-delete-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if out, err := runGit(ctx, dir, nil, "init", "-q", "--bare"); err != nil {
		return b.gitError("delete the work-in-progress ref "+ref, repository, out, err, "")
	}
	// git sends the delete of a full ref name also when the ref is not
	// there, and the server only warns: a Run with no saved work is not
	// an error.
	out, err := b.push(ctx, dir, repository, ":"+ref)
	if err != nil {
		return b.gitError("delete the work-in-progress ref "+ref, repository, out, err, gitHint(out))
	}
	return nil
}

// gitHint gives the hint of the HTTP status that git's output tells, and
// "" when the output tells none.
func gitHint(out string) string {
	switch {
	case strings.Contains(out, "Authentication failed") || strings.Contains(out, "returned error: 401"):
		return hint(http.StatusUnauthorized)
	case strings.Contains(out, "returned error: 403"):
		return hint(http.StatusForbidden)
	}
	return ""
}

// push runs git push to the repository's clone URL with the token in a
// temp file that only this push reads.
func (b *Bitbucket) push(ctx context.Context, dir, repository string, args ...string) (string, error) {
	target, err := b.CloneURL(repository)
	if err != nil {
		return "", err
	}
	secrets, err := os.MkdirTemp("", "forge-token-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(secrets)
	tokenFile := filepath.Join(secrets, "token")
	if err := os.WriteFile(tokenFile, []byte(b.Token), 0o600); err != nil {
		return "", err
	}
	settings, err := b.CredentialConfig(tokenFile)
	if err != nil {
		return "", err
	}
	return runGit(ctx, dir, GitArgs(settings), append([]string{"push", "--no-verify", "--porcelain", target}, args...)...)
}

func (b *Bitbucket) gitError(action, repository, out string, err error, hint string) error {
	message := b.scrub(lastLines(out, 3))
	if message == "" {
		message = err.Error()
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		message = "git did not run: " + err.Error()
	}
	return &Error{Forge: "Bitbucket", Action: action, Repository: repository, Message: message, Hint: hint}
}

// lastLines gives the last n lines of git's output that are not empty,
// joined with "; ".
func lastLines(out string, n int) string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" && line != "Done" && !strings.HasPrefix(line, "To ") {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "; ")
}

// runGit runs git with no prompt for a password.
func runGit(ctx context.Context, dir string, extra []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(extra, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}
