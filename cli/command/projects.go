package command

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/dispatcherapi"
)

// projects is 'projects list', 'projects get PROJECT' and 'projects set
// PROJECT --repository R [--folder F] [--base-branch B] [--connection C]'.
func projects(ctx context.Context, args []string, env Env) int {
	const use = "solidtrace projects: give list, get PROJECT, or set PROJECT --repository REPOSITORY"
	if len(args) == 0 || (args[0] != "list" && args[0] != "get" && args[0] != "set") {
		fmt.Fprintln(env.Stderr, use)
		return 2
	}
	verb := args[0]
	flags := flag.NewFlagSet("projects "+verb, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	dispatcherURL := flags.String("dispatcher", "", "the Dispatcher's URL (kept for the next commands)")
	var save dispatcherapi.SaveFactorySettings
	if verb == "set" {
		flags.StringVar(&save.Repository, "repository", "", "the repository as host/workspace/name, or its clone URL")
		flags.StringVar(&save.Folder, "folder", "", "the Project's folder in the repository (none is the root)")
		flags.StringVar(&save.BaseBranch, "base-branch", "", "the branch that Runs start from (none is the repository's default branch)")
		flags.StringVar(&save.Connection, "connection", "", "the forge connection that the Project's Runs use")
	}
	positional, ok := parseInterspersed(flags, args[1:])
	if !ok {
		return 2
	}
	if (verb == "list") != (len(positional) == 0) || len(positional) > 1 || (verb == "set" && save.Repository == "") {
		fmt.Fprintln(env.Stderr, use)
		return 2
	}
	client, err := env.dispatcher(*dispatcherURL)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace projects %s: %v\n", verb, err)
		return 1
	}
	switch verb {
	case "get", "set":
		var settings dispatcherapi.FactorySettings
		if verb == "get" {
			settings, err = client.Settings(ctx, positional[0])
		} else {
			settings, err = client.SaveSettings(ctx, positional[0], save)
		}
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace projects %s: %v\n", verb, err)
			return 1
		}
		printSettings(env.Stdout, settings)
		return 0
	}
	list, err := client.Projects(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace projects list: %v\n", err)
		return 1
	}
	if len(list) == 0 {
		fmt.Fprintln(env.Stdout, "No Project has Factory settings.")
		return 0
	}
	printProjects(env.Stdout, list)
	return 0
}

func printSettings(out io.Writer, s dispatcherapi.FactorySettings) {
	tw := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
	fmt.Fprintf(tw, "Project:\t%s\n", s.Project)
	fmt.Fprintf(tw, "Repository:\t%s\n", s.Repository)
	fmt.Fprintf(tw, "Folder:\t%s\n", orText(s.Folder, "the root"))
	fmt.Fprintf(tw, "Base branch:\t%s\n", orText(s.BaseBranch, "the repository's default branch"))
	fmt.Fprintf(tw, "Connection:\t%s\n", orText(s.Connection, "none"))
	fmt.Fprintf(tw, "Saved:\t%s by %s\n", s.UpdatedAt.UTC().Format(timeFormat), s.UpdatedBy.Name)
	tw.Flush()
	if s.Orphaned {
		fmt.Fprintln(out, "\nThese settings are orphaned: SolidTrace no longer has this Project.")
	}
}

func printProjects(out io.Writer, list []dispatcherapi.FactorySettings) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PROJECT\tREPOSITORY\tFOLDER\tBASE BRANCH\tCONNECTION")
	for _, s := range list {
		project := s.Project
		if s.Orphaned {
			project += " (orphaned)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", project, s.Repository, orText(s.Folder, "/"),
			orText(s.BaseBranch, "(default)"), orText(s.Connection, "-"))
	}
	tw.Flush()
}

func orText(value, none string) string {
	if value == "" {
		return none
	}
	return value
}

