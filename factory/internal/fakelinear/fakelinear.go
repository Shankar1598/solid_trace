// Package fakelinear is the fake Linear of Seam 3: one OAuth application
// with its approval page and token endpoint, and the GraphQL operations
// that the factory sends, on issues kept in memory. Its access tokens
// last 24 hours by the test's clock, and each refresh rotates the refresh
// token. A token of an install with actor=app acts as the app's own user,
// so its status changes and comments show as the app; another token acts
// as the person who approved the install.
package fakelinear

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// The fake's OAuth application.
const (
	ClientID     = "factory-app"
	ClientSecret = "factory-app-secret"
)

// TokenLife is how long an access token lasts.
const TokenLife = 24 * time.Hour

// App is the app's own user; Installer is the person who approves the
// install.
var (
	App       = User{ID: "app-user-1", Name: "SolidTrace Factory", App: true}
	Installer = User{ID: "user-ana", Name: "Ana"}
)

// Engineering is the team of the workspace at the start. AddTeam adds
// another.
var Engineering = Team{ID: "team-eng", Key: "ENG", Name: "Engineering"}

// Team is a team of the workspace.
type Team struct{ ID, Key, Name string }

// Statuses are the statuses of each team.
var Statuses = []string{"Backlog", "Ready for agent", "Agent working", "In review", "Waiting on you", "Done"}

// User is a Linear user. Guest is a guest of the workspace; App is an
// app's own user.
type User struct {
	ID, Name   string
	Guest, App bool
}

// By is the author of a comment: a User, or else a bot actor (an
// integration) or an external user, by name.
type By struct {
	User          *User
	Bot, External string
}

// Label is a label of an issue; Group is the name of its label group ("" is
// none).
type Label struct{ Name, Group string }

// Issue is one issue. Team is the key of its team ("" is Engineering).
type Issue struct {
	ID, Identifier, Title, Description, Status string
	Team                                       string
	Labels                                     []Label
}

// Comment is a comment that a token posted.
type Comment struct {
	Issue, Body string
	Author      User
}

// comment is a comment on an issue: a token's, or one that AddComment
// added.
type comment struct {
	id, issue, body string
	by              By
	created, edited time.Time
	// posted is a comment that a token posted.
	posted bool
}

type statusChange struct {
	at     time.Time
	by     User
	status string
}

type issue struct {
	Issue
	updated time.Time
	history []statusChange
}

type grant struct {
	redirectURI, challenge string
	app                    bool
}

type session struct {
	app     bool
	expires time.Time
}

// Linear is the fake Linear.
type Linear struct {
	mu     sync.Mutex
	server *httptest.Server
	// url is the base URL that the links name.
	url string
	now func() time.Time

	redirectURI string
	// Deny makes the approval page deny the install; Forged makes the
	// browser first send a redirect with a wrong state.
	deny, forged bool

	codes     map[string]grant
	access    map[string]session
	refresh   map[string]bool
	refreshes int
	// refusedRefresh refuses each refresh (a revoked install).
	refusedRefresh bool
	// refusedAccess refuses each access token, also a new one.
	refusedAccess bool
	actors        []string

	teams    []Team
	issues   map[string]*issue
	order    []string
	comments []*comment
}

// New starts the fake with the clock now. The test's cleanup stops it.
func New(t interface{ Cleanup(func()) }, now func() time.Time) *Linear {
	return start(t, now, nil, "")
}

// NewAt starts the fake on the listener l, with url as its base URL, for
// a process that other machines reach by a name (the local factory stack).
func NewAt(t interface{ Cleanup(func()) }, now func() time.Time, l net.Listener, url string) *Linear {
	return start(t, now, l, strings.TrimRight(url, "/"))
}

func start(t interface{ Cleanup(func()) }, now func() time.Time, ln net.Listener, url string) *Linear {
	l := &Linear{now: now, codes: map[string]grant{}, access: map[string]session{}, refresh: map[string]bool{},
		issues: map[string]*issue{}, teams: []Team{Engineering}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/authorize", l.authorize)
	mux.HandleFunc("POST /oauth/token", l.token)
	mux.HandleFunc("POST /graphql", l.graphQL)
	l.server = httptest.NewUnstartedServer(mux)
	if ln != nil {
		l.server.Listener.Close()
		l.server.Listener = ln
	}
	l.server.Start()
	t.Cleanup(l.server.Close)
	l.url = l.server.URL
	if url != "" {
		l.url = url
	}
	return l
}

// URL is the base of the API.
func (l *Linear) URL() string { return l.url }

// AuthorizeURL is the approval page.
func (l *Linear) AuthorizeURL() string { return l.url + "/oauth/authorize" }

// IssueURL is the page of the issue.
func IssueURL(identifier string) string { return "https://linear.app/example/issue/" + identifier }

// Register sets the callback URL of the app.
func (l *Linear) Register(redirectURI string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.redirectURI = redirectURI
}

// Deny makes the approval page deny each install.
func (l *Linear) Deny() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.deny = true
}

