package dispatcher

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// FactoryScope is the scope that a person's token needs for the
// Dispatcher's API.
const FactoryScope = "factory"

// mcpScope is the scope of a token that acts only over the MCP server.
// SolidTrace gives it for an API token made with no scope.
const mcpScope = "mcp"

// keepFor is the longest time that the Dispatcher keeps the answer of a
// token check: a revoked token, or a change of role, applies within it.
const keepFor = 60 * time.Second

// adminRole is the role, in SolidTrace's token check, that can do the
// Admin actions. Each other role is a member.
const adminRole = "admin"

// person is who a token acts as, from SolidTrace's token check.
type person struct {
	id, name     string
	organization organization
	// role is the person's role in the organization.
	role string
}

// admin is true for an Admin of the person's organization.
func (p person) admin() bool { return p.role == adminRole }

type organization struct {
	id, slug string
}

// tokenCheck asks SolidTrace about a person's token, with the
// Dispatcher's own token (RFC 7662 introspection). It keeps each accepted
// answer for at most keepFor, and never past the token's expiry. It keeps
// no refusal: a refused token is checked again on its next call.
type tokenCheck struct {
	url   string
	token string
	http  *http.Client
	now   func() time.Time

	mu   sync.Mutex
	kept map[[sha256.Size]byte]keptAnswer
}

type keptAnswer struct {
	person person
	until  time.Time
}

// keepLimit is the number of kept answers above which a check drops the
// answers that are out of date.
const keepLimit = 1000

// refusal is a token that the Dispatcher does not accept: status is 401 or
// 403.
type refusal struct {
	status  int
	message string
	// scope is set for a token without the factory scope.
	scope bool
}

func (r *refusal) Error() string { return r.message }

// errCallerRefused is SolidTrace's 401 to the Dispatcher's own token: its
// Admin left the Organization, or someone revoked it.
var errCallerRefused = errors.New("SolidTrace refuses the Dispatcher's own token")

// callerRefused is what a person and the status call see then.
const callerRefused = "the Dispatcher's SolidTrace token is not active: the Admin who made it may have left " +
	"the Organization, or someone revoked it. An Admin must make a new token for the Dispatcher " +
	"(DISPATCHER_SOLIDTRACE_TOKEN) and restart it"

// introspection is the part of SolidTrace's answer that the Dispatcher
// reads.
type introspection struct {
	Active        bool   `json:"active"`
	Sub           string `json:"sub"`
	Username      string `json:"username"`
	Scope         string `json:"scope"`
	Exp           int64  `json:"exp"`
	Organizations []struct {
		ID   json.Number `json:"id"`
		Slug string      `json:"slug"`
		Role string      `json:"role"`
	} `json:"organizations"`
}

// check returns the person of token. A token that the Dispatcher does not
// accept is a *refusal; errCallerRefused and other errors are a token
// check that failed.
func (c *tokenCheck) check(ctx context.Context, token string) (person, error) {
	key := sha256.Sum256([]byte(token))
	now := c.now()
	c.mu.Lock()
	kept, ok := c.kept[key]
	c.mu.Unlock()
	if ok && now.Before(kept.until) {
		return kept.person, nil
	}

	answer, err := c.introspect(ctx, token)
	if err != nil {
		return person{}, err
	}
	p, until, err := accept(answer, now)
	if err != nil {
		return person{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.kept) >= keepLimit {
		for k, a := range c.kept {
			if !now.Before(a.until) {
				delete(c.kept, k)
			}
		}
	}
	if len(c.kept) < keepLimit {
		c.kept[key] = keptAnswer{person: p, until: until}
	}
	return p, nil
}

// accept checks an answer at now, and gives its person and the time until
// which the Dispatcher can keep the answer.
func accept(answer introspection, now time.Time) (person, time.Time, error) {
	if !answer.Active {
		return person{}, time.Time{}, &refusal{status: http.StatusUnauthorized,
			message: "the token is not active: run 'solidtrace login' again"}
	}
	until := now.Add(keepFor)
	if answer.Exp != 0 {
		expiry := time.Unix(answer.Exp, 0)
		if !now.Before(expiry) {
			return person{}, time.Time{}, &refusal{status: http.StatusUnauthorized,
				message: "the token expired: run 'solidtrace login' again"}
		}
		if expiry.Before(until) {
			until = expiry
		}
	}
	scopes := strings.Fields(answer.Scope)
	// SolidTrace gives the scope mcp for an API token made with no scope.
	if len(scopes) == 0 || slices.Equal(scopes, []string{mcpScope}) {
		return person{}, time.Time{}, &refusal{status: http.StatusForbidden, scope: true,
			message: "the token acts only over the MCP server (a token made with no scope has only the mcp scope): " +
				"run 'solidtrace login' for a token with the factory scope"}
	}
	if !slices.Contains(scopes, FactoryScope) {
		return person{}, time.Time{}, &refusal{status: http.StatusForbidden, scope: true,
			message: "the token has no factory scope (it has " + strings.Join(scopes, " ") +
				"): run 'solidtrace login' for a token with the factory scope"}
	}
	if len(answer.Organizations) != 1 {
		return person{}, time.Time{}, &refusal{status: http.StatusForbidden,
			message: "the token must act in exactly one Organization"}
	}
	org := answer.Organizations[0]
	return person{
		id:           answer.Sub,
		name:         answer.Username,
		organization: organization{id: org.ID.String(), slug: org.Slug},
		role:         org.Role,
	}, until, nil
}

// introspect asks SolidTrace about token, with the Dispatcher's own token
// as the caller.
func (c *tokenCheck) introspect(ctx context.Context, token string) (introspection, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/oauth/introspect",
		strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return introspection{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return introspection{}, fmt.Errorf("check the token at SolidTrace: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return introspection{}, fmt.Errorf("check the token at SolidTrace: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return introspection{}, errCallerRefused
	}
	if resp.StatusCode != http.StatusOK {
		return introspection{}, fmt.Errorf("check the token at SolidTrace: SolidTrace answered %d", resp.StatusCode)
	}
	var answer introspection
	if err := json.Unmarshal(body, &answer); err != nil {
		return introspection{}, fmt.Errorf("check the token at SolidTrace: %w", err)
	}
	return answer, nil
}

// ownToken checks the Dispatcher's own token now, with no kept answer. It
// is nil when SolidTrace accepts it.
func (c *tokenCheck) ownToken(ctx context.Context) error {
	answer, err := c.introspect(ctx, c.token)
	if errors.Is(err, errCallerRefused) || (err == nil && !answer.Active) {
		return errors.New(callerRefused)
	}
	if err != nil {
		return fmt.Errorf("the Dispatcher could not reach SolidTrace at %s: %w", c.url, err)
	}
	return nil
}
