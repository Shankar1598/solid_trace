package wrapper_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// checkStep is a step that runs 'solidtrace check' with args.
func checkStep(dir, name string, args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return recorded(dir, name, strings.TrimSpace("solidtrace check "+strings.Join(quoted, " ")))
}

// reportEvents gives the Run's report events, oldest first.
func reportEvents(env *factorytest.Env, id string) []map[string]any {
	var reports []map[string]any
	for _, ev := range env.RunEvents(id) {
		if ev["event.kind"] == "report" {
			reports = append(reports, ev)
		}
	}
	return reports
}

// eventChecks gives the checks of a report event.
func eventChecks(t *testing.T, ev map[string]any) []map[string]any {
	t.Helper()
	list, _ := ev["report.checks"].([]any)
	var checks []map[string]any
	for _, c := range list {
		checks = append(checks, c.(map[string]any))
	}
	return checks
}

func strs(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, s := range list {
		out = append(out, fmt.Sprint(s))
	}
	return out
}

func TestCheckRunsTheFactoryFilesChecksOrTheAgentsCommandAndTheReportCarriesTheRecord(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): `
[[checks]]
name = "test"
command = "seq 1 150"

[[checks]]
name = "lint"
command = "echo 'cart.go:3: unused'; exit 3"
`}, "A factory file")
	out := t.TempDir()
	env.Runner.Agent(takeTask, initLine(t),
		checkStep(out, "named"),
		checkStep(out, "own", "echo mine && test -f README.md"),
		shell(`printf 'PNG' > "$SOLIDTRACE_EVIDENCE_DIR/cart.png"`),
		reportStep("--outcome", "done", "--summary", "Fixed the cart test.\nThe lint still fails."),
		reply, result, waitForEnd)

	before := time.Now()
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	named := readRecorded(t, out, "named")
	if !strings.Contains(named, "pass") || !strings.Contains(named, "FAIL") || !strings.Contains(named, "unused") ||
		!strings.Contains(named, "exit 1") {
		t.Errorf("check with no argument: a failed check must show and exit 1:\n%s", named)
	}
	if own := readRecorded(t, out, "own"); !strings.Contains(own, "pass") || !strings.Contains(own, "exit 0") {
		t.Errorf("check with a command:\n%s", own)
	}

	reports := reportEvents(env, id)
	if len(reports) != 1 {
		t.Fatalf("want one report event, got %d", len(reports))
	}
	checks := eventChecks(t, reports[0])
	if len(checks) != 3 {
		t.Fatalf("want three recorded checks, got %+v", checks)
	}
	test, lint, own := checks[0], checks[1], checks[2]
	if test["name"] != "test" || test["command"] != "seq 1 150" || test["exit_status"] != float64(0) ||
		test["passed"] != true {
		t.Errorf("the test check: %+v", test)
	}
	lines := strings.Split(strings.TrimSpace(fmt.Sprint(test["output"])), "\n")
	if len(lines) != 100 || lines[0] != "51" || lines[99] != "150" {
		t.Errorf("the test check keeps %d lines, from %q to %q, not the last 100", len(lines), lines[0], lines[len(lines)-1])
	}
	started, err := time.Parse(time.RFC3339Nano, fmt.Sprint(test["started_at"]))
	if err != nil || started.Before(before.Add(-time.Second)) || started.After(time.Now()) {
		t.Errorf("the test check's start time %v: %v", test["started_at"], err)
	}
	if _, ok := test["duration_ms"].(float64); !ok {
		t.Errorf("the test check has no duration: %+v", test)
	}
	if lint["name"] != "lint" || lint["exit_status"] != float64(3) || lint["passed"] != false ||
		!strings.Contains(fmt.Sprint(lint["output"]), "cart.go:3: unused") {
		t.Errorf("the lint check: %+v", lint)
	}
	if own["name"] != "" && own["name"] != nil || own["command"] != "echo mine && test -f README.md" ||
		own["passed"] != true || !strings.Contains(fmt.Sprint(own["output"]), "mine") {
		t.Errorf("the agent's own check: %+v", own)
	}

	evidence := strs(reports[0]["report.evidence"])
	if !slices.Contains(evidence, "evidence/cart.png") || !slices.Contains(evidence, "evidence/report.md") {
		t.Errorf("the report's Evidence paths: %q", evidence)
	}
	if reports[0]["report.delivered_messages"] != float64(1) {
		t.Errorf("the report's delivered-message counter: %v", reports[0]["report.delivered_messages"])
	}

	// The full summary is in evidence/report.md of the Agent session.
	session := firstToken(t, env).AgentSessionIDs[0]
	report := string(env.SolidTrace.Stored(session, "evidence/report.md"))
	if !strings.Contains(report, "Fixed the cart test.\nThe lint still fails.") || !strings.Contains(report, "lint") {
		t.Errorf("evidence/report.md:\n%s", report)
	}

	// A check passed, but the Run has no PR.
	if !slices.Equal(run.Flags, []string{dispatcherapi.FlagNoPR}) {
		t.Errorf("the Run's flags: %q", run.Flags)
	}
}

