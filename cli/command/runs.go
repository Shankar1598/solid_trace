package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/solidtrace/cli/dispatcherapi"
)

// dispatcherFile keeps the Dispatcher URL that --dispatcher last gave.
const dispatcherFile = "dispatcher.json"

// start makes a queued Run with a Project and a prompt.
func start(ctx context.Context, args []string, env Env) int {
	flags := flag.NewFlagSet("start", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	project := flags.String("project", "", "the Project whose Factory settings the Run uses")
	dispatcherURL := flags.String("dispatcher", "", "the Dispatcher's URL (kept for the next commands)")
	prompt, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	text := strings.TrimSpace(strings.Join(prompt, " "))
	if *project == "" || text == "" {
		fmt.Fprintln(env.Stderr, "solidtrace start: give --project PROJECT and a prompt")
		return 2
	}
	client, err := env.dispatcher(*dispatcherURL)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace start: %v\n", err)
		return 1
	}
	run, err := client.Start(ctx, dispatcherapi.StartRun{Project: *project, Prompt: text})
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace start: %v\n", err)
		return 1
	}
	fmt.Fprintf(env.Stdout, "Run %s is queued.\n%s\n", run.ID, run.URL)
	return 0
}

// runs is 'runs list' and 'runs get <id>'.
func runs(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 || (args[0] != "list" && args[0] != "get") {
		fmt.Fprintln(env.Stderr, "solidtrace runs: give list, or get RUN_ID")
		return 2
	}
	verb := args[0]
	flags := flag.NewFlagSet("runs "+verb, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	dispatcherURL := flags.String("dispatcher", "", "the Dispatcher's URL (kept for the next commands)")
	positional, ok := parseInterspersed(flags, args[1:])
	if !ok {
		return 2
	}
	if (verb == "get") != (len(positional) == 1) || len(positional) > 1 {
		fmt.Fprintln(env.Stderr, "solidtrace runs: give list, or get RUN_ID")
		return 2
	}
	client, err := env.dispatcher(*dispatcherURL)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace runs %s: %v\n", verb, err)
		return 1
	}
	if verb == "get" {
		run, err := client.Get(ctx, positional[0])
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace runs get: %v\n", err)
			return 1
		}
		printRun(env.Stdout, run)
		return 0
	}
	list, err := client.List(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace runs list: %v\n", err)
		return 1
	}
	if len(list) == 0 {
		fmt.Fprintln(env.Stdout, "No Runs.")
		return 0
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN\tSTATE\tPROJECT\tSTARTED\tPROMPT")
	for _, r := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.State, r.Project, r.CreatedAt.UTC().Format(timeFormat),
			firstLine(r.Prompt, 60))
	}
	tw.Flush()
	return 0
}

func printRun(out io.Writer, r dispatcherapi.Run) {
	tw := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
	fmt.Fprintf(tw, "Run:\t%s\n", r.ID)
	fmt.Fprintf(tw, "State:\t%s\n", r.State)
	fmt.Fprintf(tw, "Project:\t%s\n", r.Project)
	fmt.Fprintf(tw, "Started:\t%s by %s\n", r.CreatedAt.UTC().Format(timeFormat), r.StartedBy.Name)
	fmt.Fprintf(tw, "Link:\t%s\n", r.URL)
	tw.Flush()
	fmt.Fprintf(out, "\nPrompt:\n%s\n", r.Prompt)
}

// firstLine is the first line of s, cut to at most n characters.
func firstLine(s string, n int) string {
	line, _, more := strings.Cut(strings.TrimSpace(s), "\n")
	runes := []rune(line)
	if len(runes) > n {
		return string(runes[:n-1]) + "…"
	}
	if more {
		return line + " …"
	}
	return line
}

// parseInterspersed parses flags that come before, between or after the
// positional arguments.
func parseInterspersed(flags *flag.FlagSet, args []string) ([]string, bool) {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, false
		}
		args = flags.Args()
		if len(args) == 0 {
			return positional, true
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

// dispatcher is the client of the Dispatcher at given, or else at the URL
// that an earlier --dispatcher kept. A given URL is kept for later.
func (env Env) dispatcher(given string) (*dispatcherapi.Client, error) {
	path := filepath.Join(env.ConfigDir, dispatcherFile)
	var kept struct {
		URL string `json:"url"`
	}
	if given != "" {
		u, err := url.Parse(given)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("--dispatcher must be an http:// or https:// URL, not %q", given)
		}
		kept.URL = strings.TrimRight(given, "/")
		data, _ := json.Marshal(kept)
		if err := os.MkdirAll(env.ConfigDir, 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			return nil, fmt.Errorf("keep the Dispatcher URL: %w", err)
		}
	} else {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("no Dispatcher URL: give --dispatcher URL one time (your operator gives the URL)")
		}
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &kept); err != nil || kept.URL == "" {
			return nil, fmt.Errorf("%s is not a kept Dispatcher URL: give --dispatcher URL", path)
		}
	}
	if _, err := env.Source().Credentials(); err != nil {
		return nil, err
	}
	return &dispatcherapi.Client{URL: kept.URL, HTTP: env.HTTP, Tokens: env.Source(), Version: env.Version}, nil
}
