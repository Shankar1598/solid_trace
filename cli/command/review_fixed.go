package command

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/solidtrace/cli/review"
)

type fixEvent struct {
	Type   string   `json:"type"`
	Commit string   `json:"commit"`
	Files  []string `json:"files"`
}

type updateFinding struct {
	ID     string     `json:"id"`
	Events []fixEvent `json:"events"`
}

type updateBlock struct {
	Version  int             `json:"version"`
	Update   int             `json:"update"`
	Findings []updateFinding `json:"findings"`
}

func reviewFixed(ctx context.Context, args []string, env Env) int {
	flags := flag.NewFlagSet("review fixed", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	commit := "HEAD"
	if len(positional) == 1 {
		commit = positional[0]
	}
	if len(positional) > 1 || strings.HasPrefix(commit, "-") || commit == "" {
		fmt.Fprintln(env.Stderr, "solidtrace review fixed: give one commit, or none for HEAD")
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(env.Stderr, "solidtrace review fixed: %v\n", err)
		return 1
	}
	dir := env.Dir
	if dir == "" {
		dir = "."
	}
	hash, err := gitOutput(ctx, dir, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		return fail(fmt.Errorf("%q is not a commit", commit))
	}
	// git's own trailer parser reads only the trailer block at the end of
	// the message, so a line of the body that starts with Review-Findings:
	// is not a trailer.
	trailerLines, err := gitOutput(ctx, dir, "log", "-1", "--format=%(trailers:key=Review-Findings,valueonly,unfold)", hash)
	if err != nil {
		return fail(err)
	}
	// The first parent's diff gives the files that a merge brings in.
	changed, err := gitOutput(ctx, dir, "show", "--diff-merges=first-parent", "--name-only", "--format=", hash)
	if err != nil {
		return fail(err)
	}
	files := []string{}
	for _, f := range lines(changed) {
		if f != "" {
			files = append(files, f)
		}
	}
	var trailers []string
	for _, v := range lines(trailerLines) {
		if v = strings.TrimSpace(v); v != "" {
			trailers = append(trailers, v)
		}
	}
	if len(trailers) == 0 {
		return fail(fmt.Errorf("the commit %s has no Review-Findings trailer", hash))
	}
	var blocks []*updateBlock
	for _, value := range trailers {
		run, ids, err := review.ParseReviewFindings(value)
		if err != nil {
			return fail(err)
		}
		if len(ids) == 0 {
			return fail(fmt.Errorf("the trailer value %q names no findings", value))
		}
		i := slices.IndexFunc(blocks, func(b *updateBlock) bool { return b.Update == run })
		if i < 0 {
			blocks = append(blocks, &updateBlock{Version: 2, Update: run, Findings: []updateFinding{}})
			i = len(blocks) - 1
		}
		b := blocks[i]
		for _, id := range ids {
			if !slices.ContainsFunc(b.Findings, func(f updateFinding) bool { return f.ID == id }) {
				b.Findings = append(b.Findings, updateFinding{ID: id, Events: []fixEvent{{Type: "fix", Commit: hash, Files: files}}})
			}
		}
	}
	var out bytes.Buffer
	for i, b := range blocks {
		if i > 0 {
			out.WriteString("\n")
		}
		var body bytes.Buffer
		enc := json.NewEncoder(&body)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(b); err != nil {
			return fail(err)
		}
		block, problems := review.CheckBlock(body.Bytes())
		if len(problems) > 0 {
			return fail(fmt.Errorf("the update block of run %d is not valid: %s", b.Update, strings.Join(problems, "; ")))
		}
		out.WriteString(review.FenceBlock(block))
	}
	if _, err := env.Stdout.Write(out.Bytes()); err != nil {
		return fail(err)
	}
	return 0
}
