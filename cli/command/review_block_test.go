package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/solidtrace/cli/command"
)

func runBlockCommand(t *testing.T, stdin string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), []string{"review", "block"},
		command.Env{Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

const (
	hashA = "0123456789abcdef0123456789abcdef01234567"
	// validRunBlock has each field of a run block, on several lines.
	validRunBlock = `{"version": 2, "run": 3, "skill": "code-review", "trigger": "agent", "stage": "step", "step": "otlp-03",
 "base": "abc", "head": "def",
 "reviewers": [{"name": "bugs-and-spec", "model": "opus", "effort": "xhigh"}],
 "verifier": {"model": "opus", "effort": "xhigh"},
 "findings": [
  {"id": "F7", "reviewer": "bugs-and-spec", "kind": "hard", "severity": "high", "category": "security",
   "file": "app/routes.go", "line": 52, "title": "No <cap> & no back-off", "scenario": "s",
   "events": [{"type": "found"}, {"type": "triage", "decision": "fix"}]},
  {"id": "F8", "reviewer": "bugs-and-spec", "kind": "judgement", "severity": "low", "category": "design",
   "file": null, "line": null, "title": "t", "scenario": "s", "also": ["code-quality"],
   "found_by": {"kind": "person", "name": "ann", "access": "write", "self": true, "ref": "c-1"},
   "events": [{"type": "refound"}, {"type": "triage", "decision": "trivial", "why": "w"},
              {"type": "verify", "verdict": "CONFIRMED", "pushed_back": true, "why": "w"},
              {"type": "triage", "decision": "fix", "why": "w", "tradeoffs": "t", "approved_by": "author"}]}],
 "confirmations": [{"id": "C1", "reviewer": "tests", "ask": "run the tests"}],
 "dropped": {"bugs-and-spec": 0}}`
	validUpdateBlock = `{"version": 2, "update": 1, "findings": [
 {"id": "F12", "events": [{"type": "triage", "decision": "fix", "why": "w"},
                          {"type": "fix", "commit": "` + hashA + `", "files": ["a.go"]}]}]}`
)

func TestReviewBlockPrintsAValidBlockAsOneFencedLine(t *testing.T) {
	for name, block := range map[string]string{"a run block": validRunBlock, "an update block": validUpdateBlock} {
		t.Run(name, func(t *testing.T) {
			code, out, stderr := runBlockCommand(t, block)

			if code != 0 || stderr != "" {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			got := fixedBlocks(t, out)
			var want any
			json.Unmarshal([]byte(block), &want)
			if !reflect.DeepEqual(got, []any{want}) {
				t.Fatalf("block:\n%s", out)
			}
		})
	}
	_, out, _ := runBlockCommand(t, validRunBlock)
	if !strings.Contains(out, `"No <cap> & no back-off"`) || !strings.HasPrefix(out, "```solidtrace-findings\n{\"version\":2,\"run\":3,") {
		t.Fatalf("the block does not keep its key order and its text:\n%s", out)
	}
}

func TestReviewBlockTakesASkippedDeepReview(t *testing.T) {
	block := `{"version": 2, "run": 1, "skill": "deep-review", "trigger": "open-pr", "stage": "final", "step": null,
	 "base": "a", "head": "b", "reviewers": [], "verifier": null, "findings": [], "confirmations": [], "dropped": {},
	 "skipped": "covered by ` + hashA + `"}`

	if code, _, stderr := runBlockCommand(t, block); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

// spoilBlock decodes block, changes it, and gives it as JSON.
func spoilBlock(t *testing.T, block string, spoil func(b map[string]any)) string {
	t.Helper()
	b := decodeJSON(t, block)
	spoil(b)
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func blockFinding(b map[string]any, i int) map[string]any {
	return b["findings"].([]any)[i].(map[string]any)
}

func blockEvent(b map[string]any, finding, event int) map[string]any {
	return blockFinding(b, finding)["events"].([]any)[event].(map[string]any)
}

func TestReviewBlockNamesEachProblem(t *testing.T) {
	cases := []struct {
		name  string
		block string
		spoil func(b map[string]any)
		want  string
	}{
		{"a field that is not known", validRunBlock, func(b map[string]any) { b["reviwers"] = []any{} },
			`the block: the field "reviwers" is not known`},
		{"a missing field", validRunBlock, func(b map[string]any) { delete(b, "head") }, `the block: no "head"`},
		{"version 1", validRunBlock, func(b map[string]any) { b["version"] = 1 }, "version: 1 is not 2"},
		{"run 0", validRunBlock, func(b map[string]any) { b["run"] = 0 }, "run: 0 is not an integer of 1 or more"},
		{"an unknown skill", validRunBlock, func(b map[string]any) { b["skill"] = "review" },
			`skill: "review" is not one of code-review, deep-review, update-pr`},
		{"an unknown trigger", validRunBlock, func(b map[string]any) { b["trigger"] = "bot" },
			`trigger: "bot" is not one of person, agent, open-pr, update-pr`},
		{"an unknown stage", validRunBlock, func(b map[string]any) { b["stage"] = "late" },
			`stage: "late" is not one of step, final, pr`},
		{"a step that is a number", validRunBlock, func(b map[string]any) { b["step"] = 3 }, "step: is not a string"},
		{"a reviewer with no effort", validRunBlock, func(b map[string]any) {
			delete(b["reviewers"].([]any)[0].(map[string]any), "effort")
		}, `reviewers[0]: no "effort"`},
		{"a verifier with a name", validRunBlock, func(b map[string]any) { b["verifier"].(map[string]any)["name"] = "v" },
			`verifier: the field "name" is not known`},
		{"a wrong skipped", validRunBlock, func(b map[string]any) { b["skipped"] = "no time" },
			`skipped: "no time" is not "docs only", "person" or "covered by <head>"`},
		{"a dropped count below 0", validRunBlock, func(b map[string]any) { b["dropped"] = map[string]any{"tests": -1} },
			"dropped.tests: -1 is not an integer of 0 or more"},
		{"a finding id that is not F<n>", validRunBlock, func(b map[string]any) { blockFinding(b, 0)["id"] = "7" },
			`findings[0].id: "7" is not of the form F<n>`},
		{"a finding id twice", validRunBlock, func(b map[string]any) { blockFinding(b, 1)["id"] = "F7" },
			"findings[1].id: F7 is in the block more than once"},
		{"a finding field that is not known", validRunBlock, func(b map[string]any) { blockFinding(b, 0)["sevirity"] = "high" },
			`findings[0]: the field "sevirity" is not known`},
		{"an unknown kind", validRunBlock, func(b map[string]any) { blockFinding(b, 0)["kind"] = "soft" },
			`findings[0].kind: "soft" is not one of hard, judgement`},
		{"an unknown severity", validRunBlock, func(b map[string]any) { blockFinding(b, 0)["severity"] = "critical" },
			`findings[0].severity: "critical" is not one of high, medium, low`},
		{"an unknown category", validRunBlock, func(b map[string]any) { blockFinding(b, 0)["category"] = "style" },
			`findings[0].category: "style" is not one of correctness, spec, security, availability, performance, data-loss, test-gap, design, other`},
		{"a line that is text", validRunBlock, func(b map[string]any) { blockFinding(b, 0)["line"] = "52" },
			`findings[0].line: "52" is not an integer of 1 or more`},
		{"an empty title", validRunBlock, func(b map[string]any) { blockFinding(b, 0)["title"] = "" }, "findings[0].title: is empty"},
		{"an unknown found_by kind", validRunBlock, func(b map[string]any) { blockFinding(b, 1)["found_by"].(map[string]any)["kind"] = "bot" },
			`findings[1].found_by.kind: "bot" is not one of person, reviewer, test`},
		{"no events", validRunBlock, func(b map[string]any) { blockFinding(b, 0)["events"] = []any{} }, "findings[0].events: is empty"},
		{"a first event that is a triage", validRunBlock, func(b map[string]any) {
			blockFinding(b, 0)["events"] = []any{map[string]any{"type": "triage", "decision": "fix"}}
		}, `findings[0].events[0]: is "triage": the first event of a finding is found or refound`},
		{"an unknown event type", validRunBlock, func(b map[string]any) { blockEvent(b, 0, 1)["type"] = "note" },
			`findings[0].events[1].type: "note" is not one of found, refound, triage, verify, fix`},
		{"an unknown decision", validRunBlock, func(b map[string]any) { blockEvent(b, 0, 1)["decision"] = "skip" },
			`findings[0].events[1].decision: "skip" is not one of fix, dispute, trivial, defer, ask`},
		{"a trivial decision with no why", validRunBlock, func(b map[string]any) { delete(blockEvent(b, 1, 1), "why") },
			`findings[1].events[1]: no "why"`},
		{"a found event with a why", validRunBlock, func(b map[string]any) { blockEvent(b, 0, 0)["why"] = "w" },
			`findings[0].events[0]: the field "why" is not known`},
		{"an unknown verdict", validRunBlock, func(b map[string]any) { blockEvent(b, 1, 2)["verdict"] = "TRUE" },
			`findings[1].events[2].verdict: "TRUE" is not one of CONFIRMED, PLAUSIBLE, REFUTED`},
		{"a pushed_back that is text", validRunBlock, func(b map[string]any) { blockEvent(b, 1, 2)["pushed_back"] = "yes" },
			"findings[1].events[2].pushed_back: is not true or false"},
		{"an approved_by that is not author", validRunBlock, func(b map[string]any) { blockEvent(b, 1, 3)["approved_by"] = "bob" },
			`findings[1].events[3].approved_by: "bob" is not one of author`},
		{"a confirmation id that is not C<n>", validRunBlock, func(b map[string]any) {
			b["confirmations"].([]any)[0].(map[string]any)["id"] = "F1"
		}, `confirmations[0].id: "F1" is not of the form C<n>`},
		{"run and update", validRunBlock, func(b map[string]any) { b["update"] = 1 },
			"the block: has run and update: a block is a run block or an update block"},
		{"an update block with a title", validUpdateBlock, func(b map[string]any) { blockFinding(b, 0)["title"] = "t" },
			`findings[0]: the field "title" is not known`},
		{"an update block with a found event", validUpdateBlock, func(b map[string]any) {
			blockFinding(b, 0)["events"] = []any{map[string]any{"type": "found"}}
		}, `findings[0].events[0].type: "found" is not one of triage, fix`},
		{"an update block with a short hash", validUpdateBlock, func(b map[string]any) { blockEvent(b, 0, 1)["commit"] = "0123456" },
			`findings[0].events[1].commit: "0123456" is not the full hash of a commit`},
		{"a fix with no files", validUpdateBlock, func(b map[string]any) { delete(blockEvent(b, 0, 1), "files") },
			`findings[0].events[1]: no "files"`},
		{"an update block with no findings", validUpdateBlock, func(b map[string]any) { b["findings"] = []any{} },
			"findings: is empty: an update block adds events to one or more findings"},
		{"update 0", validUpdateBlock, func(b map[string]any) { b["update"] = 0 }, "update: 0 is not an integer of 1 or more"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, stderr := runBlockCommand(t, spoilBlock(t, c.block, c.spoil))

			if code != 1 || out != "" {
				t.Errorf("exit %d, want 1\nstdout: %s", code, out)
			}
			if want := "solidtrace review block: " + c.want + "\n"; !strings.Contains(stderr, want) {
				t.Fatalf("stderr:\n%s\nwant %q", stderr, want)
			}
		})
	}
}

func TestReviewBlockGivesEachProblemOnItsOwnLine(t *testing.T) {
	block := spoilBlock(t, validRunBlock, func(b map[string]any) {
		b["skill"] = "review"
		blockFinding(b, 0)["kind"] = "soft"
	})

	code, _, stderr := runBlockCommand(t, block)

	if code != 1 || strings.Count(stderr, "solidtrace review block: ") != 2 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
}

func TestReviewBlockRefusesInputThatIsNotOneObject(t *testing.T) {
	cases := map[string]struct{ stdin, want string }{
		"no input":       {"", "solidtrace review block: no JSON"},
		"not JSON":       {`{"version": 2,}`, "solidtrace review block: not valid JSON"},
		"two objects":    {validUpdateBlock + "\n" + validUpdateBlock, "solidtrace review block: text after the JSON"},
		"a list":         {"[]", "solidtrace review block: the block: is not an object"},
		"a fenced block": {"```solidtrace-findings\n" + validUpdateBlock + "\n```", "solidtrace review block: not valid JSON"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			code, out, stderr := runBlockCommand(t, c.stdin)
			if code != 1 || out != "" || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d\nstdout: %s\nstderr: %s\nwant %q", code, out, stderr, c.want)
			}
		})
	}
	var stdout, stderr bytes.Buffer
	if code := command.Run(context.Background(), []string{"review", "block", "x.json"},
		command.Env{Stdin: strings.NewReader(validUpdateBlock), Stdout: &stdout, Stderr: &stderr}); code != 2 {
		t.Fatalf("an argument: exit %d", code)
	}
}
