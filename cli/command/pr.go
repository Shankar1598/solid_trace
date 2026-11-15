package command

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/dispatcherapi"
)

// pr opens the Run's PR, or updates its title and description, through
// the wrapper on the agent's socket. It works only inside a Run. It
// never merges.
func pr(ctx context.Context, args []string, env Env) int {
	flags := flag.NewFlagSet("pr", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	title := flags.String("title", "", "the PR's title (one line)")
	description := flags.String("description", "", "the PR's description")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	text := dispatcherapi.PRText{Title: strings.TrimSpace(*title), Description: strings.TrimSpace(*description)}
	if len(positional) > 0 || (text.Title == "" && text.Description == "") {
		fmt.Fprintln(env.Stderr, "solidtrace pr: give --title TEXT and --description TEXT "+
			"(the first pr needs a title; a later pr changes what you give)")
		return 2
	}
	got, err := agentsocket.PR(ctx, env.AgentSocket, text)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace pr: %v\n", err)
		return 1
	}
	if got.Opened {
		fmt.Fprintf(env.Stdout, "Opened the PR: %s\n", got.URL)
	} else {
		fmt.Fprintf(env.Stdout, "Updated the PR: %s\n", got.URL)
	}
	return 0
}
