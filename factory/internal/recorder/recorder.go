// Package recorder is the SolidTrace of Seam 3: a thin HTTP server that
// answers token checks (POST /oauth/introspect) with the answers that a
// test gives it, and keeps each call that the factory sends. It has no
// rules of its own: Seam 5 tests each real call to SolidTrace.
package recorder

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// Organization is one Organization in a token check's answer.
type Organization struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Answer is the answer to a check of one active token, in the shape of
// SolidTrace's introspection.
type Answer struct {
	Sub           string         `json:"sub"`
	Username      string         `json:"username"`
	Scope         string         `json:"scope"`
	Exp           int64          `json:"exp,omitempty"`
	Organizations []Organization `json:"organizations"`
}

// Call is one request that the factory sent.
type Call struct {
	Method        string
	Path          string
	Authorization string
	// Form is the request's form, for a form body.
	Form url.Values
	Body []byte
}

// Recorder is the server. CallerToken is the Dispatcher's own API token:
// a token check with another caller, or with a caller that is not active
// (see Token and Revoke), gets 401, as SolidTrace gives.
type Recorder struct {
	CallerToken string
	server      *httptest.Server

	mu       sync.Mutex
	answers  map[string]Answer
	calls    []Call
	projects map[string][]Project
	// runEvents are the run events that the recorder accepted, in the
	// order they came; eventStoreDown makes it refuse them.
	runEvents      []map[string]any
	eventStoreDown bool

	refuseChildren int
	// hangIngest, while it is not nil, holds each agent ingest call until
	// it is closed.
	hangIngest chan struct{}
	// refusePaths are the Agent session paths whose uploads get 400.
	refusePaths map[string]bool
	children    []*ChildToken
	uploads     []Upload
	files       map[string]map[string]*storedFile
}

// RunEventsPath is EventStore's route for the Dispatcher's run events.
const RunEventsPath = "/api/agent/v1/run-events"

// Project is one Project in the answer of the MCP server's list_projects.
type Project struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// New starts a recorder that accepts token checks from callerToken.
func New(callerToken string) *Recorder {
	r := &Recorder{CallerToken: callerToken, answers: map[string]Answer{}, projects: map[string][]Project{},
		files: map[string]map[string]*storedFile{}}
	r.server = httptest.NewServer(http.HandlerFunc(r.serve))
	return r
}

// URL is SolidTrace's URL.
func (r *Recorder) URL() string { return r.server.URL }

// Close stops the server.
func (r *Recorder) Close() { r.server.Close() }

// Token makes token active, with answer as its check's answer.
func (r *Recorder) Token(token string, answer Answer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers[token] = answer
}

// Revoke makes token not active.
func (r *Recorder) Revoke(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.answers, token)
}

// SetProject puts project in the Organization with slug organization, in
// place of the Project with the same id (a change of slug) if there is one.
func (r *Recorder) SetProject(organization string, project Project) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeProject(organization, project.ID)
	r.projects[organization] = append(r.projects[organization], project)
}

// DeleteProject deletes the Project with id from the Organization.
func (r *Recorder) DeleteProject(organization string, id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeProject(organization, id)
}

func (r *Recorder) removeProject(organization string, id int64) {
	kept := r.projects[organization][:0:0]
	for _, p := range r.projects[organization] {
		if p.ID != id {
			kept = append(kept, p)
		}
	}
	r.projects[organization] = kept
}

// EventStoreDown makes the run events route answer 503 (down true), as
// SolidTrace does while EventStore is down, or accept them again. Token
// checks still work: Console is a separate process.
func (r *Recorder) EventStoreDown(down bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventStoreDown = down
}

// HangIngest makes each agent ingest call wait with no answer (hang
// true), as a SolidTrace that does not answer does, or answers again.
func (r *Recorder) HangIngest(hang bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case hang && r.hangIngest == nil:
		r.hangIngest = make(chan struct{})
	case !hang && r.hangIngest != nil:
		close(r.hangIngest)
		r.hangIngest = nil
	}
}

// RunEvents are the Logs that the run events route accepted, oldest
// first: the Dispatcher's run events and its warnings. A repeat that the
// Dispatcher sent again is there two times.
func (r *Recorder) RunEvents() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.runEvents...)
}

// Calls are the requests that the factory sent, oldest first.
func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Call(nil), r.calls...)
}

