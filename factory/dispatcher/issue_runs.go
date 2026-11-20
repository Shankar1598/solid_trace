package dispatcher

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
