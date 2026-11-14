// Package fake has small fakes of SolidTrace for the CLI's tests: Console's
// OAuth server for the solidtrace-cli client, and the agent ingest API of
// EventStore. They follow the rules of the real servers that the CLI must
// see: one Organization for each Grant, PKCE S256, iss in the redirect,
// refresh tokens that work once, and a reused refresh token that revokes
// the Grant. They have no other rules.
package fake

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClientID is the first-party client of the CLI.
const ClientID = "solidtrace-cli"

// AccessTokenLasts is how long an access token works, as in Console.
const AccessTokenLasts = time.Hour

// Organization is one Organization of the signed-in user, with the
// captured repositories of its Agent Project.
type Organization struct {
	Name         string
	Slug         string
	Repositories []string
}

// AuthorizeRequest is what the CLI sent to the approval page.
type AuthorizeRequest struct {
	Query url.Values
}

type grant struct {
	org     string
	revoked bool
}

type accessToken struct {
	grant   *grant
	expires time.Time
}

type refreshToken struct {
	grant    *grant
	resource string
	used     bool
}

type authCode struct {
	grant     *grant
	challenge string
	redirect  string
	resource  string
	used      bool
}

type deviceCode struct {
	userCode string
	resource string
	grant    *grant
	denied   bool
	expires  time.Time
	polls    int
}

// OAuth is a fake of Console's OAuth server.
type OAuth struct {
	Server *httptest.Server

	mu sync.Mutex
	// Now is the server's clock. Tests move it.
	Now func() time.Time
	// Organizations are the user's Organizations, by slug.
	Organizations map[string]*Organization
	// Pick is the slug of the Organization that the person picks on the
	// approval page or the device page.
	Pick string
	// Deny makes the person deny on the approval page or the device page.
	Deny bool
	// Iss is the iss that the approval page sends back; "" sends the issuer.
	Iss string
	// ApproveAfterPolls is how many polls the device code answers
	// authorization_pending before the person approves.
	ApproveAfterPolls int
	// SlowDownAt is the poll number (from 1) that answers slow_down.
	SlowDownAt int
	// DeviceExpiresIn is the device code's expires_in in seconds (600).
	DeviceExpiresIn int
	// RefreshDown makes the token endpoint answer 503 to a refresh.
	RefreshDown bool

	Authorizations   []AuthorizeRequest
	DeviceRequests   []url.Values
	TokenRequests    []url.Values
	RefreshRequests  int
	SentRefreshToken []string
	Polls            []time.Time

	grants  []*grant
	access  map[string]*accessToken
	refresh map[string]*refreshToken
	codes   map[string]*authCode
	devices map[string]*deviceCode
}

// NewOAuth starts a fake OAuth server. Close it with Close.
func NewOAuth(orgs ...Organization) *OAuth {
	f := &OAuth{
		Now:             time.Now,
		Organizations:   map[string]*Organization{},
		DeviceExpiresIn: 600,
		access:          map[string]*accessToken{},
		refresh:         map[string]*refreshToken{},
		codes:           map[string]*authCode{},
		devices:         map[string]*deviceCode{},
	}
	for i := range orgs {
		org := orgs[i]
		f.Organizations[org.Slug] = &org
		if f.Pick == "" {
			f.Pick = org.Slug
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", f.metadata)
	mux.HandleFunc("GET /oauth/authorize", f.authorize)
	mux.HandleFunc("POST /oauth/token", f.token)
	mux.HandleFunc("POST /oauth/device_authorization", f.deviceAuthorization)
	f.Server = httptest.NewServer(mux)
	return f
}

// Close stops the server.
func (f *OAuth) Close() { f.Server.Close() }

// URL is the issuer.
func (f *OAuth) URL() string { return f.Server.URL }

// Resource is the resource of the factory's Scopes.
func (f *OAuth) Resource() string { return f.URL() + "/factory" }

// Lock and Unlock let a test change the fake's fields while the CLI runs.
func (f *OAuth) Lock()   { f.mu.Lock() }
func (f *OAuth) Unlock() { f.mu.Unlock() }

// Check answers which Organization an access token acts in, as EventStore
// checks it: ok is false for an unknown, expired or revoked token.
func (f *OAuth) Check(secret string) (*Organization, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	token := f.access[secret]
	if token == nil || token.grant.revoked || !f.Now().Before(token.expires) {
		return nil, false
	}
	return f.Organizations[token.grant.org], true
}

// RevokeGrants revokes every Grant, as Revoke on Settings → User does.
func (f *OAuth) RevokeGrants() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.grants {
		g.revoked = true
	}
}

// ExpireAccessTokens makes each access token expired at once.
func (f *OAuth) ExpireAccessTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, token := range f.access {
		token.expires = f.Now()
	}
}