// connections is 'connections list' and 'connections set NAME --kind
// bitbucket', which reads the token from stdin, so that it is not in the
// shell's history or in the list of processes.
func connections(ctx context.Context, args []string, env Env) int {
	const use = "solidtrace connections: give list, or set NAME --kind bitbucket with the token on stdin"
	if len(args) == 0 || (args[0] != "list" && args[0] != "set") {
		fmt.Fprintln(env.Stderr, use)
		return 2
	}
	verb := args[0]
	flags := flag.NewFlagSet("connections "+verb, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	dispatcherURL := flags.String("dispatcher", "", "the Dispatcher's URL (kept for the next commands)")
	kind := dispatcherapi.ForgeBitbucket
	if verb == "set" {
		flags.StringVar(&kind, "kind", dispatcherapi.ForgeBitbucket, "the kind of forge: bitbucket (Bitbucket Cloud)")
	}
	positional, ok := parseInterspersed(flags, args[1:])
	if !ok {
		return 2
	}
	if (verb == "list") != (len(positional) == 0) || len(positional) > 1 {
		fmt.Fprintln(env.Stderr, use)
		return 2
	}
	client, err := env.dispatcher(*dispatcherURL)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace connections %s: %v\n", verb, err)
		return 1
	}
	if verb == "set" {
		token, err := readToken(env.Stdin)
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace connections set: %v\n", err)
			return 2
		}
		c, err := client.SaveConnection(ctx, positional[0], dispatcherapi.SaveConnection{Kind: kind, Token: token})
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace connections set: %v\n", err)
			return 1
		}
		fmt.Fprintf(env.Stdout, "The %s connection %s is saved. The Dispatcher keeps its token encrypted.\n", c.Kind, c.Name)
		return 0
	}
	list, err := client.Connections(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace connections list: %v\n", err)
		return 1
	}
	if len(list) == 0 {
		fmt.Fprintln(env.Stdout, "No connections.")
		return 0
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tSAVED\tBY")
	for _, c := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Name, c.Kind, c.UpdatedAt.UTC().Format(timeFormat), c.UpdatedBy.Name)
	}
	tw.Flush()
	return 0
}

// readToken reads the first line of stdin.
func readToken(stdin io.Reader) (string, error) {
	if stdin == nil {
		return "", errors.New("give the token on stdin")
	}
	line, err := bufio.NewReader(io.LimitReader(stdin, 64<<10)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if token := strings.TrimSpace(line); token != "" {
		return token, nil
	}
	return "", errors.New("give the token on stdin, such as: solidtrace connections set NAME --kind bitbucket < token-file")
}

// findProject finds the one Project whose Factory settings name a remote
// of the git repository in dir and a folder that holds dir. When the
// folders of several Projects hold dir, the deepest folder wins. It says
// what to do when not exactly one Project matches.
func findProject(ctx context.Context, client *dispatcherapi.Client, dir string) (dispatcherapi.FactorySettings, error) {
	var none dispatcherapi.FactorySettings
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return none, err
		}
	}
	remotes, folder, ok, err := capture.Remotes(ctx, dir)
	if err != nil {
		return none, err
	}
	if !ok || len(remotes) == 0 {
		return none, errors.New("this folder is not in a git repository with a remote: give --project PROJECT")
	}
	list, err := client.Projects(ctx)
	if err != nil {
		return none, err
	}
	var matches []dispatcherapi.FactorySettings
	depth := -1
	for _, s := range list {
		inside := s.Folder == "" || folder == s.Folder || strings.HasPrefix(folder, s.Folder+"/")
		if s.Orphaned || !inside || !slices.Contains(remotes, s.Repository) {
			continue
		}
		d := 0
		if s.Folder != "" {
			d = strings.Count(s.Folder, "/") + 1
		}
		if d > depth {
			matches, depth = nil, d
		}
		if d == depth {
			matches = append(matches, s)
		}
	}
	where := strings.Join(remotes, ", ")
	if folder != "" {
		where = "the folder " + folder + " of " + where
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return none, fmt.Errorf("no Project has Factory settings for %s: give --project PROJECT "+
			"('solidtrace projects list' lists them)", where)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d Projects have Factory settings for %s:\n", len(matches), where)
	printProjects(&b, matches)
	b.WriteString("Give --project PROJECT")
	return none, errors.New(b.String())
}
