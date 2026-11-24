package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/command"
)

type sessionList struct {
	Branch   string `json:"branch"`
	Base     string `json:"base"`
	Commits  int    `json:"commits"`
	Files    int    `json:"files"`
	Sessions []struct {
		ID      string    `json:"id"`
		Title   string    `json:"title"`
		First   time.Time `json:"first"`
		Last    time.Time `json:"last"`
		Current bool      `json:"current"`
		Strong  bool      `json:"strong"`
		Links   struct {
			Commits     int `json:"commits"`
			BranchLines int `json:"branch_lines"`
			Files       int `json:"files"`
		} `json:"links"`
	} `json:"sessions"`
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// prRepository makes a repository whose branch feature/x has two commits
// over main, with feature/x checked out, and a worktree of it outside its
// folder.
func prRepository(t *testing.T) (repo, worktree string) {
	repo, worktree = tempDir(t), filepath.Join(tempDir(t), "wt")
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README.md"), "start\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Start")
	git(t, repo, "checkout", "-q", "-b", "feature/x")
	writeFile(t, filepath.Join(repo, "parser", "a.go"), "package parser\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Add the parser")
	writeFile(t, filepath.Join(repo, "parser", "b.go"), "package parser\n")
	writeFile(t, filepath.Join(repo, "docs", "parser.md"), "# Parser\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Fix the parser's errors", "-m", "A body.")
	git(t, repo, "worktree", "add", "-q", "-b", "other", worktree, "main")
	return repo, worktree
}

// writeTranscript writes lines as the transcript at path.
func writeTranscript(t *testing.T, path string, lines ...map[string]any) {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		data, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(data, '\n'))
	}
	writeFile(t, path, b.String())
}

// at sets the time, the cwd and the git branch of a transcript line.
func at(l map[string]any, time, cwd, branch string) map[string]any {
	l["timestamp"], l["cwd"], l["gitBranch"] = time, cwd, branch
	return l
}

const (
	sCommit   = "11111111-1111-4111-8111-111111111111"
	sBranch   = "22222222-2222-4222-8222-222222222222"
	sWorktree = "33333333-3333-4333-8333-333333333333"
	sOther    = "44444444-4444-4444-8444-444444444444"
	sNoLink   = "55555555-5555-4555-8555-555555555555"
	sOld      = "66666666-6666-4666-8666-666666666666"
)

// sessionsFixture writes the Agent sessions of the tests in a projects
// folder:
//   - sCommit made a commit of the PR in a subagent, and edited a PR file
//     in a subagent's worktree that is not there now.
//   - sBranch has lines on the PR's branch.
//   - sWorktree edited a PR file in a worktree of the repository.
//   - sOther edited a file of the same name in another repository.
//   - sNoLink is in the repository and has no link.
//   - sOld made a commit of the PR, but its transcript changed 30 days ago.
func sessionsFixture(t *testing.T) (env command.Env, repo string) {
	repo, worktree := prRepository(t)
	other := tempDir(t)
	git(t, other, "init", "-q", "-b", "main")
	projects := tempDir(t)
	folder := filepath.Join(projects, "-repo")
	main := func(id string) string { return filepath.Join(folder, id+".jsonl") }
	gone := filepath.Join(repo, ".claude", "worktrees", "agent-gone")

	writeTranscript(t, main(sCommit),
		at(userLine("c1", "Build the parser."), "2026-11-03T08:00:00Z", repo, "main"),
		map[string]any{"type": "custom-title", "customTitle": "Parser work"},
		map[string]any{"type": "ai-title", "aiTitle": "Build a parser"},
		at(assistantLine("c2", text("I start a subagent.")), "2026-11-03T08:00:05Z", repo, "main"),
	)
	writeTranscript(t, filepath.Join(folder, sCommit, "subagents", "agent-a1.jsonl"),
		at(userLine("x1", "Add the parser."), "2026-11-03T08:01:00Z", gone, "main"),
		at(assistantLine("x2", toolUse("Edit", map[string]any{"file_path": filepath.Join(gone, "parser", "a.go")})), "2026-11-03T08:02:00Z", gone, "main"),
		at(assistantLine("x3", toolUse("Bash", map[string]any{"command": "git commit -F - <<'EOF'\nAdd   the\nparser\n\nThe body.\nEOF"})), "2026-11-03T08:03:00Z", gone, "main"),
	)
	writeTranscript(t, main(sBranch),
		at(userLine("b1", "Look at the branch."), "2026-11-03T09:00:00Z", repo, "feature/x"),
		at(assistantLine("b2", text("Done.")), "2026-11-03T09:01:00Z", repo, "feature/x"),
	)
	writeTranscript(t, main(sWorktree),
		at(userLine("w0", "<command-name>/clear</command-name>"), "2026-11-03T10:00:00Z", worktree, "other"),
		at(userLine("w1", strings.Repeat("Fix the errors of the parser. ", 4)), "2026-11-03T10:00:01Z", worktree, "other"),
		at(assistantLine("w2",
			toolUse("Write", map[string]any{"file_path": filepath.Join(worktree, "parser", "b.go")}),
			toolUse("Edit", map[string]any{"file_path": filepath.Join(worktree, "parser", "b.go")}),
			toolUse("Edit", map[string]any{"file_path": filepath.Join(worktree, "README.md")})), "2026-11-03T10:01:00Z", worktree, "other"),
	)
	writeTranscript(t, main(sOther),
		at(userLine("o1", "Another repository."), "2026-11-03T12:00:00Z", other, "main"),
		at(assistantLine("o2", toolUse("Edit", map[string]any{"file_path": filepath.Join(other, "parser", "a.go")})), "2026-11-03T12:01:00Z", other, "main"),
	)
	writeTranscript(t, main(sNoLink),
		at(userLine("n1", "Something else."), "2026-11-03T11:00:00Z", repo, "main"),
	)
	writeTranscript(t, main(sOld),
		at(userLine("d1", "An old session."), "2026-11-01T07:00:00Z", repo, "main"),
		at(assistantLine("d2", toolUse("Bash", map[string]any{"command": "git commit -m \"Fix the parser's errors\""})), "2026-11-01T07:01:00Z", repo, "main"),
	)
	month := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(main(sOld), month, month); err != nil {
		t.Fatal(err)
	}
	return command.Env{Dir: repo, ClaudeProjectsDir: projects}, repo
}

