package command

import (
	"fmt"

	"github.com/solidtrace/cli/review"
)

func reviewFinal(args []string, env Env) int {
	data, code := readStdin(args, env, "review final", "record")
	if code != 0 {
		return code
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
