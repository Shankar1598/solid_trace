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

func runFixed(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), append([]string{"review", "fixed"}, args...),
		command.Env{Dir: dir, Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

// fixedBlocks reads the solidtrace-findings blocks of out, and fails when
// out has text outside them.
func fixedBlocks(t *testing.T, out string) []any {
	t.Helper()
	var blocks []any
	for i, part := range strings.Split(strings.TrimSuffix(out, "\n"), "\n\n") {
		body, ok := strings.CutPrefix(part, "```solidtrace-findings\n")
		body, closed := strings.CutSuffix(body, "\n```")
		if !ok || !closed || strings.Contains(body, "\n") {
			t.Fatalf("block %d is not one fenced line of JSON:\n%s", i, out)
		}
		var v any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatalf("block %d: %v\n%s", i, err, out)
		}
		blocks = append(blocks, v)
	}
	return blocks
}

func fixBlock(run int, hash string, files []string, ids ...string) any {
	var findings []any
	for _, id := range ids {
		findings = append(findings, map[string]any{"id": id, "events": []any{
			map[string]any{"type": "fix", "commit": hash, "files": toAny(files)}}})
	}
	return map[string]any{"version": float64(2), "update": float64(run), "findings": findings}
}

func toAny(s []string) []any {
	out := []any{}
	for _, v := range s {
		out = append(out, v)
	}
	return out
}

// fixRepository makes a repository with a start commit, and a commit that
// changes two files and has Review-Findings trailers of two runs.
func fixRepository(t *testing.T) string {
	repo := tempDir(t)
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README.md"), "start\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Start")
	writeFile(t, filepath.Join(repo, "parser", "a.go"), "package parser\n")
	writeFile(t, filepath.Join(repo, "docs", "parser.md"), "# Parser\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Fix the parser's errors", "-m", "A body with a line that wraps at the trailer's name:\nReview-Findings: run <n>: <ids> is the form.",
		"-m", "Review-Findings: run 1: F1, F2\nReview-Findings: run 2: F3\nReview-Findings: run 1: F2, F4\nCo-Authored-By: T <t@example.com>")
	return repo
}

func TestReviewFixedPrintsOneUpdateBlockForEachRunOfTheTrailers(t *testing.T) {
	repo := fixRepository(t)
	hash := git(t, repo, "rev-parse", "HEAD")

	code, out, stderr := runFixed(t, repo)

	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	files := []string{"docs/parser.md", "parser/a.go"}
	want := []any{fixBlock(1, hash, files, "F1", "F2", "F4"), fixBlock(2, hash, files, "F3")}
	if got := fixedBlocks(t, out); !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks:\n%v\nwant\n%v", got, want)
	}
}

func TestReviewFixedReadsTheCommitThatYouGive(t *testing.T) {
	repo := fixRepository(t)
	hash := git(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "later.go"), "package later\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Later", "-m", "Review-Findings: run 3: F9")

	code, out, stderr := runFixed(t, repo, "HEAD~1")

	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := fixedBlocks(t, out); len(got) != 2 || !reflect.DeepEqual(got[1], fixBlock(2, hash, []string{"docs/parser.md", "parser/a.go"}, "F3")) {
		t.Fatalf("blocks: %v", got)
	}
}

func TestReviewFixedGivesAMergeTheFilesThatItBringsIn(t *testing.T) {
	repo := tempDir(t)
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README.md"), "start\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Start")
	git(t, repo, "checkout", "-q", "-b", "fix")
	writeFile(t, filepath.Join(repo, "fix.go"), "package fix\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "The fix")
	git(t, repo, "checkout", "-q", "main")
	writeFile(t, filepath.Join(repo, "main.go"), "package main\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Other work")
	git(t, repo, "merge", "-q", "--no-ff", "fix", "-m", "Merge the fix", "-m", "Review-Findings: run 1: F2")
	hash := git(t, repo, "rev-parse", "HEAD")

	code, out, stderr := runFixed(t, repo)

	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got, want := fixedBlocks(t, out), []any{fixBlock(1, hash, []string{"fix.go"}, "F2")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks:\n%v\nwant\n%v", got, want)
	}
}

func TestReviewFixedFailsWhenTheCommitHasNoUsableTrailer(t *testing.T) {
	repo := fixRepository(t)
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "No trailer")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "No run", "-m", "Review-Findings: F1")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "No ids", "-m", "Review-Findings: run 1:")
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no trailer", []string{"HEAD~2"}, 1, "has no Review-Findings trailer"},
		{"a trailer with no run", []string{"HEAD~1"}, 1, `the trailer value "F1" has no run`},
		{"a trailer with no ids", []string{"HEAD"}, 1, `the trailer value "run 1:" names no findings`},
		{"not a commit", []string{"nothing"}, 1, `"nothing" is not a commit`},
		{"two commits", []string{"HEAD", "HEAD~1"}, 2, "give one commit"},
		{"an option as the commit", []string{"--", "-x"}, 2, "give one commit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, stderr := runFixed(t, repo, c.args...)
			if code != c.code || out != "" || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s\nwant %q", code, c.code, out, stderr, c.want)
			}
		})
	}
}

func TestReviewFixedFailsWhenATrailerNamesAnIDThatIsNotOfABlock(t *testing.T) {
	repo := fixRepository(t)
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "Lower case", "-m", "Review-Findings: run 1: f1")

	code, out, stderr := runFixed(t, repo)

	want := `solidtrace review fixed: the update block of run 1 is not valid: findings[0].id: "f1" is not of the form F<n>` + "\n"
	if code != 1 || out != "" || stderr != want {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewFixedFailsOutsideARepository(t *testing.T) {
	code, out, stderr := runFixed(t, tempDir(t))

	if code != 1 || out != "" || !strings.Contains(stderr, "is not a commit") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, stderr)
	}
}

func TestReviewUsageListsEachVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	command.Run(context.Background(), []string{"--help"}, command.Env{Stdout: &stdout, Stderr: &stderr})
	for _, want := range []string{"solidtrace review check --digest PATH --record PATH", "solidtrace review fixed [COMMIT]",
		"solidtrace review block\n", "solidtrace review final\n", "solidtrace review record --digest PATH [--notes PATH] [--pr URL] [--round N]"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the usage has no %q", want)
		}
	}
	command.Run(context.Background(), []string{"review"}, command.Env{Stdout: &stdout, Stderr: &stderr})
	if !strings.Contains(stderr.String(), "give digest, sessions, check, fixed, block, record or final") {
		t.Errorf("review with no verb: %s", stderr.String())
	}
}