// Forged makes the browser send a redirect with a wrong state before the
// approval's own redirect.
func (l *Linear) Forged() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.forged = true
}

// RefuseRefresh refuses each refresh from now on, as for a revoked
// install.
func (l *Linear) RefuseRefresh() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refusedRefresh = true
}

// RevokeAccessTokens stops each access token before its expiry; the
// refresh tokens still work.
func (l *Linear) RevokeAccessTokens() {
	l.mu.Lock()
	defer l.mu.Unlock()
	clear(l.access)
}

// RefuseAccessTokens refuses each access token from now on (401), also
// the new access token of a refresh; the refresh tokens still work.
func (l *Linear) RefuseAccessTokens() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refusedAccess = true
}

// Refreshes counts the refreshes that the token endpoint gave.
func (l *Linear) Refreshes() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.refreshes
}

// Actors gives the actor parameter of each approval, in order.
func (l *Linear) Actors() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.actors)
}

// Browser is a person's browser that opens the approval page and
// approves: it follows the redirect to the app's callback URL.
func (l *Linear) Browser(page string) error {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout: 10 * time.Second}
	resp, err := client.Get(page)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return fmt.Errorf("the approval page answered %d", resp.StatusCode)
	}
	callback := resp.Header.Get("Location")
	l.mu.Lock()
	forged := l.forged
	l.mu.Unlock()
	if forged {
		u, _ := url.Parse(callback)
		q := u.Query()
		q.Set("state", "forged")
		q.Set("code", "forged-code")
		u.RawQuery = q.Encode()
		if resp, err := client.Get(u.String()); err == nil {
			resp.Body.Close()
		}
	}
	resp, err = client.Get(callback)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (l *Linear) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	l.mu.Lock()
	defer l.mu.Unlock()
	if q.Get("client_id") != ClientID || q.Get("redirect_uri") == "" || q.Get("redirect_uri") != l.redirectURI ||
		q.Get("response_type") != "code" || q.Get("state") == "" {
		http.Error(w, "invalid authorize request", http.StatusBadRequest)
		return
	}
	l.actors = append(l.actors, q.Get("actor"))
	callback, _ := url.Parse(l.redirectURI)
	answer := url.Values{"state": {q.Get("state")}}
	if l.deny {
		answer.Set("error", "access_denied")
	} else {
		code := random()
		l.codes[code] = grant{redirectURI: l.redirectURI, challenge: q.Get("code_challenge"), app: q.Get("actor") == "app"}
		answer.Set("code", code)
	}
	callback.RawQuery = answer.Encode()
	http.Redirect(w, r, callback.String(), http.StatusFound)
}

func oauthError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": "the fake refused it"})
}

func (l *Linear) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, "invalid_request")
		return
	}
	f := r.PostForm
	l.mu.Lock()
	defer l.mu.Unlock()
	if f.Get("client_id") != ClientID || subtle.ConstantTimeCompare([]byte(f.Get("client_secret")), []byte(ClientSecret)) != 1 {
		oauthError(w, "invalid_client")
		return
	}
	var app bool
	switch f.Get("grant_type") {
	case "authorization_code":
		g, ok := l.codes[f.Get("code")]
		delete(l.codes, f.Get("code"))
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if !ok || g.redirectURI != f.Get("redirect_uri") ||
			(g.challenge != "" && base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge) {
			oauthError(w, "invalid_grant")
			return
		}
		app = g.app
	case "refresh_token":
		old := f.Get("refresh_token")
		if l.refusedRefresh || !l.refresh[old] {
			oauthError(w, "invalid_grant")
			return
		}
		delete(l.refresh, old)
		l.refreshes++
		app = strings.HasPrefix(old, "app-")
	default:
		oauthError(w, "unsupported_grant_type")
		return
	}
	prefix := "user-"
	if app {
		prefix = "app-"
	}
	accessToken, refreshToken := prefix+"access-"+random(), prefix+"refresh-"+random()
	l.access[accessToken] = session{app: app, expires: l.now().Add(TokenLife)}
	l.refresh[refreshToken] = true
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": accessToken, "token_type": "Bearer",
		"expires_in": int(TokenLife/time.Second) - 1, "scope": "read write", "refresh_token": refreshToken})
}

// AddTeam adds a team to the workspace, which each install can read.
func (l *Linear) AddTeam(t Team) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.teams = append(l.teams, t)
}

func (l *Linear) team(key string) Team {
	for _, t := range l.teams {
		if t.Key == key {
			return t
		}
	}
	return Engineering
}

