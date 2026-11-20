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
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Issue and Monitor Runs. Console's 'Factory' Integration starts a Run
// for a SolidTrace Issue or Monitor through the generic webhook, with
// these attributes (Notifiers::FactoryNotifier):
//
//   - solidtrace.notification: the kind of the Notification.
//   - solidtrace.issue_number: the Issue's number in the Run's Project.
//   - solidtrace.monitor_id (and solidtrace.monitor_group): the Monitor.
//
// For such a Run, the Dispatcher adds the standing task to the first
// message, gives each Execution an mcp:read token for the Run's Project
// only, and posts the cause report, or the PR-opened comment, on the
// Issue as 'Factory'. A Monitor has no comments: its cause report is on
// the Run's page only.
//
// Only a sender sets attributes, and an Admin makes each sender: a
// sender's delivery with these attributes is an Issue or a Monitor Run.

// The attributes of a delivery from Console's 'Factory' Integration.
const (
	attrNotification = "solidtrace.notification"
	attrIssueNumber  = "solidtrace.issue_number"
	attrMonitorID    = "solidtrace.monitor_id"
)

// runCause is the Issue or the Monitor that started a Run.
type runCause struct {
	// issue is the Issue's number in the Run's Project (0 is none).
	issue int
	// monitor is the Monitor's id ("" is none).
	monitor string
}

// any is true for an Issue or a Monitor Run.
func (c runCause) any() bool { return c.issue > 0 || c.monitor != "" }

// causeOf is the cause of a Run from who started it and its attributes
// (JSON text, "" is none).
func causeOf(startedBy, attributes string) (runCause, error) {
	if startedBy != actorSender || attributes == "" {
		return runCause{}, nil
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(attributes), &values); err != nil {
		return runCause{}, fmt.Errorf("read the Run's attributes: %w", err)
	}
	if values[attrNotification] == "" {
		return runCause{}, nil
	}
	if n, err := strconv.Atoi(values[attrIssueNumber]); err == nil && n > 0 {
		return runCause{issue: n}, nil
	}
	if id := strings.TrimSpace(values[attrMonitorID]); id != "" {
		return runCause{monitor: id}, nil
	}
	return runCause{}, nil
}

// withStandingTask is the first message of an Issue or a Monitor Run:
// the standing task, then the delivery's prompt.
func (c runCause) withStandingTask(prompt string) string {
	what := "a SolidTrace Monitor that fired"
	if c.issue > 0 {
		what = "a SolidTrace Issue"
	}
	return "This Run is for " + what + ". Your standing task:\n\n" +
		"1. Find the cause, and report it with 'solidtrace report --outcome done --summary TEXT --cause-report TEXT'. " +
		"The cause report says what causes the problem, and shows the Logs, the code and the steps that prove it.\n" +
		"2. Open a PR only when a check proves the fix: a test that fails before the fix and passes after it. " +
		"Run it with 'solidtrace check' before and after the fix. With no such check, open no PR: the cause report " +
		"is the result.\n" +
		"3. The MCP server 'solidtrace' reads the Logs and the Issues of this Project only. Its token is in the file " +
		"that $" + dispatcherapi.EnvMCPTokenFile + " names, and stops at the end of this Execution.\n\n" +
		"The notification:\n\n" + prompt
}

// doneFlags are the flags of a done Run of this cause, from its last
// report: "Cause report" alone for an Issue or a Monitor Run with a cause
// report and no PR, else the usual flags. With a PR, a check that passed
// is still needed: a PR with no passed check keeps "no Evidence".
func (c runCause) doneFlags(report dispatcherapi.Report, prID int) []string {
	if c.any() && report.CauseReport != "" && prID == 0 {
		return []string{dispatcherapi.FlagCauseReport}
	}
	return doneFlags(report.Checks, prID)
}

// runCause reads the cause of the Run.
func (s *store) runCause(runID string) (runCause, error) {
	var startedBy, attributes string
	if err := s.db.QueryRow(`SELECT started_by_kind, attributes FROM runs WHERE id = ?`, runID).
		Scan(&startedBy, &attributes); err != nil {
		return runCause{}, err
	}
	return causeOf(startedBy, attributes)
}

// The comments on an Issue. They go through the write-back outbox
// (ticket_writes) with the connection id issueConnection and the Issue's
// number as the ticket, so they have its retries and its order:
//
//   - execution_ended of a done Run with a cause report and no PR: the
//     cause report.
//   - the PR-opened comment of each PR, at the same steps as a tracker
//     ticket's: with the PR's link, the Run's link, the summary, the
//     pass/fail list of the checks ("no Evidence" when none passed) and
//     the cause report, if there is one.
//
// No other step comments on the Issue. Console shows each of them with
// "Factory" and the Run's link as the author: the Dispatcher's own token
// sends them, with the Run's id.

