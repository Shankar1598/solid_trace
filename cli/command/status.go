package command

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/capture"
)

// captureStatus prints the sign-in, the captured repositories list of its
// Organization, who can read the uploads, and the state of each Agent
// session on the machine. It sends no byte of an Agent session.
func captureStatus(ctx context.Context, env Env) int {
	out := env.Stdout
	source := env.Source()
	creds, err := source.Credentials()
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace: %v\n", err)
		return 1
	}
	org := creds.Organization.Name
	fmt.Fprintf(out, "Signed in to %s, in the Organization %s.\n", creds.InstanceURL, org)

	client := &agentapi.Client{IngestURL: creds.IngestURL, HTTP: env.HTTP, Tokens: source, Version: env.Version,
		Warnings: env.Stderr}
	list, err := client.CapturedRepositories(ctx)
	entries := list.Repositories
	if err == nil {
		if list.Organization.Name != "" {
			org = list.Organization.Name
		}
	} else {
		fmt.Fprintf(out, "\nThe captured repositories list could not be read: %v\n", err)
		signin := signinName(creds.InstanceURL, creds.IngestURL, creds.Organization.Slug)
		_, cached, at, ok := capture.CachedList(env.ConfigDir, signin)
		if !ok {
			fmt.Fprintln(out, "No list is kept on this machine, so nothing uploads.")
		} else {
			fmt.Fprintf(out, "The list kept on this machine is from %s.\n", at.UTC().Format(timeFormat))
		}
		entries = cached
	}
	if entries != nil {
		fmt.Fprintf(out, "\nCaptured repositories of %s:\n", org)
		if len(entries) == 0 {
			fmt.Fprintln(out, "  (none: the list is empty, so nothing uploads)")
		}
		for _, e := range entries {
			fmt.Fprintf(out, "  %s\n", e)
		}
	}
	fmt.Fprintf(out, "\nEach member of %s can read the Agent sessions that this machine uploads.\n", org)

	printSessions(env)
	return 0
}

const timeFormat = "2006-01-02 15:04 UTC"

func printSessions(env Env) {
	out := env.Stdout
	dir := env.ClaudeProjectsDir
	sessions, err := capture.Sessions(dir)
	if err != nil {
		fmt.Fprintf(out, "\nThe Agent sessions in %s could not be read: %v\n", dir, err)
		return
	}
	fmt.Fprintf(out, "\nAgent sessions on this machine (%s):\n", dir)
	if len(sessions) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ModTime.After(sessions[j].ModTime) })
	for _, s := range sessions {
		fmt.Fprintf(out, "  %s  %s\n", s.ID, describe(env.ConfigDir, s))
	}
}

// describe is the state of an Agent session: uploaded, not in the list,
// or failed, from the record of its last capture.
func describe(dir string, s capture.FoundSession) string {
	r, ok := capture.RecordOf(dir, s.ID)
	if !ok {
		return "not checked yet (the next SessionStart sweep checks it)"
	}
	at := r.At.UTC().Format(timeFormat)
	switch r.Status {
	case capture.Uploaded:
		return fmt.Sprintf("uploaded         %s  %s", r.Repository, at)
	case capture.NotInList:
		start := r.StartDir
		if start == "" {
			start = "no working directory"
		}
		return fmt.Sprintf("not in the list  %s  %s", start, at)
	default:
		return fmt.Sprintf("failed           %s  %s: %s", orDash(r.Repository), at, oneLine(r.Error))
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ClaudeProjectsDir is where Claude Code keeps its transcripts:
// $CLAUDE_CONFIG_DIR/projects, else ~/.claude/projects.
func ClaudeProjectsDir(getenv func(string) string, home string) string {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects")
	}
	return filepath.Join(home, ".claude", "projects")
}
