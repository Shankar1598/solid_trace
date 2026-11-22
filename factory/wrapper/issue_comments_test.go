package wrapper_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// oneIssueComment waits for the Issue's first comment, and checks that
// no second one comes.
func oneIssueComment(t *testing.T, env *factorytest.Env) recorder.IssueComment {
	t.Helper()
	waitUntil(t, "the Dispatcher commented on the Issue", func() bool { return len(env.SolidTrace.IssueComments()) > 0 })
	time.Sleep(300 * time.Millisecond)
	comments := env.SolidTrace.IssueComments()
	if len(comments) != 1 {
		t.Fatalf("want one comment on the Issue, got %+v", comments)
	}
	return comments[0]
}

func TestAnIssueRunsCauseReportIsACommentOnTheIssueByFactory(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "done", "--summary", "Found the cause", "--cause-report", cause), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	c := oneIssueComment(t, env)
	// The Dispatcher's own token: Console shows its comment as by Factory,
	// with the Run's link.
	if c.Token != factorytest.DispatcherToken || c.RunID != id || c.Project != "shop" || c.Issue != 7 {
		t.Errorf("the comment is by %q for Run %q on %s#%d", c.Token, c.RunID, c.Project, c.Issue)
	}
	if !strings.Contains(c.Body, cause) || !strings.Contains(c.Body, run.URL) {
		t.Errorf("the comment has no cause report or no Run link:\n%s", c.Body)
	}
}

func TestAMonitorRunsCauseReportIsOnTheRunsPageOnly(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "done", "--summary", "Found the cause", "--cause-report", cause), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", monitorAttributes("12"))
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	time.Sleep(500 * time.Millisecond)
	if calls := env.SolidTrace.CallsTo(recorder.IssueCommentsPath); len(calls) != 0 {
		t.Errorf("a Monitor Run commented: %+v", calls)
	}
	if reports := reportEvents(env, id); len(reports) != 1 || reports[0]["report.cause_report"] != cause {
		t.Errorf("the Run's page has no cause report: %v", reports)
	}
}

func TestAnIssueRunWithNoCauseReportAndNoPRAddsNoComment(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Looked"), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	time.Sleep(500 * time.Millisecond)
	if calls := env.SolidTrace.CallsTo(recorder.IssueCommentsPath); len(calls) != 0 {
		t.Errorf("an Issue Run with no cause report commented: %+v", calls)
	}
}

func TestTheCauseReportCommentIsSentAgainWhileSolidTraceIsDown(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	env.SolidTrace.RefuseIssueComments(http.StatusServiceUnavailable)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "done", "--summary", "Found the cause", "--cause-report", cause), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	waitUntil(t, "the Dispatcher tried two times", func() bool {
		return len(env.SolidTrace.CallsTo(recorder.IssueCommentsPath)) >= 2
	})
	env.SolidTrace.RefuseIssueComments(0)
	if c := oneIssueComment(t, env); c.RunID != id || !strings.Contains(c.Body, cause) {
		t.Errorf("the comment after SolidTrace is up: %+v", c)
	}
}

func TestTheCauseReportCommentNamesTheProjectByItsSlugAtTheTimeOfTheComment(t *testing.T) {
	env := factorytest.New(t, factorytest.Executions)
	shop := env.Project("shop")
	ana := env.Member("Ana")
	sender, path := env.Sender("Factory")
	env.SolidTrace.RefuseIssueComments(http.StatusServiceUnavailable)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "done", "--summary", "Found the cause", "--cause-report", cause), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	waitUntil(t, "the Dispatcher tried to comment", func() bool {
		return len(env.SolidTrace.CallsTo(recorder.IssueCommentsPath)) >= 1
	})
	// A person renames the Project in SolidTrace before the comment goes:
	// Console finds the Project by its slug of now only.
	shop.Slug = "store"
	env.SolidTrace.SetProject(factorytest.Organization.Slug, shop)
	env.SolidTrace.RefuseIssueComments(0)
	if c := oneIssueComment(t, env); c.Project != "store" || c.Issue != 7 || c.RunID != id {
		t.Errorf("the comment after the rename is on %s#%d for Run %q, want store#7 for %s", c.Project, c.Issue, c.RunID, id)
	}
}

