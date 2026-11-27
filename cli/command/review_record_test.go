package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/solidtrace/cli/command"
)

// recordDigest has, in s1: a review start with a block (u-b1, run 1 of the
// main writer); run 1 of the subagent a1 (u-b2); a commit whose trailer
// names the main writer's F1; a1's update block; a later main block that
// finds F2 again (u-b3, run 2); its update block; and a review start with
// no block. In s2: a block of run 1, and a commit whose trailer names its
// F1.
const recordDigest = `{"sessions": [{"id": "s1", "title": "Parser"}, {"id": "s2", "title": "Docs"}], "reviews": 2, "events": [
 {"seq": 0, "session": "s1", "uuid": "u-task", "time": "2026-11-03T08:00:00Z", "kind": "task", "text": "Build the parser."},
 {"seq": 1, "session": "s1", "uuid": "u-start", "time": "2026-11-03T08:01:00Z", "kind": "review_start", "skill": "solidtrace-skills:code-review", "trigger": "person"},
 {"seq": 2, "session": "s1", "uuid": "u-b1", "time": "2026-11-03T08:02:00Z", "kind": "findings", "findings":
  {"version": 2, "run": 1, "skill": "code-review", "trigger": "person", "stage": "step", "step": "parser-01", "base": "a", "head": "b",
   "reviewers": [{"name": "bugs", "model": "opus", "effort": "high"}], "verifier": {"model": "opus", "effort": "high"},
   "findings": [
    {"id": "F1", "reviewer": "bugs", "kind": "hard", "severity": "high", "category": "correctness", "file": "a.go", "line": 3, "title": "No cap", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "fix"}]},
    {"id": "F2", "reviewer": "bugs", "kind": "judgement", "severity": "low", "category": "design", "file": "a.go", "line": null, "title": "A name", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "trivial", "why": "a name"}]},
    {"id": "F3", "reviewer": "bugs", "kind": "hard", "severity": "medium", "category": "spec", "file": null, "line": null, "title": "t3", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "dispute", "why": "the spec says so"}]},
    {"id": "F4", "reviewer": "bugs", "kind": "hard", "severity": "medium", "category": "spec", "file": null, "line": null, "title": "t4", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "defer", "why": "later"}]},
    {"id": "F5", "reviewer": "bugs", "kind": "judgement", "severity": "low", "category": "design", "file": null, "line": null, "title": "t5", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "trivial", "why": "small"},
                {"type": "verify", "verdict": "CONFIRMED", "pushed_back": true, "why": "real"}]},
    {"id": "F6", "reviewer": "bugs", "kind": "hard", "severity": "low", "category": "other", "file": null, "line": null, "title": "t6", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "ask", "why": "the person decides"}]}],
   "confirmations": [{"id": "C1", "reviewer": "tests", "ask": "run the tests"}], "dropped": {"bugs": 0}}},
 {"seq": 3, "session": "s1", "uuid": "u-b2", "time": "2026-11-03T08:03:00Z", "kind": "findings", "writer": "a1", "findings":
  {"version": 2, "run": 1, "skill": "code-review", "trigger": "agent", "stage": "step", "step": "parser-02", "base": "b", "head": "c",
   "reviewers": [{"name": "tests", "model": "sonnet", "effort": "high"}], "verifier": null,
   "findings": [{"id": "F1", "reviewer": "tests", "kind": "hard", "severity": "medium", "category": "test-gap", "file": "b.go", "line": null, "title": "No test", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "fix"}]}],
   "confirmations": [], "dropped": {"tests": 1}}},
 {"seq": 4, "session": "s1", "uuid": "u-commit", "time": "2026-11-03T08:04:00Z", "kind": "commit", "command": "git commit -m fix", "review_findings": ["run 1: F1"]},
 {"seq": 5, "session": "s1", "uuid": "u-upd", "time": "2026-11-03T08:05:00Z", "kind": "findings", "writer": "a1", "findings":
  {"version": 2, "update": 1, "findings": [{"id": "F1", "events": [{"type": "fix", "commit": "` + hashA + `", "files": ["b_test.go"]}]}]}},
 {"seq": 6, "session": "s1", "uuid": "u-b3", "time": "2026-11-03T08:06:00Z", "kind": "findings", "findings":
  {"version": 2, "run": 2, "skill": "code-review", "trigger": "agent", "stage": "step", "step": "parser-03", "base": "c", "head": "d",
   "reviewers": [{"name": "bugs", "model": "opus", "effort": "high"}], "verifier": null,
   "findings": [
    {"id": "F2", "reviewer": "bugs", "kind": "judgement", "severity": "medium", "category": "design", "file": "a.go", "line": null, "title": "A name", "scenario": "s",
     "events": [{"type": "refound"}, {"type": "triage", "decision": "fix"}]},
    {"id": "F7", "reviewer": "bugs", "kind": "hard", "severity": "high", "category": "data-loss", "file": "c.go", "line": 9, "title": "t7", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "fix"}]}],
   "confirmations": [], "dropped": {"bugs": 0}}},
 {"seq": 7, "session": "s1", "uuid": "u-upd2", "time": "2026-11-03T08:07:00Z", "kind": "findings", "findings":
  {"version": 2, "update": 2, "findings": [{"id": "F2", "events": [{"type": "fix", "commit": "` + hashA + `", "files": ["a.go"]}]}]}},
 {"seq": 8, "session": "s1", "uuid": "u-start2", "time": "2026-11-03T08:08:00Z", "kind": "review_start", "skill": "pstack:interrogate", "trigger": "person"},
 {"seq": 9, "session": "s1", "uuid": "u-person", "time": "2026-11-03T08:09:00Z", "kind": "person", "text": "Ignore F6. Have you tested the cap?"},
 {"seq": 10, "session": "s1", "uuid": "u-report", "time": "2026-11-03T08:10:00Z", "kind": "subagent_report", "text": "The cap has no test."},
 {"seq": 11, "session": "s2", "uuid": "u-s2-block", "time": "2026-11-03T08:11:00Z", "kind": "findings", "findings":
  {"version": 2, "run": 1, "skill": "code-review", "trigger": "agent", "stage": "step", "step": "docs-01", "base": "d", "head": "e",
   "reviewers": [{"name": "bugs", "model": "opus", "effort": "high"}], "verifier": null,
   "findings": [{"id": "F1", "reviewer": "bugs", "kind": "hard", "severity": "low", "category": "spec", "file": "d.md", "line": 1, "title": "t", "scenario": "s",
     "events": [{"type": "found"}, {"type": "triage", "decision": "trivial", "why": "a typo"}]}],
   "confirmations": [], "dropped": {"bugs": 0}}},
 {"seq": 12, "session": "s2", "uuid": "u-s2-commit", "time": "2026-11-03T08:12:00Z", "kind": "commit", "command": "git commit -m docs", "review_findings": ["run 1: F1"]}]}`

