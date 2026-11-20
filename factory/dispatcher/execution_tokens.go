package dispatcher

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// tokenMargin: the Execution's child token expires at the Execution's
// total limit plus tokenMargin, so the expiry is the backstop of the
// revocation.
const tokenMargin = time.Hour

// maxRunSessions is the most Agent session ids that one Run can have. It
// stops a wrapper that asks for each id that the run user writes.
const maxRunSessions = 32

// sessionScopes are the scopes of an Execution's child token.
var sessionScopes = []string{"sessions:write", "sessions:read"}

// mcpScopes are the scopes of an Issue or a Monitor Run's MCP token, for
// the Run's Project only.
var mcpScopes = []string{"mcp:read"}

// sessionIDPattern is an Agent session id as Claude Code makes it: a UUID
// in lower case.
var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var (
	errOtherRun        = errors.New("the Agent session belongs to another Run")
	errTooManySessions = fmt.Errorf("a Run can have at most %d Agent sessions", maxRunSessions)
)

// childTokens makes and revokes child tokens at Console, with the
// Dispatcher's token.
type childTokens struct {
	url, token string
	http       *http.Client
}

// child is a child token to make: its name, scopes, expiry and limits.
type child struct {
	name    string
	scopes  []string
	expires time.Time
	// sessionIDs is the limit to Agent sessions (nil is none).
	sessionIDs []string
	// projectID is the limit to one SolidTrace Project ("" is none).
	projectID string
}

