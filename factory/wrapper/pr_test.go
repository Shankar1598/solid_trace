package wrapper_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/fakeforge"
)

// commitStep is a step in which the agent commits a change on the Run's
// branch, and does not push it.
var commitStep = shell(`echo "fixed" > cart.txt && git add cart.txt && git commit -q -m "Fix the cart"`)

// recorded is a step that runs command, and writes its output and exit
// code to a file in dir that the test reads.
func recorded(dir, name, command string) string {
	path := filepath.Join(dir, name)
	return shell(command + " > " + path + " 2>&1; echo \"exit $?\" >> " + path)
}

func readRecorded(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("the agent's step %s did not run: %v", name, err)
	}
	return string(b)
}

func TestPROpensTheRunsPRAndASecondPRUpdatesIt(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	mainBefore, _ := env.Forge.Ref("example/shop", "refs/heads/main")
	env.Runner.Agent(takeTask, initLine(t), commitStep,
		recorded(out, "first", `solidtrace pr --title "Fix the cart" --description "The cart test waited on a timer."`),
		recorded(out, "second", `solidtrace pr --title "Fix the flaky cart test" --description "It waits on the event now."`),
		reportStep("--outcome", "done", "--summary", "Fixed the cart test"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the flaky cart test")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	prs := env.Forge.PRs("example/shop")
	if len(prs) != 1 {
		t.Fatalf("want one PR on the forge, got %+v\nfirst pr: %s\nsecond pr: %s", prs,
			readRecorded(t, out, "first"), readRecorded(t, out, "second"))
	}
	pr := prs[0]
	if pr.Source != "agent/"+id || pr.Destination != "main" {
		t.Errorf("the PR goes from %q to %q, not from agent/%s to main", pr.Source, pr.Destination, id)
	}
	// The report then adds the Evidence block after the agent's text.
	agentText, _, _ := strings.Cut(pr.Description, dispatcherapi.EvidenceStart)
	if pr.Title != "Fix the flaky cart test" || strings.TrimSpace(agentText) != "It waits on the event now." {
		t.Errorf("the second pr did not update the PR: title %q, description %q", pr.Title, pr.Description)
	}
	if pr.Author != fakeforge.FactoryAuthor {
		t.Errorf("the PR's author is %q, not the factory", pr.Author)
	}
	if first := readRecorded(t, out, "first"); !strings.Contains(first, pr.Link) || !strings.Contains(first, "exit 0") {
		t.Errorf("the first pr's output does not give the PR link %s:\n%s", pr.Link, first)
	}
	if second := readRecorded(t, out, "second"); !strings.Contains(second, "Updated") || !strings.Contains(second, "exit 0") {
		t.Errorf("the second pr's output:\n%s", second)
	}

	// The pr pushed the agent's commit, so the PR has it.
	if _, ok := env.Forge.File("example/shop", "refs/heads/agent/"+id, "cart.txt"); !ok {
		t.Errorf("the Run's branch on the forge has no cart.txt")
	}
	// Never a merge: the PR stays open and the base branch did not move.
	if pr.State != "OPEN" {
		t.Errorf("the PR is %s, not OPEN", pr.State)
	}
	if mainAfter, _ := env.Forge.Ref("example/shop", "refs/heads/main"); mainAfter != mainBefore {
		t.Errorf("the base branch moved from %s to %s", mainBefore, mainAfter)
	}

	if run.PRURL != pr.Link {
		t.Errorf("the Run shows the PR link %q, not %q", run.PRURL, pr.Link)
	}
	if got := ana.CLI("runs", "get", id); !strings.Contains(got.Stdout, pr.Link) {
		t.Errorf("runs get does not show the PR link:\n%s", got.Stdout)
	}

	var opened []map[string]any
	for _, ev := range env.WaitForRunEvents(id, 6) {
		if ev["event.kind"] == "pr_opened" {
			opened = append(opened, ev)
		}
	}
	if len(opened) != 1 {
		t.Fatalf("want one PR opened event, got %d: %+v", len(opened), env.RunEvents(id))
	}
	if opened[0]["run.pr.url"] != pr.Link {
		t.Errorf("the PR opened event has run.pr.url %v, not %s", opened[0]["run.pr.url"], pr.Link)
	}
	events := env.RunEvents(id)
	if last := events[len(events)-1]; last["run.pr.url"] != pr.Link {
		t.Errorf("the last event %v does not carry the PR link: %v", last["event.kind"], last["run.pr.url"])
	}
}

func TestAPRThatTheForgeRefusesGivesTheAgentTheReasonAndRecordsNoPR(t *testing.T) {
	env, ana := newEnv(t)
	out := t.TempDir()
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, commitStep,
		recorded(out, "refused", `solidtrace pr --title "Fix the cart"`),
		recorded(out, "no-title", `solidtrace pr --description "No title"`),
		reportStep("--outcome", "done", "--summary", "Fixed the cart test"),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the flaky cart test")
	waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	env.Forge.FailNext(http.StatusForbidden, "Your credentials lack one or more required privilege scopes.")
	open()
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	refused := readRecorded(t, out, "refused")
	if !strings.Contains(refused, "required privilege scopes") || !strings.Contains(refused, "exit 1") {
		t.Errorf("the refused pr's output does not give the forge's reason:\n%s", refused)
	}
	if noTitle := readRecorded(t, out, "no-title"); !strings.Contains(noTitle, "title") || strings.Contains(noTitle, "exit 0") {
		t.Errorf("a first pr with no title:\n%s", noTitle)
	}
	if prs := env.Forge.PRs("example/shop"); len(prs) != 0 {
		t.Errorf("want no PR, got %+v", prs)
	}
	if run.PRURL != "" {
		t.Errorf("the Run shows a PR %q", run.PRURL)
	}
}