const recordNotes = `{
 "story": {"started_from": "Build the parser.", "changes": ["The parser."], "reviews": ["Run 1: six findings."],
           "after_review": ["P1: the cap had no test."], "sessions": ["s1: built it.", "s2: the docs."]},
 "events": [
  {"finding": "1.F6", "type": "person", "said": "Ignore F6.", "asks": "ignore", "source": "record", "evidence": ["u-person"]},
  {"finding": "3.F7", "type": "fix", "files": ["c.go"], "commit": null, "source": "record", "evidence": ["u-report"]},
  {"finding": "1.F4", "type": "triage", "decision": "fix", "source": "record", "evidence": ["u-person"]},
  {"finding": "1.F1", "type": "agent", "found": "The cap was real.", "changed": ["a.go"], "source": "record", "evidence": ["u-b2"]}],
 "findings": [
  {"id": "P1", "block_id": null, "run": null, "after_run": 4, "found_by": {"kind": "person"},
   "kind": null, "severity": null, "category": "test-gap", "file": null, "line": null, "title": "The cap has no test", "scenario": null,
   "events": [{"type": "person", "said": "Have you tested the cap?", "asks": "question", "source": "record", "evidence": ["u-person"]},
              {"type": "agent", "found": "It has no test.", "changed": [], "source": "record", "evidence": ["u-report"]}]},
  {"id": "T4-1", "run": 4, "found_by": {"kind": "reviewer", "name": "interrogate"}, "title": "The cap is not tested",
   "events": [{"type": "found", "source": "record", "evidence": ["u-report"]}]}],
 "confirmations": [{"id": "1.C1", "given": true, "evidence": ["u-commit"]}],
 "confidence": {"1.F5": "low"}
}`

func runRecord(t *testing.T, args ...string) (int, map[string]any, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), append([]string{"review", "record"}, args...),
		command.Env{Stdout: &stdout, Stderr: &stderr})
	var out map[string]any
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("%v\n%s", err, stdout.String())
		}
	}
	return code, out, stderr.String()
}

func writeText(t *testing.T, name, s string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeFile(t, path, s)
	return path
}

// eventSteps are the events of a record finding as type/source/evidence.
func eventSteps(f map[string]any) []string {
	var out []string
	for _, e := range f["events"].([]any) {
		e := e.(map[string]any)
		out = append(out, fmt.Sprintf("%s/%s/%s", e["type"], e["source"], strings.Join(toStrings(e["evidence"]), ",")))
	}
	return out
}

func toStrings(v any) []string {
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}

func recordFinding(t *testing.T, record map[string]any, id string) map[string]any {
	t.Helper()
	for _, f := range record["findings"].([]any) {
		if f := f.(map[string]any); f["id"] == id {
			return f
		}
	}
	t.Fatalf("no finding %s", id)
	return nil
}

