// Package signin signs the CLI in through SolidTrace OAuth as the
// first-party client solidtrace-cli: the browser grant with a loopback
// redirect and PKCE, or the device flow (RFC 8628) on a machine with no
// browser. It asks for the factory's Scopes, sessions:write and factory.
//
// Source gives a live access token from the stored sign-in, and refreshes
// it with the rotating refresh token when it expires.
package signin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ClientID is the first-party OAuth client of the CLI in Console.
	ClientID = "solidtrace-cli"
	// Scope is what `solidtrace login` asks for.
	Scope = "sessions:write factory"
	// ResourcePath is the resource of the factory's Scopes, under the
	// issuer: the agent ingest API and the Dispatcher.
	ResourcePath = "/factory"
	// DeviceGrantType is the grant type of the device flow's poll.
	DeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	// BrowserWait is how long the browser grant waits for the approval,
	// as long as Console keeps the approval request.
	BrowserWait = 10 * time.Minute
)

// ErrSignInAgain is in the error when the server refuses the sign-in: the
// refresh token is used, expired or revoked. Only a new sign-in helps.
var ErrSignInAgain = errors.New("the sign-in has ended: run 'solidtrace login' again")

// Client makes the OAuth requests.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	Now       func() time.Time
	// Sleep waits d, or less when ctx ends (the device flow's polls).
	Sleep func(ctx context.Context, d time.Duration) error
}

// Server is what the CLI reads from the authorization server metadata
// (RFC 8414).
type Server struct {
	Issuer                      string
	AuthorizationEndpoint       string
	TokenEndpoint               string
	DeviceAuthorizationEndpoint string
	// IssInResponse is true when the server sends iss to the redirect
	// (RFC 9207): the CLI then requires it.
	IssInResponse bool
}

// Resource is the resource that the CLI's tokens are for.
func (s Server) Resource() string {
	return s.Issuer + ResourcePath
}

// Tokens is the answer of the token endpoint.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	Scope        string
	ExpiresAt    time.Time
}

// Error is an OAuth error answer (RFC 6749 §5.2).
type Error struct {
	Code        string
	Description string
	Status      int
}

func (e *Error) Error() string {
	if e.Description != "" {
		return e.Code + ": " + e.Description
	}
	return e.Code
}

// CleanURL checks a Console URL and returns it with no trailing slash. The
// tokens go to it, so it must be https://, or http:// on a loopback host
// (development), with no path, as Console's issuer is.
func CleanURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("%q is not a Console URL: give its origin, such as https://solidtrace.example.com", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !loopback(u.Hostname()) {
			return "", fmt.Errorf("%s is not https://: the CLI sends tokens only over HTTPS, or to a loopback address", raw)
		}
	default:
		return "", fmt.Errorf("%q is not a Console URL: it must start with https://", raw)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Discover reads the authorization server metadata of the Console at
// instanceURL (from CleanURL). The metadata's issuer must be that URL
// (RFC 8414 §3.3).
func (c *Client) Discover(ctx context.Context, instanceURL string) (Server, error) {
	var meta struct {
		Issuer                      string   `json:"issuer"`
		AuthorizationEndpoint       string   `json:"authorization_endpoint"`
		TokenEndpoint               string   `json:"token_endpoint"`
		DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint"`
		IssInResponse               bool     `json:"authorization_response_iss_parameter_supported"`
		Methods                     []string `json:"code_challenge_methods_supported"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, instanceURL+"/.well-known/oauth-authorization-server", nil)
	if err != nil {
		return Server{}, err
	}
	resp, err := c.do(req)
	if err != nil {
		return Server{}, fmt.Errorf("reach %s: %w", instanceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Server{}, fmt.Errorf("%s has no OAuth sign-in: Console needs an HTTPS Console URL (Settings → Instance)", instanceURL)
	}
	if resp.StatusCode != http.StatusOK {
		return Server{}, fmt.Errorf("%s answered %s to the OAuth metadata request", instanceURL, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&meta); err != nil {
		return Server{}, fmt.Errorf("read the OAuth metadata of %s: %w", instanceURL, err)
	}
	if strings.TrimRight(meta.Issuer, "/") != instanceURL {
		return Server{}, fmt.Errorf("%s names another issuer (%q): use the Console URL that Console's Settings → Instance shows",
			instanceURL, meta.Issuer)
	}
	if !contains(meta.Methods, "S256") || meta.TokenEndpoint == "" {
		return Server{}, fmt.Errorf("%s does not give the sign-in that the CLI needs (PKCE S256 and a token endpoint)", instanceURL)
	}
	// The codes and tokens go only to the issuer that the person named.
	for _, endpoint := range []string{meta.AuthorizationEndpoint, meta.TokenEndpoint, meta.DeviceAuthorizationEndpoint} {
		if endpoint != "" && !strings.HasPrefix(endpoint, instanceURL+"/") {
			return Server{}, fmt.Errorf("%s names an OAuth endpoint at another address (%q): the sign-in stopped", instanceURL, endpoint)
		}
	}
	return Server{
		Issuer:                      instanceURL,
		AuthorizationEndpoint:       meta.AuthorizationEndpoint,
		TokenEndpoint:               meta.TokenEndpoint,
		DeviceAuthorizationEndpoint: meta.DeviceAuthorizationEndpoint,
		IssInResponse:               meta.IssInResponse,
	}, nil
}

// Refresh exchanges a refresh token for a new pair. The old refresh token
// works no more after this call, whatever its result.
func (c *Client) Refresh(ctx context.Context, tokenEndpoint, refreshToken, resource string) (Tokens, error) {
	return c.token(ctx, tokenEndpoint, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {ClientID},
		"resource":      {resource},
	})
}

// token posts form to the token endpoint and reads the pair.
func (c *Client) token(ctx context.Context, endpoint string, form url.Values) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return Tokens{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Tokens{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Tokens{}, oauthError(resp, body)
	}
	var answer struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return Tokens{}, fmt.Errorf("read the token answer: %w", err)
	}
	if answer.AccessToken == "" || answer.RefreshToken == "" || !strings.EqualFold(answer.TokenType, "Bearer") ||
		answer.ExpiresIn <= 0 {
		return Tokens{}, errors.New("the token answer has no Bearer access token, refresh token or expiry")
	}
	return Tokens{
		AccessToken:  answer.AccessToken,
		RefreshToken: answer.RefreshToken,
		Scope:        answer.Scope,
		ExpiresAt:    c.Now().Add(time.Duration(answer.ExpiresIn) * time.Second),
	}, nil
}

func oauthError(resp *http.Response, body []byte) error {
	var answer struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &answer) == nil && answer.Error != "" {
		return &Error{Code: answer.Error, Description: answer.Description, Status: resp.StatusCode}
	}
	return &StatusError{URL: resp.Request.URL.Redacted(), Status: resp.StatusCode, Text: resp.Status}
}

// StatusError is an answer of the token endpoint that is not a token and
// not an OAuth error.
type StatusError struct {
	URL    string
	Status int
	Text   string
}

func (e *StatusError) Error() string {
	return e.URL + " answered " + e.Text
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.UserAgent)
	return c.HTTP.Do(req)
}

// random returns n random bytes as base64url, with no padding.
func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