// GrantRevoked is true when some Grant is revoked.
func (f *OAuth) GrantRevoked() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.grants {
		if g.revoked {
			return true
		}
	}
	return false
}

// DeviceCodes lists the user codes that the server gave.
func (f *OAuth) DeviceCodes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var codes []string
	for _, d := range f.devices {
		codes = append(codes, d.userCode)
	}
	return codes
}

func (f *OAuth) metadata(w http.ResponseWriter, r *http.Request) {
	issuer := f.URL()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         issuer,
		"authorization_endpoint":                         issuer + "/oauth/authorize",
		"token_endpoint":                                 issuer + "/oauth/token",
		"revocation_endpoint":                            issuer + "/oauth/revoke",
		"device_authorization_endpoint":                  issuer + "/oauth/device_authorization",
		"code_challenge_methods_supported":               []string{"S256"},
		"authorization_response_iss_parameter_supported": true,
	})
}

// authorize is the approval page, with the person's choice made at once:
// it redirects to the client's loopback redirect with the code.
func (f *OAuth) authorize(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	f.Authorizations = append(f.Authorizations, AuthorizeRequest{Query: q})
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get("client_id") != ClientID || redirect.Hostname() != "127.0.0.1" || redirect.Path != "/callback" {
		http.Error(w, "bad client or redirect", http.StatusBadRequest)
		return
	}
	answer := url.Values{}
	if state := q.Get("state"); state != "" {
		answer.Set("state", state)
	}
	iss := f.Iss
	if iss == "" {
		iss = f.URL()
	}
	answer.Set("iss", iss)
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("response_type") != "code" {
		answer.Set("error", "invalid_request")
	} else if f.Deny {
		answer.Set("error", "access_denied")
	} else {
		g := &grant{org: f.Pick}
		f.grants = append(f.grants, g)
		code := random()
		f.codes[code] = &authCode{grant: g, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"),
			resource: q.Get("resource")}
		answer.Set("code", code)
	}
	redirect.RawQuery = answer.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (f *OAuth) deviceAuthorization(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = r.ParseForm()
	f.DeviceRequests = append(f.DeviceRequests, r.PostForm)
	if r.PostForm.Get("client_id") != ClientID {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	code := random()
	userCode := fmt.Sprintf("WDJB-%04d", len(f.devices)+1)
	f.devices[code] = &deviceCode{userCode: userCode, resource: r.PostForm.Get("resource"),
		expires: f.Now().Add(time.Duration(f.DeviceExpiresIn) * time.Second)}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code":      code,
		"user_code":        userCode,
		"verification_uri": f.URL() + "/device",
		"expires_in":       f.DeviceExpiresIn,
		"interval":         5,
	})
}

