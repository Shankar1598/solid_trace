package command

import (
	"flag"
	"fmt"
	"io"

	"github.com/solidtrace/cli/review"
)

func reviewFinal(args []string, env Env) int {
	flags := flag.NewFlagSet("review final", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) > 0 {
		fmt.Fprintln(env.Stderr, "solidtrace review final: give the record's JSON on stdin, and no arguments")
		return 2
	}
	var data []byte
	if env.Stdin != nil {
		var err error
		if data, err = io.ReadAll(io.LimitReader(env.Stdin, maxBlock+1)); err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace review final: %v\n", err)
			return 1
		}
	}
	if len(data) > maxBlock {
		fmt.Fprintf(env.Stderr, "solidtrace review final: the record is more than %d bytes\n", maxBlock)
		return 1
	}
	record, err := review.ReadFinal(data)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review final: %v\n", err)
		return 1
	}
	fixed := 0
	for _, f := range record.Findings {
		if f.Outcome == "fixed" {
			fixed++
		}
	}
	fmt.Fprintf(env.Stdout, "record: round %d, %d findings, %d fixed\n", record.Round, len(record.Findings), fixed)
	return 0
}
