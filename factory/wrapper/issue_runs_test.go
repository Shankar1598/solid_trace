package wrapper_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// The attributes that Console's 'Factory' Integration puts on a delivery
// (Notifiers::FactoryNotifier).
func issueAttributes(number string) map[string]string {
	return map[string]string{"solidtrace.notification": "issue_created", "solidtrace.issue_number": number}
}

func monitorAttributes(id string) map[string]string {
	return map[string]string{"solidtrace.notification": "monitor_firing", "solidtrace.monitor_id": id,
		"solidtrace.monitor_group": "service=api"}
}

// factoryDelivery sends a signed delivery for the Project shop with the
// attributes, as Console's 'Factory' Integration does, and gives the
// Run's id.
func factoryDelivery(t *testing.T, env *factorytest.Env, path, secret, delivery string, attributes map[string]string) string {
	t.Helper()
	raw, _ := json.Marshal(dispatcherapi.GenericWebhook{Project: "shop", Prompt: "SolidTrace Issue #7: the cart total is wrong",
		Title: "The cart total is wrong", DedupeKey: "solid-trace-" + delivery, Attributes: attributes})
	a := env.Webhook(path, factorytest.GenericDelivery(secret, delivery, env.Clock.Now(), raw), raw)
	if a.Status != http.StatusAccepted {
		t.Fatalf("the delivery %s: %d %s, want 202", delivery, a.Status, a.Body)
	}
	var answer dispatcherapi.GenericWebhookAnswer
	a.JSON(t, &answer)
	return answer.RunID
}

// standingTask is a sentence of the standing task that the Dispatcher
// adds to an Issue or a Monitor Run's first message.
const standingTask = "Open a PR only when a check proves the fix"

// firstMessage is the transcript that the Run's first Execution uploaded.
func uploadedTranscript(t *testing.T, env *factorytest.Env) []byte {
	t.Helper()
	session := firstToken(t, env).AgentSessionIDs[0]
	return env.SolidTrace.Stored(session, session+".jsonl")
}

func TestAnIssueOrAMonitorDeliveryStartsARunWhoseFirstMessageHasTheStandingTask(t *testing.T) {
	for name, attributes := range map[string]map[string]string{
		"Issue": issueAttributes("7"), "Monitor": monitorAttributes("12"),
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			sender, path := env.Sender("Factory")
			env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Looked"), reply, result,
				waitForEnd)
			id := factoryDelivery(t, env, path, sender.Secret, "d-1", attributes)
			waitFor(t, env, ana, id, dispatcherapi.StateDone)
			transcript := uploadedTranscript(t, env)
			if !bytes.Contains(transcript, []byte(standingTask)) || !bytes.Contains(transcript, []byte("the cart total is wrong")) {
				t.Errorf("the first message has no standing task, or not the delivery's prompt:\n%s", transcript)
			}
		})
	}
}

func TestADeliveryWithNoIssueOrMonitorGetsNoStandingTask(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Alerts")
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd)
	// The notification attribute alone names no Issue and no Monitor.
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", map[string]string{"solidtrace.notification": "issue_created"})
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if transcript := uploadedTranscript(t, env); bytes.Contains(transcript, []byte(standingTask)) {
		t.Errorf("a plain delivery got the standing task:\n%s", transcript)
	}
}

// keepMCPToken is a step that writes, to the file kept, the path of the
// MCP token's file, the MCP server's URL, the file's mode and the token,
// one on each line, as the run user sees them. It fails when the run user
// can change the file.
func keepMCPToken(kept string) string {
	return shell(`f="$` + dispatcherapi.EnvMCPTokenFile + `" && test -n "$f" && ` +
		`{ printf '%s\n' "$f" "$` + dispatcherapi.EnvMCPURL + `" "$(stat -c %a "$f")"; cat "$f"; } > ` + kept +
		` && ! test -w "$f"`)
}

// mcpTokens are the child tokens with mcp:read, oldest first.
func mcpTokens(env *factorytest.Env) []recorder.ChildToken {
	var found []recorder.ChildToken
	for _, tok := range env.SolidTrace.ChildTokens() {
		if slices.Contains(tok.Scopes, "mcp:read") {
			found = append(found, tok)
		}
	}
	return found
}

