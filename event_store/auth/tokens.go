package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The prefixes of the secrets that Console makes (ApiToken::PREFIX,
// Oauth::Token::ACCESS_PREFIX). "sto_" does not start with "st_".
const (
	apiTokenPrefix    = "st_"
	accessTokenPrefix = "sto_"
)

// FactoryResourcePath is where the factory's APIs are under the issuer, as
// Console's Oauth::Scopes::FACTORY_PATH gives it: the resource of an OAuth
// access token for the agent ingest API.
const FactoryResourcePath = "/factory"

// scopeResourcePaths is the resource path of each Scope that Console's
// registry (Oauth::Scopes) has. A Grant's Scope that is not here, or that
// is for another resource than its access token, does not reach a call.
var scopeResourcePaths = map[string]string{
	"mcp":               "/mcp",
	"mcp:read":          "/mcp",
	"sessions:write":    FactoryResourcePath,
	"sessions:read":     FactoryResourcePath,
	"sessions:download": FactoryResourcePath,
	"run_events:write":  FactoryResourcePath,
	"factory":           FactoryResourcePath,
	"issues:comment":    FactoryResourcePath,
	"tokens:create":     FactoryResourcePath,
}

// Token is a live token that a client sent: an API token or an OAuth access
// token, with what it can do. It is the same answer as Console's
// BearerToken gives for the agent ingest API.
type Token struct {
	// Kind is "api_token" or "access_token".
	Kind string
	// ID is the token's row in api_tokens or oauth_tokens.
	ID     int64
	UserID int64
	// Organization is the one Organization the token acts in. It is zero
	// only for an API token with no Scope, which acts over the MCP server
	// alone.
	Organization Organization
	// Scopes are the names of the Scopes the token acts with: mcp for an
	// API token with none.
	Scopes []string
	// AgentSessionIDs is the list of Agent sessions that each call of the
	// token that names an Agent session is limited to; nil is no list.
	AgentSessionIDs []string
	// ExpiresAt is nil for an API token that does not expire.
	ExpiresAt *time.Time
	// TwoFactorMissing is true when the Organization requires Two-factor and
	// the user has none. Console refuses the token in that Organization.
	TwoFactorMissing bool
}

// Organization is the Organization a token acts in.
type Organization struct {
	ID   int64
	Name string
	Slug string
}

// Has reports whether the token has the Scope name.
func (t Token) Has(name string) bool {
	for _, scope := range t.Scopes {
		if scope == name {
			return true
		}
	}
	return false
}

// Tokens checks the tokens that Console makes, from Console's database,
// which it opens read-only, as ProjectAuth reads Project keys (ADR 0001).
// It applies Console's rules (ApiToken#live?, Oauth::Token.find_live_access,
// Oauth::Grant#live?): a token works when its row exists and is not revoked,
// it is not expired, a Grant above it is not revoked, a child token's parent
// exists and is not expired, and its user is still a member of its
// Organization. A child has its parent's user and Organization, so the
// membership check holds for both.
type Tokens struct {
	db *sql.DB
}

func NewTokens(sqlitePath string) (*Tokens, error) {
	db, err := sql.Open("sqlite3", sqlitePath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	return &Tokens{db: db}, nil
}

func (t *Tokens) Close() error {
	return t.db.Close()
}

// Check returns the live token whose secret this is, at now. ok is false for
// a secret that is unknown, revoked or expired, under a revoked Grant, or
// whose user is not a member of its Organization. An error is a failed read.
func (t *Tokens) Check(secret string, now time.Time) (token Token, ok bool, err error) {
	switch {
	case strings.HasPrefix(secret, accessTokenPrefix):
		return t.checkAccessToken(secret, now)
	case strings.HasPrefix(secret, apiTokenPrefix):
		return t.checkAPIToken(secret, now)
	}
	return Token{}, false, nil
}

// railsTime reads a datetime as Rails writes it to SQLite, in UTC
// ("2026-11-14 10:00:00.123456"). The columns are datetime(6), which the
// driver does not turn into a time.Time. A value it cannot read is an
// error, so the token is refused.
func railsTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00"} {
		if at, err := time.ParseInLocation(layout, value.String, time.UTC); err == nil {
			return &at, nil
		}
	}
	return nil, fmt.Errorf("%q is not a datetime", value.String)
}

