package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/solidtrace/cli/command"
)

// checkDigest has one run (u-block) with the findings F1 and F2, a commit
// that fixes F1, an update block that records the fix, and a person's
// message.
const checkDigest = `{"sessions": [{"id": "s1", "title": "Parser work"}], "reviews": 0, "events": [
 {"seq": 0, "session": "s1", "uuid": "u-task", "time": "2026-11-03T08:00:00Z", "kind": "task", "text": "Build the parser."},
 {"seq": 1, "session": "s1", "uuid": "u-block", "time": "2026-11-03T08:10:00Z", "kind": "findings",
  "findings": {"version": 2, "run": 1, "skill": "code-review", "findings": [
   {"id": "F1", "events": [{"type": "found"}, {"type": "triage", "decision": "trivial", "why": "small"},
                           {"type": "verify", "verdict": "CONFIRMED", "pushed_back": true, "why": "real"},
                           {"type": "triage", "decision": "fix"}]},
   {"id": "F2", "events": [{"type": "found"}, {"type": "triage", "decision": "trivial", "why": "a name"}]}]}},
 {"seq": 2, "session": "s1", "uuid": "u-commit", "time": "2026-11-03T08:20:00Z", "kind": "commit",
  "command": "git commit -m fix", "review_findings": ["run 1: F1"]},
 {"seq": 3, "session": "s1", "uuid": "u-update", "time": "2026-11-03T08:21:00Z", "kind": "findings",
  "findings": {"version": 2, "update": 1, "findings": [{"id": "F1", "events": [{"type": "fix", "commit": "abc", "files": ["a.go"]}]}]}},
 {"seq": 4, "session": "s1", "uuid": "u-person", "time": "2026-11-03T08:30:00Z", "kind": "person", "text": "Do the docs later."}]}`

const checkRecord = `{"version": 2, "sessions": [{"id": "s1", "title": "Parser work"}], "pr": null, "round": 1,
 "story": {"started_from": "Build the parser.", "changes": ["The parser."], "reviews": ["Run 1: 2 findings."],
           "after_review": [], "sessions": ["s1: built it."]},
 "reviews": [{"run": 1, "block_run": 1, "session": "s1", "skill": "code-review", "start": "u-block"}],
 "findings": [
  {"id": "1.F1", "block_id": "F1", "run": 1, "found_by": {"kind": "reviewer", "name": "bugs-and-spec"}, "events": [
    {"type": "found", "source": "block", "evidence": ["u-block"]},
    {"type": "triage", "decision": "trivial", "source": "block", "evidence": ["u-block"]},
    {"type": "verify", "verdict": "CONFIRMED", "pushed_back": true, "source": "block", "evidence": ["u-block"]},
    {"type": "triage", "decision": "fix", "source": "block", "evidence": ["u-block"]},
    {"type": "fix", "commit": "abc", "files": ["a.go"], "source": "block", "evidence": ["u-update"]}],
   "outcome": "fixed", "confidence": "high"},
  {"id": "1.F2", "block_id": "F2", "run": 1, "found_by": {"kind": "reviewer", "name": "code-quality"}, "events": [
    {"type": "found", "source": "block", "evidence": ["u-block"]},
    {"type": "triage", "decision": "trivial", "source": "block", "evidence": ["u-block"]}],
   "outcome": "ignored_trivial", "confidence": "high"},
  {"id": "P1", "block_id": null, "run": null, "after_run": 1, "found_by": {"kind": "person"}, "events": [
    {"type": "person", "said": "Do the docs later.", "asks": "defer", "source": "record", "evidence": ["u-person"]}],
   "outcome": "deferred", "confidence": "high"}],
 "confirmations": [{"id": "1.C1", "run": 1, "given": true, "evidence": ["u-person"]}]}`

type checkOutput struct {
	Runs       int            `json:"runs"`
	Outcomes   map[string]int `json:"outcomes"`
	FoundBy    map[string]int `json:"found_by"`
	PushedBack int            `json:"pushed_back"`
	Problems   []string       `json:"problems"`
}

func decodeJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func writeJSON(t *testing.T, path string, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data))
	return path
}

func runCheck(t *testing.T, digest, record map[string]any) (int, checkOutput, string) {
	t.Helper()
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), []string{"review", "check",
		"--digest", writeJSON(t, filepath.Join(dir, "digest.json"), digest),
		"--record", writeJSON(t, filepath.Join(dir, "record.json"), record)},
		command.Env{Stdout: &stdout, Stderr: &stderr})
	var out checkOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("exit %d: %v\nstdout: %s\nstderr: %s", code, err, stdout.String(), stderr.String())
	}
	return code, out, stderr.String()
}

func findingAt(record map[string]any, i int) map[string]any {
	return record["findings"].([]any)[i].(map[string]any)
}

func eventAt(record map[string]any, finding, event int) map[string]any {
	return findingAt(record, finding)["events"].([]any)[event].(map[string]any)
}

func addEvent(record map[string]any, finding int, event map[string]any) {
	f := findingAt(record, finding)
	f["events"] = append(f["events"].([]any), event)
}

func digestEvent(digest map[string]any, i int) map[string]any {
	return digest["events"].([]any)[i].(map[string]any)
}

func TestReviewCheckPassesARecordThatAgreesWithItsDigest(t *testing.T) {
	code, out, stderr := runCheck(t, decodeJSON(t, checkDigest), decodeJSON(t, checkRecord))

	if code != 0 {
		t.Fatalf("exit %d, problems %v, stderr %s", code, out.Problems, stderr)
	}
	want := checkOutput{Runs: 1, Outcomes: map[string]int{"fixed": 1, "ignored_trivial": 1, "deferred": 1},
		FoundBy: map[string]int{"reviewer": 2, "person": 1}, PushedBack: 1, Problems: []string{}}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("output %+v, want %+v", out, want)
	}
}