// Put adds the issue, or replaces it, as the user by.
func (l *Linear) Put(i Issue, by User) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()
	old := l.issues[i.ID]
	if old == nil {
		old = &issue{}
		l.order = append(l.order, i.ID)
	}
	if old.Status != i.Status {
		old.history = append(old.history, statusChange{at: now, by: by, status: i.Status})
	}
	old.Issue = i
	old.updated = now
	l.issues[i.ID] = old
}

// SetStatus sets the status of the issue as the user by.
func (l *Linear) SetStatus(id, status string, by User) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.setStatus(id, status, by)
}

func (l *Linear) setStatus(id, status string, by User) {
	i := l.issues[id]
	now := l.now().UTC()
	i.Status = status
	i.updated = now
	i.history = append(i.history, statusChange{at: now, by: by, status: status})
}

// Status gives the status of the issue and the user who set it.
func (l *Linear) Status(id string) (string, User) {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := l.issues[id]
	return i.Status, i.history[len(i.history)-1].by
}

// Comments gives the comments that tokens posted, in order.
func (l *Linear) Comments() []Comment {
	l.mu.Lock()
	defer l.mu.Unlock()
	var list []Comment
	for _, c := range l.comments {
		if c.posted {
			list = append(list, Comment{Issue: c.issue, Body: c.body, Author: *c.by.User})
		}
	}
	return list
}

// AddComment adds a comment on the issue, and gives its id. A comment
// changes the issue's updatedAt.
func (l *Linear) AddComment(issueID, body string, by By) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := l.issues[issueID]
	if i == nil {
		panic("fakelinear: no issue " + issueID)
	}
	return l.addComment(i, body, by, false)
}

func (l *Linear) addComment(i *issue, body string, by By, posted bool) string {
	now := l.now().UTC()
	c := &comment{id: "comment-" + random(), issue: i.ID, body: body, by: by, created: now, posted: posted}
	l.comments = append(l.comments, c)
	i.updated = now
	return c.id
}

// EditComment changes the text of a comment.
func (l *Linear) EditComment(id, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.comments {
		if c.id == id {
			last := c.edited
			if last.IsZero() {
				last = c.created
			}
			edited := l.now().UTC()
			if !edited.After(last) {
				edited = last.Add(time.Millisecond)
			}
			c.body, c.edited = body, edited
			return
		}
	}
	panic("fakelinear: no comment " + id)
}

func (c *comment) json() map[string]any {
	var user, bot, external any
	if u := c.by.User; u != nil {
		user = map[string]any{"id": u.ID, "name": u.Name, "guest": u.Guest, "app": u.App}
	}
	if c.by.Bot != "" {
		bot = map[string]string{"id": "bot-" + c.by.Bot, "name": c.by.Bot}
	}
	if c.by.External != "" {
		external = map[string]string{"id": "ext-" + c.by.External, "name": c.by.External}
	}
	var edited any
	if !c.edited.IsZero() {
		edited = c.edited.Format(time.RFC3339Nano)
	}
	return map[string]any{"id": c.id, "body": c.body, "createdAt": c.created.Format(time.RFC3339Nano),
		"editedAt": edited, "user": user, "botActor": bot, "externalUser": external}
}

// Webhook is a webhook of the type (Issue, Comment) about the issue at the
// time at, signed with secret. A Comment webhook names the issue as its
// issueId.
func Webhook(secret, delivery, kind, issueID string, at time.Time) (http.Header, []byte) {
	data := map[string]any{"id": issueID}
	if kind != "Issue" {
		data = map[string]any{"id": "comment-" + delivery, "issueId": issueID}
	}
	body, _ := json.Marshal(map[string]any{"action": "update", "type": kind, "data": data,
		"url": "https://linear.app/example", "webhookTimestamp": at.UnixMilli(), "organizationId": "org-1"})
	return Signed(secret, delivery, body), body
}

// Signed gives the headers of a delivery of body, signed with secret.
func Signed(secret, delivery string, body []byte) http.Header {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	header := http.Header{}
	header.Set("Linear-Signature", hex.EncodeToString(mac.Sum(nil)))
	header.Set("Linear-Delivery", delivery)
	header.Set("Linear-Event", "Issue")
	header.Set("Content-Type", "application/json")
	return header
}

