package dispatcher

import (
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The wrapper never sends a check or an Evidence path that the
// Dispatcher refuses, so only a direct call reaches these rules.
func TestCheckEvidenceRefusesWhatTheWrapperNeverSends(t *testing.T) {
	long := strings.Repeat("x", dispatcherapi.MaxCheckText+1)
	many := func(n int, v string) []string {
		list := make([]string, n)
		for i := range list {
			list[i] = v
		}
		return list
	}
	checks := func(n int) []dispatcherapi.CheckResult {
		return make([]dispatcherapi.CheckResult, n)
	}
	for name, r := range map[string]dispatcherapi.Report{
		"a path with no evidence/": {Evidence: []string{"../../etc/passwd"}},
		"an absolute path":         {Evidence: []string{"/x"}},
		"a path like evidence/":    {Evidence: []string{"evidencex/a.png"}},
		"a path that is too long":  {Evidence: []string{"evidence/" + strings.Repeat("a", dispatcherapi.MaxEvidencePath)}},
		"a path that is not UTF-8": {Evidence: []string{"evidence/bad\xff"}},
		"too many paths":           {Evidence: many(dispatcherapi.MaxEvidence+1, "evidence/a.png")},
		"too many checks":          {Checks: checks(dispatcherapi.MaxChecks + 1)},
		"a command too long":       {Checks: []dispatcherapi.CheckResult{{Command: long}}},
		"a name too long":          {Checks: []dispatcherapi.CheckResult{{Name: long, Command: "true"}}},
		"an output too long": {Checks: []dispatcherapi.CheckResult{{Command: "true",
			Output: strings.Repeat("x", dispatcherapi.MaxCheckOutput+1)}}},
		"an output not UTF-8": {Checks: []dispatcherapi.CheckResult{{Command: "true", Output: "\xff"}}},
		"a negative counter":  {DeliveredMessages: -1},
	} {
		if checkEvidence(r) == "" {
			t.Errorf("%s: accepted", name)
		}
	}
	ok := dispatcherapi.Report{Checks: checks(dispatcherapi.MaxChecks),
		Evidence: append(many(dispatcherapi.MaxEvidence-1, "evidence/a.png"), "evidence/report.md")}
	if problem := checkEvidence(ok); problem != "" {
		t.Errorf("a report at the limits is refused: %s", problem)
	}
}

// reportOn keeps a report with checks on the running Execution id, ends
// it, and queues its Run again.
func reportOn(t *testing.T, d *Dispatcher, id string, checks []dispatcherapi.CheckResult) {
	t.Helper()
	if err := d.store.saveReport(id, dispatcherapi.Report{Outcome: dispatcherapi.OutcomeDone, Summary: "Fixed",
		Checks: checks}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.db.Exec(`UPDATE executions SET state = ? WHERE id = ?`, dispatcherapi.ExecutionEnded,
		id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.db.Exec(`UPDATE runs SET state = ? WHERE id = 'r-1'`, dispatcherapi.StateQueued); err != nil {
		t.Fatal(err)
	}
}

// The Evidence block in the PR has the checks of the Run's last report:
// that of the last Execution with a report, not that of an earlier one.
func TestTheEvidenceBlockHasTheChecksOfTheLastReportOfTheRun(t *testing.T) {
	d, f := prWorld(t)
	prRun(t, d, f, "7", "42", "r-1", "e-1")
	reportOn(t, d, "e-1", []dispatcherapi.CheckResult{{Command: "go test ./...", ExitStatus: 1}})

	later := time.Now().Add(time.Minute)
	if ok, err := d.store.addExecution("r-1", "e-2", later); !ok || err != nil {
		t.Fatalf("add the second Execution: %v %v", ok, err)
	}
	if _, err := d.store.startExecution("e-2", later, defaultLimits); err != nil {
		t.Fatal(err)
	}
	reportOn(t, d, "e-2", []dispatcherapi.CheckResult{{Name: "test", Command: "true", Passed: true}})
	// A third Execution with no report does not change the block.
	if ok, err := d.store.addExecution("r-1", "e-3", later.Add(time.Minute)); !ok || err != nil {
		t.Fatalf("add the third Execution: %v %v", ok, err)
	}

	description, err := d.withBlock("r-1", "The agent's text")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(description, "The agent's text") || !strings.Contains(description, "- pass: test `true`") ||
		strings.Contains(description, "go test") || strings.Contains(description, "no Evidence") {
		t.Errorf("the PR description:\n%s", description)
	}
}
