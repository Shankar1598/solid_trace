package wrapper_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// A message, by the state of the Run (P4), for a Run with no live
// Execution. A running Run's message is in messages_test.go.

// continuedLine is what 'solidtrace message' prints for a message that
// queues the Run.
const continuedLine = "The Run is queued: a new Execution continues its Agent session with the message."

// sessionOf is the Run's Agent session id, from its first Execution's
// started event.
func sessionOf(t *testing.T, env *factorytest.Env, id string) string {
	t.Helper()
	started := eventsOf(env, id, "execution_started")
	if len(started) == 0 {
		t.Fatalf("the Run has no execution_started event: %v", env.RunEvents(id))
	}
	session, _ := started[0]["agent.session_id"].(string)
	if session == "" {
		t.Fatalf("the started event has no Agent session: %v", started[0])
	}
	return session
}

// sendContinued runs 'solidtrace message' for a Run that the message
// continues, and gives the message id.
func sendContinued(t *testing.T, env *factorytest.Env, p *factorytest.Person, id, text string) string {
	t.Helper()
	got := p.CLI("message", "--dispatcher", env.URL(), id, text)
	m := acceptedLine.FindStringSubmatch(got.Stdout)
	if got.Code != 0 || m == nil || !strings.Contains(got.Stdout, continuedLine) {
		t.Fatalf("solidtrace message: exit %d, want the accepted and continued lines\n%s%s", got.Code, got.Stdout,
			got.Stderr)
	}
	return m[1]
}

// acceptedEvent checks the message's accepted event: the Run is queued
// from the state from, and no Execution got the message yet.
func acceptedEvent(t *testing.T, env *factorytest.Env, id, message, from string) {
	t.Helper()
	ev := waitForMessageEvent(t, env, id, message, "message_accepted")
	if ev["run.state"] != dispatcherapi.StateQueued || ev["run.continued_from"] != from || ev["execution.id"] != nil {
		t.Errorf("the accepted event: state %v, continued from %v, Execution %v; want queued from %s and none",
			ev["run.state"], ev["run.continued_from"], ev["execution.id"], from)
	}
}

