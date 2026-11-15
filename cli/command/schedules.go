package command

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/solidtrace/cli/dispatcherapi"
)

const schedulesUse = "solidtrace schedules: give list, get ID, create PROJECT --name NAME --cron CRON PROMPT, " +
	"update ID, pause ID, continue ID or delete ID"

// schedules is 'schedules list|get|create|update|pause|continue|delete'.
func schedules(ctx context.Context, args []string, env Env) int {
	verbs := []string{"list", "get", "create", "update", "pause", "continue", "delete"}
	if len(args) == 0 || !slices.Contains(verbs, args[0]) {
		fmt.Fprintln(env.Stderr, schedulesUse)
		return 2
	}
	verb := args[0]
	flags := flag.NewFlagSet("schedules "+verb, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	dispatcherURL := flags.String("dispatcher", "", "the Dispatcher's URL (kept for the next commands)")
	var project string
	var save dispatcherapi.SaveSchedule
	switch verb {
	case "list":
		flags.StringVar(&project, "project", "", "list only the schedules of this Project")
	case "create", "update":
		flags.StringVar(&save.Name, "name", "", "the schedule's name")
		flags.StringVar(&save.Cron, "cron", "", `a five-field cron expression, such as "0 9 * * 1"`)
		flags.StringVar(&save.TimeZone, "time-zone", "", "an IANA time zone, such as Europe/Berlin (none is UTC)")
		flags.StringVar(&save.BaseBranch, "base-branch", "", "the branch that its Runs start from "+
			"(none is the Project's default base branch)")
		if verb == "update" {
			flags.StringVar(&save.Prompt, "prompt", "", "the prompt of its Runs")
		}
	}
	positional, ok := parseInterspersed(flags, args[1:])
	if !ok {
		return 2
	}
	switch {
	case verb == "list" && len(positional) != 0,
		verb == "create" && (len(positional) < 2 || save.Name == "" || save.Cron == ""),
		verb != "list" && verb != "create" && len(positional) != 1:
		fmt.Fprintln(env.Stderr, schedulesUse)
		return 2
	}
	client, err := env.dispatcher(*dispatcherURL)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace schedules %s: %v\n", verb, err)
		return 1
	}
	if verb == "list" {
		list, err := client.Schedules(ctx, project)
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace schedules list: %v\n", err)
			return 1
		}
		printSchedules(env.Stdout, list)
		return 0
	}
	var s dispatcherapi.Schedule
	switch verb {
	case "get":
		s, err = client.Schedule(ctx, positional[0])
	case "create":
		save.Project = positional[0]
		save.Prompt = strings.TrimSpace(strings.Join(positional[1:], " "))
		s, err = client.CreateSchedule(ctx, save)
	case "update":
		update := dispatcherapi.UpdateSchedule{}
		flags.Visit(func(f *flag.Flag) {
			value := f.Value.String()
			switch f.Name {
			case "name":
				update.Name = &value
			case "cron":
				update.Cron = &value
			case "time-zone":
				update.TimeZone = &value
			case "base-branch":
				update.BaseBranch = &value
			case "prompt":
				update.Prompt = &value
			}
		})
		s, err = client.UpdateSchedule(ctx, positional[0], update)
	case "pause":
		s, err = client.PauseSchedule(ctx, positional[0])
	case "continue":
		s, err = client.ContinueSchedule(ctx, positional[0])
	case "delete":
		s, err = client.DeleteSchedule(ctx, positional[0])
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace schedules %s: %v\n", verb, err)
		return 1
	}
	switch verb {
	case "create":
		fmt.Fprintf(env.Stdout, "Schedule %s is made.\n\n", s.ID)
	case "delete":
		fmt.Fprintf(env.Stdout, "Schedule %s is deleted. Its Runs continue.\n", s.ID)
		return 0
	}
	printSchedule(env.Stdout, s)
	return 0
}

func printSchedule(out io.Writer, s dispatcherapi.Schedule) {
	tw := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
	fmt.Fprintf(tw, "Schedule:\t%s\n", s.ID)
	fmt.Fprintf(tw, "Name:\t%s\n", s.Name)
	fmt.Fprintf(tw, "Project:\t%s\n", s.Project)
	fmt.Fprintf(tw, "Cron:\t%s (%s)\n", s.Cron, s.TimeZone)
	fmt.Fprintf(tw, "Base branch:\t%s\n", orText(s.BaseBranch, "the Project's default base branch"))
	fmt.Fprintf(tw, "Next:\t%s\n", nextText(s))
	if s.LastRun != nil {
		fmt.Fprintf(tw, "Last Run:\t%s, %s, for %s\n", s.LastRun.ID, s.LastRun.State,
			s.LastRun.FiredAt.UTC().Format(timeFormat))
	} else {
		fmt.Fprintf(tw, "Last Run:\tnone\n")
	}
	fmt.Fprintf(tw, "Made:\t%s by %s\n", s.CreatedAt.UTC().Format(timeFormat), s.CreatedBy.Name)
	fmt.Fprintf(tw, "Changed:\t%s by %s\n", s.UpdatedAt.UTC().Format(timeFormat), s.UpdatedBy.Name)
	tw.Flush()
	fmt.Fprintf(out, "\nPrompt:\n%s\n", s.Prompt)
}

func printSchedules(out io.Writer, list []dispatcherapi.Schedule) {
	if len(list) == 0 {
		fmt.Fprintln(out, "No schedules.")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SCHEDULE\tPROJECT\tNAME\tCRON\tNEXT\tLAST RUN")
	for _, s := range list {
		last := "-"
		if s.LastRun != nil {
			last = s.LastRun.ID + " (" + s.LastRun.State + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s (%s)\t%s\t%s\n", s.ID, s.Project, firstLine(s.Name, 40), s.Cron, s.TimeZone,
			nextText(s), last)
	}
	tw.Flush()
}

func nextText(s dispatcherapi.Schedule) string {
	switch {
	case s.Paused:
		return "paused"
	case s.NextAt == nil:
		return "never"
	default:
		return s.NextAt.UTC().Format(timeFormat)
	}
}
