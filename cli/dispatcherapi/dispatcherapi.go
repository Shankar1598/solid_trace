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
	StateQueued          = "queued"
	StateRunning         = "running"
	StateWaitingOnPerson = "waiting_on_person"
	StateDone            = "done"
	StateFailed          = "failed"
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
	// Reason says why the Run is in its state: for failed, the summary of
	// the agent's report or the wrapper's reason, such as "the harness
	// exited".
	Reason string `json:"reason,omitempty"`
	// Question is the question of a report of needs a person.
	Question string `json:"question,omitempty"`
	// Summary is the summary of the report that set the state.
	Summary string `json:"summary,omitempty"`
	// Executions are the Run's Executions, oldest first.
	Executions []Execution `json:"executions,omitempty"`
	// URL is the Run's page in Console.
	URL string `json:"url"`
}

// Execution states.
const (
	// ExecutionDispatched is an Execution that the Runner has, whose
	// wrapper did not take its task yet.
	ExecutionDispatched = "dispatched"
	// ExecutionRunning is an Execution whose wrapper took its task.
	ExecutionRunning = "running"
	ExecutionEnded   = "ended"
)

// Execution is one time that a Run works on a machine.
type Execution struct {
	ID           string     `json:"id"`
	State        string     `json:"state"`
	DispatchedAt time.Time  `json:"dispatched_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	// TaskDeliveredAt is when the harness's replay echo of the task came,
	// with the wrapper's uuid: the proof that the agent got the task.
	TaskDeliveredAt *time.Time `json:"task_delivered_at,omitempty"`
	// EndReason is why the Execution ended: an End reason of the wrapper,
	// or what the Runner saw.
	EndReason string `json:"end_reason,omitempty"`
	// ExitCode is the harness's exit code, when the harness ran. It never
	// sets the outcome.
	ExitCode *int `json:"exit_code,omitempty"`
	// Account is the name of the subscription account that the Execution
	// got.
	Account string `json:"account,omitempty"`
}

// Report outcomes.
const (
	OutcomeDone         = "done"
	OutcomeNeedsAPerson = "needs_a_person"
	OutcomeFailed       = "failed"
)

// Report is what the agent reports with 'solidtrace report'. The agent
// sends it to the wrapper on the agent's socket, and the wrapper sends it
// to the Dispatcher for its own Execution.
type Report struct {
	Outcome string `json:"outcome"`
	Summary string `json:"summary"`
	// Question is what the agent asks a person (needs a person only).
	Question string `json:"question,omitempty"`
}

// Task is the Dispatcher's answer to the wrapper's task and secrets call.
type Task struct {
	RunID       string `json:"run_id"`
	ExecutionID string `json:"execution_id"`
	Prompt      string `json:"prompt"`
	// Repository is host/workspace/name.
	Repository string `json:"repository"`
	// CloneURL is the HTTPS URL that git clones from. It holds no token.
	CloneURL string `json:"clone_url"`
	// BaseBranch is the branch to clone ("" is the repository's default
	// branch).
	BaseBranch string `json:"base_branch"`
	// ForgeUsername and ForgeToken are what the git credential helper
	// gives git ("" token: the Project names no connection).
	ForgeUsername string `json:"forge_username"`
	ForgeToken    string `json:"forge_token"`
	// AccountToken is the token of the subscription account that the
	// Execution got (from 'claude setup-token'). Only the harness gets it,
	// as CLAUDE_CODE_OAUTH_TOKEN.
	AccountToken string `json:"account_token"`
	// SessionID is the Agent session id: --session-id for the first
	// Execution, --resume when Resume is true.
	SessionID string `json:"session_id"`
	Resume    bool   `json:"resume"`
}

// End reasons that the wrapper gives.
const (
	// EndReported: the wrapper ended the Execution after a report.
	EndReported = "reported"
	// EndNoReport: the agent's turn ended with no report.
	EndNoReport = "no report"
	// EndHarnessExited: the harness exited before the wrapper started the
	// end.
	EndHarnessExited = "the harness exited"
	// EndSetupFailed: the wrapper could not start the harness; Error says
	// why.
	EndSetupFailed = "the setup failed"
)

// ExecutionEnd is the body of the wrapper's end call.
type ExecutionEnd struct {
	Reason string `json:"reason"`
	// Error says what failed, for EndSetupFailed.
	Error string `json:"error,omitempty"`
	// ExitCode is the harness's exit code (nil: the harness did not run).
	ExitCode        *int       `json:"exit_code,omitempty"`
	TaskDeliveredAt *time.Time `json:"task_delivered_at,omitempty"`
}

// StartRun is the body of a start.
type StartRun struct {
	Project string `json:"project"`
	Prompt  string `json:"prompt"`
}

// FactorySettings are a Project's Factory settings, as the Dispatcher
// gives them.
type FactorySettings struct {
	// Project is the Project's slug in SolidTrace.
	Project string `json:"project"`
	// Repository is host/workspace/name, such as bitbucket.org/team/shop.
	Repository string `json:"repository"`
	// Folder is the Project's folder in the repository ("" is the root).
	Folder string `json:"folder"`
	// BaseBranch is the branch that a Run starts from when none is given
	// ("" is the repository's default branch).
	BaseBranch string `json:"base_branch"`
	// Connection is the name of the forge connection that the Project's
	// Runs use ("" is none).
	Connection string `json:"connection"`
	// Orphaned is true when the Dispatcher found that SolidTrace no longer has
	// the Project.
	Orphaned  bool      `json:"orphaned"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy Person    `json:"updated_by"`
}