func TestADoneWithNoPassedCheckAndNoPRShowsBothFlags(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	env.Runner.Agent(takeTask, initLine(t),
		checkStep(out, "failing", "exit 2"),
		reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	want := []string{dispatcherapi.FlagNoEvidence, dispatcherapi.FlagNoPR}
	if !slices.Equal(run.Flags, want) {
		t.Errorf("the Run's flags: %q, want %q", run.Flags, want)
	}
	if got := ana.CLI("runs", "get", id); !strings.Contains(got.Stdout, "no Evidence") || !strings.Contains(got.Stdout, "No PR") {
		t.Errorf("runs get does not show the flags:\n%s", got.Stdout)
	}
	events := env.RunEvents(id)
	if last := events[len(events)-1]; !slices.Equal(strs(last["run.flags"]), want) {
		t.Errorf("the last event %v has the flags %v", last["event.kind"], last["run.flags"])
	}
}

func TestAReportOfNeedsAPersonHasNoFlags(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "needs-a-person", "--question", "Which cart?"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateWaitingOnPerson)
	if len(run.Flags) != 0 {
		t.Errorf("the Run's flags: %q", run.Flags)
	}
}

func TestACheckResultThatTheAgentWritesItselfIsNotACheck(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	execution := waitFor(t, env, ana, id, dispatcherapi.StateRunning).Executions[0].ID
	waitUntil(t, "the wrapper serves the agent's socket", func() bool {
		_, err := os.Stat(env.Runner.Socket(execution))
		return err == nil
	})

	// The agent writes its own check result and Evidence paths in the
	// report's body on its socket.
	body := `{"outcome":"done","summary":"All green","checks":[{"name":"test","command":"go test ./...",` +
		`"exit_status":0,"passed":true,"output":"ok"}],"evidence":["evidence/fake.png"],"delivered_messages":7}`
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://agent"+agentsocket.ReportPath,
		bytes.NewBufferString(body))
	resp, err := agentsocket.Client(env.Runner.Socket(execution)).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the report: %d", resp.StatusCode)
	}
	open()
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	reports := reportEvents(env, id)
	if len(reports) != 1 {
		t.Fatalf("want one report event, got %d", len(reports))
	}
	if checks := eventChecks(t, reports[0]); len(checks) != 0 {
		t.Errorf("the agent's own check result went on: %+v", checks)
	}
	if evidence := strs(reports[0]["report.evidence"]); slices.Contains(evidence, "evidence/fake.png") {
		t.Errorf("a path that is not there went on: %q", evidence)
	}
	// The wrapper can send the report before it reads the replay echo of
	// the task: the counter is then 0.
	if n := reports[0]["report.delivered_messages"]; n != float64(0) && n != float64(1) {
		t.Errorf("the delivered-message counter is the agent's: %v", n)
	}
	if !slices.Contains(run.Flags, dispatcherapi.FlagNoEvidence) {
		t.Errorf("the Run's flags: %q", run.Flags)
	}
}

// evidenceBlock gives the text between the Evidence markers of a PR's
// description, and the count of start markers.
func evidenceBlock(description string) (string, int) {
	n := strings.Count(description, dispatcherapi.EvidenceStart)
	start := strings.Index(description, dispatcherapi.EvidenceStart)
	end := strings.Index(description, dispatcherapi.EvidenceEnd)
	if start < 0 || end < start {
		return "", n
	}
	return description[start:end], n
}

