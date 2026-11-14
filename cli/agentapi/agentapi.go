// Package agentapi is the client of SolidTrace's agent ingest API, on the
// ingest listener at /api/agent/v1. Each call sends the CLI's version in
// the User-Agent and the sign-in's access token. A 401 gets one refresh of
// the token and one more try. When the server's version header differs
// from the CLI's version, the client writes one warning line, once.
package agentapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// AgentSessionFile is what the server has of one path of an Agent
// session: its highest version, the end offset of that version and, for a
// file that is not .jsonl, its content hash.
type AgentSessionFile struct {
	Path        string `json:"path"`
	Version     uint32 `json:"version"`
	EndOffset   uint64 `json:"end_offset"`
	ContentHash string `json:"content_hash"`
}

// AgentSessionFiles asks what the server has of an Agent session.
func (c *Client) AgentSessionFiles(ctx context.Context, sessionID string) ([]AgentSessionFile, error) {
	var answer struct {
		Files []AgentSessionFile `json:"files"`
	}
	err := c.call(ctx, http.MethodGet, "/agent-sessions/"+url.PathEscape(sessionID)+"/files", nil, &answer)
	return answer.Files, err
}

// Unit is one upload of a file of an Agent session: Bytes from Offset of
// the Path's Version. Size is the size of the unit on disk when Bytes are
// only its first 32 MiB, else 0.
type Unit struct {
	Path        string
	Version     uint32
	Offset      uint64
	Size        uint64
	ContentHash string
	Harness     string
	Repository  string
	Folder      string
	Bytes       []byte
}

// Uploaded is the server's answer to an upload.
type Uploaded struct {
	Path      string `json:"path"`
	Version   uint32 `json:"version"`
	EndOffset uint64 `json:"end_offset"`
	Stored    bool   `json:"stored"`
}

// Upload sends one unit, gzip-compressed. A 409 (a gap, an overlap or an
// older version) is an *Error with Status 409.
func (c *Client) Upload(ctx context.Context, sessionID string, unit Unit) (Uploaded, error) {
	query := url.Values{
		"path":       {unit.Path},
		"version":    {strconv.FormatUint(uint64(unit.Version), 10)},
		"offset":     {strconv.FormatUint(unit.Offset, 10)},
		"harness":    {unit.Harness},
		"repository": {unit.Repository},
	}
	if unit.Folder != "" {
		query.Set("folder", unit.Folder)
	}
	if unit.ContentHash != "" {
		query.Set("content_hash", unit.ContentHash)
	}
	if unit.Size != 0 {
		query.Set("size", strconv.FormatUint(unit.Size, 10))
	}
	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	if _, err := zw.Write(unit.Bytes); err != nil {
		return Uploaded{}, err
	}
	if err := zw.Close(); err != nil {
		return Uploaded{}, err
	}
	var answer Uploaded
	err := c.call(ctx, http.MethodPost, "/agent-sessions/"+url.PathEscape(sessionID)+"/files?"+query.Encode(),
		body.Bytes(), &answer)
	return answer, err
}

func (c *Client) getJSON(ctx context.Context, path string, into any) error {
	return c.call(ctx, http.MethodGet, path, nil, into)
}

// call sends one request; body nil is a GET with no body, else a gzip body.
func (c *Client) call(ctx context.Context, method, path string, body []byte, into any) error {
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return err
	}
	resp, err := c.send(ctx, method, path, body, token)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if token, err = c.Tokens.Refreshed(ctx, token); err != nil {
			return err
		}
		if resp, err = c.send(ctx, method, path, body, token); err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var problem struct {
			Error          string `json:"error"`
			MinimumVersion string `json:"minimum_version"`
		}
		_ = json.Unmarshal(answer, &problem)
		return &Error{Status: resp.StatusCode, Message: problem.Error, MinimumVersion: problem.MinimumVersion}
	}
	if err := json.Unmarshal(answer, into); err != nil {
		return fmt.Errorf("read the answer of %s: %w", path, err)
	}
	return nil
}

func (c *Client) send(ctx context.Context, method, path string, body []byte, token string) (*http.Response, error) {
	if c.IngestURL == "" {
		return nil, errors.New("no ingest URL: run 'solidtrace login' again")
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.IngestURL, "/")+Path+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "solidtrace/"+c.Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Content-Encoding", "gzip")
	}
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
