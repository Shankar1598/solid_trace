package command_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/solidtrace/cli/command"
)

func runFinal(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), append([]string{"review", "final"}, args...),
		command.Env{Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

// finalRecord is a record of round 2 with three findings, two of them
// fixed.
const finalRecord = `{"version":2,"sessions":[{"id":"s1","title":"Parser"}],"pr":"local:/tmp/pr.md","round":2,` +
	`"story":{"started_from":"s","changes":[],"reviews":[],"after_review":[],"sessions":[]},"reviews":[],` +
	`"findings":[{"id":"1.F1","found_by":{"kind":"reviewer","name":"bugs"},"events":[],"outcome":"fixed","confidence":"high"},` +
	`{"id":"P1","found_by":{"kind":"person"},"events":[],"outcome":"fixed","confidence":"high"},` +
	`{"id":"1.F2","found_by":{"kind":"reviewer","name":"bugs"},"events":[],"outcome":"deferred","confidence":"high"}],` +
	`"confirmations":[]}`

func TestReviewFinalPrintsTheRoundAndTheCountsOfARecord(t *testing.T) {
	code, out, stderr := runFinal(t, finalRecord+"\n")

	if code != 0 || stderr != "" || out != "record: round 2, 3 findings, 2 fixed\n" {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewFinalTakesARecordOfRound1WithNoFindings(t *testing.T) {
	record := `{"version":2,"sessions":[],"pr":null,"round":1,"story":{"started_from":"s","changes":[],"reviews":[],"after_review":[],"sessions":[]},` +
		`"reviews":[],"findings":[],"confirmations":[]}`

	code, out, stderr := runFinal(t, record)

	if code != 0 || stderr != "" || out != "record: round 1, 0 findings, 0 fixed\n" {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewFinalTakesTheRecordThatReviewRecordPrints(t *testing.T) {
	var record bytes.Buffer
	digest := writeText(t, "digest.json", recordDigest)
	if code := command.Run(context.Background(), []string{"review", "record", "--digest", digest, "--notes", writeText(t, "notes.json", recordNotes), "--round", "3"},
		command.Env{Stdout: &record, Stderr: &bytes.Buffer{}}); code != 0 {
		t.Fatalf("review record: exit %d", code)
	}

	code, out, stderr := runFinal(t, record.String())

	if code != 0 || stderr != "" || !strings.HasPrefix(out, "record: round 3, ") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewFinalRefusesWhatIsNotARecordOfVersion2(t *testing.T) {
	cases := map[string]struct{ stdin, want string }{
		"no JSON":            {"", "no JSON"},
		"JSON with no end":   {finalRecord[:len(finalRecord)-1], "not a review record"},
		"version 1":          {strings.Replace(finalRecord, `"version":2`, `"version":1`, 1), "version 1 is not 2"},
		"no version":         {`{"round":1}`, "version 0 is not 2"},
		"a findings block":   {validUpdateBlock, `not a review record: json: unknown field "update"`},
		"an unknown field":   {strings.Replace(finalRecord, `"round":2`, `"round":2,"rnd":2`, 1), `unknown field "rnd"`},
		"two records":        {finalRecord + finalRecord, "text after the record"},
		"a list":             {`[2]`, "not a review record"},
		"a version 1 record": {`{"version":1,"session":{"id":"s1"},"findings":[]}`, "version 1 is not 2"},
		"only a version":     {`{"version":2}`, "no sessions"},
		"null findings":      {`{"version":2,"sessions":[],"round":1,"findings":null}`, "no findings"},
		"round 0":            {strings.Replace(finalRecord, `"round":2`, `"round":0`, 1), "round 0 is not 1 or more"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			code, out, stderr := runFinal(t, c.stdin)

			if code != 1 || out != "" || !strings.HasPrefix(stderr, "solidtrace review final: ") || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
			}
		})
	}
}

func TestReviewFinalTakesNoArgumentsAndNoFlags(t *testing.T) {
	if code, _, stderr := runFinal(t, finalRecord, "record.json"); code != 2 || !strings.Contains(stderr, "on stdin") {
		t.Fatalf("an argument: exit %d: %s", code, stderr)
	}
	if code, out, _ := runFinal(t, finalRecord, "--round", "2"); code != 2 || out != "" {
		t.Fatalf("a flag: exit %d: %s", code, out)
	}
}

func TestReviewFinalReadsAtMostOneMiBOfStdin(t *testing.T) {
	pad := func(n int) string { return finalRecord + strings.Repeat(" ", n-len(finalRecord)) }

	if code, _, stderr := runFinal(t, pad(1<<20)); code != 0 {
		t.Fatalf("1 MiB: exit %d: %s", code, stderr)
	}
	code, out, stderr := runFinal(t, pad(1<<20+1))
	if code != 1 || out != "" || stderr != "solidtrace review final: the record is more than 1048576 bytes\n" {
		t.Fatalf("1 MiB and 1 byte: exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}
