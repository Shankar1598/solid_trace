package command

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/dispatcherapi"
)

// outcomes are the values of report's --outcome, with their wire form.
var outcomes = map[string]string{
	"done":           dispatcherapi.OutcomeDone,
	"needs-a-person": dispatcherapi.OutcomeNeedsAPerson,
	"needs_a_person": dispatcherapi.OutcomeNeedsAPerson,
	"failed":         dispatcherapi.OutcomeFailed,
}

// report sends the agent's outcome to the wrapper on the agent's socket.
// It works only inside a Run.
func report(ctx context.Context, args []string, env Env) int {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	outcome := flags.String("outcome", "", "done, needs-a-person or failed")
	summary := flags.String("summary", "", "what you did, or why it failed")
	question := flags.String("question", "", "what you ask a person (with needs-a-person)")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	wire, known := outcomes[*outcome]
	if !known || len(positional) > 0 {
		fmt.Fprintln(env.Stderr, "solidtrace report: give --outcome done, needs-a-person or failed, with --summary TEXT "+
			"(and --question TEXT for needs-a-person)")
		return 2
	}
	r := dispatcherapi.Report{Outcome: wire, Summary: strings.TrimSpace(*summary), Question: strings.TrimSpace(*question)}
	if err := agentsocket.Report(ctx, env.AgentSocket, r); err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace report: %v\n", err)
		return 1
	}
	fmt.Fprintf(env.Stdout, "Reported %s.\n", *outcome)
	return 0
}