// SaveFactorySettings is the body of a save of a Project's Factory
// settings. It replaces each value.
type SaveFactorySettings struct {
	Repository string `json:"repository"`
	Folder     string `json:"folder"`
	BaseBranch string `json:"base_branch"`
	Connection string `json:"connection"`
}

// ProjectList is the answer of the list of the Factory settings of the
// Organization's Projects.
type ProjectList struct {
	Projects []FactorySettings `json:"projects"`
}

// ForgeBitbucket is the kind of a Bitbucket Cloud connection.
const ForgeBitbucket = "bitbucket"

// Connection is a forge connection of the Organization. The Dispatcher
// never gives its token.
type Connection struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy Person    `json:"updated_by"`
}

// SaveConnection is the body of a save of a connection. It makes the
// connection, or replaces its token.
type SaveConnection struct {
	Kind  string `json:"kind"`
	Token string `json:"token"`
}

// Connections is the answer of the list of the Organization's
// connections, by name.
type Connections struct {
	Connections []Connection `json:"connections"`
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

// Projects reads the Factory settings of the Organization's Projects.
func (c *Client) Projects(ctx context.Context) ([]FactorySettings, error) {
	var list ProjectList
	err := c.call(ctx, http.MethodGet, "/projects", nil, &list)
	return list.Projects, err
}

// Settings reads one Project's Factory settings.
func (c *Client) Settings(ctx context.Context, project string) (FactorySettings, error) {
	var settings FactorySettings
	err := c.call(ctx, http.MethodGet, "/projects/"+url.PathEscape(project)+"/settings", nil, &settings)
	return settings, err
}

// SaveSettings replaces a Project's Factory settings (an Admin only).
func (c *Client) SaveSettings(ctx context.Context, project string, save SaveFactorySettings) (FactorySettings, error) {
	var settings FactorySettings
	err := c.call(ctx, http.MethodPut, "/projects/"+url.PathEscape(project)+"/settings", save, &settings)
	return settings, err
}

// Connections reads the Organization's connections.
func (c *Client) Connections(ctx context.Context) ([]Connection, error) {
	var list Connections
	err := c.call(ctx, http.MethodGet, "/connections", nil, &list)
	return list.Connections, err
}

// SaveConnection makes a connection or replaces its token (an Admin only).
func (c *Client) SaveConnection(ctx context.Context, name string, save SaveConnection) (Connection, error) {
	var connection Connection
	err := c.call(ctx, http.MethodPut, "/connections/"+url.PathEscape(name), save, &connection)
	return connection, err
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