// mcpCall calls the recorder's MCP server with token, as the agent's MCP
// client does, and gives the status.
func mcpCall(t *testing.T, env *factorytest.Env, token string) int {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_projects","arguments":{"organization":"` +
		factorytest.Organization.Slug + `"}}}`
	req, _ := http.NewRequest("POST", env.SolidTrace.URL()+"/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestAnIssueOrAMonitorRunsMCPTokenReadsOnlyItsProjectAndStopsAtTheEndOfTheExecution(t *testing.T) {
	for name, attributes := range map[string]map[string]string{
		"Issue": issueAttributes("7"), "Monitor": monitorAttributes("12"),
	} {
		t.Run(name, func(t *testing.T) {
			env := factorytest.New(t, factorytest.Executions)
			shop := env.Project("shop")
			ana := env.Member("Ana")
			sender, path := env.Sender("Factory")
			kept := filepath.Join(t.TempDir(), "kept")
			env.Runner.Agent(takeTask, initLine(t), keepMCPToken(kept),
				reportStep("--outcome", "done", "--summary", "Looked"), reply, result, waitForEnd)
			started := env.Clock.Now()
			id := factoryDelivery(t, env, path, sender.Secret, "d-1", attributes)
			waitFor(t, env, ana, id, dispatcherapi.StateDone)
			allRevoked(t, env)

			sessions, mcp := firstToken(t, env), mcpTokens(env)
			if len(mcp) != 1 || len(env.SolidTrace.ChildTokens()) != 2 {
				t.Fatalf("want one Execution token and one MCP token, got %+v", env.SolidTrace.ChildTokens())
			}
			tok := mcp[0]
			if !slices.Equal(tok.Scopes, []string{"mcp:read"}) || tok.ProjectID != strconv.FormatInt(shop.ID, 10) ||
				tok.AgentSessionIDs != nil {
				t.Errorf("the MCP token has scopes %v, Project %q, Agent sessions %v; want mcp:read for the Project %d only",
					tok.Scopes, tok.ProjectID, tok.AgentSessionIDs, shop.ID)
			}
			// The Execution's expiry: the total limit (8 hours) plus 1 hour.
			if want := started.Add(9 * time.Hour); !tok.ExpiresAt.Equal(want) || !tok.ExpiresAt.Equal(sessions.ExpiresAt) {
				t.Errorf("the MCP token expires at %s, not %s", tok.ExpiresAt, want)
			}
			if !tok.Revoked {
				t.Error("the end step did not revoke the MCP token")
			}
			if status := mcpCall(t, env, tok.Secret); status != http.StatusUnauthorized {
				t.Errorf("the MCP token after the end: %d, want 401", status)
			}

			b, err := os.ReadFile(kept)
			if err != nil {
				t.Fatalf("the agent could not read its MCP token: %v\nwrapper output:\n%s", err,
					env.Runner.Fake.Output(env.Runner.Dispatched()[0]))
			}
			lines := strings.Split(string(b), "\n")
			if len(lines) < 4 {
				t.Fatalf("the agent kept %q", b)
			}
			file, url, mode, secret := lines[0], lines[1], lines[2], lines[3]
			if secret != tok.Secret || secret == sessions.Secret {
				t.Errorf("the run user's token file holds %q, not the MCP token", secret)
			}
			if mode != "440" {
				t.Errorf("the token file's mode is %s, not 440", mode)
			}
			if url != env.SolidTrace.URL()+"/mcp" {
				t.Errorf("the MCP server's URL is %q", url)
			}
			workspace := env.Runner.Workspace(env.Runner.Dispatched()[0])
			if !filepath.IsAbs(file) || strings.HasPrefix(file, workspace+"/") {
				t.Errorf("the token file %s is in the workspace %s", file, workspace)
			}
		})
	}
}

func TestEachExecutionOfAnIssueRunGetsItsOwnMCPToken(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	// The second Execution resumes the Agent session on the same machine.
	env.Runner.KeepAgentSessions()
	here, reached := marker(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), here, wait, reportStep("--outcome", "done", "--summary", "First"),
		reply, result, waitForEnd)
	env.Runner.Agent(takeTask, reportStep("--outcome", "done", "--summary", "Second"), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	reached()
	// After the wrapper's compare-and-end call agreed, a message is kept
	// for a new Execution.
	execution := onlyExecution(t, getRun(t, ana, id)).ID
	if answer := compareEnd(t, env, execution, 0); !answer.Agreed {
		t.Fatalf("compare-end: %+v", answer)
	}
	send(t, env, ana, id, "Also look at the tax")
	// Less than the idle limit (30 minutes) of the first Execution.
	env.Clock.Advance(10 * time.Minute)
	second := env.Clock.Now()
	open()
	waitForExecutions(t, env, ana, id, 2)
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	allRevoked(t, env)

	mcp := mcpTokens(env)
	if len(mcp) != 2 {
		t.Fatalf("want an MCP token for each of the two Executions, got %+v", mcp)
	}
	if mcp[0].Secret == mcp[1].Secret || !mcp[1].ExpiresAt.Equal(second.Add(9*time.Hour)) {
		t.Errorf("the second Execution's MCP token is not its own: %+v", mcp)
	}
}

func TestARunThatAnIssueDidNotStartGetsNoMCPToken(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		shell(`test -z "$`+dispatcherapi.EnvMCPTokenFile+`" && solidtrace report --outcome done --summary "No MCP"`),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	if run := waitFor(t, env, ana, id, dispatcherapi.StateDone); run.Summary != "No MCP" {
		t.Errorf("the person's Run: summary %q, reason %q", run.Summary, run.Reason)
	}
	if mcp := mcpTokens(env); len(mcp) != 0 {
		t.Errorf("a person's Run got an MCP token: %+v", mcp)
	}
}

const cause = "The cart rounds each line before the sum: see the Log of 10:02 and cart.go line 40."

func TestADoneIssueOrMonitorRunWithACauseReportAndNoPRShowsCauseReportAndNeitherFlag(t *testing.T) {
	for name, attributes := range map[string]map[string]string{
		"Issue": issueAttributes("7"), "Monitor": monitorAttributes("12"),
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			sender, path := env.Sender("Factory")
			env.Runner.Agent(takeTask, initLine(t),
				reportStep("--outcome", "done", "--summary", "Found the cause", "--cause-report", cause), reply, result, waitForEnd)
			id := factoryDelivery(t, env, path, sender.Secret, "d-1", attributes)
			run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
			if !slices.Equal(run.Flags, []string{dispatcherapi.FlagCauseReport}) {
				t.Errorf("flags %v, want only %q", run.Flags, dispatcherapi.FlagCauseReport)
			}
			reports := reportEvents(env, id)
			if len(reports) != 1 || reports[0]["report.cause_report"] != cause {
				t.Errorf("the report events: %v", reports)
			}
			session := firstToken(t, env).AgentSessionIDs[0]
			if md := env.SolidTrace.Stored(session, "evidence/report.md"); !bytes.Contains(md, []byte("## Cause report\n\n"+cause)) {
				t.Errorf("evidence/report.md has no cause report:\n%s", md)
			}
		})
	}
}