func (f *OAuth) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = r.ParseForm()
	form := r.PostForm
	f.TokenRequests = append(f.TokenRequests, form)
	if form.Get("client_id") != ClientID {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		code := f.codes[form.Get("code")]
		if code == nil {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		if code.used {
			code.grant.revoked = true
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		sum := sha256.Sum256([]byte(form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != code.challenge || form.Get("redirect_uri") != code.redirect ||
			(form.Get("resource") != "" && form.Get("resource") != code.resource) {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		code.used = true
		f.issue(w, code.grant, code.resource)
	case "refresh_token":
		f.RefreshRequests++
		f.SentRefreshToken = append(f.SentRefreshToken, form.Get("refresh_token"))
		if f.RefreshDown {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		token := f.refresh[form.Get("refresh_token")]
		if token == nil || token.grant.revoked {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		if token.used {
			// A copy is in other hands: Console revokes the Grant.
			token.grant.revoked = true
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		token.used = true
		f.issue(w, token.grant, token.resource)
	case "urn:ietf:params:oauth:grant-type:device_code":
		f.Polls = append(f.Polls, f.Now())
		device := f.devices[form.Get("device_code")]
		if device == nil {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		device.polls++
		switch {
		case !f.Now().Before(device.expires):
			oauthError(w, http.StatusBadRequest, "expired_token")
		case f.SlowDownAt == device.polls:
			oauthError(w, http.StatusBadRequest, "slow_down")
		case device.polls <= f.ApproveAfterPolls:
			oauthError(w, http.StatusBadRequest, "authorization_pending")
		case f.Deny:
			oauthError(w, http.StatusBadRequest, "access_denied")
		case device.grant != nil:
			oauthError(w, http.StatusBadRequest, "invalid_grant")
		default:
			device.grant = &grant{org: f.Pick}
			f.grants = append(f.grants, device.grant)
			f.issue(w, device.grant, device.resource)
		}
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func (f *OAuth) issue(w http.ResponseWriter, g *grant, resource string) {
	access, refresh := "sto_"+random(), "str_"+random()
	f.access[access] = &accessToken{grant: g, expires: f.Now().Add(AccessTokenLasts)}
	f.refresh[refresh] = &refreshToken{grant: g, resource: resource}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(AccessTokenLasts / time.Second),
		"refresh_token": refresh,
		"scope":         "sessions:write factory",
	})
}

// AgentAPI is a fake of the agent ingest API.
type AgentAPI struct {
	Server *httptest.Server
	OAuth  *OAuth

	mu sync.Mutex
	// Version is the server's version header; "" sends none.
	Version string
	// MinimumVersion refuses an older solidtrace client with 426.
	MinimumVersion string
	// RefuseTokens answers 401 to each token.
	RefuseTokens bool
	// Calls counts the calls of the captured repositories list.
	Calls      int
	UserAgents []string
}

// NewAgentAPI starts a fake agent ingest API that checks tokens with oauth.
func NewAgentAPI(oauth *OAuth) *AgentAPI {
	f := &AgentAPI{OAuth: oauth}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agent/v1/captured-repositories", f.capturedRepositories)
	f.Server = httptest.NewServer(mux)
	return f
}

// Close stops the server.
func (f *AgentAPI) Close() { f.Server.Close() }

// URL is the ingest URL.
func (f *AgentAPI) URL() string { return f.Server.URL }

// Set changes the fake's fields while the CLI runs.
func (f *AgentAPI) Set(change func(*AgentAPI)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func (f *AgentAPI) capturedRepositories(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.Calls++
	f.UserAgents = append(f.UserAgents, r.UserAgent())
	version, minimum, refuse := f.Version, f.MinimumVersion, f.RefuseTokens
	f.mu.Unlock()
	if version != "" {
		w.Header().Set("SolidTrace-Version", version)
	}
	if minimum != "" && olderThan(r.UserAgent(), minimum) {
		writeJSON(w, http.StatusUpgradeRequired, map[string]any{
			"error":           "This solidtrace is too old for this server. Install " + minimum + " or later.",
			"minimum_version": minimum,
		})
		return
	}
	secret, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Send a token."})
		return
	}
	org, ok := f.OAuth.Check(secret)
	if !ok || org == nil || refuse {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "The token is wrong, expired or revoked."})
		return
	}
	repositories := org.Repositories
	if repositories == nil {
		repositories = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"organization":          map[string]any{"name": org.Name, "slug": org.Slug},
		"captured_repositories": repositories,
	})
}

// olderThan compares solidtrace/<version> with minimum (major.minor.patch).
func olderThan(userAgent, minimum string) bool {
	client, ok := strings.CutPrefix(strings.Fields(userAgent + " ")[0], "solidtrace/")
	if !ok {
		return false
	}
	a, okA := parts(client)
	b, okB := parts(minimum)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func parts(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "-")
	fields := strings.Split(v, ".")
	if len(fields) != 3 {
		return out, false
	}
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code, "error_description": "The fake server says " + code + "."})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