// issueConnection is the connection id of the write-back steps that go to
// a SolidTrace Issue. It is not a connection's id (connectionPattern).
const issueConnection = "@solidtrace-issue"

// stepCauseReport is the write-back step of an Issue Run's cause report.
const stepCauseReport = "cause_report"

// issueCommentsPath is Console's API for a comment on an Issue.
const issueCommentsPath = "/api/issue_comments"

// queueIssueComment queues the comment on the Issue of an Issue Run for
// the run event kind, in tx.
func queueIssueComment(tx *sql.Tx, at time.Time, runID, kind string, details map[string]any) error {
	if kind != eventExecutionEnded && kind != eventStop {
		return nil
	}
	var state, summary, prURL, startedBy, attributes string
	err := tx.QueryRow(`SELECT state, summary, pr_url, started_by_kind, attributes FROM runs WHERE id = ?`, runID).
		Scan(&state, &summary, &prURL, &startedBy, &attributes)
	if err != nil {
		return fmt.Errorf("read the Run for its Issue comment: %w", err)
	}
	cause, err := causeOf(startedBy, attributes)
	if err != nil || cause.issue == 0 {
		return err
	}
	executionID, _ := details["execution.id"].(string)
	issue := strconv.Itoa(cause.issue)
	if prURL != "" {
		return queuePROpenedComment(tx, at, runID, executionID, issueConnection, issue, summary, prURL)
	}
	if kind != eventExecutionEnded || state != dispatcherapi.StateDone {
		return nil
	}
	var causeReport string
	if err := tx.QueryRow(`SELECT report_cause FROM executions WHERE id = ?`, executionID).Scan(&causeReport); err != nil {
		return fmt.Errorf("read the cause report: %w", err)
	}
	if causeReport == "" {
		return nil
	}
	_, err = tx.Exec(`INSERT INTO ticket_writes (run_id, execution_id, connection_id, ticket_id, step, status,
		comment, text, pr_url, created_at) VALUES (?, ?, ?, ?, ?, '', 1, ?, '', ?)`, runID, executionID, issueConnection,
		issue, stepCauseReport, summary, at.UnixMicro())
	return err
}

// commentOnIssue posts a write-back step's comment on the Issue, with the
// Dispatcher's token. An Issue that SolidTrace does not have (404), or a
// comment that it refuses (422), is given up.
func (d *Dispatcher) commentOnIssue(ctx context.Context, w ticketWrite) error {
	number, err := strconv.Atoi(w.ticketID)
	if err != nil {
		return fmt.Errorf("%w: the Issue number %q is not a number", errGiveUp, w.ticketID)
	}
	project, causeReport, err := d.store.issueCommentParts(w.runID, w.executionID)
	if err != nil {
		return err
	}
	run := "Run: " + d.config.SolidTraceURL + "/" + w.organizationSlug + "/runs/" + w.runID
	var body string
	switch w.step {
	case stepCauseReport:
		body = "SolidTrace Factory found the cause of this Issue, and opened no PR.\n" + run + "\n\n"
		if w.text != "" {
			body += "Summary:\n" + w.text + "\n\n"
		}
		body += "Cause report:\n" + causeReport
	case stepPROpened:
		checks, err := d.store.reportChecks(w.executionID)
		if err != nil {
			return err
		}
		body = prOpenedComment("this Issue", w.prURL, run, w.text, checks)
		if causeReport != "" {
			body += "\n\nCause report:\n" + causeReport
		}
	default:
		return fmt.Errorf("%w: the step %s has no Issue comment", errGiveUp, w.step)
	}
	request, _ := json.Marshal(map[string]any{"project": project, "issue": number, "body": body, "run_id": w.runID})
	call, cancel := context.WithTimeout(ctx, writeBackTimeout)
	defer cancel()
	resp, err := d.children.send(call, http.MethodPost, d.children.url+issueCommentsPath, request)
	if err != nil {
		return fmt.Errorf("post the comment on the Issue: %w", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	switch {
	case resp.StatusCode == http.StatusCreated:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: SolidTrace answered %d: %s", errGiveUp, resp.StatusCode, bytes.TrimSpace(answer))
	}
	return fmt.Errorf("SolidTrace answered %d: %s", resp.StatusCode, bytes.TrimSpace(answer))
}

// issueCommentParts are the Run's Project (its slug) and the cause report
// of the Execution's last valid report ("" is none).
func (s *store) issueCommentParts(runID, executionID string) (project, causeReport string, err error) {
	err = s.db.QueryRow(`SELECT r.project, COALESCE((SELECT report_cause FROM executions WHERE id = ?), '')
		FROM runs r WHERE r.id = ?`, executionID, runID).Scan(&project, &causeReport)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("%w: the Run is gone", errGiveUp)
	}
	return project, causeReport, err
}