func TestReviewCheckNamesEachProblemOfARecord(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(digest, record map[string]any)
		want  string
	}{
		{"an unknown outcome", func(_, r map[string]any) { findingAt(r, 2)["outcome"] = "parked" },
			`P1: the outcome "parked" is not known`},
		{"an unknown event type", func(_, r map[string]any) { eventAt(r, 2, 0)["type"] = "chat" },
			`P1: the event type "chat" is not known`},
		{"an unknown decision", func(_, r map[string]any) { eventAt(r, 1, 1)["decision"] = "skip" },
			`1.F2: the decision "skip" is not known`},
		{"an unknown found_by kind", func(_, r map[string]any) { findingAt(r, 2)["found_by"] = map[string]any{"kind": "bot"} },
			`P1: the found_by kind "bot" is not known`},
		{"no found_by", func(_, r map[string]any) { delete(findingAt(r, 2), "found_by") },
			`P1: the found_by kind "" is not known`},
		{"an event with no source", func(_, r map[string]any) { delete(eventAt(r, 2, 0), "source") },
			`P1: the person event has the source "", not block or record`},
		{"evidence that is not in the digest", func(_, r map[string]any) { eventAt(r, 2, 0)["evidence"] = []any{"u-nothing"} },
			"P1: the evidence u-nothing is not an event of the digest"},
		{"an event with no evidence", func(_, r map[string]any) { eventAt(r, 2, 0)["evidence"] = []any{} },
			"P1: the person event has no evidence"},
		{"a finding with no events", func(_, r map[string]any) { findingAt(r, 2)["events"] = []any{} },
			"P1: no events"},
		{"an empty story key", func(_, r map[string]any) { r["story"].(map[string]any)["changes"] = []any{} },
			"story.changes is empty"},
		{"a missing story key", func(_, r map[string]any) { delete(r["story"].(map[string]any), "started_from") },
			"story.started_from is empty"},
		{"other sessions", func(_, r map[string]any) { r["sessions"] = []any{map[string]any{"id": "s2"}} },
			"the sessions of the record [s2] are not the sessions of the digest [s1]"},
		{"no reviews", func(_, r map[string]any) { r["reviews"] = []any{} },
			"the digest has 1 runs, and the record has no reviews"},
		{"a block finding that is not in the record", func(_, r map[string]any) {
			r["findings"] = []any{findingAt(r, 0), findingAt(r, 2)}
		}, "F2 of the block u-block is not in the record"},
		{"fewer block events than the block", func(_, r map[string]any) {
			f := findingAt(r, 0)
			events := f["events"].([]any)
			f["events"] = append(append([]any{}, events[:2]...), events[3:]...)
		}, "F1 of the block u-block: the record has 3 block events, and the block has 4"},
		{"an update block that is not in the record", func(_, r map[string]any) {
			eventAt(r, 0, 4)["source"], eventAt(r, 0, 4)["evidence"] = "record", []any{"u-commit"}
		}, "F1 of the block u-update is not in the record"},
		{"fixed with no fix event", func(_, r map[string]any) { findingAt(r, 1)["outcome"] = "fixed" },
			"1.F2: the outcome is fixed, and no event is a fix"},
		{"ignored_trivial with a fix event", func(_, r map[string]any) {
			addEvent(r, 1, map[string]any{"type": "fix", "files": []any{"a.go"}, "source": "record", "evidence": []any{"u-commit"}})
		}, "1.F2: the outcome is ignored_trivial, and an event is a fix"},
		{"ignored_trivial after a decision to fix", func(_, r map[string]any) {
			addEvent(r, 1, map[string]any{"type": "triage", "decision": "fix", "source": "record", "evidence": []any{"u-person"}})
		}, `1.F2: the outcome is ignored_trivial, and the last triage decision is "fix", not "trivial"`},
		{"disputed with no decision", func(_, r map[string]any) { findingAt(r, 2)["outcome"] = "disputed" },
			`P1: the outcome is disputed, and the last triage decision is "", not "dispute"`},
		{"deferred with no defer", func(_, r map[string]any) { eventAt(r, 2, 0)["asks"] = "fix" },
			`P1: the outcome is deferred, and the last triage decision is "", not "defer"`},
		{"a trailer that names a finding with no fix", func(d, _ map[string]any) {
			digestEvent(d, 2)["review_findings"] = []any{"run 1: F1, F2"}
		}, `the commit u-commit: "run 1: F1, F2" names F2, which has no fix event in the record`},
		{"a trailer of a run that the record does not have", func(d, _ map[string]any) {
			digestEvent(d, 2)["review_findings"] = []any{"run 2: F1"}
		}, `the commit u-commit: "run 2: F1" names F1, which has no fix event in the record`},
		{"a trailer with no run", func(d, _ map[string]any) { digestEvent(d, 2)["review_findings"] = []any{"F1"} },
			`the commit u-commit: the trailer value "F1" has no run`},
		{"an update block of another writer", func(d, _ map[string]any) { digestEvent(d, 3)["writer"] = "a1" },
			"the update block u-update names run 1, and the record has no review of that block_run from its session and writer"},
		{"an update block on a finding of another run", func(d, r map[string]any) {
			digestEvent(d, 3)["writer"] = "a1"
			r["reviews"] = append(r["reviews"].([]any), map[string]any{"run": 2, "block_run": 1, "session": "s1", "writer": "a1", "start": "u-update"})
		}, "F1 of the update block u-update is not on a finding of run [2]"},
		{"a trailer of another writer's run", func(_, r map[string]any) {
			r["reviews"].([]any)[0].(map[string]any)["writer"] = "a1"
		}, `the commit u-commit: "run 1: F1" names F1, which has no fix event in the record`},
		{"fixed and found again later", func(_, r map[string]any) {
			addEvent(r, 0, map[string]any{"type": "refound", "run": 2, "source": "block", "evidence": []any{"u-person"}})
		}, "1.F1: the outcome is fixed, and a later run found it again"},
		{"a confirmation with no given", func(_, r map[string]any) {
			delete(r["confirmations"].([]any)[0].(map[string]any), "given")
		}, "1.C1: given is not true or false"},
		{"a confirmation given as text", func(_, r map[string]any) {
			r["confirmations"].([]any)[0].(map[string]any)["given"] = "yes"
		}, "1.C1: given is not true or false"},
		{"round 0", func(_, r map[string]any) { r["round"] = 0 }, "round 0 is not an integer of 1 or more"},
		{"round 1.5", func(_, r map[string]any) { r["round"] = 1.5 }, "round 1.5 is not an integer of 1 or more"},
		{"round as text", func(_, r map[string]any) { r["round"] = "1" }, `round "1" is not an integer of 1 or more`},
		{"round null", func(_, r map[string]any) { r["round"] = nil }, "round null is not an integer of 1 or more"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			digest, record := decodeJSON(t, checkDigest), decodeJSON(t, checkRecord)
			c.spoil(digest, record)

			code, out, _ := runCheck(t, digest, record)

			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			found := false
			for _, p := range out.Problems {
				found = found || p == c.want
			}
			if !found {
				t.Fatalf("problems:\n%s\nwant %q", strings.Join(out.Problems, "\n"), c.want)
			}
		})
	}
}