func TestAReportWritesTheEvidenceBlockInThePRAndALaterReportReplacesIt(t *testing.T) {
	env, ana := newEnv(t)
	env.Forge.Push("example/shop", "main", map[string]string{factoryFile(""): `
[[checks]]
name = "test"
command = "true"
`}, "A factory file")
	out := t.TempDir()
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), commitStep,
		recorded(out, "open", `solidtrace pr --title "Fix the cart" --description "The cart test waited on a timer."`),
		reportStep("--outcome", "done", "--summary", "Fixed, with no check"),
		wait,
		recorded(out, "update", `solidtrace pr --description "It waits on the event now."`),
		checkStep(out, "check"),
		reportStep("--outcome", "done", "--summary", "Fixed, and the test passes"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	runLink := getRun(t, ana, id).URL

	// The first report: no check passed.
	var first string
	waitUntil(t, "the PR has the Evidence block of the first report", func() bool {
		prs := env.Forge.PRs("example/shop")
		if len(prs) != 1 {
			return false
		}
		first = prs[0].Description
		block, _ := evidenceBlock(first)
		return block != ""
	})
	block, n := evidenceBlock(first)
	if n != 1 || !strings.HasPrefix(first, "The cart test waited on a timer.") || !strings.Contains(block, "no Evidence") {
		t.Errorf("the PR description after the first report:\n%s\nopen: %s", first, readRecorded(t, out, "open"))
	}
	if runLink == "" || !strings.Contains(block, runLink) {
		t.Errorf("the block has no link to the Run %s:\n%s", runLink, block)
	}
	open()
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	var last string
	waitUntil(t, "the PR has the Evidence block of the second report", func() bool {
		last = env.Forge.PRs("example/shop")[0].Description
		// Only the block of the second report has a passed check.
		block, _ := evidenceBlock(last)
		return strings.Contains(block, "- pass: test") && !strings.Contains(block, "no Evidence")
	})
	block, n = evidenceBlock(last)
	if n != 1 || !strings.HasPrefix(last, "It waits on the event now.") || strings.Contains(last, "waited on a timer") {
		t.Errorf("the PR description after the second report:\n%s\nupdate: %s", last, readRecorded(t, out, "update"))
	}
	if strings.Contains(block, "no Evidence") || !strings.Contains(block, "test") || !strings.Contains(block, runLink) {
		t.Errorf("the block of the second report:\n%s", block)
	}
	if len(run.Flags) != 0 {
		t.Errorf("a done with a PR and a passed check has the flags %q", run.Flags)
	}
}

