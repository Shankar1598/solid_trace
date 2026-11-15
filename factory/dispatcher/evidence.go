package dispatcher

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

// maxEvidencePath is the longest Evidence path that a report keeps.
const maxEvidencePath = 1024

// checkEvidence checks the wrapper's part of a report: the checks and the
// Evidence paths.
func checkEvidence(r dispatcherapi.Report) string {
	if len(r.Checks) > dispatcherapi.MaxChecks {
		return fmt.Sprintf("a report has at most %d checks", dispatcherapi.MaxChecks)
	}
	for _, c := range r.Checks {
		if len(c.Output) > dispatcherapi.MaxCheckOutput || len(c.Command) > maxReportText || len(c.Name) > maxReportText ||
			!utf8.ValidString(c.Output+c.Command+c.Name) {
			return fmt.Sprintf("a check has a command and a name of UTF-8 text, and at most %d bytes of output",
				dispatcherapi.MaxCheckOutput)
		}
	}
	if len(r.Evidence) > dispatcherapi.MaxEvidence {
		return fmt.Sprintf("a report names at most %d Evidence files", dispatcherapi.MaxEvidence)
	}
	for _, p := range r.Evidence {
		if !strings.HasPrefix(p, "evidence/") || len(p) > maxEvidencePath || !utf8.ValidString(p) {
			return "an Evidence path is evidence/<path>, at most 1024 bytes"
		}
	}
	if r.DeliveredMessages < 0 {
		return "the delivered-message counter cannot be less than 0"
	}
	return ""
}

// doneFlags are the flags of a done Run whose last report has checks:
// no Evidence when no check passed, and No PR when the Run has no PR. A
// PR opens only from the Run's pushed branch, so a PR is also the proof
// of the push.
func doneFlags(checks []dispatcherapi.CheckResult, prID int) []string {
	flags := []string{}
	if !anyPassed(checks) {
		flags = append(flags, dispatcherapi.FlagNoEvidence)
	}
	if prID == 0 {
		flags = append(flags, dispatcherapi.FlagNoPR)
	}
	return flags
}

func anyPassed(checks []dispatcherapi.CheckResult) bool {
	for _, c := range checks {
		if c.Passed {
			return true
		}
	}
	return false
}

// runFlags gives the stored flags of a Run in state: only a done Run has
// flags.
func runFlags(state, stored string) []string {
	if state != dispatcherapi.StateDone || stored == "" {
		return nil
	}
	var flags []string
	if json.Unmarshal([]byte(stored), &flags) != nil || len(flags) == 0 {
		return nil
	}
	return flags
}