func TestACommentOnAnIssueWhoseProjectIsGoneIsGivenUpWithAWarning(t *testing.T) {
	env := factorytest.New(t, factorytest.Executions)
	shop := env.Project("shop")
	ana := env.Member("Ana")
	sender, path := env.Sender("Factory")
	env.SolidTrace.RefuseIssueComments(http.StatusServiceUnavailable)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "done", "--summary", "Found the cause", "--cause-report", cause), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	waitUntil(t, "the Dispatcher tried to comment", func() bool {
		return len(env.SolidTrace.CallsTo(recorder.IssueCommentsPath)) >= 1
	})
	env.SolidTrace.DeleteProject(factorytest.Organization.Slug, shop.ID)
	calls := len(env.SolidTrace.CallsTo(recorder.IssueCommentsPath))
	env.SolidTrace.RefuseIssueComments(0)
	if warning := env.WaitForWarning("gave it up"); warning["factory.run_id"] != id {
		t.Errorf("the warning names the Run %v, not %s: %v", warning["factory.run_id"], id, warning)
	}
	time.Sleep(500 * time.Millisecond)
	if got := len(env.SolidTrace.CallsTo(recorder.IssueCommentsPath)); got > calls+1 {
		t.Errorf("a comment on a Project that is gone was sent %d more times", got-calls)
	}
	if comments := env.SolidTrace.IssueComments(); len(comments) != 0 {
		t.Errorf("a comment on a Project that is gone was added: %+v", comments)
	}
}

func TestACommentOnAnIssueThatIsGoneIsGivenUpWithAWarning(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	env.SolidTrace.RefuseIssueComments(http.StatusNotFound)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "done", "--summary", "Found the cause", "--cause-report", cause), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	warning := env.WaitForWarning("gave it up")
	if warning["factory.run_id"] != id {
		t.Errorf("the warning names the Run %v, not %s: %v", warning["factory.run_id"], id, warning)
	}
	time.Sleep(time.Second)
	if calls := env.SolidTrace.CallsTo(recorder.IssueCommentsPath); len(calls) != 1 {
		t.Errorf("a comment on an Issue that is gone was sent %d times", len(calls))
	}
}

// issueRunWithPR starts an Issue Run whose agent commits, opens a PR, runs
// the check command, and reports done with a cause report.
func issueRunWithPR(t *testing.T, check string) (*factorytest.Env, dispatcherapi.Run) {
	t.Helper()
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): `
[[checks]]
name = "cart-test"
command = "` + check + `"
`}, "A factory file")
	sender, path := env.Sender("Factory")
	out := t.TempDir()
	env.Runner.Agent(takeTask, initLine(t), commitStep,
		recorded(out, "open", `solidtrace pr --title "Fix the cart" --description "Round the sum."`),
		checkStep(out, "check"),
		reportStep("--outcome", "done", "--summary", "Fixed the rounding", "--cause-report", cause),
		reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.PRURL == "" {
		t.Fatalf("the Run has no PR: %s", readRecorded(t, out, "open"))
	}
	return env, run
}

func TestAnIssueRunThatOpensAPRCommentsOnTheIssueWithThePRAndThePassFailList(t *testing.T) {
	env, run := issueRunWithPR(t, "true")
	if len(run.Flags) != 0 {
		t.Errorf("a done with a PR and a passed check has the flags %q", run.Flags)
	}
	c := oneIssueComment(t, env)
	if c.RunID != run.ID || c.Issue != 7 {
		t.Errorf("the comment is for Run %q on Issue %d", c.RunID, c.Issue)
	}
	for _, want := range []string{run.PRURL, run.URL, "passed: cart-test", cause} {
		if !strings.Contains(c.Body, want) {
			t.Errorf("the PR-opened comment has no %q:\n%s", want, c.Body)
		}
	}
	if strings.Contains(c.Body, "no Evidence") {
		t.Errorf("the comment says no Evidence, but a check passed:\n%s", c.Body)
	}
}

func TestAnIssueRunWithAPRAndNoPassedCheckKeepsNoEvidenceAndItsCommentSaysSo(t *testing.T) {
	env, run := issueRunWithPR(t, "false")
	if !slices.Equal(run.Flags, []string{dispatcherapi.FlagNoEvidence}) {
		t.Errorf("flags %v, want only %q: a cause report does not remove it from a PR", run.Flags, dispatcherapi.FlagNoEvidence)
	}
	c := oneIssueComment(t, env)
	for _, want := range []string{run.PRURL, "failed: cart-test", "no Evidence"} {
		if !strings.Contains(c.Body, want) {
			t.Errorf("the PR-opened comment has no %q:\n%s", want, c.Body)
		}
	}
}