func runSessions(t *testing.T, env command.Env, args ...string) sessionList {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env.Stdout, env.Stderr = &stdout, &stderr
	if code := command.Run(context.Background(), append([]string{"review", "sessions"}, args...), env); code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
	var list sessionList
	if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
		t.Fatalf("%v\n%s", err, stdout.String())
	}
	return list
}

func ids(list sessionList) []string {
	got := []string{}
	for _, s := range list.Sessions {
		got = append(got, s.ID)
	}
	return got
}

func TestReviewSessionsListsTheSessionsLinkedToTheBranch(t *testing.T) {
	env, repo := sessionsFixture(t)

	list := runSessions(t, env)

	if list.Branch != "feature/x" || list.Base != git(t, repo, "rev-parse", "main") || list.Commits != 2 || list.Files != 3 {
		t.Fatalf("list: %+v", list)
	}
	if got := ids(list); !reflect.DeepEqual(got, []string{sCommit, sBranch, sWorktree}) {
		t.Fatalf("sessions: %v", got)
	}
	commit, branch, worktree := list.Sessions[0], list.Sessions[1], list.Sessions[2]
	if commit.Title != "Parser work" || !commit.Strong || commit.Current ||
		commit.Links.Commits != 1 || commit.Links.BranchLines != 0 || commit.Links.Files != 1 ||
		!commit.First.Equal(time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)) || !commit.Last.Equal(time.Date(2026, 11, 3, 8, 3, 0, 0, time.UTC)) {
		t.Errorf("the session with a commit: %+v", commit)
	}
	if branch.Strong || branch.Links.Commits != 0 || branch.Links.BranchLines != 2 || branch.Links.Files != 0 {
		t.Errorf("the session on the branch: %+v", branch)
	}
	if worktree.Strong || worktree.Links.Files != 1 || worktree.Links.BranchLines != 0 ||
		worktree.Title != strings.Repeat("Fix the errors of the parser. ", 4)[:80]+"…" {
		t.Errorf("the session in a worktree: %+v", worktree)
	}
}

func TestReviewSessionsAllAlsoListsTheSessionsWithNoLink(t *testing.T) {
	env, _ := sessionsFixture(t)

	list := runSessions(t, env, "--all")

	if got := ids(list); !reflect.DeepEqual(got, []string{sCommit, sBranch, sWorktree, sNoLink}) {
		t.Fatalf("sessions: %v", got)
	}
	if s := list.Sessions[3]; s.Title != "Something else." || s.Strong || s.Links.Commits+s.Links.BranchLines+s.Links.Files != 0 {
		t.Errorf("the session with no link: %+v", s)
	}
}

func TestReviewSessionsAlwaysListsTheCurrentSession(t *testing.T) {
	env, repo := sessionsFixture(t)
	env.Dir = filepath.Join(repo, "parser")

	list := runSessions(t, env, "--session", sOld, "--branch", "feature/x", "--base", "main")

	if got := ids(list); !reflect.DeepEqual(got, []string{sOld, sCommit, sBranch, sWorktree}) {
		t.Fatalf("sessions: %v", got)
	}
	if old := list.Sessions[0]; !old.Current || !old.Strong || old.Links.Commits != 1 {
		t.Errorf("the current session: %+v", old)
	}
	for _, s := range list.Sessions[1:] {
		if s.Current {
			t.Errorf("%s is not the current session", s.ID)
		}
	}

	if list := runSessions(t, env, "--days", "40"); !reflect.DeepEqual(ids(list), []string{sOld, sCommit, sBranch, sWorktree}) {
		t.Errorf("--days 40: %v", ids(list))
	}
}

func TestReviewSessionsRefusesWrongArguments(t *testing.T) {
	env, _ := sessionsFixture(t)
	for args, want := range map[string]int{
		"--days 0":       2,
		"extra":          2,
		"--session ../x": 1,
		"--session 77777777-7777-4777-8777-777777777777": 1,
		"--branch nothing": 1,
	} {
		var stderr bytes.Buffer
		env.Stdout, env.Stderr = &bytes.Buffer{}, &stderr
		if code := command.Run(context.Background(), append([]string{"review", "sessions"}, strings.Fields(args)...), env); code != want {
			t.Errorf("%s: exit %d, want %d\n%s", args, code, want, stderr.String())
		}
	}
	env.Dir = t.TempDir()
	if code := command.Run(context.Background(), []string{"review", "sessions"}, env); code != 1 {
		t.Errorf("outside a repository: exit %d, want 1", code)
	}
}