func TestCheckKeepsTheShellWordsOfItsArguments(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	env.Runner.Agent(takeTask, initLine(t),
		checkStep(out, "words", "printf", "<%s>", "a|b c'd"),
		checkStep(out, "one", "echo one && echo two"),
		reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if got := readRecorded(t, out, "words"); !strings.Contains(got, "pass") {
		t.Errorf("check with quoted words:\n%s", got)
	}
	checks := eventChecks(t, reportEvents(env, id)[0])
	if len(checks) != 2 {
		t.Fatalf("want two recorded checks, got %+v", checks)
	}
	if checks[0]["command"] != `printf '<%s>' 'a|b c'\''d'` || checks[0]["output"] != "<a|b c'd>" {
		t.Errorf("the check of more words: %+v", checks[0])
	}
	if checks[1]["command"] != "echo one && echo two" || checks[1]["output"] != "one\ntwo" {
		t.Errorf("the check of one argument: %+v", checks[1])
	}
}

func TestTheWrapperSendsNoCheckOrEvidencePathThatTheDispatcherRefuses(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	long := strings.Repeat("d", 250)
	env.Runner.Agent(takeTask, initLine(t),
		checkStep(out, "long", ": "+strings.Repeat("x", dispatcherapi.MaxCheckText)+"; true"),
		// 19 checks, then two new checks at the same time: one of them
		// is the 20th, and the other gets no record.
		shell(`for i in $(seq 1 19); do solidtrace check "true $i" >/dev/null || exit 1; done
solidtrace check "sleep 0.3; true a" >/dev/null & solidtrace check "sleep 0.3; true b" >/dev/null & wait`),
		shell(`d="$SOLIDTRACE_EVIDENCE_DIR/`+long+"/"+long+"/"+long+"/"+long+`" && mkdir -p "$d" && echo x > "$d/`+long+`.txt"
printf x > "$SOLIDTRACE_EVIDENCE_DIR/$(printf 'bad\377')" && printf x > "$SOLIDTRACE_EVIDENCE_DIR/good.txt"`),
		reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if got := readRecorded(t, out, "long"); !strings.Contains(got, "at most") {
		t.Errorf("a check of a command that is too long:\n%s", got)
	}
	reports := reportEvents(env, id)
	if len(reports) != 1 {
		t.Fatalf("want one report event, got %d", len(reports))
	}
	if checks := eventChecks(t, reports[0]); len(checks) != dispatcherapi.MaxChecks {
		t.Errorf("want %d recorded checks, got %d", dispatcherapi.MaxChecks, len(checks))
	}
	evidence := strs(reports[0]["report.evidence"])
	if !slices.Equal(evidence, []string{"evidence/good.txt", "evidence/report.md"}) {
		t.Errorf("the report's Evidence paths: %q", evidence)
	}
}

// The checks of commands near MaxCheckText, which JSON writes as 6 bytes
// for each byte (\u003c), can make a report larger than the Dispatcher's
// body limit. The wrapper refuses the check that can do so.
func TestTheWrapperRecordsNoChecksThatMakeTheReportTooLarge(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	steps := []string{takeTask, initLine(t)}
	for i := range 3 {
		steps = append(steps, checkStep(out, fmt.Sprint("big", i), fmt.Sprintf(": '%s'; true %d", strings.Repeat("<", 60000), i)))
	}
	steps = append(steps, reportStep("--outcome", "done", "--summary", "Fixed"), reply, result, waitForEnd)
	env.Runner.Agent(steps...)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if got := readRecorded(t, out, "big0"); !strings.Contains(got, "pass") {
		t.Errorf("the first big check:\n%s", got)
	}
	for _, name := range []string{"big1", "big2"} {
		if got := readRecorded(t, out, name); !strings.Contains(got, "at most") || !strings.Contains(got, "exit 1") {
			t.Errorf("the check %s must be refused:\n%s", name, got)
		}
	}
	if checks := eventChecks(t, reportEvents(env, id)[0]); len(checks) != 1 {
		t.Errorf("want one recorded check, got %d", len(checks))
	}
}

func TestTheEvidencePathsAlwaysHaveEvidenceReportMd(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		shell(`for i in $(seq 100 349); do printf x > "$SOLIDTRACE_EVIDENCE_DIR/a$i.png"; done`),
		reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	evidence := strs(reportEvents(env, id)[0]["report.evidence"])
	if len(evidence) != dispatcherapi.MaxEvidence || !slices.Contains(evidence, "evidence/report.md") ||
		!slices.Contains(evidence, "evidence/a100.png") || slices.Contains(evidence, "evidence/a349.png") {
		t.Errorf("want the first %d paths with evidence/report.md, got %d: %q", dispatcherapi.MaxEvidence-1,
			len(evidence), evidence)
	}
}

func TestARefusedReportLeavesEvidenceReportMdAsItWas(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	next := `"$SOLIDTRACE_EVIDENCE_DIR/../.report.md.next"`
	env.Runner.Agent(takeTask, initLine(t),
		reportStep("--outcome", "done", "--summary", "GOODSUMMARY"),
		recorded(out, "after-good", "test ! -e "+next),
		recorded(out, "refused", "solidtrace report --outcome needs-a-person --summary BADSUMMARY"),
		recorded(out, "after-refused", "test ! -e "+next),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if got := readRecorded(t, out, "refused"); !strings.Contains(got, "question") {
		t.Errorf("a needs a person with no question must be refused:\n%s", got)
	}
	for _, name := range []string{"after-good", "after-refused"} {
		if got := readRecorded(t, out, name); !strings.Contains(got, "exit 0") {
			t.Errorf("%s: .report.md.next stays in the project directory:\n%s", name, got)
		}
	}
	session := firstToken(t, env).AgentSessionIDs[0]
	report := string(env.SolidTrace.Stored(session, "evidence/report.md"))
	if !strings.Contains(report, "GOODSUMMARY") || strings.Contains(report, "BADSUMMARY") {
		t.Errorf("evidence/report.md:\n%s", report)
	}
}
