package command

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/solidtrace/cli/review"
)

// prBodyLimit is the most characters that GitHub takes in a PR body.
const prBodyLimit = 65536

func reviewRecord(args []string, env Env) int {
	flags := flag.NewFlagSet("review record", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	digestPath := flags.String("digest", "", "the path of the digest JSON (from review digest)")
	notesPath := flags.String("notes", "", "the path of the notes JSON")
	pr := flags.String("pr", "", "the PR's URL (default: null)")
	prBodyPath := flags.String("pr-body", "", "the path of the PR body, to keep in the record (default: none)")
	round := flags.Int("round", 1, "the record's round: 1 for open-pr, one more for each update-pr")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) > 0 || *digestPath == "" || *round < 1 {
		fmt.Fprintln(env.Stderr, "solidtrace review record: give --digest PATH, and --round 1 or more")
		return 2
	}
	digest, err := os.ReadFile(*digestPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review record: %v\n", err)
		return 2
	}
	opts := review.RecordOptions{Round: *round}
	if *notesPath != "" {
		if opts.Notes, err = os.ReadFile(*notesPath); err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace review record: %v\n", err)
			return 2
		}
	}
	if *pr != "" {
		opts.PR = pr
	}
	if *prBodyPath != "" {
		body, err := os.ReadFile(*prBodyPath)
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace review record: %v\n", err)
			return 2
		}
		if n := utf8.RuneCount(body); n > prBodyLimit {
			fmt.Fprintf(env.Stderr, "solidtrace review record: the PR body has %d characters, more than GitHub's limit of %d\n", n, prBodyLimit)
			return 1
		}
		opts.PRBody = string(body)
	}
	record, warnings, err := review.BuildRecord(digest, opts)
	var refused *review.NotesError
	switch {
	case errors.As(err, &refused):
		for _, p := range refused.Problems {
			fmt.Fprintf(env.Stderr, "solidtrace review record: %s\n", p)
		}
		return 1
	case err != nil:
		fmt.Fprintf(env.Stderr, "solidtrace review record: %v\n", err)
		return 2
	}
	for _, w := range warnings {
		fmt.Fprintf(env.Stderr, "solidtrace review record: warning: %s\n", w)
	}
	return printJSON(env, "review record", record)
}