func digest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (t *Tokens) checkAPIToken(secret string, now time.Time) (Token, bool, error) {
	var (
		organizationID    sql.NullInt64
		scopesJSON        string
		sessionIDsJSON    sql.NullString
		expiresText       sql.NullString
		parentID          sql.NullInt64
		parentFound       sql.NullInt64
		parentExpiresText sql.NullString
	)
	token := Token{Kind: "api_token"}
	// A child token (parent_id) works only while its parent, the
	// Dispatcher's token, works (ApiToken#live?). Console deletes the
	// children with the parent; this check does not count on that.
	err := t.db.QueryRow(`
		SELECT t.id, t.user_id, t.organization_id, t.scopes, t.agent_session_ids, t.expires_at, t.parent_id, p.id, p.expires_at
		FROM api_tokens t LEFT JOIN api_tokens p ON p.id = t.parent_id
		WHERE t.token_digest = ?`,
		digest(secret),
	).Scan(&token.ID, &token.UserID, &organizationID, &scopesJSON, &sessionIDsJSON, &expiresText,
		&parentID, &parentFound, &parentExpiresText)
	if err == sql.ErrNoRows {
		return Token{}, false, nil
	}
	if err != nil {
		return Token{}, false, err
	}
	expiresAt, err := railsTime(expiresText)
	if err != nil {
		return Token{}, false, fmt.Errorf("expiry of API token %d: %w", token.ID, err)
	}
	if expiresAt != nil && !expiresAt.After(now) {
		return Token{}, false, nil
	}
	if parentID.Valid {
		if !parentFound.Valid {
			return Token{}, false, nil
		}
		parentExpiresAt, err := railsTime(parentExpiresText)
		if err != nil {
			return Token{}, false, fmt.Errorf("expiry of the parent of API token %d: %w", token.ID, err)
		}
		if parentExpiresAt != nil && !parentExpiresAt.After(now) {
			return Token{}, false, nil
		}
	}
	token.ExpiresAt = expiresAt
	if err := json.Unmarshal([]byte(scopesJSON), &token.Scopes); err != nil {
		return Token{}, false, fmt.Errorf("scopes of API token %d: %w", token.ID, err)
	}
	if sessionIDsJSON.Valid {
		if err := json.Unmarshal([]byte(sessionIDsJSON.String), &token.AgentSessionIDs); err != nil {
			return Token{}, false, fmt.Errorf("Agent session ids of API token %d: %w", token.ID, err)
		}
	}

	if !organizationID.Valid {
		// Console gives each token with Scopes one Organization. One with
		// none acts as mcp, over the MCP server only.
		if len(token.Scopes) > 0 {
			return Token{}, false, nil
		}
		token.Scopes = []string{"mcp"}
		return token, true, nil
	}
	return t.withMembership(token, organizationID.Int64)
}

func (t *Tokens) checkAccessToken(secret string, now time.Time) (Token, bool, error) {
	var (
		resource       sql.NullString
		expiresText    sql.NullString
		tokenRevoked   sql.NullString
		grantRevoked   sql.NullString
		organizationID int64
		scopesJSON     string
	)
	token := Token{Kind: "access_token"}
	err := t.db.QueryRow(`
		SELECT t.id, t.resource, t.expires_at, t.revoked_at, g.user_id, g.organization_id, g.scopes, g.revoked_at
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.oauth_grant_id
		WHERE t.token_digest = ? AND t.kind = 'access'`,
		digest(secret),
	).Scan(&token.ID, &resource, &expiresText, &tokenRevoked, &token.UserID, &organizationID, &scopesJSON, &grantRevoked)
	if err == sql.ErrNoRows {
		return Token{}, false, nil
	}
	if err != nil {
		return Token{}, false, err
	}
	// A revoke sets revoked_at on the token or its Grant; any value refuses.
	if tokenRevoked.Valid || grantRevoked.Valid {
		return Token{}, false, nil
	}
	expiresAt, err := railsTime(expiresText)
	if err != nil {
		return Token{}, false, fmt.Errorf("expiry of access token %d: %w", token.ID, err)
	}
	if expiresAt == nil || !expiresAt.After(now) {
		return Token{}, false, nil
	}
	token.ExpiresAt = expiresAt

	var granted []string
	if err := json.Unmarshal([]byte(scopesJSON), &granted); err != nil {
		return Token{}, false, fmt.Errorf("scopes of the Grant of access token %d: %w", token.ID, err)
	}
	// A token gets only its Grant's Scopes for its own resource (RFC 8707),
	// so a token for the MCP server cannot upload.
	path, ok := resourcePath(resource.String)
	if !ok {
		return Token{}, false, nil
	}
	token.Scopes = []string{}
	for _, name := range granted {
		if scopeResourcePaths[name] == path {
			token.Scopes = append(token.Scopes, name)
		}
	}
	return t.withMembership(token, organizationID)
}

// resourcePath is the path of an OAuth resource, with no trailing slash, as
// Oauth::Issuer.normalize_resource compares it. ok is false for a resource
// with no scheme or host.
func resourcePath(resource string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(resource))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	return strings.TrimRight(parsed.Path, "/"), true
}

// withMembership gives token its Organization when its user is a member of
// it now (a kept membership row); ok is false otherwise.
func (t *Tokens) withMembership(token Token, organizationID int64) (Token, bool, error) {
	var requireTwoFactor, twoFactor bool
	err := t.db.QueryRow(`
		SELECT o.id, COALESCE(o.name, ''), COALESCE(o.slug, ''), o.require_two_factor,
			u.authenticator_confirmed_at IS NOT NULL OR EXISTS (SELECT 1 FROM passkeys p WHERE p.user_id = u.id)
		FROM organizations_users m
		JOIN organizations o ON o.id = m.organization_id
		JOIN users u ON u.id = m.user_id
		WHERE m.user_id = ? AND m.organization_id = ? AND m.discarded_at IS NULL`,
		token.UserID, organizationID,
	).Scan(&token.Organization.ID, &token.Organization.Name, &token.Organization.Slug, &requireTwoFactor, &twoFactor)
	if err == sql.ErrNoRows {
		return Token{}, false, nil
	}
	if err != nil {
		return Token{}, false, err
	}
	token.TwoFactorMissing = requireTwoFactor && !twoFactor
	return token, true, nil
}
