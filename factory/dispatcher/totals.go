package dispatcher

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The closing totals (story 122). The closed event of a Run carries:
//
//   - totals.seconds_to_first_execution, totals.seconds_to_pr_opened (from
//     the Run's trigger) and totals.seconds_from_pr_opened_to_outcome;
//     each is absent when its event did not happen;
//   - totals.interventions.total and totals.interventions.<kind> for
//     message, interrupt, stop, take_over, account_switch and continue:
//     the events of a person, a sender, a forge user or a tracker user,
//     not the Dispatcher's own. A message that continues a Run is a
//     message and a continue; the total counts each event one time;
//   - totals.restarts: the automatic restarts after a lost machine (the
//     snapshot's run.executions counts the Executions);
//   - totals.agent_sessions, totals.tokens.input, .output, .cache_read,
//     .cache_write, totals.cost_usd (absent when no Agent session has a
//     cost) and totals.no_cost (the Agent sessions with no cost): the sums
//     of the agent_sessions rows of the Run's Agent sessions, from
//     SolidTrace's totals call;
//   - totals.complete: false when the totals call did not answer, or
//     answered that the parse has not caught up. The close does not wait.
//
// The snapshot of each event has the PR outcome (run.pr.outcome), the
// trigger kind (run.trigger) and the Project (factory.project).

// sessionTotalsPath is the agent ingest API's totals call.
const sessionTotalsPath = "/api/agent/v1/agent-session-totals"

// totalsTokenFor is the expiry of the totals call's child token after its
// make. The Dispatcher revokes it after the call.
const totalsTokenFor = 10 * time.Minute

// totalsCallBudget is the longest time of the totals call.
const totalsCallBudget = 30 * time.Second

// Names of run_counts: each intervention kind, and these.
const (
	countIntervention   = "intervention"
	countRestart        = "restart"
	countFirstExecution = "execution_started"
	countPROpened       = "pr_opened"
	countPROutcome      = "pr_outcome"
)

// interventionKinds are the event kinds of an intervention, with the kind
// that the totals name.
var interventionKinds = map[string]string{
	eventMessageAccepted: "message",
	eventInterrupt:       "interrupt",
	eventStop:            "stop",
	eventTakeOver:        "take_over",
	eventAccountSwitched: "account_switch",
}

// countedKinds are the kinds of totals.interventions.<kind>, in order.
var countedKinds = []string{"message", "interrupt", "stop", "take_over", "account_switch", "continue"}

// countsOf are the run_counts names that the run event of kind counts
// as. event is the event's Attributes.
func countsOf(kind string, event map[string]any) []string {
	switch kind {
	case eventExecutionStarted:
		return []string{countFirstExecution}
	case eventPROpened:
		return []string{countPROpened}
	case eventPROutcome:
		return []string{countPROutcome}
	case eventPreemption:
		if event["preemption.restart"] == true {
			return []string{countRestart}
		}
	case eventMachineLost:
		if event["machine_lost.restart"] == true {
			return []string{countRestart}
		}
	}
	name, ok := interventionKinds[kind]
	if !ok || event["actor.kind"] == actorDispatcher {
		return nil
	}
	names := []string{countIntervention, name}
	if _, continued := event["run.continued_from"]; continued {
		names = append(names, "continue")
	}
	return names
}

// countEvent adds the run event of kind at at to the Run's run_counts.
func countEvent(tx *sql.Tx, at time.Time, runID, kind string, event map[string]any) error {
	for _, name := range countsOf(kind, event) {
		if _, err := tx.Exec(`INSERT INTO run_counts (run_id, name, n, first_at) VALUES (?, ?, 1, ?)
			ON CONFLICT (run_id, name) DO UPDATE SET n = n + 1, first_at = MIN(first_at, excluded.first_at)`,
			runID, name, at.UnixMicro()); err != nil {
			return fmt.Errorf("count the run event: %w", err)
		}
	}
	return nil
}