func TestReviewRecordBuildsTheMechanicalPartOfTheRecord(t *testing.T) {
	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", recordDigest))

	if code != 0 || stderr != "" {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if record["version"] != 2.0 || record["pr"] != nil || record["round"] != 1.0 || len(record["sessions"].([]any)) != 2 {
		t.Fatalf("record: %v", record)
	}
	story := map[string]any{"started_from": "", "changes": []any{}, "reviews": []any{}, "after_review": []any{}, "sessions": []any{}}
	if !reflect.DeepEqual(record["story"], story) {
		t.Errorf("story: %v", record["story"])
	}
	var reviews []string
	for _, rv := range record["reviews"].([]any) {
		rv := rv.(map[string]any)
		reviews = append(reviews, fmt.Sprintf("%v %v %v %q %v %v %v", rv["run"], rv["block_run"], rv["session"], rv["writer"], rv["skill"], rv["step"], rv["start"]))
	}
	wantReviews := []string{
		`1 1 s1 "" code-review parser-01 u-b1`,
		`2 1 s1 "a1" code-review parser-02 u-b2`,
		`3 2 s1 "" code-review parser-03 u-b3`,
		`4 <nil> s1 "" pstack:interrogate <nil> u-start2`,
		`5 1 s2 "" code-review docs-01 u-s2-block`,
	}
	if !reflect.DeepEqual(reviews, wantReviews) {
		t.Errorf("reviews:\n%s\nwant\n%s", strings.Join(reviews, "\n"), strings.Join(wantReviews, "\n"))
	}
	want := map[string]struct {
		outcome string
		events  []string
	}{
		// The trailer of the main writer's commit names the main writer's F1.
		"1.F1": {"fixed", []string{"found/block/u-b1", "triage/block/u-b1", "fix/record/u-commit"}},
		// F2 found again in run 3 is one entry, and update 2 gives it its fix.
		"1.F2": {"fixed", []string{"found/block/u-b1", "triage/block/u-b1", "refound/block/u-b3", "triage/block/u-b3", "fix/block/u-upd2"}},
		"1.F3": {"disputed", []string{"found/block/u-b1", "triage/block/u-b1"}},
		"1.F4": {"deferred", []string{"found/block/u-b1", "triage/block/u-b1"}},
		"1.F5": {"open", []string{"found/block/u-b1", "triage/block/u-b1", "verify/block/u-b1"}},
		"1.F6": {"open", []string{"found/block/u-b1", "triage/block/u-b1"}},
		// a1's update 1 goes to a1's run 1, and the main trailer does not.
		"2.F1": {"fixed", []string{"found/block/u-b2", "triage/block/u-b2", "fix/block/u-upd"}},
		"3.F7": {"open", []string{"found/block/u-b3", "triage/block/u-b3"}},
		"5.F1": {"fixed", []string{"found/block/u-s2-block", "triage/block/u-s2-block", "fix/record/u-s2-commit"}},
	}
	if n := len(record["findings"].([]any)); n != len(want) {
		t.Errorf("%d findings, want %d", n, len(want))
	}
	for id, w := range want {
		f := recordFinding(t, record, id)
		if f["outcome"] != w.outcome || f["confidence"] != "high" || !reflect.DeepEqual(eventSteps(f), w.events) {
			t.Errorf("%s: %v %v, want %s %v", id, f["outcome"], eventSteps(f), w.outcome, w.events)
		}
	}
	f2, _ := json.Marshal(recordFinding(t, record, "1.F2"))
	wantF2 := `{"block_id":"F2","category":"design","confidence":"high","events":[` +
		`{"evidence":["u-b1"],"source":"block","type":"found"},` +
		`{"decision":"trivial","evidence":["u-b1"],"source":"block","type":"triage","why":"a name"},` +
		`{"evidence":["u-b3"],"run":3,"source":"block","type":"refound"},` +
		`{"decision":"fix","evidence":["u-b3"],"source":"block","type":"triage"},` +
		`{"commit":"` + hashA + `","evidence":["u-upd2"],"files":["a.go"],"source":"block","type":"fix"}],` +
		`"file":"a.go","found_by":{"kind":"reviewer","name":"bugs"},"id":"1.F2","kind":"judgement","line":null,` +
		`"outcome":"fixed","run":1,"scenario":"s","severity":"low","title":"A name"}`
	if string(f2) != wantF2 {
		t.Errorf("1.F2:\n%s\nwant\n%s", f2, wantF2)
	}
	trailerFix := recordFinding(t, record, "1.F1")["events"].([]any)[2].(map[string]any)
	if !reflect.DeepEqual(trailerFix["files"], []any{}) || trailerFix["commit"] != nil {
		t.Errorf("the fix from a trailer: %v", trailerFix)
	}
	if _, ok := trailerFix["commit"]; !ok {
		t.Errorf("the fix from a trailer has no commit: %v", trailerFix)
	}
	conf := []any{map[string]any{"id": "1.C1", "run": 1.0, "reviewer": "tests", "ask": "run the tests", "given": nil, "evidence": []any{}}}
	if !reflect.DeepEqual(record["confirmations"], conf) {
		t.Errorf("confirmations: %v", record["confirmations"])
	}
}

func TestReviewRecordWithNotesPassesReviewCheck(t *testing.T) {
	digest := writeText(t, "digest.json", recordDigest)

	code, record, stderr := runRecord(t, "--digest", digest, "--notes", writeText(t, "notes.json", recordNotes))

	if code != 0 || stderr != "" {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	for id, outcome := range map[string]string{
		"1.F1": "fixed", "1.F4": "open", "1.F6": "ignored_by_person", "3.F7": "fixed", "P1": "open", "T4-1": "open",
	} {
		if f := recordFinding(t, record, id); f["outcome"] != outcome {
			t.Errorf("%s: outcome %v, want %s", id, f["outcome"], outcome)
		}
	}
	// A note event takes its place by the time of its evidence.
	want := []string{"found/block/u-b1", "triage/block/u-b1", "agent/record/u-b2", "fix/record/u-commit"}
	if got := eventSteps(recordFinding(t, record, "1.F1")); !reflect.DeepEqual(got, want) {
		t.Errorf("1.F1: %v, want %v", got, want)
	}
	if recordFinding(t, record, "1.F5")["confidence"] != "low" || recordFinding(t, record, "P1")["after_run"] != 4.0 {
		t.Errorf("record: %v", record["findings"])
	}
	if c := record["confirmations"].([]any)[0].(map[string]any); c["given"] != true || !reflect.DeepEqual(c["evidence"], []any{"u-commit"}) {
		t.Errorf("confirmation: %v", c)
	}
	if record["story"].(map[string]any)["started_from"] != "Build the parser." {
		t.Errorf("story: %v", record["story"])
	}

	code, out, stderr := runCheck(t, decodeJSON(t, recordDigest), record)
	if code != 0 {
		t.Fatalf("review check: exit %d\n%s\n%s", code, strings.Join(out.Problems, "\n"), stderr)
	}
	wantCounts := map[string]int{"fixed": 5, "disputed": 1, "open": 4, "ignored_by_person": 1}
	if !reflect.DeepEqual(out.Outcomes, wantCounts) || out.Runs != 5 || out.PushedBack != 1 {
		t.Errorf("check: %+v", out)
	}
}

func TestReviewRecordSetsThePRAndTheRound(t *testing.T) {
	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", recordDigest),
		"--pr", "https://example.com/pr/7", "--round", "3")

	if code != 0 || record["pr"] != "https://example.com/pr/7" || record["round"] != 3.0 {
		t.Fatalf("exit %d: pr %v round %v\n%s", code, record["pr"], record["round"], stderr)
	}
}

func TestReviewRecordPutsThePRBodyInTheRecord(t *testing.T) {
	digest := writeText(t, "digest.json", recordDigest)
	body := "## What changes\n\nThe parser has a cap.\n"

	code, record, stderr := runRecord(t, "--digest", digest, "--pr-body", writeText(t, "body.md", body))

	if code != 0 || record["pr_body"] != body {
		t.Fatalf("exit %d: pr_body %q\n%s", code, record["pr_body"], stderr)
	}
	code, record, stderr = runRecord(t, "--digest", digest)
	if _, ok := record["pr_body"]; code != 0 || ok {
		t.Fatalf("with no --pr-body: exit %d, pr_body %v\n%s", code, record["pr_body"], stderr)
	}
}

func TestReviewRecordRefusesAPRBodyOverGitHubsLimit(t *testing.T) {
	digest := writeText(t, "digest.json", recordDigest)
	// GitHub counts characters, not bytes: 65,536 two-byte characters fit.
	atLimit := strings.Repeat("é", 65536)

	if code, record, stderr := runRecord(t, "--digest", digest, "--pr-body", writeText(t, "body.md", atLimit)); code != 0 || record["pr_body"] != atLimit {
		t.Fatalf("65,536 characters: exit %d\n%s", code, stderr)
	}
	code, _, stderr := runRecord(t, "--digest", digest, "--pr-body", writeText(t, "long.md", atLimit+"x"))
	if want := "solidtrace review record: the PR body has 65537 characters, more than GitHub's limit of 65536\n"; code != 1 || stderr != want {
		t.Fatalf("exit %d\nstderr: %q", code, stderr)
	}
	if code, _, _ := runRecord(t, "--digest", digest, "--pr-body", filepath.Join(t.TempDir(), "none.md")); code != 2 {
		t.Fatalf("a missing body: exit %d", code)
	}
}

func TestReviewRecordCutsThePersonsWords(t *testing.T) {
	long := strings.Repeat("é", 300)
	notes := `{"events": [{"finding": "1.F6", "type": "person", "said": "` + long + `", "asks": "dig", "source": "record", "evidence": ["u-person"]}]}`

	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", recordDigest), "--notes", writeText(t, "notes.json", notes))

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	events := recordFinding(t, record, "1.F6")["events"].([]any)
	if said := events[len(events)-1].(map[string]any)["said"].(string); said != strings.Repeat("é", 199)+"…" {
		t.Fatalf("said: %d characters", len([]rune(said)))
	}
}

const personEvent = `{"type": "person", "said": "x", "asks": "fix", "source": "record", "evidence": ["u-person"]}`

// personFinding is a note finding of the person, with more fields.
func personFinding(id, fields string) string {
	return `{"id": "` + id + `", ` + fields + `"found_by": {"kind": "person"}, "events": [` + personEvent + `]}`
}

func TestReviewRecordRefusesNotesThatNameWhatIsNotThere(t *testing.T) {
	event := func(fields string) string {
		return `{"events": [{"finding": "1.F6", "source": "record", "evidence": ["u-person"], ` + fields + `}]}`
	}
	cases := []struct{ name, notes, want string }{
		{"a finding that is not in the record", `{"events": [{"finding": "9.F9", "type": "person", "said": "x", "asks": "fix", "source": "record", "evidence": ["u-person"]}]}`,
			`events[0]: the finding "9.F9" is not in the record`},
		{"evidence that is not in the digest", `{"events": [{"finding": "1.F6", "type": "person", "said": "x", "asks": "fix", "source": "record", "evidence": ["u-none"]}]}`,
			`events[0]: the evidence "u-none" is not an event of the digest`},
		{"an event with no evidence", `{"events": [{"finding": "1.F6", "type": "person", "said": "x", "asks": "fix", "source": "record", "evidence": []}]}`,
			`events[0]: a person event needs "evidence"`},
		{"a field that the notes do not have", `{"storey": {}}`, `the notes: json: unknown field "storey"`},
		{"a field of another event type", event(`"type": "person", "said": "x", "asks": "fix", "decision": "fix"`),
			`events[0]: a person event has no field "decision"`},
		{"an unknown asks", event(`"type": "person", "said": "x", "asks": "maybe"`),
			`events[0]: asks "maybe" is not one of fix, ignore, defer, dig, question`},
		{"an event type that the notes do not add", event(`"type": "verify"`),
			`events[0]: the type "verify" is not one of person, agent, fix, triage`},
		{"a triage with no why", event(`"type": "triage", "decision": "defer"`), `events[0]: a triage event needs "why"`},
		{"a block source", `{"events": [{"finding": "1.F6", "type": "agent", "found": "x", "source": "block", "evidence": ["u-person"]}]}`,
			`events[0]: the source is "block", not record`},
		{"a T finding of a run with a block", `{"findings": [{"id": "T3-1", "run": 3, "found_by": {"kind": "reviewer"}, "events": [{"type": "found", "source": "record", "evidence": ["u-report"]}]}]}`,
			"findings[0]: a T<run>-<n> finding has the run of a review start with no block, and T3-1 names run 3"},
		{"a P finding found by an agent", `{"findings": [{"id": "P2", "found_by": {"kind": "agent"}, "events": [{"type": "person", "said": "x", "asks": "fix", "source": "record", "evidence": ["u-person"]}]}]}`,
			`findings[0]: the found_by kind of P2 is person, not "agent"`},
		{"a finding id that the record has", `{"findings": [{"id": "1.F1"}]}`,
			`findings[0]: the id "1.F1" is not P<n>, A<n>, X<n> or T<run>-<n>`},
		{"a new finding with a field that it does not have", `{"findings": [{"id": "P1", "outcome": "fixed"}]}`,
			`findings[0]: json: unknown field "outcome"`},
		{"a finding id twice", `{"findings": [` + personFinding("P1", "") + `, ` + personFinding("P1", "") + `]}`,
			"findings[1]: the id P1 is in the record already"},
		{"a block_id", `{"findings": [` + personFinding("P1", `"block_id": "F1", `) + `]}`,
			"findings[0]: block_id is not null: only a block gives a finding a block_id"},
		{"a found_by with no kind", `{"findings": [{"id": "P1", "found_by": {}, "events": [` + personEvent + `]}]}`,
			"findings[0]: found_by has no kind of reviewer, person, agent, test"},
		{"an after_run that is not a run", `{"findings": [` + personFinding("P1", `"after_run": 6, `) + `]}`,
			"findings[0]: after_run 6 is not a run of the record"},
		{"a run on a P finding", `{"findings": [` + personFinding("P1", `"run": 4, `) + `]}`,
			"findings[0]: run is not null: a finding from outside the reviews has after_run"},
		{"a finding with no events", `{"findings": [{"id": "P1", "found_by": {"kind": "person"}, "events": []}]}`,
			"findings[0]: no events"},
		{"a T finding with the run of another id", `{"findings": [{"id": "T4-1", "run": 3, "found_by": {"kind": "reviewer"}, "events": [{"type": "found", "source": "record", "evidence": ["u-report"]}]}]}`,
			"findings[0]: a T<run>-<n> finding has the run of a review start with no block, and T4-1 names run 4"},
		{"a confirmation that is not in the record", `{"confirmations": [{"id": "1.C9", "given": true, "evidence": []}]}`,
			`confirmations[0]: the confirmation "1.C9" is not in the record`},
		{"a confirmation with no given", `{"confirmations": [{"id": "1.C1", "evidence": []}]}`,
			"confirmations[0]: given is not true or false"},
		{"a confidence of a finding that is not there", `{"confidence": {"2.F9": "low"}}`,
			`confidence: the finding "2.F9" is not in the record`},
		{"a confidence that is not low or high", `{"confidence": {"1.F1": "medium"}}`,
			`confidence.1.F1: "medium" is not low or high`},
	}
	digest := writeText(t, "digest.json", recordDigest)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, stderr := runRecord(t, "--digest", digest, "--notes", writeText(t, "notes.json", c.notes))
			if code != 1 || !strings.Contains(stderr, "solidtrace review record: "+c.want+"\n") {
				t.Fatalf("exit %d\nstderr: %s\nwant %q", code, stderr, c.want)
			}
		})
	}
}