func TestADoneIssueRunWithNoCauseReportKeepsTheUsualFlags(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Looked"), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if want := []string{dispatcherapi.FlagNoEvidence, dispatcherapi.FlagNoPR}; !slices.Equal(run.Flags, want) {
		t.Errorf("flags %v, want %v", run.Flags, want)
	}
}

func TestTheDispatcherRefusesACauseReportThatDoesNotFit(t *testing.T) {
	long := strings.Repeat("x", 64<<10+1)
	for name, c := range map[string]struct {
		issue bool
		args  []string
	}{
		"a person's Run":   {false, []string{"--outcome", "done", "--summary", "S", "--cause-report", cause}},
		"needs a person":   {true, []string{"--outcome", "needs-a-person", "--question", "Q", "--cause-report", cause}},
		"failed":           {true, []string{"--outcome", "failed", "--summary", "S", "--cause-report", cause}},
		"more than 64 KiB": {true, []string{"--outcome", "done", "--summary", "S", "--cause-report", long}},
	} {
		t.Run(name, func(t *testing.T) {
			env, ana := newEnv(t)
			refused := filepath.Join(t.TempDir(), "refused")
			// The refused report's error goes to the file refused. It changes
			// nothing, and a report after it counts.
			step, _ := json.Marshal(map[string]any{"fake": "run", "args": append([]string{"sh", "-c",
				`"$@" 2> ` + refused, "sh", "solidtrace", "report"}, c.args...)})
			env.Runner.Agent(takeTask, initLine(t), string(step), reportStep("--outcome", "done", "--summary", "Second"),
				reply, result, waitForEnd)
			var id string
			if c.issue {
				sender, path := env.Sender("Factory")
				id = factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
			} else {
				id = start(t, env, ana, "Fix the cart")
			}
			run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
			stderr, _ := os.ReadFile(refused)
			if run.Summary != "Second" || !strings.Contains(string(stderr), "cause report") {
				t.Errorf("summary %q; the refused report printed %q", run.Summary, stderr)
			}
			for _, ev := range reportEvents(env, id) {
				if ev["report.cause_report"] != nil && ev["report.cause_report"] != "" {
					t.Errorf("a refused cause report is in a report event: %v", ev)
				}
			}
		})
	}
}

func TestAnIssueRunFailsWhenSolidTraceRefusesItsMCPToken(t *testing.T) {
	env, ana := newEnv(t)
	sender, path := env.Sender("Factory")
	env.SolidTrace.RefuseChildTokensWith("mcp:read", http.StatusUnprocessableEntity)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Looked"), reply, result, waitForEnd)
	id := factoryDelivery(t, env, path, sender.Secret, "d-1", issueAttributes("7"))
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if !strings.Contains(run.Reason, "MCP token") {
		t.Errorf("the Run failed with %q, not a reason that names the MCP token", run.Reason)
	}
	// The Execution's token, made first, does not stay live.
	allRevoked(t, env)
	if len(env.SolidTrace.ChildTokens()) != 1 {
		t.Errorf("want only the Execution's token, got %+v", env.SolidTrace.ChildTokens())
	}
}
