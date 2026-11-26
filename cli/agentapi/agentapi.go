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
	"time"
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
	// RetryAfter is the server's Retry-After, in seconds (0: none).
	RetryAfter int
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

// AgentSessionAnswer is the server's answer about one Agent session.
// Session is nil when the server does not have it in the token's
// Organization.
type AgentSessionAnswer struct {
	Session *AgentSession      `json:"agent_session"`
	Files   []AgentSessionFile `json:"files"`
}

// AgentSession is who and where an Agent session is. Yours is true when it
// is the token's person's and not a Run's.
type AgentSession struct {
	ID         string   `json:"id"`
	Yours      bool     `json:"yours"`
	Repository string   `json:"repository"`
	Folder     string   `json:"folder"`
	RunID      string   `json:"run_id"`
	Flags      []string `json:"flags"`
}

// AgentSession asks what the server has of an Agent session: who and
// where it is, and its files. Another person's Agent session needs a
// token with sessions:download.
func (c *Client) AgentSession(ctx context.Context, sessionID string) (AgentSessionAnswer, error) {
	var answer AgentSessionAnswer
	err := c.call(ctx, http.MethodGet, "/agent-sessions/"+url.PathEscape(sessionID)+"/files", &answer)
	return answer, err
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
	// RunID and Project name the Run and its Project: only a token of a
	// Run (limited to its Agent sessions) sends them.
	RunID   string
	Project string
	Bytes   []byte
}

// Uploaded is the server's answer to an upload.
type Uploaded struct {
	Path      string `json:"path"`
	Version   uint32 `json:"version"`
	EndOffset uint64 `json:"end_offset"`
	Stored    bool   `json:"stored"`
}

// UploadBase and UploadRate give an upload its time limit: UploadBase, and
// one more second for each UploadRate bytes of the gzip body (a 32 MiB
// body has about 9 minutes). The HTTP client's Timeout, which is for the
// small calls, does not apply to an upload.
const (
	UploadBase = 30 * time.Second
	UploadRate = 64 << 10
)

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
	if unit.RunID != "" {
		query.Set("run_id", unit.RunID)
		query.Set("project", unit.Project)
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
	ctx, cancel, hc := c.uploadClient(ctx, body.Len())
	defer cancel()
	var answer Uploaded
	err := c.callWith(ctx, hc, http.MethodPost, "/agent-sessions/"+url.PathEscape(sessionID)+"/files?"+query.Encode(),
		&requestBody{data: body.Bytes(), encoding: "gzip"}, &answer)
	return answer, err
}

func (c *Client) uploadClient(ctx context.Context, size int) (context.Context, context.CancelFunc, *http.Client) {
	ctx, cancel := context.WithTimeout(ctx, UploadBase+time.Duration(size/UploadRate)*time.Second)
	hc := *c.HTTP
	hc.Timeout = 0
	return ctx, cancel, &hc
}

const MaxEvidenceBytes = 32 << 20

// MaxEvidenceCaption is the most characters (runes) of the caption of an
// Evidence file.
const MaxEvidenceCaption = 200

// Evidence is the server's answer to an upload of an Evidence file. URL is
// the file's page in Console. It starts with "/" when the server does not
// know its Console URL.
type Evidence struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
}

// AddEvidence uploads one Evidence file of an Agent session, with an
// optional caption. The server finds the type from the bytes, so they go
// with no Content-Encoding. The same bytes again to the same Agent session
// give the first upload's answer.
func (c *Client) AddEvidence(ctx context.Context, sessionID, caption string, data []byte) (Evidence, error) {
	path := "/agent-sessions/" + url.PathEscape(sessionID) + "/evidence"
	if caption != "" {
		path += "?" + url.Values{"caption": {caption}}.Encode()
	}
	ctx, cancel, hc := c.uploadClient(ctx, len(data))
	defer cancel()
	var answer Evidence
	err := c.callWith(ctx, hc, http.MethodPost, path, &requestBody{data: data}, &answer)
	return answer, err
}

type requestBody struct {
	data     []byte
	encoding string
}

func (c *Client) getJSON(ctx context.Context, path string, into any) error {
	return c.call(ctx, http.MethodGet, path, into)
}

func (c *Client) call(ctx context.Context, method, path string, into any) error {
	return c.callWith(ctx, c.HTTP, method, path, nil, into)
}

func (c *Client) callWith(ctx context.Context, hc *http.Client, method, path string, body *requestBody, into any) error {
	resp, err := c.answer(ctx, hc, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(answer, into); err != nil {
		return fmt.Errorf("read the answer of %s: %w", path, err)
	}
	return nil
}

// answer sends one request, with one refresh of the token and one more
// try after a 401, and gives a 2xx answer. Any other status is an *Error.
func (c *Client) answer(ctx context.Context, hc *http.Client, method, path string, body *requestBody) (*http.Response, error) {
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(ctx, hc, method, path, body, token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if token, err = c.Tokens.Refreshed(ctx, token); err != nil {
			return nil, err
		}
		if resp, err = c.send(ctx, hc, method, path, body, token); err != nil {
			return nil, err
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var problem struct {
		Error          string `json:"error"`
		MinimumVersion string `json:"minimum_version"`
	}
	_ = json.Unmarshal(answer, &problem)
	retry, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
	return nil, &Error{Status: resp.StatusCode, Message: problem.Error, MinimumVersion: problem.MinimumVersion,
		RetryAfter: max(retry, 0)}
}

// Download is the Agent session directory that the server has: a tar
// archive with the highest version of each path, as the server stored it.
// The PAX records of each file give its version, offsets and flags.
type Download struct {
	// Archive is the tar archive. The caller closes it.
	Archive io.ReadCloser
	// Flags are the Agent session's flags (cut, partial, trimmed), as the
	// server gives them, between commas.
	Flags string
}

// The PAX records of a file in a download.
const (
	PAXVersion     = "SOLIDTRACE.version"
	PAXStartOffset = "SOLIDTRACE.start_offset"
	PAXEndOffset   = "SOLIDTRACE.end_offset"
	// PAXFlags are "partial" (no start) and "cut" (missing ranges).
	PAXFlags = "SOLIDTRACE.flags"
)

// DownloadAgentSession asks for the Agent session directory, for a
// resume. The token needs sessions:download, or sessions:read for the
// Agent session. A 503 has RetryAfter: the server has the last uploads but
// cannot give them yet. hc is the client to use (nil: c.HTTP); a download
// can take longer than the usual limit of one call.
func (c *Client) DownloadAgentSession(ctx context.Context, hc *http.Client, sessionID string) (Download, error) {
	if hc == nil {
		hc = c.HTTP
	}
	resp, err := c.answer(ctx, hc, http.MethodGet, "/agent-sessions/"+url.PathEscape(sessionID)+"/download", nil)
	if err != nil {
		return Download{}, err
	}
	return Download{Archive: resp.Body, Flags: resp.Header.Get("SolidTrace-Agent-Session-Flags")}, nil
}

func (c *Client) send(ctx context.Context, hc *http.Client, method, path string, body *requestBody, token string) (*http.Response, error) {
	if c.IngestURL == "" {
		return nil, errors.New("no ingest URL: run 'solidtrace login' again")
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body.data)
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
		if body.encoding != "" {
			req.Header.Set("Content-Encoding", body.encoding)
		}
	}
	resp, err := hc.Do(req)
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
