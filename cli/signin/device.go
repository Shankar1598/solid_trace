package signin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SlowDownStep is what slow_down adds to the poll interval (RFC 8628 §3.5).
const SlowDownStep = 5 * time.Second

// Device signs in with the device flow (RFC 8628): it asks for a code,
// prints the code and the URL of the device page to out, and polls the
// token endpoint until the person approves on another device, denies, or
// the code expires.
func (c *Client) Device(ctx context.Context, server Server, out io.Writer) (Tokens, error) {
	if server.DeviceAuthorizationEndpoint == "" {
		return Tokens{}, errors.New("the server has no device sign-in: use a machine with a browser")
	}
	form := url.Values{"client_id": {ClientID}, "scope": {Scope}, "resource": {server.Resource()}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.DeviceAuthorizationEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return Tokens{}, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return Tokens{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Tokens{}, oauthError(resp, body)
	}
	var answer struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int64  `json:"expires_in"`
		Interval        int64  `json:"interval"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.DeviceCode == "" || answer.UserCode == "" ||
		answer.VerificationURI == "" {
		return Tokens{}, errors.New("the device sign-in answer has no code or URL")
	}
	interval := time.Duration(answer.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := c.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	if answer.ExpiresIn <= 0 {
		deadline = c.Now().Add(10 * time.Minute)
	}

	fmt.Fprintf(out, "On any device, open %s and type this code:\n\n    %s\n\nType it only on that page. Waiting for your approval...\n",
		answer.VerificationURI, answer.UserCode)

	poll := url.Values{"grant_type": {DeviceGrantType}, "device_code": {answer.DeviceCode}, "client_id": {ClientID}}
	for {
		if err := c.Sleep(ctx, interval); err != nil {
			return Tokens{}, err
		}
		tokens, err := c.token(ctx, server.TokenEndpoint, poll)
		var oauthErr *Error
		switch {
		case err == nil:
			return tokens, nil
		case errors.As(err, &oauthErr) && oauthErr.Code == "authorization_pending":
		case errors.As(err, &oauthErr) && oauthErr.Code == "slow_down":
			interval += SlowDownStep
		case errors.As(err, &oauthErr) && oauthErr.Code == "access_denied":
			return Tokens{}, errors.New("you denied the sign-in on the device page")
		case errors.As(err, &oauthErr) && oauthErr.Code == "expired_token":
			return Tokens{}, errors.New("the code expired before the approval: run 'solidtrace login' again")
		default:
			return Tokens{}, err
		}
		if !c.Now().Before(deadline) {
			return Tokens{}, errors.New("the code expired before the approval: run 'solidtrace login' again")
		}
	}
}
