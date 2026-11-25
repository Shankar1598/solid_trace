package command

import (
	"flag"
	"fmt"
	"io"

	"github.com/solidtrace/cli/review"
)

// maxStdin is the most bytes of a block or a record on stdin.
const maxStdin = 1 << 20

// readStdin parses the flags of the command name, which takes no
// arguments, and reads at most maxStdin bytes of what on stdin. A code
// that is not 0 is the exit code of a failure.
func readStdin(args []string, env Env, name, what string) (data []byte, code int) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return nil, 2
	}
	if len(positional) > 0 {
		fmt.Fprintf(env.Stderr, "solidtrace %s: give the %s's JSON on stdin, and no arguments\n", name, what)
		return nil, 2
	}
	if env.Stdin != nil {
		var err error
		if data, err = io.ReadAll(io.LimitReader(env.Stdin, maxStdin+1)); err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace %s: %v\n", name, err)
			return nil, 1
		}
	}
	if len(data) > maxStdin {
		fmt.Fprintf(env.Stderr, "solidtrace %s: the %s is more than %d bytes\n", name, what, maxStdin)
		return nil, 1
	}
	return data, 0
}

func reviewBlock(args []string, env Env) int {
	data, code := readStdin(args, env, "review block", "block")
	if code != 0 {
		return code
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
