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

// Person is who started a Run or saved a value: the SolidTrace user of
// the token. A Run that a schedule started has the schedule as its
// starter, with Kind ActorSchedule.
type Person struct {
	// Kind is "" for a person, or ActorSchedule.
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ActorSchedule is the kind of a Run's starter that is a schedule.
const ActorSchedule = "schedule"

// Run is one unit of work, from its trigger to its PR outcome.
type Run struct {
	ID    string `json:"id"`
	State string `json:"state"`
	// Project is the Project whose Factory settings the Run uses.
	Project string `json:"project"`
	Prompt  string `json:"prompt"`
	// Organization is the slug of the Run's Organization.
	Organization string `json:"organization"`
	StartedBy    Person `json:"started_by"`
	// ScheduledBy is the member who made the schedule that started the
	// Run (nil for another trigger).
	ScheduledBy *Person   `json:"scheduled_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
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
	// PRURL is the link of the Run's PR, from the agent's first pr call
	// ("" when the Run has no PR).
	PRURL string `json:"pr_url,omitempty"`
	// URL is the Run's page in Console.
	URL string `json:"url"`
}

// PRText is the agent's 'solidtrace pr' call: the title and the
// description of the Run's PR. The first call opens the PR from the
// Run's branch to its base branch, and needs a title. A later call
// updates the same PR; an empty field stays as it is. The agent sends it
// to the wrapper on the agent's socket; the wrapper pushes the Run's
// branch, then sends the call to the Dispatcher for its own Execution.
type PRText struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// PR is the answer to a pr call: the Run's PR.
type PR struct {
	ID  int    `json:"id"`
	URL string `json:"url"`
	// Opened is true when this call opened the PR, and false when it
	// updated it.
	Opened bool `json:"opened"`
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
	// BackgroundTasksKilled is the count of the agent's background tasks
	// that still ran at the end, and that the end killed.
	BackgroundTasksKilled int `json:"background_tasks_killed,omitempty"`
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
	// BaseBranch is the branch that the Run's branch starts from ("" is
	// the repository's default branch).
	BaseBranch string `json:"base_branch"`
	// Branch is the Run's branch, agent/<run id> (RunBranch). The first
	// Execution makes it from BaseBranch; later Executions check it out.
	Branch string `json:"branch"`
	// GitURL is the base of the forge's HTTPS clone URLs. git over ssh to
	// the repository's host (git@host: and ssh://git@host/) is rewritten
	// to it, for submodules and private gems too.
	GitURL string `json:"git_url"`
	// ForgeUsername and ForgeToken are what the git credential helper
	// gives git ("" token: the Project names no connection).
	ForgeUsername string `json:"forge_username"`
	ForgeToken    string `json:"forge_token"`
	// SessionID is the Agent session id: --session-id for the first
	// Execution, --resume when Resume is true.
	SessionID string `json:"session_id"`
	Resume    bool   `json:"resume"`
	// Project is the Run's Project (its slug), and Folder the Project's
	// folder in the repository ("" is the root). An upload names both.
	Project string `json:"project"`
	Folder  string `json:"folder"`
	// IngestURL is where the wrapper uploads the Run's Agent sessions,
	// with Uploads.
	IngestURL string `json:"ingest_url"`
	// Uploads is the Execution's child token. Only the wrapper keeps it.
	Uploads SessionToken `json:"uploads"`
}

// RunBranch is the name of a Run's branch.
func RunBranch(runID string) string { return "agent/" + runID }

// SessionToken is a child token of one Execution: sessions:write and
// sessions:read for the Run's Agent sessions, until the Execution's total
// limit plus 1 hour. The Dispatcher revokes it at the Execution's end.
type SessionToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	// AgentSessionIDs are the Run's Agent sessions that the token can
	// reach. SessionID is the current one: it has the Run's evidence/.
	AgentSessionIDs []string `json:"agent_session_ids"`
	SessionID       string   `json:"session_id"`
}

// TokenRequest is the body of the wrapper's token call. A new Agent
// session id (a /clear, or a second Claude Code process) is added to the
// Run first, and the Run's current Agent session stays; with none, the call
// gives a new token for the same Agent sessions.
type TokenRequest struct {
	AgentSessionID string `json:"agent_session_id,omitempty"`
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
	// Error says what failed, for EndSetupFailed, and a failure of the
	// push before the end, for each reason.
	Error string `json:"error,omitempty"`
	// ExitCode is the harness's exit code (nil: the harness did not run).
	ExitCode        *int       `json:"exit_code,omitempty"`
	TaskDeliveredAt *time.Time `json:"task_delivered_at,omitempty"`
	// BackgroundTasksKilled is the count of the agent's background tasks
	// that still ran at the end, and that the end killed.
	BackgroundTasksKilled int `json:"background_tasks_killed,omitempty"`
}

// StartRun is the body of a start.
type StartRun struct {
	Project string `json:"project"`
	Prompt  string `json:"prompt"`
	// Branch is the Run's base branch ("" is the Project's default base
	// branch, else the repository's default branch).
	Branch string `json:"branch,omitempty"`
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

// Connection is a forge or tracker connection of the Organization. The
// Dispatcher never gives its token or its webhook secret.
type Connection struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy Person    `json:"updated_by"`
	// WebhookURL is the public URL to paste into the connection's service,
	// for a kind that sends webhooks ("" when the kind sends none or the
	// operator configuration has no public webhook URL).
	WebhookURL string `json:"webhook_url,omitempty"`
	// WebhookSecret is true when the connection has a webhook secret.
	WebhookSecret bool `json:"webhook_secret"`
	// LastWebhookAt is the time of the last verified webhook (nil is
	// none).
	LastWebhookAt *time.Time `json:"last_webhook_at,omitempty"`
}

// SaveWebhookSecret is the body of a save of a connection's webhook
// secret: the secret that signs its webhooks.
type SaveWebhookSecret struct {
	Secret string `json:"secret"`
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

// Schedule is a Project's schedule: it starts a Run at each fire time of
// its cron expression in its time zone.
type Schedule struct {
	ID string `json:"id"`
	// Project is the Project's slug in SolidTrace.
	Project string `json:"project"`
	Name    string `json:"name"`
	// Cron is a five-field cron expression: minute, hour, day of the
	// month, month and day of the week.
	Cron string `json:"cron"`
	// TimeZone is an IANA time zone, such as Europe/Berlin.
	TimeZone string `json:"time_zone"`
	Prompt   string `json:"prompt"`
	// BaseBranch is the branch that its Runs start from ("" is the
	// Project's default base branch).
	BaseBranch string `json:"base_branch"`
	Paused     bool   `json:"paused"`
	// NextAt is the next fire time (nil while the schedule is paused).
	NextAt *time.Time `json:"next_at"`
	// LastRun is the last Run that the schedule started (nil for none).
	LastRun   *ScheduleRun `json:"last_run"`
	CreatedAt time.Time    `json:"created_at"`
	CreatedBy Person       `json:"created_by"`
	UpdatedAt time.Time    `json:"updated_at"`
	UpdatedBy Person       `json:"updated_by"`
}

// ScheduleRun is a Run that a schedule started for the fire time FiredAt.
type ScheduleRun struct {
	ID      string    `json:"id"`
	State   string    `json:"state"`
	FiredAt time.Time `json:"fired_at"`
}

// SaveSchedule is the body of a new schedule. An empty TimeZone is UTC.
type SaveSchedule struct {
	Project    string `json:"project"`
	Name       string `json:"name"`
	Cron       string `json:"cron"`
	TimeZone   string `json:"time_zone"`
	Prompt     string `json:"prompt"`
	BaseBranch string `json:"base_branch"`
}

// UpdateSchedule is the body of a change to a schedule: each value that is
// not nil replaces the schedule's value.
type UpdateSchedule struct {
	Name       *string `json:"name,omitempty"`
	Cron       *string `json:"cron,omitempty"`
	TimeZone   *string `json:"time_zone,omitempty"`
	Prompt     *string `json:"prompt,omitempty"`
	BaseBranch *string `json:"base_branch,omitempty"`
}

// Schedules is the answer of a list of schedules, by Project and name.
type Schedules struct {
	Schedules []Schedule `json:"schedules"`
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

// SaveWebhookSecret makes or replaces a connection's webhook secret (an
// Admin only).
func (c *Client) SaveWebhookSecret(ctx context.Context, name, secret string) (Connection, error) {
	var connection Connection
	err := c.call(ctx, http.MethodPut, "/connections/"+url.PathEscape(name)+"/webhook-secret",
		SaveWebhookSecret{Secret: secret}, &connection)
	return connection, err
}

// Schedules reads the Organization's schedules, or the schedules of the
// Project with the slug project ("" is each Project).
func (c *Client) Schedules(ctx context.Context, project string) ([]Schedule, error) {
	var list Schedules
	path := "/schedules"
	if project != "" {
		path += "?project=" + url.QueryEscape(project)
	}
	err := c.call(ctx, http.MethodGet, path, nil, &list)
	return list.Schedules, err
}

// Schedule reads one schedule.
func (c *Client) Schedule(ctx context.Context, id string) (Schedule, error) {
	var schedule Schedule
	err := c.call(ctx, http.MethodGet, "/schedules/"+url.PathEscape(id), nil, &schedule)
	return schedule, err
}

// CreateSchedule makes a schedule.
func (c *Client) CreateSchedule(ctx context.Context, save SaveSchedule) (Schedule, error) {
	var schedule Schedule
	err := c.call(ctx, http.MethodPost, "/schedules", save, &schedule)
	return schedule, err
}

// UpdateSchedule changes each value of a schedule that update gives.
func (c *Client) UpdateSchedule(ctx context.Context, id string, update UpdateSchedule) (Schedule, error) {
	var schedule Schedule
	err := c.call(ctx, http.MethodPatch, "/schedules/"+url.PathEscape(id), update, &schedule)
	return schedule, err
}

// PauseSchedule stops a schedule's fire times until ContinueSchedule.
func (c *Client) PauseSchedule(ctx context.Context, id string) (Schedule, error) {
	var schedule Schedule
	err := c.call(ctx, http.MethodPost, "/schedules/"+url.PathEscape(id)+"/pause", nil, &schedule)
	return schedule, err
}

// ContinueSchedule starts a paused schedule's fire times again, from now.
func (c *Client) ContinueSchedule(ctx context.Context, id string) (Schedule, error) {
	var schedule Schedule
	err := c.call(ctx, http.MethodPost, "/schedules/"+url.PathEscape(id)+"/continue", nil, &schedule)
	return schedule, err
}

// DeleteSchedule deletes a schedule and gives it as it was. Its Runs
// continue.
func (c *Client) DeleteSchedule(ctx context.Context, id string) (Schedule, error) {
	var schedule Schedule
	err := c.call(ctx, http.MethodDelete, "/schedules/"+url.PathEscape(id), nil, &schedule)
	return schedule, err
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
