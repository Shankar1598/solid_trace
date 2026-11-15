package command

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/solidtrace/cli/dispatcherapi"
)

// projectList is a flag that can be given more than one time.
type projectList []string

func (l *projectList) String() string { return strings.Join(*l, ",") }

func (l *projectList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// senders is 'senders list', 'senders create NAME [--project P]...' and
// 'senders delete ID'.
func senders(ctx context.Context, args []string, env Env) int {
	const use = "solidtrace senders: give list, create NAME [--project PROJECT]..., or delete SENDER_ID"
	if len(args) == 0 || (args[0] != "list" && args[0] != "create" && args[0] != "delete") {
		fmt.Fprintln(env.Stderr, use)
		return 2
	}
	verb := args[0]
	flags := flag.NewFlagSet("senders "+verb, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	dispatcherURL := flags.String("dispatcher", "", "the Dispatcher's URL (kept for the next commands)")
	var projects projectList
	if verb == "create" {
		flags.Var(&projects, "project", "a Project that the sender can start Runs in (give it again for more; none is each Project)")
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
		fmt.Fprintf(env.Stderr, "solidtrace senders %s: %v\n", verb, err)
		return 1
	}
	switch verb {
	case "create":
		s, err := client.CreateSender(ctx, dispatcherapi.SaveSender{Name: positional[0], Projects: projects})
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace senders create: %v\n", err)
			return 1
		}
		fmt.Fprintf(env.Stdout, "The sender %s is made.\n", s.Name)
		tw := tabwriter.NewWriter(env.Stdout, 0, 0, 1, ' ', 0)
		fmt.Fprintf(tw, "Sender:\t%s\n", s.ID)
		fmt.Fprintf(tw, "Projects:\t%s\n", orText(strings.Join(s.Projects, ", "), "each Project of the Organization"))
		fmt.Fprintf(tw, "Webhook URL:\t%s\n", orText(s.WebhookURL,
			"none: the Dispatcher's configuration has no public webhook URL; use /webhooks/generic/"+s.ID+
				" on its private URL"))
		fmt.Fprintf(tw, "Secret:\t%s\n", s.Secret)
		tw.Flush()
		fmt.Fprintln(env.Stdout, "\nThe Dispatcher shows this secret this one time only. Keep it in the sending tool: "+
			"it signs each delivery.")
		return 0
	case "delete":
		if err := client.DeleteSender(ctx, positional[0]); err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace senders delete: %v\n", err)
			return 1
		}
		fmt.Fprintf(env.Stdout, "The sender %s is deleted. Its Runs stay.\n", positional[0])
		return 0
	}
	list, err := client.Senders(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace senders list: %v\n", err)
		return 1
	}
	if len(list) == 0 {
		fmt.Fprintln(env.Stdout, "No senders.")
		return 0
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPROJECTS\tMADE\tBY\tLAST DELIVERY\tWEBHOOK URL")
	for _, s := range list {
		last := "never"
		if s.LastDeliveryAt != nil {
			last = s.LastDeliveryAt.UTC().Format(timeFormat)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, orText(strings.Join(s.Projects, ","), "(each)"),
			s.CreatedAt.UTC().Format(timeFormat), s.CreatedBy.Name, last, orText(s.WebhookURL, "-"))
	}
	tw.Flush()
	return 0
}
