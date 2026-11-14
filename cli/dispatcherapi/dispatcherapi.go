// Package dispatcherapi is the client of the Dispatcher's private API, at
// /api/v1 on the private listener, and the shapes of its requests and
// answers. The Dispatcher uses the same shapes, so the two cannot differ.
// Each call sends the sign-in's access token; a 401 gets one refresh of
// the token and one more try.
package dispatcherapi

import (
	"bytes"
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

// Path is where the API is under the Dispatcher's URL.
const Path = "/api/v1"

// Run states.
const (
	StateQueued = "queued"
)

// Person is who started a Run: the SolidTrace user of the token.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Run is one unit of work, from its trigger to its PR outcome.
type Run struct {
	ID    string `json:"id"`
	State string `json:"state"`
	// Project is the Project whose Factory settings the Run uses.
	Project string `json:"project"`
	Prompt  string `json:"prompt"`
	// Organization is the slug of the Run's Organization.
	Organization string    `json:"organization"`
	StartedBy    Person    `json:"started_by"`
	CreatedAt    time.Time `json:"created_at"`
	// URL is the Run's page in Console.
	URL string `json:"url"`
}

// StartRun is the body of a start.
type StartRun struct {
	Project string `json:"project"`
	Prompt  string `json:"prompt"`
}

// Runs is the answer of a list, newest first.
type Runs struct {
	Runs []Run `json:"runs"`
}

// Problem is the body of an answer that is not a success.
type Problem struct {
	Error string `json:"error"`
	// MinimumVersion is the oldest solidtrace CLI that the Dispatcher
	// accepts, on a 426 to an older CLI.
	MinimumVersion string `json:"minimum_version,omitempty"`
}

// Status is the answer of GET /status: the Dispatcher's version, and
// whether SolidTrace accepts the Dispatcher's own token. It needs no
// token, so it can say when the Dispatcher cannot check tokens.
type Status struct {
	Version    string           `json:"version"`
	SolidTrace SolidTraceStatus `json:"solidtrace"`
}

// SolidTraceStatus is the state of the Dispatcher's link to SolidTrace.
type SolidTraceStatus struct {
	URL string `json:"url"`
	// TokenActive is true when SolidTrace accepts the Dispatcher's token.
	TokenActive bool `json:"token_active"`
	// Error says what is wrong when TokenActive is false.
	Error string `json:"error,omitempty"`
}

// Tokens gives the access token (signin.Source).
type Tokens interface {
	Token(ctx context.Context) (string, error)
	Refreshed(ctx context.Context, rejected string) (string, error)
}

// Client calls the Dispatcher at URL.
type Client struct {
	URL     string
	HTTP    *http.Client
	Tokens  Tokens
	Version string
}

// Error is an answer that is not a success.
type Error struct {
	Status  int
	Message string
	// MinimumVersion is the oldest CLI version that the Dispatcher
	// accepts, on a 426.
	MinimumVersion string
}

func (e *Error) Error() string {
	switch {
	case e.Status == http.StatusUpgradeRequired && e.MinimumVersion != "":
		return fmt.Sprintf("the Dispatcher refuses this solidtrace version: install solidtrace %s or later",
			e.MinimumVersion)
	case e.Status == http.StatusUnauthorized:
		return fmt.Sprintf("the Dispatcher refused the sign-in (%s): run 'solidtrace login' again", e.Message)
	case e.Message != "":
		return fmt.Sprintf("the Dispatcher answered %d: %s", e.Status, e.Message)
	default:
		return fmt.Sprintf("the Dispatcher answered %d", e.Status)
	}
}

// Start makes a queued Run.
func (c *Client) Start(ctx context.Context, start StartRun) (Run, error) {
	var run Run
	err := c.call(ctx, http.MethodPost, "/runs", start, &run)
	return run, err
}

// Get reads one Run.
func (c *Client) Get(ctx context.Context, id string) (Run, error) {
	var run Run
	err := c.call(ctx, http.MethodGet, "/runs/"+url.PathEscape(id), nil, &run)
	return run, err
}

// List reads the Runs of the token's Organization, newest first.
func (c *Client) List(ctx context.Context) ([]Run, error) {
	var runs Runs
	err := c.call(ctx, http.MethodGet, "/runs", nil, &runs)
	return runs.Runs, err
}

func (c *Client) call(ctx context.Context, method, path string, body, into any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return err
	}
	resp, err := c.send(ctx, method, path, payload, token)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if token, err = c.Tokens.Refreshed(ctx, token); err != nil {
			return err
		}
		if resp, err = c.send(ctx, method, path, payload, token); err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var problem Problem
		_ = json.Unmarshal(answer, &problem)
		return &Error{Status: resp.StatusCode, Message: problem.Error, MinimumVersion: problem.MinimumVersion}
	}
	if err := json.Unmarshal(answer, into); err != nil {
		return fmt.Errorf("read the Dispatcher's answer to %s %s: %w", method, path, err)
	}
	return nil
}

func (c *Client) send(ctx context.Context, method, path string, body []byte, token string) (*http.Response, error) {
	if c.URL == "" {
		return nil, errors.New("no Dispatcher URL")
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.URL, "/")+Path+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "solidtrace/"+c.Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach the Dispatcher at %s: %w", c.URL, err)
	}
	return resp, nil
}
