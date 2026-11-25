package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/review"
)

func reviewSessions(ctx context.Context, args []string, env Env) int {
	flags := flag.NewFlagSet("review sessions", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	branch := flags.String("branch", "", "the PR's branch (default: the current branch)")
	base := flags.String("base", "", "the PR's base (default: the merge base of the branch with origin/HEAD, else with main)")
	current := flags.String("session", "", "the id of the current Agent session, which is always in the list")
	days := flags.Int("days", 14, "list the transcripts that changed in the last N days")
	all := flags.Bool("all", false, "also list the repository's Agent sessions with no link to the PR")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) > 0 || *days < 1 {
		fmt.Fprintln(env.Stderr, "solidtrace review sessions: give only flags, and --days 1 or more")
		return 2
	}
	if *current != "" && !sessionIDPattern.MatchString(*current) {
		fmt.Fprintf(env.Stderr, "solidtrace review sessions: %q is not an Agent session id\n", *current)
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintf(env.Stderr, "solidtrace review sessions: %v\n", err)
		return 1
	}
	dir := env.Dir
	if dir == "" {
		dir = "."
	}
	change, list, err := readChange(ctx, dir, *branch, *base)
	if err != nil {
		return fail(err)
	}
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	found, err := capture.Sessions(env.ClaudeProjectsDir)
	if err != nil {
		return fail(err)
	}
	since := now().Add(-time.Duration(*days) * 24 * time.Hour)
	list.Sessions = []review.Session{}
	currentFound := false
	for _, f := range found {
		isCurrent := f.ID == *current
		if f.ModTime.Before(since) && !isCurrent {
			continue
		}
		subagents, _ := filepath.Glob(filepath.Join(filepath.Dir(f.TranscriptPath), f.ID, "subagents", "*.jsonl"))
		s, inRepo, err := review.Link(f.ID, f.TranscriptPath, subagents, change)
		if err != nil {
			// A transcript can be removed or be not readable: only the
			// current session's transcript is necessary.
			if isCurrent {
				return fail(err)
			}
			fmt.Fprintf(env.Stderr, "solidtrace review sessions: skip the Agent session %s: %v\n", f.ID, err)
			continue
		}
		s.Current = isCurrent
		currentFound = currentFound || isCurrent
		if isCurrent || inRepo && (*all || s.Links.Any()) {
			list.Sessions = append(list.Sessions, s)
		}
	}
	if *current != "" && !currentFound {
		return fail(fmt.Errorf("no transcript of the Agent session %s in %s", *current, env.ClaudeProjectsDir))
	}
	slices.SortStableFunc(list.Sessions, func(a, b review.Session) int {
		if c := a.First.Compare(b.First); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return printJSON(env, "review sessions", list)
}

// readChange reads with git what the PR of branch changes since base, in
// the repository of dir. An empty branch is the current branch, and an
// empty base is the merge base of the branch with origin/HEAD, else with
// main.
func readChange(ctx context.Context, dir, branch, base string) (review.Change, review.SessionList, error) {
	git := func(args ...string) (string, error) { return gitOutput(ctx, dir, args...) }
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return review.Change{}, review.SessionList{}, err
	}
	if branch == "" {
		if branch, err = git("branch", "--show-current"); err != nil {
			return review.Change{}, review.SessionList{}, err
		}
		if branch == "" {
			return review.Change{}, review.SessionList{}, fmt.Errorf("no branch is checked out: give --branch")
		}
	}
	if base == "" {
		if base, err = git("merge-base", branch, "origin/HEAD"); err != nil {
			base, err = git("merge-base", branch, "main")
		}
	} else {
		base, err = git("rev-parse", "--verify", base+"^{commit}")
	}
	if err != nil {
		return review.Change{}, review.SessionList{}, err
	}
	log, err := git("log", "--format=%s", base+".."+branch, "--")
	if err != nil {
		return review.Change{}, review.SessionList{}, err
	}
	diff, err := git("diff", "--name-only", base+"..."+branch, "--")
	if err != nil {
		return review.Change{}, review.SessionList{}, err
	}
	worktrees, err := git("worktree", "list", "--porcelain")
	if err != nil {
		return review.Change{}, review.SessionList{}, err
	}
	c := review.Change{Branch: branch, Roots: []string{filepath.Clean(top)}}
	c.Subjects, c.Files = lines(log), lines(diff)
	for _, l := range lines(worktrees) {
		if path, ok := strings.CutPrefix(l, "worktree "); ok {
			c.Roots = append(c.Roots, filepath.Clean(path))
		}
	}
	return c, review.SessionList{Branch: branch, Base: base, Commits: len(c.Subjects), Files: len(c.Files)}, nil
}

// gitOutput runs git with args in the repository of dir, and gives its
// output with no new line at the end.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		err = fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exit.Stderr)))
	}
	return strings.TrimRight(string(out), "\n"), err
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