func TestReviewCheckTakesATrailerOfARunThatFoundAFindingAgain(t *testing.T) {
	digest, record := decodeJSON(t, checkDigest), decodeJSON(t, checkRecord)
	digestEvent(digest, 2)["review_findings"] = []any{"run 2: F1"}
	record["reviews"] = append(record["reviews"].([]any), map[string]any{"run": 2, "block_run": 2, "session": "s1", "writer": "", "start": "u-person"})
	f := findingAt(record, 0)
	events := f["events"].([]any)
	refound := map[string]any{"type": "refound", "run": 2, "source": "record", "evidence": []any{"u-person"}}
	f["events"] = append(append(append([]any{}, events[:4]...), refound), events[4])

	code, out, _ := runCheck(t, digest, record)

	if code != 0 {
		t.Fatalf("exit %d, problems %v", code, out.Problems)
	}
}

func TestReviewCheckPassesARecordWithNoRound(t *testing.T) {
	record := decodeJSON(t, checkRecord)
	delete(record, "round")

	code, out, _ := runCheck(t, decodeJSON(t, checkDigest), record)

	if code != 0 {
		t.Fatalf("exit %d, problems %v", code, out.Problems)
	}
}

func TestReviewCheckCountsAReviewStartWithNoBlockAsARun(t *testing.T) {
	digest := decodeJSON(t, `{"sessions": [{"id": "s1"}], "events": [
	 {"seq": 0, "session": "s1", "uuid": "u-start", "kind": "review_start", "skill": "code-review", "trigger": "person"}]}`)
	record := decodeJSON(t, checkRecord)
	record["reviews"], record["findings"], record["confirmations"] = []any{}, []any{}, []any{}

	code, out, _ := runCheck(t, digest, record)

	if code != 1 || !reflect.DeepEqual(out.Problems, []string{"the digest has 1 runs, and the record has no reviews"}) {
		t.Fatalf("exit %d, problems %v", code, out.Problems)
	}
}

func TestReviewCheckExitsWith2WhenAFileCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	digest := writeJSON(t, filepath.Join(dir, "digest.json"), decodeJSON(t, checkDigest))
	notJSON := filepath.Join(dir, "record.json")
	writeFile(t, notJSON, "{not json")
	cases := map[string][]string{
		"no record flag":     {"--digest", digest},
		"no digest flag":     {"--record", notJSON},
		"an extra argument":  {"--digest", digest, "--record", notJSON, "more"},
		"a missing file":     {"--digest", digest, "--record", filepath.Join(dir, "none.json")},
		"a record not JSON":  {"--digest", digest, "--record", notJSON},
		"a digest not JSON":  {"--digest", notJSON, "--record", digest},
		"a record of a list": {"--digest", digest, "--record", writeJSON(t, filepath.Join(dir, "list.json"), []any{})},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := command.Run(context.Background(), append([]string{"review", "check"}, args...),
				command.Env{Stdout: &stdout, Stderr: &stderr})
			if code != 2 || stdout.Len() > 0 || !strings.Contains(stderr.String(), "solidtrace review check") {
				t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
			}
		})
	}
}