// make makes the child token, and gives its id and secret.
func (c *childTokens) make(ctx context.Context, want child) (id, secret string, err error) {
	request := map[string]any{"name": want.name, "scopes": want.scopes,
		"expires_at": want.expires.UTC().Format(time.RFC3339)}
	if want.sessionIDs != nil {
		request["agent_session_ids"] = want.sessionIDs
	}
	if want.projectID != "" {
		request["project_id"] = want.projectID
	}
	body, _ := json.Marshal(request)
	resp, err := c.send(ctx, http.MethodPost, c.url+"/api/child_tokens", body)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("SolidTrace answered %d: %s", resp.StatusCode, bytes.TrimSpace(b))
	}
	var answer struct {
		APIToken struct {
			ID json.Number `json:"id"`
		} `json:"api_token"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(b, &answer); err != nil || answer.Secret == "" || answer.APIToken.ID == "" {
		return "", "", fmt.Errorf("SolidTrace's answer has no token: %s", bytes.TrimSpace(b))
	}
	return answer.APIToken.ID.String(), answer.Secret, nil
}

// revoke revokes the child token id. A token that SolidTrace does not
// know (404) is revoked already.
func (c *childTokens) revoke(ctx context.Context, id string) error {
	resp, err := c.send(ctx, http.MethodDelete, c.url+"/api/child_tokens/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("SolidTrace answered %d: %s", resp.StatusCode, bytes.TrimSpace(b))
	}
	return nil
}

func (c *childTokens) send(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// sessionToken makes a child token for the Run's Agent sessions, which
// expires at the Execution's total limit plus tokenMargin from its start,
// and keeps it for the revocation. A token made while the Execution ended
// is revoked at once, and gives errEnded.
func (d *Dispatcher) sessionToken(ctx context.Context, e storedExecution, started time.Time) (dispatcherapi.SessionToken, error) {
	ids, current, err := d.store.runSessions(e.runID)
	if err != nil {
		return dispatcherapi.SessionToken{}, err
	}
	expires, err := d.executionExpiry(e, started)
	if err != nil {
		return dispatcherapi.SessionToken{}, err
	}
	secret, err := d.executionToken(ctx, e, child{name: "Execution " + e.id + " of Run " + e.runID,
		scopes: sessionScopes, expires: expires, sessionIDs: ids})
	if err != nil {
		return dispatcherapi.SessionToken{}, fmt.Errorf("make the Execution's token: %w", err)
	}
	return dispatcherapi.SessionToken{Token: secret, ExpiresAt: expires, AgentSessionIDs: ids, SessionID: current}, nil
}

// mcpToken makes the MCP token of an Issue or a Monitor Run's Execution:
// mcp:read for the Run's Project only, with the Execution's expiry. It is
// kept for the revocation as the Execution's other token is.
func (d *Dispatcher) mcpToken(ctx context.Context, e storedExecution, started time.Time) (*dispatcherapi.MCPToken, error) {
	if e.projectID == "" {
		return nil, errors.New("the Run has no SolidTrace Project id")
	}
	expires, err := d.executionExpiry(e, started)
	if err != nil {
		return nil, err
	}
	secret, err := d.executionToken(ctx, e, child{name: "MCP of Execution " + e.id + " of Run " + e.runID,
		scopes: mcpScopes, expires: expires, projectID: e.projectID})
	if err != nil {
		return nil, fmt.Errorf("make the Run's MCP token: %w", err)
	}
	return &dispatcherapi.MCPToken{Token: secret, ExpiresAt: expires, URL: d.config.SolidTraceURL + "/mcp"}, nil
}

// executionExpiry is the expiry of an Execution's tokens: its total limit
// plus tokenMargin from its start.
func (d *Dispatcher) executionExpiry(e storedExecution, started time.Time) (time.Time, error) {
	limit, err := d.store.totalLimit(e.id)
	if err != nil {
		return time.Time{}, err
	}
	return started.Add(limit + tokenMargin).UTC().Truncate(time.Second), nil
}

// executionToken makes a child token for the running Execution and keeps
// it for the revocation, and gives its secret. A token made while the
// Execution ended is revoked at once, and gives errEnded.
func (d *Dispatcher) executionToken(ctx context.Context, e storedExecution, want child) (string, error) {
	id, secret, err := d.children.make(ctx, want)
	if err != nil {
		return "", err
	}
	if err := d.store.addExecutionToken(e.id, id, want.expires); err != nil {
		if rerr := d.children.revoke(context.Background(), id); rerr != nil {
			d.log.Warn("could not revoke a child token", "execution", e.id, "token", id, "error", rerr)
		}
		return "", err
	}
	return secret, nil
}

// token is the wrapper's token call: it adds a new Agent session id to
// the Run (a /clear, or a second Claude Code process), and gives a new
// child token for each of the Run's Agent sessions.
func (d *Dispatcher) token(w http.ResponseWriter, r *http.Request, e storedExecution) {
	var req dispatcherapi.TokenRequest
	if !decode(w, r, &req, "the body must be a JSON object with agent_session_id") {
		return
	}
	if req.AgentSessionID != "" && !sessionIDPattern.MatchString(req.AgentSessionID) {
		writeProblem(w, http.StatusBadRequest, "an Agent session id is a UUID in lower case, not "+quote(req.AgentSessionID))
		return
	}
	started, err := d.store.runningSince(e.id)
	if err == nil && req.AgentSessionID != "" {
		err = d.store.addRunSession(e.id, req.AgentSessionID, d.now())
	}
	var tok dispatcherapi.SessionToken
	if err == nil {
		tok, err = d.sessionToken(r.Context(), e, started)
	}
	switch {
	case errors.Is(err, errEnded):
		writeProblem(w, http.StatusConflict, "the Execution is not running: "+errEnded.Error()+" or has not started")
	case errors.Is(err, errOtherRun), errors.Is(err, errTooManySessions):
		writeProblem(w, http.StatusConflict, err.Error())
	case err != nil:
		d.log.Error("could not give an Execution a token", "execution", e.id, "error", err)
		writeProblem(w, http.StatusBadGateway, "the Dispatcher could not make the Execution's token at SolidTrace")
	default:
		writeJSON(w, http.StatusOK, tok)
	}
}

// revokeTokens revokes the child tokens of an ended Execution: the end
// step. A failure is logged; the token's expiry is the backstop.
func (d *Dispatcher) revokeTokens(executionID string) {
	ids, err := d.store.tokensToRevoke(executionID)
	if err != nil {
		d.log.Error("could not read an Execution's tokens", "execution", executionID, "error", err)
		return
	}
	for _, id := range ids {
		if err := d.children.revoke(context.Background(), id); err != nil {
			d.log.Warn("could not revoke a child token; it stops at its expiry", "execution", executionID,
				"token", id, "error", err)
			continue
		}
		if err := d.store.tokenRevoked(id, d.now()); err != nil {
			d.log.Error("could not keep a token's revocation", "token", id, "error", err)
		}
	}
}

// runSessions are the Run's Agent session ids, oldest first, and its
// current one.
func (s *store) runSessions(runID string) (ids []string, current string, err error) {
	if err := s.db.QueryRow(`SELECT session_id FROM runs WHERE id = ?`, runID).Scan(&current); err != nil {
		return nil, "", err
	}
	rows, err := s.db.Query(`SELECT session_id FROM run_sessions WHERE run_id = ? ORDER BY added_at, rowid`, runID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, "", err
		}
		ids = append(ids, id)
	}
	return ids, current, rows.Err()
}

// runningSince is the start of a running Execution, else errEnded.
func (s *store) runningSince(executionID string) (time.Time, error) {
	var started sql.NullInt64
	var state string
	if err := s.db.QueryRow(`SELECT state, started_at FROM executions WHERE id = ?`, executionID).
		Scan(&state, &started); err != nil {
		return time.Time{}, err
	}
	if state != dispatcherapi.ExecutionRunning || !started.Valid {
		return time.Time{}, errEnded
	}
	return time.UnixMicro(started.Int64).UTC(), nil
}

// addRunSession adds an Agent session id to the running Execution's Run.
// It does not make it the Run's current Agent session (runs.session_id,
// which the next Execution resumes and which has evidence/): a new file in
// the project directory can be the transcript of a second Claude Code
// process that a tool started. Only a sure signal moves the current one
// (the take-over, ticket 61). An id that the Run has is no change; an id
// of another Run gives errOtherRun.
func (s *store) addRunSession(executionID, sessionID string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID, state string
	if err := tx.QueryRow(`SELECT run_id, state FROM executions WHERE id = ?`, executionID).Scan(&runID, &state); err != nil {
		return err
	}
	if state != dispatcherapi.ExecutionRunning {
		return errEnded
	}
	var owner string
	err = tx.QueryRow(`SELECT run_id FROM run_sessions WHERE session_id = ?`, sessionID).Scan(&owner)
	switch {
	case err == nil && owner != runID:
		return errOtherRun
	case err == nil:
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM run_sessions WHERE run_id = ?`, runID).Scan(&count); err != nil {
		return err
	}
	if count >= maxRunSessions {
		return errTooManySessions
	}
	if _, err := tx.Exec(`INSERT INTO run_sessions (session_id, run_id, added_at) VALUES (?, ?, ?)`,
		sessionID, runID, at.UnixMicro()); err != nil {
		return err
	}
	return tx.Commit()
}

// addExecutionToken keeps a child token of a running Execution. When the
// Execution is not running, it keeps nothing and gives errEnded: its end
// step may have read the tokens already.
func (s *store) addExecutionToken(executionID, id string, expires time.Time) error {
	res, err := s.db.Exec(`INSERT INTO execution_tokens (id, execution_id, expires_at)
		SELECT ?, id, ? FROM executions WHERE id = ? AND state = ?`, id, expires.UnixMicro(), executionID,
		dispatcherapi.ExecutionRunning)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errEnded
	}
	return nil
}

// tokensToRevoke are the Execution's child tokens that are not revoked.
func (s *store) tokensToRevoke(executionID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM execution_tokens WHERE execution_id = ? AND revoked_at IS NULL`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *store) tokenRevoked(id string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE execution_tokens SET revoked_at = ? WHERE id = ?`, at.UnixMicro(), id)
	return err
}
