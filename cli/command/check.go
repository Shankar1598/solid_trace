package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/dispatcherapi"
)

// check asks the wrapper on the agent's socket to run the factory file's
// checks, or the command in args, and prints the records. The wrapper
// runs the commands and keeps the records for the next report, so the
// agent does not claim a check. It works only inside a Run.
func check(ctx context.Context, args []string, env Env) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(env.Stdout, "solidtrace check [COMMAND...]: run the factory file's checks, or COMMAND, as Evidence")
		return 0
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	run := dispatcherapi.CheckRun{Command: strings.TrimSpace(strings.Join(args, " "))}
	checks, err := agentsocket.Check(ctx, env.AgentSocket, run)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace check: %v\n", err)
		return 1
	}
	failed := false
	for _, c := range checks {
		verdict := "pass"
		if !c.Passed {
			verdict, failed = "FAIL", true
		}
		label := c.Command
		if c.Name != "" {
			label = c.Name + " (" + c.Command + ")"
		}
		fmt.Fprintf(env.Stdout, "%s %s: exit status %d in %s\n", verdict, label, c.ExitStatus,
			(time.Duration(c.DurationMS) * time.Millisecond).String())
		if !c.Passed && strings.TrimSpace(c.Output) != "" {
			fmt.Fprintf(env.Stdout, "%s\n", strings.TrimRight(c.Output, "\n"))
		}
	}
	if failed {
		return 1
	}
	return 0
}
