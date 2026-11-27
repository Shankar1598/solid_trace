package command_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/command"
)

// finalSession is an Agent session with its main transcript in a Claude
// project folder of a projects folder.
const finalSession = "6f1c2a7e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"

// finalProjects is a projects folder with the transcript of finalSession;
// it answers the projects folder and the project folder.
func finalProjects(t *testing.T) (string, string) {
	t.Helper()
	projects := filepath.Join(t.TempDir(), "projects")
	project := filepath.Join(projects, "-home-ann-shop")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, finalSession+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return projects, project
}

// runFinal runs review final on a file with record in it, in the Agent
// session finalSession of projects.
func runFinal(t *testing.T, projects, record string, args ...string) (int, string, string) {
	t.Helper()
	file := writeText(t, "record.json", record)
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), append([]string{"review", "final", file}, args...),
		command.Env{Stdout: &stdout, Stderr: &stderr, ClaudeProjectsDir: projects, ClaudeSessionID: finalSession})
	return code, stdout.String(), stderr.String()
}

// recordFile is the path of the file of round of pr in project.
func recordFile(project string, round int, pr string) string {
	sum := sha256.Sum256([]byte(pr))
	return filepath.Join(project, finalSession, "solidtrace", "review-records", fmt.Sprintf("round-%d-%s.json", round, hex.EncodeToString(sum[:])[:12]))
}

// finalRecord is a record of round 2 with three findings, two of them
// fixed.
const finalRecord = `{"version":2,"sessions":[{"id":"s1","title":"Parser"}],"pr":"local:/tmp/pr.md","round":2,` +
	`"story":{"started_from":"s","changes":[],"reviews":[],"after_review":[],"sessions":[]},"reviews":[],` +
	`"findings":[{"id":"1.F1","found_by":{"kind":"reviewer","name":"bugs"},"events":[],"outcome":"fixed","confidence":"high"},` +
	`{"id":"P1","found_by":{"kind":"person"},"events":[],"outcome":"fixed","confidence":"high"},` +
	`{"id":"1.F2","found_by":{"kind":"reviewer","name":"bugs"},"events":[],"outcome":"deferred","confidence":"high"}],` +
	`"confirmations":[]}`

