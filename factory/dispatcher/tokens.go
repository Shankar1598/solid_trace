package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// person is who a token acts as, from SolidTrace's token check.
type person struct {
	id, name     string
	organization organization
}

type organization struct {
	id, slug string
}

// tokenCheck asks SolidTrace about a person's token, with the
// Dispatcher's own token (RFC 7662 introspection).
type tokenCheck struct {
	url   string
	token string
	http  *http.Client
}

// refusal is a token that the Dispatcher does not accept: status is 401 or
// 403.
type refusal struct {
	status  int
	message string
}

func (r *refusal) Error() string { return r.message }

// check returns the person of token. A token that is not active is a
// *refusal; a token check that fails is another error.
func (c *tokenCheck) check(ctx context.Context, token string) (person, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/oauth/introspect",
		strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return person{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return person{}, fmt.Errorf("check the token at SolidTrace: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return person{}, fmt.Errorf("check the token at SolidTrace: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return person{}, fmt.Errorf("check the token at SolidTrace: SolidTrace answered %d", resp.StatusCode)
	}
	var answer struct {
		Active        bool   `json:"active"`
		Sub           string `json:"sub"`
		Username      string `json:"username"`
		Organizations []struct {
			ID   json.Number `json:"id"`
			Slug string      `json:"slug"`
		} `json:"organizations"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return person{}, fmt.Errorf("check the token at SolidTrace: %w", err)
	}
	if !answer.Active {
		return person{}, &refusal{http.StatusUnauthorized, "the token is not active: run 'solidtrace login' again"}
	}
	if len(answer.Organizations) != 1 {
		return person{}, &refusal{http.StatusForbidden, "the token must act in exactly one Organization"}
	}
	org := answer.Organizations[0]
	return person{
		id:           answer.Sub,
		name:         answer.Username,
		organization: organization{id: org.ID.String(), slug: org.Slug},
	}, nil
}