func TestReviewRecordWarnsOfABlockThatItCannotPlace(t *testing.T) {
	digest := decodeJSON(t, recordDigest)
	digestEvent(digest, 5)["writer"] = "b9"
	path := writeJSON(t, filepath.Join(t.TempDir(), "digest.json"), digest)

	code, record, stderr := runRecord(t, "--digest", path)

	want := "solidtrace review record: warning: the update block u-upd names run 1, and its session and writer have no block of that run\n"
	if code != 0 || stderr != want {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	if f := recordFinding(t, record, "2.F1"); f["outcome"] != "open" {
		t.Errorf("2.F1: %v", f)
	}
}

func TestReviewRecordRefusesWrongArguments(t *testing.T) {
	digest := writeText(t, "digest.json", recordDigest)
	notJSON := writeText(t, "bad.json", "{not json")
	for name, args := range map[string][]string{
		"no digest":          {},
		"round 0":            {"--digest", digest, "--round", "0"},
		"an extra argument":  {"--digest", digest, "more"},
		"a missing digest":   {"--digest", filepath.Join(t.TempDir(), "none.json")},
		"a missing notes":    {"--digest", digest, "--notes", filepath.Join(t.TempDir(), "none.json")},
		"a digest not JSON":  {"--digest", notJSON},
		"a digest of a list": {"--digest", writeText(t, "list.json", "[]")},
	} {
		t.Run(name, func(t *testing.T) {
			if code, _, stderr := runRecord(t, args...); code != 2 || !strings.Contains(stderr, "solidtrace review record") {
				t.Fatalf("exit %d\n%s", code, stderr)
			}
		})
	}
	if code, _, stderr := runRecord(t, "--digest", digest, "--notes", notJSON); code != 1 || !strings.Contains(stderr, "the notes: ") {
		t.Fatalf("notes not JSON: exit %d\n%s", code, stderr)
	}
}

// The parts of a small digest for the record's tests.
const (
	evFound     = `{"type": "found"}`
	evRefound   = `{"type": "refound"}`
	evTriageFix = `{"type": "triage", "decision": "fix"}`
	// storyNotes fill in the story, so that review check passes.
	storyNotes = `{"story": {"started_from": "x", "changes": ["c"], "reviews": ["r"], "after_review": [], "sessions": ["s"]}}`
)

func recDigest(events ...string) string {
	return `{"sessions": [{"id": "s1", "title": "t"}], "events": [` + strings.Join(events, ",\n") + `]}`
}

func recFinding(id string, events ...string) string {
	return fmt.Sprintf(`{"id": %q, "reviewer": "bugs", "events": [%s]}`, id, strings.Join(events, ", "))
}

// recBlock is a findings event of session with a run block of findings.
func recBlock(session, uuid, writer, skill string, run int, findings ...string) string {
	return fmt.Sprintf(`{"session": %q, "uuid": %q, "kind": "findings", "writer": %q, "findings": {"version": 2, "run": %d, "skill": %q, "findings": [%s]}}`,
		session, uuid, writer, run, skill, strings.Join(findings, ", "))
}

func recUpdate(uuid string, update int, id string) string {
	return fmt.Sprintf(`{"session": "s1", "uuid": %q, "kind": "findings", "findings": {"version": 2, "update": %d, "findings": [{"id": %q, "events": [{"type": "fix", "commit": %q, "files": ["a.go"]}]}]}}`,
		uuid, update, id, hashA)
}

func recCommit(uuid, trailer string) string {
	return fmt.Sprintf(`{"session": "s1", "uuid": %q, "kind": "commit", "command": "git commit", "review_findings": [%q]}`, uuid, trailer)
}

// recordAndCheck builds the record of digest with the story notes, and
// checks it.
func recordAndCheck(t *testing.T, digest string) (map[string]any, string) {
	t.Helper()
	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", digest), "--notes", writeText(t, "notes.json", storyNotes))
	if code != 0 {
		t.Fatalf("review record: exit %d\n%s", code, stderr)
	}
	if code, out, stderr := runCheck(t, decodeJSON(t, digest), record); code != 0 {
		t.Fatalf("review check: exit %d\n%s\n%s", code, strings.Join(out.Problems, "\n"), stderr)
	}
	return record, stderr
}

func TestReviewRecordAddsTheFixOfATrailerAfterALaterRunFoundTheFindingAgain(t *testing.T) {
	digest := recDigest(
		recBlock("s1", "u-b1", "", "code-review", 1, recFinding("F1", evFound, evTriageFix)),
		recCommit("u-c1", "run 1: F1"),
		recBlock("s1", "u-b2", "", "code-review", 2, recFinding("F1", evRefound, evTriageFix)),
		recCommit("u-c2", "run 2: F1"),
	)

	record, _ := recordAndCheck(t, digest)

	f := recordFinding(t, record, "1.F1")
	want := []string{"found/block/u-b1", "triage/block/u-b1", "fix/record/u-c1", "refound/block/u-b2", "triage/block/u-b2", "fix/record/u-c2"}
	if f["outcome"] != "fixed" || !reflect.DeepEqual(eventSteps(f), want) {
		t.Fatalf("1.F1: %v %v, want fixed %v", f["outcome"], eventSteps(f), want)
	}

	// review check wants a fix after the run that found it again.
	f["events"] = f["events"].([]any)[:5]
	f["outcome"] = "open"
	code, out, _ := runCheck(t, decodeJSON(t, digest), record)
	if want := `the commit u-c2: "run 2: F1" names F1, which has no fix event in the record`; code != 1 || !slices.Contains(out.Problems, want) {
		t.Fatalf("exit %d, problems %v, want %q", code, out.Problems, want)
	}
}

func TestReviewRecordGivesATrailerTheLatestRunBeforeTheCommit(t *testing.T) {
	// The count of the main writer's runs started again.
	digest := recDigest(
		recBlock("s1", "u-b1", "", "code-review", 1, recFinding("F1", evFound, evTriageFix)),
		recCommit("u-c1", "run 1: F1"),
		recBlock("s1", "u-b2", "", "code-review", 1, recFinding("F1", evFound, evTriageFix)),
	)

	record, stderr := recordAndCheck(t, digest)

	if want := "warning: the block u-b2 has the run 1 of an earlier block of its writer"; !strings.Contains(stderr, want) {
		t.Errorf("stderr: %s", stderr)
	}
	if got := eventSteps(recordFinding(t, record, "1.F1")); !reflect.DeepEqual(got, []string{"found/block/u-b1", "triage/block/u-b1", "fix/record/u-c1"}) {
		t.Errorf("1.F1: %v", got)
	}
	if f := recordFinding(t, record, "2.F1"); f["outcome"] != "open" || len(f["events"].([]any)) != 2 {
		t.Errorf("2.F1: %v", f)
	}
}

func TestReviewRecordGivesOneFixForATrailerAndItsUpdateBlock(t *testing.T) {
	digest := recDigest(
		recBlock("s1", "u-b1", "", "code-review", 1, recFinding("F1", evFound, evTriageFix)),
		recCommit("u-c1", "run 1: F1"),
		recUpdate("u-upd", 1, "F1"),
	)

	record, _ := recordAndCheck(t, digest)

	if got := eventSteps(recordFinding(t, record, "1.F1")); !reflect.DeepEqual(got, []string{"found/block/u-b1", "triage/block/u-b1", "fix/block/u-upd"}) {
		t.Fatalf("1.F1: %v", got)
	}
}

func TestReviewRecordGivesAReviewStartARunOfItsOwnWhenNoBlockOfItsSkillAndSessionFollows(t *testing.T) {
	digest := `{"sessions": [{"id": "s1", "title": "t"}, {"id": "s2", "title": "t"}], "events": [
	 {"session": "s1", "uuid": "u-i", "kind": "review_start", "skill": "pstack:interrogate", "trigger": "person"},
	 ` + recBlock("s1", "u-b1", "", "code-review", 1, recFinding("F1", evFound)) + `,
	 {"session": "s1", "uuid": "u-c", "kind": "review_start", "skill": "solidtrace-skills:code-review", "trigger": "person"},
	 ` + recBlock("s2", "u-b2", "", "code-review", 1, recFinding("F1", evFound)) + `]}`

	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", digest))

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	var got []string
	for _, rv := range record["reviews"].([]any) {
		rv := rv.(map[string]any)
		got = append(got, fmt.Sprintf("%v %v %v", rv["run"], rv["session"], rv["start"]))
	}
	if want := []string{"1 s1 u-i", "2 s1 u-b1", "3 s1 u-c", "4 s2 u-b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reviews: %v, want %v", got, want)
	}
}