// jsonList is v as JSON, with "" for an empty list.
func jsonList[T any](v []T) (string, error) {
	if len(v) == 0 {
		return "", nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

func unmarshalList[T any](s string, v *[]T) error {
	if s == "" {
		*v = nil
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

// nonNil is v, or an empty list: an event has the field also when it is
// empty.
func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// lastChecks gives the checks of the Run's last report, and false when
// the Run has no report.
func (s *store) lastChecks(runID string) ([]dispatcherapi.CheckResult, bool, error) {
	var stored string
	err := s.db.QueryRow(`SELECT report_checks FROM executions WHERE run_id = ? AND report_outcome != ''
		ORDER BY dispatched_at DESC, rowid DESC LIMIT 1`, runID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var checks []dispatcherapi.CheckResult
	err = unmarshalList(stored, &checks)
	return checks, true, err
}

// runLink is the Run's page in Console.
func (s *store) runLink(config Config, runID string) (string, error) {
	var slug string
	err := s.db.QueryRow(`SELECT organization_slug FROM runs WHERE id = ?`, runID).Scan(&slug)
	return config.SolidTraceURL + "/" + slug + "/runs/" + runID, err
}

// evidenceBlock is the block that the Dispatcher writes in the Run's PR
// description, between the markers: the pass/fail list of the checks of
// the last report and the Run's link.
func evidenceBlock(checks []dispatcherapi.CheckResult, runLink string) string {
	var b strings.Builder
	b.WriteString(dispatcherapi.EvidenceStart + "\n---\n\n**Evidence** (the factory ran these checks)\n\n")
	if len(checks) == 0 {
		b.WriteString("No check ran.\n")
	}
	for _, c := range checks {
		verdict := "pass"
		if !c.Passed {
			verdict = "fail"
		}
		label := "`" + strings.ReplaceAll(c.Command, "`", "'") + "`"
		if c.Name != "" {
			label = c.Name + " " + label
		}
		fmt.Fprintf(&b, "- %s: %s (exit status %d)\n", verdict, label, c.ExitStatus)
	}
	if !anyPassed(checks) {
		b.WriteString("\n**no Evidence**: no check passed.\n")
	}
	fmt.Fprintf(&b, "\nRun: %s\n%s", runLink, dispatcherapi.EvidenceEnd)
	return b.String()
}

// withoutBlock is description with no Evidence block: the agent's text.
func withoutBlock(description string) string {
	for {
		start := strings.Index(description, dispatcherapi.EvidenceStart)
		if start < 0 {
			return strings.TrimSpace(description)
		}
		end := strings.Index(description[start:], dispatcherapi.EvidenceEnd)
		if end < 0 {
			return strings.TrimSpace(description[:start])
		}
		description = description[:start] + description[start+end+len(dispatcherapi.EvidenceEnd):]
	}
}

// withBlock is the agent's text of description, then the Evidence block
// of the Run's last report. It is the agent's text alone when the Run has
// no report.
func (d *Dispatcher) withBlock(runID, description string) (string, error) {
	text := withoutBlock(description)
	checks, reported, err := d.store.lastChecks(runID)
	if err != nil || !reported {
		return text, err
	}
	link, err := d.store.runLink(d.config, runID)
	if err != nil {
		return text, err
	}
	block := evidenceBlock(checks, link)
	if text == "" {
		return block, nil
	}
	return text + "\n\n" + block, nil
}

// writeEvidenceBlock writes the Evidence block of the Run's last report in
// its PR's description, after a report. It keeps the text outside the
// markers: the agent's (and a person's edits). It does nothing when the
// Run has no PR. A failure is a warning: the report counts all the same.
func (d *Dispatcher) writeEvidenceBlock(e storedExecution) {
	defer d.prLocks.lock(e.runID)()
	ctx, cancel := context.WithTimeout(context.Background(), dispatcherapi.PRBudget)
	defer cancel()
	fail := func(err error) {
		d.warn("the Dispatcher could not write the Evidence block in the Run's PR", "run.id", e.runID,
			"error", err.Error())
	}
	target, err := d.store.prTarget(e.runID)
	if err != nil {
		fail(err)
		return
	}
	if target.prID == 0 {
		return
	}
	settings, err := d.store.settings(e.organizationID, e.projectID)
	if err != nil {
		fail(err)
		return
	}
	if target.repository == "" {
		target.repository = settings.Repository
	}
	f, err := d.projectForge(e.organizationID, settings.Connection)
	if err != nil {
		fail(err)
		return
	}
	pr, err := f.PR(ctx, target.repository, target.prID)
	if err != nil {
		fail(err)
		return
	}
	description, err := d.withBlock(e.runID, pr.Description)
	if err != nil {
		fail(err)
		return
	}
	if description == pr.Description {
		return
	}
	if _, err := f.UpdatePR(ctx, target.repository, target.prID, forge.PRText{Description: description}); err != nil {
		fail(err)
	}
}

// afterReport starts the write of the Evidence block. It does not hold
// the report's answer: the forge can be slow.
func (d *Dispatcher) afterReport(e storedExecution) {
	d.loops.Add(1)
	go func() {
		defer d.loops.Done()
		d.writeEvidenceBlock(e)
	}()
}
