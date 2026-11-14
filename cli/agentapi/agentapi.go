// Package agentapi is the client of SolidTrace's agent ingest API, on the
// ingest listener at /api/agent/v1. Each call sends the CLI's version in
// the User-Agent and the sign-in's access token. A 401 gets one refresh of
// the token and one more try. When the server's version header differs
// from the CLI's version, the client writes one warning line, once.
package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Path is where the agent ingest API is under the ingest URL.
const Path = "/api/agent/v1"

// VersionHeader is the server's version on each answer.
const VersionHeader = "SolidTrace-Version"

// Tokens gives the access token (signin.Source).
type Tokens interface {
	Token(ctx context.Context) (string, error)
	Refreshed(ctx context.Context, rejected string) (string, error)
}

// Client calls the agent ingest API at IngestURL.
type Client struct {
	IngestURL string
	HTTP      *http.Client
	Tokens    Tokens
	// Version is the CLI's version (version.Version).
	Version string
	// Warnings gets the version warning line.
	Warnings io.Writer

	warnOnce sync.Once
}

// Organization is the Organization that the token acts in.
type Organization struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// CapturedRepositories is the Agent Project's list of the token's
// Organization: the repositories whose Agent sessions upload.
type CapturedRepositories struct {
	Organization Organization `json:"organization"`
	Repositories []string     `json:"captured_repositories"`
}

// Error is an answer that is not a success.
type Error struct {
	Status  int
	Message string
	// MinimumVersion is the oldest CLI version that the server accepts,
	// on a 426.
	MinimumVersion string
}

func (e *Error) Error() string {
	if e.Status == http.StatusUpgradeRequired && e.MinimumVersion != "" {
		return fmt.Sprintf("the server refuses this solidtrace version: install %s or later (%s)", e.MinimumVersion, e.Message)
	}
	if e.Status == http.StatusUnauthorized {
		return fmt.Sprintf("the agent ingest API refused the sign-in (%s): run 'solidtrace login' again", e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("the agent ingest API answered %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("the agent ingest API answered %d", e.Status)
}

// CapturedRepositories gets the list.
func (c *Client) CapturedRepositories(ctx context.Context) (CapturedRepositories, error) {
	var list CapturedRepositories
	err := c.getJSON(ctx, "/captured-repositories", &list)
	if list.Repositories == nil {
		list.Repositories = []string{}
	}
	return list, err
}

func (c *Client) getJSON(ctx context.Context, path string, into any) error {
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return err
	}
	resp, err := c.get(ctx, path, token)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if token, err = c.Tokens.Refreshed(ctx, token); err != nil {
			return err
		}
		if resp, err = c.get(ctx, path, token); err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var answer struct {
			Error          string `json:"error"`
			MinimumVersion string `json:"minimum_version"`
		}
		_ = json.Unmarshal(body, &answer)
		return &Error{Status: resp.StatusCode, Message: answer.Error, MinimumVersion: answer.MinimumVersion}
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("read the answer of %s: %w", path, err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path, token string) (*http.Response, error) {
	if c.IngestURL == "" {
		return nil, errors.New("no ingest URL: run 'solidtrace login' again")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.IngestURL, "/")+Path+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "solidtrace/"+c.Version)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach the agent ingest API at %s: %w", c.IngestURL, err)
	}
	c.checkVersion(resp.Header.Get(VersionHeader))
	return resp, nil
}

// checkVersion writes one warning line, once, when the server's version
// differs from the CLI's. No header gives no warning.
func (c *Client) checkVersion(server string) {
	if server == "" || c.Warnings == nil || normal(server) == normal(c.Version) {
		return
	}
	c.warnOnce.Do(func() {
		fmt.Fprintf(c.Warnings, "Warning: this solidtrace is %s, and the server is %s. Install the server's version "+
			"(Console's capture setup page gives the command).\n", c.Version, server)
	})
}

func normal(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}