func TestReviewFinalWritesTheRecordToTheFolderOfItsAgentSession(t *testing.T) {
	projects, project := finalProjects(t)

	code, out, stderr := runFinal(t, projects, finalRecord+"\n")

	want := recordFile(project, 2, "local:/tmp/pr.md")
	if code != 0 || stderr != "" || out != "record: round 2, 3 findings, 2 fixed, kept in "+want+"\n" {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
	if got, err := os.ReadFile(want); err != nil || string(got) != finalRecord+"\n" {
		t.Fatalf("Expected the record as the file has it in %s, got %q: %v", want, got, err)
	}
	if left, _ := filepath.Glob(filepath.Join(project, ".*")); len(left) != 0 {
		t.Errorf("Expected no temporary file left, got %v", left)
	}
}

func TestReviewFinalWritesARoundAgainOverItsFile(t *testing.T) {
	projects, project := finalProjects(t)
	again := strings.Replace(finalRecord, `"outcome":"deferred"`, `"outcome":"fixed"`, 1)

	runFinal(t, projects, finalRecord)
	code, out, stderr := runFinal(t, projects, again)

	files, _ := filepath.Glob(filepath.Join(project, finalSession, "solidtrace", "review-records", "*"))
	if code != 0 || !strings.HasPrefix(out, "record: round 2, 3 findings, 3 fixed") || len(files) != 1 {
		t.Fatalf("exit %d, files %v\nstdout: %s\nstderr: %s", code, files, out, stderr)
	}
	if got, _ := os.ReadFile(files[0]); string(got) != again {
		t.Errorf("Expected the record of the second call, got %s", got)
	}
}

func TestReviewFinalTakesARecordOfRound1WithNoPRAndNoFindings(t *testing.T) {
	projects, project := finalProjects(t)
	record := `{"version":2,"sessions":[],"pr":null,"round":1,"story":{"started_from":"s","changes":[],"reviews":[],"after_review":[],"sessions":[]},` +
		`"reviews":[],"findings":[],"confirmations":[]}`

	code, out, stderr := runFinal(t, projects, record)

	if code != 0 || stderr != "" || out != "record: round 1, 0 findings, 0 fixed, kept in "+recordFile(project, 1, "")+"\n" {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewFinalTakesTheRecordThatReviewRecordPrints(t *testing.T) {
	projects, _ := finalProjects(t)
	var record bytes.Buffer
	digest := writeText(t, "digest.json", recordDigest)
	if code := command.Run(context.Background(), []string{"review", "record", "--digest", digest, "--notes", writeText(t, "notes.json", recordNotes), "--round", "3",
		"--pr-body", writeText(t, "body.md", "## What changes\n")},
		command.Env{Stdout: &record, Stderr: &bytes.Buffer{}}); code != 0 {
		t.Fatalf("review record: exit %d", code)
	}

	code, out, stderr := runFinal(t, projects, record.String())

	if code != 0 || stderr != "" || !strings.HasPrefix(out, "record: round 3, ") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewFinalRefusesWhatIsNotARecordOfVersion2AndWritesNothing(t *testing.T) {
	cases := map[string]struct{ record, want string }{
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
		"round 2^32":         {strings.Replace(finalRecord, `"round":2`, `"round":4294967296`, 1), "round 4294967296 is more than 4294967295"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			projects, project := finalProjects(t)

			code, out, stderr := runFinal(t, projects, c.record)

			if code != 1 || out != "" || !strings.HasPrefix(stderr, "solidtrace review final: ") || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
			}
			if _, err := os.Stat(filepath.Join(project, finalSession)); !os.IsNotExist(err) {
				t.Errorf("Expected no folder of the Agent session, got %v", err)
			}
		})
	}
}

func TestReviewFinalNeedsAnAgentSessionWithATranscript(t *testing.T) {
	projects, _ := finalProjects(t)
	file := writeText(t, "record.json", finalRecord)
	run := func(env command.Env, args ...string) (int, string) {
		var stderr bytes.Buffer
		env.Stdout, env.Stderr, env.ClaudeProjectsDir = &bytes.Buffer{}, &stderr, projects
		return command.Run(context.Background(), append([]string{"review", "final", file}, args...), env), stderr.String()
	}

	if code, stderr := run(command.Env{}); code != 1 || !strings.Contains(stderr, "no Agent session: run this from Claude Code, or give --session") {
		t.Errorf("no session: exit %d: %s", code, stderr)
	}
	other := "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
	if code, stderr := run(command.Env{ClaudeSessionID: other}); code != 1 || !strings.Contains(stderr, "no transcript of the Agent session "+other) {
		t.Errorf("no transcript: exit %d: %s", code, stderr)
	}
	if code, stderr := run(command.Env{ClaudeSessionID: other}, "--session", finalSession); code != 0 {
		t.Errorf("--session before the environment: exit %d: %s", code, stderr)
	}
	if code, stderr := run(command.Env{ClaudeSessionID: "../x"}); code != 1 || !strings.Contains(stderr, "is not an Agent session id") {
		t.Errorf("a session id that is a path: exit %d: %s", code, stderr)
	}
}

func TestReviewFinalTakesOneFile(t *testing.T) {
	var stderr bytes.Buffer
	for _, args := range [][]string{{}, {"a.json", "b.json"}} {
		stderr.Reset()
		code := command.Run(context.Background(), append([]string{"review", "final"}, args...), command.Env{Stdout: &bytes.Buffer{}, Stderr: &stderr})
		if code != 2 || !strings.Contains(stderr.String(), "give the record's file") {
			t.Errorf("%v: exit %d: %s", args, code, stderr.String())
		}
	}
	projects, _ := finalProjects(t)
	if code, _, stderr := runFinal(t, projects, finalRecord, "--round", "2"); code != 2 {
		t.Errorf("an unknown flag: exit %d: %s", code, stderr)
	}
}

func TestReviewFinalTakesAFileOfUpTo32MiBTheMostThatTheCaptureSendsWhole(t *testing.T) {
	projects, _ := finalProjects(t)
	pad := func(n int) string { return finalRecord + strings.Repeat(" ", n-len(finalRecord)) }

	if code, _, stderr := runFinal(t, projects, pad(capture.UnitLimit)); code != 0 {
		t.Fatalf("32 MiB: exit %d: %s", code, stderr)
	}
	code, out, stderr := runFinal(t, projects, pad(capture.UnitLimit+1))
	if code != 1 || out != "" || !strings.Contains(stderr, fmt.Sprintf("is more than %d bytes", capture.UnitLimit)) {
		t.Fatalf("32 MiB and 1 byte: exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewFinalSaysSoWhenItCannotWriteTheFile(t *testing.T) {
	projects, project := finalProjects(t)
	writeFile(t, filepath.Join(project, finalSession), "not a folder")

	code, out, stderr := runFinal(t, projects, finalRecord)

	if code != 1 || out != "" || !strings.HasPrefix(stderr, "solidtrace review final: ") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewFinalRefusesAFolderOfTheRecordThatIsASymbolicLink(t *testing.T) {
	for _, link := range []string{"", "solidtrace", "solidtrace/review-records"} {
		t.Run("<id>/"+link, func(t *testing.T) {
			projects, project := finalProjects(t)
			target := t.TempDir()
			at := filepath.Join(project, finalSession, link)
			if err := os.MkdirAll(filepath.Dir(at), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, at); err != nil {
				t.Fatal(err)
			}

			code, out, stderr := runFinal(t, projects, finalRecord)

			if code != 1 || out != "" || !strings.Contains(stderr, at+" is a symbolic link") {
				t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
			}
			if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
				t.Errorf("Expected nothing in the link's target, got %v, %v", entries, err)
			}
		})
	}
}

func TestReviewFinalWithTwoTranscriptsOfTheSessionWritesToTheFolderOfTheNewer(t *testing.T) {
	projects, project := finalProjects(t)
	copied := filepath.Join(projects, "-home-ann-sh")
	writeFile(t, filepath.Join(copied, finalSession+".jsonl"), "{}\n")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(copied, finalSession+".jsonl"), old, old); err != nil {
		t.Fatal(err)
	}

	code, out, stderr := runFinal(t, projects, finalRecord)

	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "kept in "+recordFile(project, 2, "local:/tmp/pr.md")) {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(copied, finalSession)); !os.IsNotExist(err) {
		t.Errorf("Expected no folder of the Agent session in the older copy, got %v", err)
	}
}

func TestTheFinalReplyOfOpenPRWritesTheRecordFile(t *testing.T) {
	data, err := os.ReadFile(openPRSkill)
	if err != nil {
		t.Fatal(err)
	}
	_, step, _ := strings.Cut(string(data), "## 8. Final reply")
	step, _, _ = strings.Cut(step, "\n## ")
	_, block, found := strings.Cut(step, "```bash\n")
	block, _, _ = strings.Cut(block, "```")
	if !found || strings.TrimSpace(block) != `solidtrace review final "$dir/record.json"` {
		t.Fatalf("Expected the Final reply step to give the review final command on the record file, got %q", block)
	}
	update, err := os.ReadFile(updatePRSkill)
	if err != nil || !strings.Contains(string(update), "write the record with `solidtrace review final` as `open-pr` › Final reply says") {
		t.Fatalf("Expected update-pr to write the record as open-pr's Final reply says: %v", err)
	}

	projects, project := finalProjects(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "record.json"), finalRecord)
	args := strings.Fields(strings.ReplaceAll(strings.TrimSpace(block), `"$dir/record.json"`, filepath.Join(dir, "record.json")))
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), args[1:], command.Env{Stdout: &stdout, Stderr: &stderr, ClaudeProjectsDir: projects, ClaudeSessionID: finalSession})
	if got, _ := os.ReadFile(recordFile(project, 2, "local:/tmp/pr.md")); code != 0 || string(got) != finalRecord {
		t.Fatalf("Expected the skill's command to write the record file, got exit %d: %s", code, stderr.String())
	}
}
