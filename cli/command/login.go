package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/credentials"
	"github.com/solidtrace/cli/signin"
)

// DefaultIngestPort is EventStore's ingest port. With no --ingest-url, the
// ingest URL is the Console URL's host at this port, as Console's own
// fallback for the Ingest URL builds it.
const DefaultIngestPort = "4000"

// login signs in, stores the sign-in, and prints the engineer's notice.
func login(ctx context.Context, args []string, env Env) int {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	device := flags.Bool("device", false, "sign in with a code on another device (for a machine with no browser)")
	ingestFlag := flags.String("ingest-url", "", "the Ingest URL that Console's capture setup page gives")
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return 2
		}
		args = flags.Args()
		if len(args) == 0 {
			break
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
	if len(positional) > 1 {
		fmt.Fprintln(env.Stderr, "solidtrace login: give one Console URL")
		return 2
	}

	store := env.store()
	previous, err := store.Load()
	if err != nil && !errors.Is(err, credentials.ErrNotSignedIn) {
		// A file that we cannot use is replaced by the new sign-in.
		previous = credentials.Credentials{}
	}

	rawURL := previous.InstanceURL
	if len(positional) == 1 {
		rawURL = positional[0]
	}
	if rawURL == "" {
		fmt.Fprintln(env.Stderr, "solidtrace login: give the Console URL, such as 'solidtrace login https://solidtrace.example.com'")
		return 2
	}
	instance, err := signin.CleanURL(rawURL)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace login: %v\n", err)
		return 2
	}
	ingest, err := ingestURL(*ingestFlag, instance, previous)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace login: %v\n", err)
		return 2
	}

	client := env.signinClient()
	server, err := client.Discover(ctx, instance)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace login: %v\n", err)
		return 1
	}

	tokens, err := signIn(ctx, client, server, *device, env)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace login: %v\n", err)
		return 1
	}

	c := credentials.Credentials{
		InstanceURL:          instance,
		IngestURL:            ingest,
		TokenEndpoint:        server.TokenEndpoint,
		ClientID:             signin.ClientID,
		Resource:             server.Resource(),
		Scope:                tokens.Scope,
		AccessToken:          tokens.AccessToken,
		AccessTokenExpiresAt: tokens.ExpiresAt,
		RefreshToken:         tokens.RefreshToken,
	}
	if err := save(ctx, store, c); err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace login: keep the sign-in in %s: %v\n", store.Path(), err)
		return 1
	}
	fmt.Fprintf(env.Stdout, "Signed in to %s.\n", instance)

	api := &agentapi.Client{IngestURL: ingest, HTTP: env.HTTP, Tokens: env.Source(), Version: env.Version, Warnings: env.Stderr}
	list, err := api.CapturedRepositories(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace login: you are signed in, but the CLI could not get the captured repositories "+
			"from %s: %v\nCheck the Ingest URL on Console's capture setup page, and give it with --ingest-url.\n", ingest, err)
		return 1
	}
	if err := keepOrganization(ctx, store, list.Organization); err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace login: keep the Organization in %s: %v\n", store.Path(), err)
		return 1
	}
	printNotice(env.Stdout, list)
	return 0
}

// signIn uses the browser grant, or the device flow with --device, with no
// browser on the machine, or when the browser does not open.
func signIn(ctx context.Context, client *signin.Client, server signin.Server, device bool, env Env) (signin.Tokens, error) {
	if !device && env.OpenBrowser != nil {
		tokens, err := client.Browser(ctx, server, env.OpenBrowser, env.Stdout)
		if !errors.Is(err, signin.ErrNoBrowser) {
			return tokens, err
		}
		fmt.Fprintf(env.Stdout, "The browser did not open (%v). Sign in with a code instead.\n", err)
	}
	return client.Device(ctx, server, env.Stdout)
}

// ingestURL is --ingest-url, else the stored one for the same instance,
// else the Console URL's host at the ingest port.
func ingestURL(flagValue, instance string, previous credentials.Credentials) (string, error) {
	if flagValue != "" {
		u, err := url.Parse(strings.TrimRight(strings.TrimSpace(flagValue), "/"))
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			return "", fmt.Errorf("%q is not an Ingest URL: give it as Console's capture setup page shows it", flagValue)
		}
		return u.String(), nil
	}
	if previous.InstanceURL == instance && previous.IngestURL != "" {
		return previous.IngestURL, nil
	}
	u, _ := url.Parse(instance)
	return u.Scheme + "://" + net.JoinHostPort(u.Hostname(), DefaultIngestPort), nil
}

// save replaces the stored sign-in, under the lock that a refresh takes.
func save(ctx context.Context, store credentials.Store, c credentials.Credentials) error {
	unlock, err := store.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return store.Save(c)
}

func keepOrganization(ctx context.Context, store credentials.Store, org agentapi.Organization) error {
	unlock, err := store.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	c, err := store.Load()
	if err != nil {
		return err
	}
	c.Organization = &credentials.Organization{Name: org.Name, Slug: org.Slug}
	return store.Save(c)
}

// printNotice prints the engineer's notice: which repositories upload, to
// which Organization, and that each member can read them.
func printNotice(out io.Writer, list agentapi.CapturedRepositories) {
	org := list.Organization.Name
	fmt.Fprintf(out, "This sign-in is for the Organization %s (%s).\n\n", org, list.Organization.Slug)
	if len(list.Repositories) == 0 {
		fmt.Fprintf(out, "%s has no captured repositories, so no Agent session uploads from this machine.\n", org)
		return
	}
	fmt.Fprintf(out, "Your Claude Code Agent sessions in these repositories upload in full to %s, "+
		"and each member of %s can read them:\n", org, org)
	for _, repository := range list.Repositories {
		fmt.Fprintf(out, "  %s\n", repository)
	}
	fmt.Fprintln(out, "\nAgent sessions in other repositories, or in no repository, do not upload.")
}