// closingTotals are the totals of the Run's closed event that the
// Dispatcher keeps itself: the times and the counts, in tx.
func closingTotals(tx *sql.Tx, runID string) (map[string]any, error) {
	var created int64
	if err := tx.QueryRow(`SELECT created_at FROM runs WHERE id = ?`, runID).Scan(&created); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT name, n, first_at FROM run_counts WHERE run_id = ?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts, firsts := map[string]int64{}, map[string]int64{}
	for rows.Next() {
		var name string
		var n, first int64
		if err := rows.Scan(&name, &n, &first); err != nil {
			return nil, err
		}
		counts[name], firsts[name] = n, first
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seconds := func(from, to int64) int64 { return (to - from) / int64(time.Second/time.Microsecond) }
	totals := map[string]any{
		"totals.interventions.total": counts[countIntervention],
		"totals.restarts":            counts[countRestart],
	}
	for _, kind := range countedKinds {
		totals["totals.interventions."+kind] = counts[kind]
	}
	if at, ok := firsts[countFirstExecution]; ok {
		totals["totals.seconds_to_first_execution"] = seconds(created, at)
	}
	opened, ok := firsts[countPROpened]
	if ok {
		totals["totals.seconds_to_pr_opened"] = seconds(created, opened)
	}
	if at, decided := firsts[countPROutcome]; ok && decided {
		totals["totals.seconds_from_pr_opened_to_outcome"] = seconds(opened, at)
	}
	return totals, nil
}

// sessionTotals is the totals call's answer.
type sessionTotals struct {
	AgentSessions int      `json:"agent_sessions"`
	Input         uint64   `json:"input_tokens"`
	Output        uint64   `json:"output_tokens"`
	CacheRead     uint64   `json:"cache_read_tokens"`
	CacheWrite    uint64   `json:"cache_write_tokens"`
	CostUSD       *float64 `json:"cost_usd"`
	NoCost        int      `json:"no_cost"`
	Complete      bool     `json:"complete"`
}

// attributes are the totals of the closed event: totals.complete false
// and no sums for nil (the totals call did not answer).
func (t *sessionTotals) attributes() map[string]any {
	if t == nil {
		return map[string]any{"totals.complete": false}
	}
	a := map[string]any{
		"totals.complete":           t.Complete,
		"totals.agent_sessions":     t.AgentSessions,
		"totals.tokens.input":       t.Input,
		"totals.tokens.output":      t.Output,
		"totals.tokens.cache_read":  t.CacheRead,
		"totals.tokens.cache_write": t.CacheWrite,
		"totals.no_cost":            t.NoCost,
	}
	if t.CostUSD != nil {
		a["totals.cost_usd"] = *t.CostUSD
	}
	return a
}

// runSessionTotals asks SolidTrace for the sums of the agent_sessions
// rows of the Run's Agent sessions, with a child token that has only
// sessions:read for them, and revokes the token after the call. It is nil
// when SolidTrace gives no answer: a failure is a warning, and the close
// goes on.
func (d *Dispatcher) runSessionTotals(ctx context.Context, runID string) *sessionTotals {
	ids, _, err := d.store.runSessions(runID)
	if err != nil {
		d.log.Error("could not read the Agent sessions of a Run", "run", runID, "error", err)
		return nil
	}
	if len(ids) == 0 {
		return &sessionTotals{Complete: true}
	}
	id, secret, err := d.children.make(ctx, child{name: "Totals of Run " + runID, scopes: []string{"sessions:read"},
		expires: d.now().Add(totalsTokenFor).UTC().Truncate(time.Second), sessionIDs: ids})
	if err != nil {
		d.log.Warn("could not make the token of a Run's totals: its closing event has totals that are not complete",
			"run", runID, "error", err)
		return nil
	}
	defer func() {
		if err := d.children.revoke(context.Background(), id); err != nil {
			d.log.Warn("could not revoke the token of a Run's totals; it stops at its expiry", "run", runID,
				"token", id, "error", err)
		}
	}()
	totals, err := d.callTotals(ctx, secret, ids)
	if err != nil {
		d.log.Warn("could not read a Run's totals: its closing event has totals that are not complete",
			"run", runID, "error", err)
		return nil
	}
	return totals
}

func (d *Dispatcher) callTotals(ctx context.Context, token string, ids []string) (*sessionTotals, error) {
	ctx, cancel := context.WithTimeout(ctx, totalsCallBudget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.config.ingestURL()+sessionTotalsPath+
		"?agent_session_ids="+url.QueryEscape(strings.Join(ids, ",")), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SolidTrace answered %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var totals sessionTotals
	if err := json.Unmarshal(body, &totals); err != nil {
		return nil, fmt.Errorf("SolidTrace's answer is not the totals: %w", err)
	}
	return &totals, nil
}