func TestAMessageToAQueuedRunIsInTheTaskOfItsFirstExecution(t *testing.T) {
	env, ana := newEnv(t)
	// No free account: the Run stays queued.
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(time.Hour))
	// The first turn gets the task and the message.
	env.Runner.Agent(takeTask, takeTask, initLine(t), done("Fixed both"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	time.Sleep(settle)

	message := send(t, env, ana, id, "Also fix the tax")
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateQueued || len(run.Executions) != 0 {
		t.Fatalf("the Run is %s with %d Executions, want queued with none", run.State, len(run.Executions))
	}
	accepted := waitForMessageEvent(t, env, id, message, "message_accepted")
	if accepted["run.state"] != dispatcherapi.StateQueued || accepted["execution.id"] != nil ||
		accepted["run.continued_from"] != nil {
		t.Errorf("the accepted event: %v", accepted)
	}

	env.Clock.Advance(time.Hour)
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	e := onlyExecution(t, run)
	if e.TaskDeliveredAt == nil {
		t.Errorf("the Execution did not deliver the task")
	}
	delivered := messageEvents(env, id, message)["message_delivered"]
	if len(delivered) != 1 || delivered[0]["execution.id"] != e.ID {
		t.Errorf("the delivered events: %v, want one in %s", delivered, e.ID)
	}
	if reports := eventsOf(env, id, "report"); len(reports) != 1 || reports[0]["report.delivered_messages"] != float64(2) {
		t.Errorf("want one report that counts the task and the message: %v", reports)
	}
	session := sessionOf(t, env, id)
	transcript := string(env.SolidTrace.Stored(session, session+".jsonl"))
	task, msg := strings.Index(transcript, "Fix the cart"), strings.Index(transcript, "Also fix the tax")
	if task < 0 || msg < task {
		t.Errorf("the transcript does not have the task and then the message:\n%s", transcript)
	}
	if env.SolidTrace.Downloads() != 0 {
		t.Errorf("the first Execution downloaded an Agent session")
	}
}

func TestAMessageToADoneRunResumesItsAgentSessionOnAnotherMachineOnTheRunsBranch(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	env.Runner.Agent(takeTask, initLine(t), commitStep, done("First"), reply, result, waitForEnd)
	// The second Execution's first message is the message. It records
	// its branch, the branch's commits and the file of the first
	// Execution.
	env.Runner.Agent(takeTask, shell(`git rev-parse --abbrev-ref HEAD > `+out+`/branch && git log --format=%s > `+
		out+`/log && cat cart.txt > `+out+`/cart`), done("Second"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	first := onlyExecution(t, waitFor(t, env, ana, id, dispatcherapi.StateDone))
	session := sessionOf(t, env, id)
	before := len(env.SolidTrace.Stored(session, session+".jsonl"))
	// SolidTrace has the last uploads, but needs a moment to give them.
	env.SolidTrace.DownloadsNotReady(1)

	message := sendContinued(t, env, ana, id, "Also fix the tax")
	acceptedEvent(t, env, id, message, dispatcherapi.StateDone)

	waitForExecutions(t, env, ana, id, 2)
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if len(run.Executions) != 2 || run.Executions[0].ID != first.ID || run.Summary != "Second" || run.Reason != "" {
		t.Fatalf("the Run is done with %q (%q) in %+v", run.Summary, run.Reason, run.Executions)
	}
	second := run.Executions[1]
	if second.TaskDeliveredAt != nil {
		t.Errorf("the second Execution wrote the task again")
	}
	if delivered := messageEvents(env, id, message)["message_delivered"]; len(delivered) != 1 ||
		delivered[0]["execution.id"] != second.ID {
		t.Errorf("the delivered events: %v, want one in %s", delivered, second.ID)
	}
	if reports := eventsOf(env, id, "report"); len(reports) != 2 || reports[1]["report.delivered_messages"] != float64(1) {
		t.Errorf("the second report must count the message only: %v", reports)
	}

	// The same Agent session continues: its transcript has the task one
	// time, then the message, and the uploads went on from the end of
	// the restored bytes, in the same version.
	if env.SolidTrace.Downloads() != 1 {
		t.Errorf("%d downloads, want 1", env.SolidTrace.Downloads())
	}
	transcript := string(env.SolidTrace.Stored(session, session+".jsonl"))
	if strings.Count(transcript, "Fix the cart") != 1 || !strings.Contains(transcript, "Also fix the tax") ||
		len(transcript) <= before {
		t.Errorf("the transcript of the Agent session:\n%s", transcript)
	}
	continued := false
	for _, up := range env.SolidTrace.Uploads() {
		if up.Session != session || up.Path != session+".jsonl" {
			continue
		}
		if up.Version != 1 {
			t.Errorf("an upload of the transcript has version %d: the restored file differs", up.Version)
		}
		continued = continued || up.Offset >= uint64(before)
	}
	if !continued {
		t.Errorf("no upload of the transcript goes on from the restored %d bytes", before)
	}

	// The second Execution checked out the Run's branch with the first
	// Execution's commit.
	if b := readFile(t, out, "branch"); b != "agent/"+id {
		t.Errorf("the second Execution is on %q, want agent/%s", b, id)
	}
	if log := readFile(t, out, "log"); !strings.HasPrefix(log, "Fix the cart") {
		t.Errorf("the branch's commits:\n%s", log)
	}
	if c := readFile(t, out, "cart"); c != "fixed" {
		t.Errorf("cart.txt is %q", c)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestAMessageContinuesARunThatWaitsOnAPersonFailedOrStopped(t *testing.T) {
	for name, c := range map[string]struct {
		state string
		first []string
		// then makes the Run reach state after the agent reached here.
		then func(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string)
	}{
		"waits on a person": {dispatcherapi.StateWaitingOnPerson, []string{
			reportStep("--outcome", "needs-a-person", "--summary", "Two fixes", "--question", "Which one?"),
			`{"fake":"await_message","eof_code":0}`}, nil},
		"failed": {dispatcherapi.StateFailed, []string{
			reportStep("--outcome", "failed", "--summary", "The tax table is missing"), reply, result, waitForEnd}, nil},
		"stopped": {dispatcherapi.StateStopped, []string{inToolCall, interrupted, thenEOF},
			func(t *testing.T, env *factorytest.Env, p *factorytest.Person, id string) {
				if got := p.CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
					t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
				}
			}},
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			here, reached := marker(t)
			env.Runner.Agent(append([]string{takeTask, initLine(t), here}, c.first...)...)
			env.Runner.Agent(takeTask, done("Used the second fix"), reply, result, waitForEnd)
			id := start(t, env, ana, "Fix the cart")
			reached()
			if c.then != nil {
				c.then(t, env, ana, id)
			}
			waitFor(t, env, ana, id, c.state)

			message := sendContinued(t, env, ana, id, "Use the second fix")
			acceptedEvent(t, env, id, message, c.state)
			waitForExecutions(t, env, ana, id, 2)
			run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
			if len(run.Executions) != 2 || run.Summary != "Used the second fix" || run.Question != "" {
				t.Fatalf("the Run is done with %q (question %q) in %+v", run.Summary, run.Question, run.Executions)
			}
			if run.Executions[1].TaskDeliveredAt != nil || env.SolidTrace.Downloads() != 1 {
				t.Errorf("the second Execution did not resume the Agent session: task delivered %v, %d downloads",
					run.Executions[1].TaskDeliveredAt, env.SolidTrace.Downloads())
			}
			if delivered := messageEvents(env, id, message)["message_delivered"]; len(delivered) != 1 ||
				delivered[0]["execution.id"] != run.Executions[1].ID {
				t.Errorf("the delivered events: %v", delivered)
			}
		})
	}
}

func TestAMessageToARunStoppedBeforeItsFirstExecutionStartsItWithTheTaskAndTheMessage(t *testing.T) {
	env, ana := newEnv(t)
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(time.Hour))
	env.Runner.Agent(takeTask, takeTask, initLine(t), done("Fixed both"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	if got := ana.CLI("stop", "--dispatcher", env.URL(), id); got.Code != 0 {
		t.Fatalf("solidtrace stop: exit %d: %s", got.Code, got.Stderr)
	}
	waitFor(t, env, ana, id, dispatcherapi.StateStopped)

	message := sendContinued(t, env, ana, id, "Also fix the tax")
	acceptedEvent(t, env, id, message, dispatcherapi.StateStopped)
	env.Clock.Advance(time.Hour)
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	// No Execution started before: this one starts the Agent session.
	if e := onlyExecution(t, run); e.TaskDeliveredAt == nil {
		t.Errorf("the first Execution did not write the task")
	}
	if reports := eventsOf(env, id, "report"); len(reports) != 1 || reports[0]["report.delivered_messages"] != float64(2) {
		t.Errorf("want one report that counts the task and the message: %v", reports)
	}
	if env.SolidTrace.Downloads() != 0 {
		t.Errorf("a first Execution downloaded an Agent session")
	}
}

func TestWithNoAgentSessionToResumeTheNewExecutionStartsItAgainWithTheTaskAndTheMessage(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, env *factorytest.Env, p *factorytest.Person) string{
		// The clone failed: the harness never started.
		"no Agent session": func(t *testing.T, env *factorytest.Env, p *factorytest.Person) string {
			env.Forge.RefuseGit("example/shop")
			id := start(t, env, p, "Fix the cart")
			waitFor(t, env, p, id, dispatcherapi.StateFailed)
			env.Forge.AcceptGit("example/shop")
			return id
		},
		// Retention dropped the start of the transcript.
		"a partial transcript": func(t *testing.T, env *factorytest.Env, p *factorytest.Person) string {
			env.Runner.Agent(takeTask, initLine(t), done("First"), reply, result, waitForEnd)
			id := start(t, env, p, "Fix the cart")
			waitFor(t, env, p, id, dispatcherapi.StateDone)
			session := sessionOf(t, env, id)
			env.SolidTrace.FlagFile(session, session+".jsonl", "partial")
			return id
		},
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			id := setup(t, env, ana)
			// The agent gets the task again, then the message.
			env.Runner.Agent(takeTask, takeTask, initLine(t), done("Fixed both"), reply, result, waitForEnd)
			message := sendContinued(t, env, ana, id, "Also fix the tax")
			waitForExecutions(t, env, ana, id, 2)
			run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
			if len(run.Executions) != 2 || run.Summary != "Fixed both" {
				t.Fatalf("the Run is done with %q in %+v", run.Summary, run.Executions)
			}
			if run.Executions[1].TaskDeliveredAt == nil {
				t.Errorf("the new Execution did not write the task")
			}
			if delivered := messageEvents(env, id, message)["message_delivered"]; len(delivered) != 1 ||
				delivered[0]["execution.id"] != run.Executions[1].ID {
				t.Errorf("the delivered events: %v", delivered)
			}
		})
	}
}