func TestReviewRecordPutsARefoundFindingOnlyOnTheFindingOfItsWriterAndSession(t *testing.T) {
	digest := recDigest(
		recBlock("s1", "u-m1", "", "code-review", 1, recFinding("F2", evFound)),
		recBlock("s1", "u-a1", "a1", "code-review", 1, recFinding("F2", evFound)),
		recBlock("s2", "u-o1", "", "code-review", 1, recFinding("F2", evFound)),
		recBlock("s1", "u-m2", "", "code-review", 2, recFinding("F2", evRefound)),
	)

	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", digest))

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	want := map[string][]string{
		"1.F2": {"found/block/u-m1", "refound/block/u-m2"},
		"2.F2": {"found/block/u-a1"},
		"3.F2": {"found/block/u-o1"},
	}
	if n := len(record["findings"].([]any)); n != len(want) {
		t.Errorf("%d findings, want %d", n, len(want))
	}
	for id, w := range want {
		if got := eventSteps(recordFinding(t, record, id)); !reflect.DeepEqual(got, w) {
			t.Errorf("%s: %v, want %v", id, got, w)
		}
	}
}

func TestReviewRecordAndReviewCheckWarnOfABlockThatCannotBeRead(t *testing.T) {
	digest := recDigest(`{"session": "s1", "uuid": "u-bad", "kind": "findings", "findings": null, "error": "the solidtrace-findings block has no closing ` + "```" + `"}`)

	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", digest), "--notes", writeText(t, "notes.json", storyNotes))

	want := "solidtrace review record: warning: the block u-bad cannot be read (the solidtrace-findings block has no closing ```): the record has none of its findings\n"
	if code != 0 || stderr != want {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	if n := len(record["reviews"].([]any)); n != 0 {
		t.Errorf("%d reviews, want 0: a block that cannot be read is not a run", n)
	}
	code, out, _ := runCheck(t, decodeJSON(t, digest), record)
	wantWarnings := []string{"the block u-bad cannot be read (the solidtrace-findings block has no closing ```): the record has none of its findings"}
	if code != 0 || !reflect.DeepEqual(out.Warnings, wantWarnings) {
		t.Fatalf("review check: exit %d, %+v", code, out)
	}
}

func TestReviewRecordGivesAReviewStartARunOfItsOwnWhenOnlyABlockThatCannotBeReadFollows(t *testing.T) {
	digest := recDigest(
		`{"session": "s1", "uuid": "u-start", "kind": "review_start", "skill": "solidtrace-skills:code-review", "trigger": "person"}`,
		`{"session": "s1", "uuid": "u-bad", "kind": "findings", "findings": null, "error": "unexpected end of JSON input"}`)

	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", digest), "--notes", writeText(t, "notes.json", storyNotes))

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	reviews := record["reviews"].([]any)
	if len(reviews) != 1 {
		t.Fatalf("reviews: %v, want one run of the review start", reviews)
	}
	if rv := reviews[0].(map[string]any); rv["start"] != "u-start" || rv["skill"] != "solidtrace-skills:code-review" {
		t.Errorf("review: %v", rv)
	}
	code, out, _ := runCheck(t, decodeJSON(t, digest), record)
	if code != 0 || len(out.Problems) != 0 {
		t.Fatalf("review check: exit %d, %+v", code, out)
	}
}

func TestReviewRecordTakesWhatTheNotesGive(t *testing.T) {
	notes := `{"story": {"started_from": "x", "changes": ["c"]},
	 "events": [{"finding": "1.F6", "type": "fix", "files": ["a.go"], "commit": "` + hashA + `", "source": "record", "evidence": ["u-person"]}]}`

	code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", recordDigest), "--notes", writeText(t, "notes.json", notes))

	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	story := map[string]any{"started_from": "x", "changes": []any{"c"}, "reviews": []any{}, "after_review": []any{}, "sessions": []any{}}
	if !reflect.DeepEqual(record["story"], story) {
		t.Errorf("story: %v", record["story"])
	}
	events := recordFinding(t, record, "1.F6")["events"].([]any)
	if fix := events[len(events)-1].(map[string]any); fix["type"] != "fix" || fix["commit"] != hashA || !reflect.DeepEqual(fix["files"], []any{"a.go"}) {
		t.Errorf("fix: %v", fix)
	}
}

func TestReviewRecordAndReviewCheckWarnOfABlockThatIsNotAJSONObject(t *testing.T) {
	for _, body := range []string{"null", `[{"id": "F1"}]`} {
		path := transcript(t, assistantLine("a-block", text("Reviewed.\n```solidtrace-findings\n"+body+"\n```")))
		code, digest, stderr := runDigest(t, command.Env{}, "--file", path)
		if code != 0 {
			t.Fatalf("%s: review digest: exit %d\n%s", body, code, stderr)
		}

		code, record, stderr := runRecord(t, "--digest", writeText(t, "digest.json", digest), "--notes", writeText(t, "notes.json", storyNotes))

		want := "solidtrace review record: warning: the block a-block cannot be read (the solidtrace-findings block is not a JSON object): the record has none of its findings\n"
		if code != 0 || stderr != want {
			t.Fatalf("%s: exit %d\nstderr: %s", body, code, stderr)
		}
		if n := len(record["reviews"].([]any)); n != 0 {
			t.Errorf("%s: %d reviews, want 0: a block that is not a JSON object is not a run", body, n)
		}
		code, out, _ := runCheck(t, decodeJSON(t, digest), record)
		wantWarnings := []string{"the block a-block cannot be read (the solidtrace-findings block is not a JSON object): the record has none of its findings"}
		if code != 0 || len(out.Problems) != 0 || !reflect.DeepEqual(out.Warnings, wantWarnings) {
			t.Fatalf("%s: review check: exit %d, %+v", body, code, out)
		}
	}
}
