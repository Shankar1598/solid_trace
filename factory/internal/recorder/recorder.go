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

	mu      sync.Mutex
	answers map[string]Answer
	calls   []Call
}

// New starts a recorder that accepts token checks from callerToken.
func New(callerToken string) *Recorder {
	r := &Recorder{CallerToken: callerToken, answers: map[string]Answer{}}
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
	body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
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
