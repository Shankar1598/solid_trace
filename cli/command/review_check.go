package command

import (
	"flag"
	"fmt"
	"os"

	"github.com/solidtrace/cli/review"
)

func reviewCheck(args []string, env Env) int {
	flags := flag.NewFlagSet("review check", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	digestPath := flags.String("digest", "", "the path of the digest JSON (from review digest)")
	recordPath := flags.String("record", "", "the path of the record JSON")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) > 0 || *digestPath == "" || *recordPath == "" {
		fmt.Fprintln(env.Stderr, "solidtrace review check: give --digest PATH and --record PATH")
		return 2
	}
	digest, err := os.ReadFile(*digestPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review check: %v\n", err)
		return 2
	}
	record, err := os.ReadFile(*recordPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review check: %v\n", err)
		return 2
	}
	result, err := review.Check(digest, record)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review check: %v\n", err)
		return 2
	}
	if code := printJSON(env, "review check", result); code != 0 {
		return code
	}
	if len(result.Problems) > 0 {
		return 1
	}
	return 0
}
