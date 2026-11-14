package signin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/solidtrace/cli/credentials"
)

// ExpiryMargin is how long before its expiry an access token is refreshed,
// so that it does not expire on the way to the server.
const ExpiryMargin = time.Minute

// refused are the token endpoint's errors that end a sign-in. Others (a
// rate limit, a server error) do not: the refresh token stays.
var refused = map[string]bool{"invalid_grant": true, "invalid_client": true, "unauthorized_client": true}

// Source gives the access token of the stored sign-in, for the calls of
// the agent ingest API and the Dispatcher.
//
// A refresh token works once, and Console revokes the whole Grant when a
// used one comes again. So each refresh holds the credentials lock, reads
// the file again under it, and refreshes only when no other process did
// it first. The new pair replaces the old one in one step.
type Source struct {
	Store  credentials.Store
	Client *Client
}

// Credentials reads the stored sign-in.
func (s *Source) Credentials() (credentials.Credentials, error) {
	c, err := s.Store.Load()
	if err != nil {
		return c, err
	}
	if !c.SignedIn() {
		return c, fmt.Errorf("not signed in to %s: %w", c.InstanceURL, ErrSignInAgain)
	}
	return c, nil
}

// Token returns a live access token, refreshed first when it expires.
func (s *Source) Token(ctx context.Context) (string, error) {
	c, err := s.Credentials()
	if err != nil {
		return "", err
	}
	if s.live(c) {
		return c.AccessToken, nil
	}
	return s.Refreshed(ctx, c.AccessToken)
}

// Refreshed returns an access token in place of rejected, which the server
// refused or which expired. When another process has already put a new one
// in the file, it returns that one, with no refresh.
func (s *Source) Refreshed(ctx context.Context, rejected string) (string, error) {
	unlock, err := s.Store.Lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()

	c, err := s.Credentials()
	if err != nil {
		return "", err
	}
	if c.AccessToken != rejected && s.live(c) {
		return c.AccessToken, nil
	}
	tokens, err := s.Client.Refresh(ctx, c.TokenEndpoint, c.RefreshToken, c.Resource)
	var oauthErr *Error
	if errors.As(err, &oauthErr) && refused[oauthErr.Code] {
		// The refresh token is used, expired or revoked. Forget the tokens,
		// so that no later call sends them again.
		c.AccessToken, c.RefreshToken, c.AccessTokenExpiresAt = "", "", time.Time{}
		if saveErr := s.Store.Save(c); saveErr != nil {
			return "", saveErr
		}
		return "", fmt.Errorf("%s refused the sign-in (%v): %w", c.InstanceURL, err, ErrSignInAgain)
	}
	if err != nil {
		// No answer: the refresh token may be used or not. Keep it; Console
		// gives the same pair again for a repeat in the next 60 seconds.
		return "", fmt.Errorf("refresh the sign-in at %s: %w", c.InstanceURL, err)
	}
	c.AccessToken, c.RefreshToken, c.AccessTokenExpiresAt = tokens.AccessToken, tokens.RefreshToken, tokens.ExpiresAt
	if tokens.Scope != "" {
		c.Scope = tokens.Scope
	}
	if err := s.Store.Save(c); err != nil {
		return "", fmt.Errorf("keep the new tokens in %s: %v: %w", s.Store.Path(), err, ErrSignInAgain)
	}
	return c.AccessToken, nil
}

func (s *Source) live(c credentials.Credentials) bool {
	return c.AccessToken != "" && s.Client.Now().Add(ExpiryMargin).Before(c.AccessTokenExpiresAt)
}