func (i *issue) json(team Team) map[string]any {
	labels := []map[string]any{}
	for _, label := range i.Labels {
		var parent any
		if label.Group != "" {
			parent = map[string]string{"name": label.Group}
		}
		labels = append(labels, map[string]any{"name": label.Name, "parent": parent})
	}
	history := []map[string]any{}
	// Newest first, as a list in any order must be read.
	for _, h := range slices.Backward(i.history) {
		history = append(history, map[string]any{"createdAt": h.at.Format(time.RFC3339Nano),
			"actor": map[string]string{"id": h.by.ID, "name": h.by.Name}, "toState": map[string]string{"name": h.status}})
	}
	return map[string]any{"id": i.ID, "identifier": i.Identifier, "url": IssueURL(i.Identifier), "title": i.Title,
		"description": i.Description, "updatedAt": i.updated.Format(time.RFC3339Nano),
		"state": map[string]string{"name": i.Status}, "team": map[string]string{"id": team.ID},
		"labels": map[string]any{"nodes": labels}, "history": map[string]any{"nodes": history}}
}

func answer(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func graphQLError(w http.ResponseWriter, message string) {
	answer(w, http.StatusOK, map[string]any{"data": nil, "errors": []map[string]string{{"message": message}}})
}

func (l *Linear) graphQL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OperationName string         `json:"operationName"`
		Query         string         `json:"query"`
		Variables     map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		answer(w, http.StatusBadRequest, map[string]any{"errors": []map[string]string{{"message": "bad body"}}})
		return
	}
	// Linear refuses a query above its complexity limit before it runs it.
	if tenths, err := Complexity(req.Query); err != nil || tenths > MaxComplexity*10 {
		message := "Syntax Error: the fake cannot read the query"
		if err == nil {
			message = complexityError(tenths)
		}
		answer(w, http.StatusBadRequest, map[string]any{"errors": []map[string]any{{"message": message,
			"extensions": map[string]string{"code": "GRAPHQL_VALIDATION_FAILED"}}}})
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok || !l.now().Before(s.expires) || l.refusedAccess {
		answer(w, http.StatusUnauthorized, map[string]any{"errors": []map[string]string{{"message": "Authentication required"}}})
		return
	}
	actor := Installer
	if s.app {
		actor = App
	}
	str := func(name string) string { v, _ := req.Variables[name].(string); return v }
	find := func() (*issue, error) {
		if i := l.issues[str("id")]; i != nil {
			return i, nil
		}
		return nil, errors.New("Entity not found: Issue - Could not find referenced Issue.")
	}
	var data any
	switch req.OperationName {
	case "Viewer":
		data = map[string]any{"viewer": map[string]string{"id": actor.ID, "name": actor.Name}}
	case "Ticket":
		i, err := find()
		if err != nil {
			graphQLError(w, err.Error())
			return
		}
		data = map[string]any{"issue": i.json(l.team(i.Team))}
	case "Teams":
		nodes := []map[string]string{}
		for _, t := range l.teams {
			nodes = append(nodes, map[string]string{"id": t.ID, "key": t.Key, "name": t.Name})
		}
		data = map[string]any{"teams": map[string]any{"nodes": nodes}}
	case "ReadyTickets":
		nodes := []map[string]any{}
		for _, id := range l.order {
			i := l.issues[id]
			if team := l.team(i.Team); i.Status == str("status") && team.ID == str("team") {
				nodes = append(nodes, i.json(team))
			}
		}
		data = map[string]any{"issues": map[string]any{"nodes": nodes,
			"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""}}}
	case "TicketStatuses":
		if _, err := find(); err != nil {
			graphQLError(w, err.Error())
			return
		}
		nodes := []map[string]string{}
		if slices.Contains(Statuses, str("name")) {
			nodes = append(nodes, map[string]string{"id": "state-" + str("name")})
		}
		data = map[string]any{"issue": map[string]any{"team": map[string]any{"states": map[string]any{"nodes": nodes}}}}
	case "SetTicketStatus":
		i, err := find()
		status := strings.TrimPrefix(str("stateId"), "state-")
		if err != nil || !slices.Contains(Statuses, status) {
			graphQLError(w, "Entity not found: WorkflowState")
			return
		}
		l.setStatus(i.ID, status, actor)
		data = map[string]any{"issueUpdate": map[string]bool{"success": true}}
	case "CommentOnTicket":
		i, err := find()
		if err != nil {
			graphQLError(w, err.Error())
			return
		}
		// A comment changes the issue's updatedAt too.
		l.addComment(i, str("body"), By{User: &actor}, true)
		data = map[string]any{"commentCreate": map[string]bool{"success": true}}
	case "TicketComments":
		i, err := find()
		if err != nil {
			graphQLError(w, err.Error())
			return
		}
		nodes := []map[string]any{}
		for _, c := range l.comments {
			if c.issue == i.ID {
				nodes = append(nodes, c.json())
			}
		}
		data = map[string]any{"issue": map[string]any{"team": map[string]string{"id": l.team(i.Team).ID},
			"comments": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""}}}}
	default:
		graphQLError(w, "the fake has no operation "+req.OperationName)
		return
	}
	answer(w, http.StatusOK, map[string]any{"data": data})
}