// CallsTo are the calls to path.
func (r *Recorder) CallsTo(path string) []Call {
	var found []Call
	for _, c := range r.Calls() {
		if c.Path == path {
			found = append(found, c)
		}
	}
	return found
}

func (r *Recorder) serve(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(req.Body, 10<<20))
	call := Call{Method: req.Method, Path: req.URL.Path, Authorization: req.Header.Get("Authorization"), Body: body}
	if form, err := url.ParseQuery(string(body)); err == nil {
		call.Form = form
	}
	r.mu.Lock()
	r.calls = append(r.calls, call)
	answer, active := r.answers[call.Form.Get("token")]
	_, callerActive := r.answers[r.CallerToken]
	r.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if req.Method == http.MethodPost && req.URL.Path == "/oauth/token" {
		// The recorder gives no new tokens: a refresh (after a 401) ends
		// the CLI's sign-in, as SolidTrace does for a revoked token.
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	if req.Method == http.MethodPost && req.URL.Path == RunEventsPath {
		r.serveRunEvents(w, call)
		return
	}
	if req.URL.Path == ChildTokensPath || strings.HasPrefix(req.URL.Path, ChildTokensPath+"/") {
		r.serveChildTokens(w, req, call)
		return
	}
	if strings.HasPrefix(req.URL.Path, AgentSessionsPath) {
		r.mu.Lock()
		hang := r.hangIngest
		r.mu.Unlock()
		if hang != nil {
			// The call gets no answer while the ingest hangs.
			select {
			case <-hang:
			case <-req.Context().Done():
				return
			}
		}
		r.serveAgentSessions(w, req, call)
		return
	}
	if req.Method == http.MethodPost && req.URL.Path == "/mcp" {
		r.serveMCP(w, call)
		return
	}
	if req.Method != http.MethodPost || req.URL.Path != "/oauth/introspect" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if call.Authorization != "Bearer "+r.CallerToken || !callerActive {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_token"})
		return
	}
	if !active {
		_ = json.NewEncoder(w).Encode(map[string]bool{"active": false})
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		Active bool `json:"active"`
		Answer
	}{true, answer})
}

// serveMCP answers the MCP server's tools/call of list_projects with one
// JSON answer, as SolidTrace's stateless MCP server does. Only an active
// token with mcp or mcp:read gets an answer, and only for one of its
// Organizations.
func (r *Recorder) serveMCP(w http.ResponseWriter, call Call) {
	token, _ := strings.CutPrefix(call.Authorization, "Bearer ")
	r.mu.Lock()
	answer, active := r.answers[token]
	r.mu.Unlock()
	if token == "" || !active {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Send an API token as Authorization: Bearer st_…."})
		return
	}
	scopes := strings.Fields(answer.Scope)
	if !slices.Contains(scopes, "mcp:read") && !slices.Contains(scopes, "mcp") {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "The MCP server needs the mcp or mcp:read scope."})
		return
	}
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(call.Body, &request); err != nil || request.Method != "tools/call" ||
		request.Params.Name != "list_projects" {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID,
			"error": map[string]any{"code": -32601, "message": "the recorder answers only tools/call list_projects"}})
		return
	}
	slug := request.Params.Arguments["organization"]
	known := slices.ContainsFunc(answer.Organizations, func(o Organization) bool { return o.Slug == slug })
	text, isError := "No Organization "+slug+" for this token.", true
	if known {
		r.mu.Lock()
		projects := append([]Project{}, r.projects[slug]...)
		r.mu.Unlock()
		body, _ := json.Marshal(map[string]any{"organization": slug, "projects": projects})
		text, isError = string(body)+"\nConsole: "+r.URL()+"/"+slug+"/projects", false
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}}, "isError": isError}})
}

// serveRunEvents takes a batch of run events (a JSON array of Logs) from
// the Dispatcher's active token, and answers with EventStore's counts.
func (r *Recorder) serveRunEvents(w http.ResponseWriter, call Call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.eventStoreDown {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "the recorder's EventStore is down"})
		return
	}
	_, active := r.answers[r.CallerToken]
	if call.Authorization != "Bearer "+r.CallerToken || !active {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_token"})
		return
	}
	var logs []map[string]any
	if err := json.Unmarshal(call.Body, &logs); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "the recorder takes a JSON array of Logs"})
		return
	}
	r.runEvents = append(r.runEvents, logs...)
	_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(logs), "dropped": 0, "truncated": 0})
}