func TestAMessageRestartsTheThirtyDaysOfActivity(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), done("First"), reply, result, waitForEnd)
	env.Runner.Agent(takeTask, done("Second"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	// One hour before the close, a reviewer's message continues the Run.
	// (Ana's sign-in of 30 days ago has ended.)
	env.Clock.Advance(closeAfter - time.Hour)
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "1 hour before 30 days")
	sendContinued(t, env, env.Member("Ben"), id, "Also fix the tax")
	waitForExecutions(t, env, ana, id, 2)
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	env.Clock.Advance(2 * time.Hour)
	env.CheckClosedRuns()
	notClosed(t, env, ana, id, "30 days after the first Execution, with a message since")

	env.Clock.Advance(closeAfter)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, "", "no_activity")
}

func TestAMessageToARunWhosePRIsMergedIsRefusedAndSaysWhy(t *testing.T) {
	env, ana := newEnv(t)
	id, pr := runWithPR(t, env, ana)
	env.Forge.SetState("example/shop", pr.ID, "MERGED")
	env.Clock.Advance(prCheckEvery)
	env.CheckClosedRuns()
	closedEvent(t, env, id, dispatcherapi.StateDone, dispatcherapi.PRMerged, "pr_merged")

	got := ana.API("POST", "/api/v1/runs/"+id+"/messages", dispatcherapi.SendMessage{Text: "Also fix the tax"})
	if got.Status != http.StatusConflict || !strings.Contains(string(got.Body), "the Run is closed") ||
		!strings.Contains(string(got.Body), "start a new Run") {
		t.Errorf("a message to a Run whose PR is merged: %d %s, want 409 that says it is closed", got.Status, got.Body)
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateDone || len(run.Executions) != 1 {
		t.Errorf("the Run is %s with %d Executions, want done with one", run.State, len(run.Executions))
	}
	if accepted := eventsOf(env, id, "message_accepted"); len(accepted) != 0 {
		t.Errorf("a refused message is accepted: %v", accepted)
	}
}

func TestARestoreThatSolidTraceCannotGiveEndsTheExecutionBeforeTheHarnessAndTheMessageGoesAgain(t *testing.T) {
	for name, c := range map[string]struct {
		fails int
		state string
	}{
		// The next Execution gets the message, and resumes.
		"one time": {1, dispatcherapi.StateDone},
		// The message went to two Executions: the Run fails.
		"each time": {2, dispatcherapi.StateFailed},
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			env.Runner.Agent(takeTask, initLine(t), done("First"), reply, result, waitForEnd)
			// Each Execution takes a script. Only an Execution that
			// restored the Agent session starts the harness.
			for range c.fails {
				env.Runner.Agent(takeTask, done("Not restored"), reply, result, waitForEnd)
			}
			env.Runner.Agent(takeTask, done("Second"), reply, result, waitForEnd)
			id := start(t, env, ana, "Fix the cart")
			waitFor(t, env, ana, id, dispatcherapi.StateDone)
			env.SolidTrace.DownloadsFail(c.fails)

			message := sendContinued(t, env, ana, id, "Also fix the tax")
			waitForExecutions(t, env, ana, id, 3)
			run := waitFor(t, env, ana, id, c.state)
			if len(run.Executions) != 3 {
				t.Fatalf("the Run is %s with %d Executions, want 3: %+v", run.State, len(run.Executions),
					run.Executions)
			}
			for _, e := range run.Executions[1 : 1+c.fails] {
				if !strings.HasPrefix(e.EndReason, dispatcherapi.EndSetupFailed+": ") ||
					!strings.Contains(e.EndReason, "restore the Agent session") || e.TaskDeliveredAt != nil {
					t.Errorf("the Execution %s ended with %q (task delivered %v), want the failed restore",
						e.ID, e.EndReason, e.TaskDeliveredAt)
				}
			}
			delivered := messageEvents(env, id, message)["message_delivered"]
			if c.state == dispatcherapi.StateDone {
				last := run.Executions[2]
				if run.Summary != "Second" || last.TaskDeliveredAt != nil || env.SolidTrace.Downloads() != 1 {
					t.Errorf("the last Execution did not resume the Agent session: %q, task delivered %v, "+
						"%d downloads", run.Summary, last.TaskDeliveredAt, env.SolidTrace.Downloads())
				}
				if len(delivered) != 1 || delivered[0]["execution.id"] != last.ID {
					t.Errorf("the delivered events: %v, want one in %s", delivered, last.ID)
				}
				return
			}
			if len(delivered) != 0 || env.SolidTrace.Downloads() != 0 {
				t.Errorf("no Execution restored the Agent session, but %d delivered events and %d downloads",
					len(delivered), env.SolidTrace.Downloads())
			}
			if !strings.Contains(run.Reason, "restore the Agent session") {
				t.Errorf("the failed Run's reason is %q, want the failed restore", run.Reason)
			}
		})
	}
}
