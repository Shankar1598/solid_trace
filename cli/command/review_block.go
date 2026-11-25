package command

import (
	"flag"
	"fmt"
	"io"

	"github.com/solidtrace/cli/review"
)

// maxBlock is the most bytes of a block on stdin.
const maxBlock = 1 << 20

func reviewBlock(args []string, env Env) int {
	flags := flag.NewFlagSet("review block", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) > 0 {
		fmt.Fprintln(env.Stderr, "solidtrace review block: give the block's JSON on stdin, and no arguments")
		return 2
	}
	var data []byte
	if env.Stdin != nil {
		var err error
		if data, err = io.ReadAll(io.LimitReader(env.Stdin, maxBlock+1)); err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace review block: %v\n", err)
			return 1
		}
	}
	if len(data) > maxBlock {
		fmt.Fprintf(env.Stderr, "solidtrace review block: the block is more than %d bytes\n", maxBlock)
		return 1
	}
	block, problems := review.CheckBlock(data)
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintf(env.Stderr, "solidtrace review block: %s\n", p)
		}
		return 1
	}
	if _, err := io.WriteString(env.Stdout, review.FenceBlock(block)); err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review block: %v\n", err)
		return 1
	}
	return 0
}
