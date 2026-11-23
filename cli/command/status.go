package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/credentials"
	"github.com/solidtrace/cli/signin"
)

// pluginName is the SolidTrace plugin as Claude Code names it.
const pluginName = "solidtrace@solidtrace"

// statusUsage is the help of `capture status`.
const statusUsage = "solidtrace capture status [--all]: show whether capture works on this machine. --all lists each Agent session."

// step is one setup step of capture, with what to do when it is not done.
type step struct {
	ok   bool
	name string
	text string
	fix  string
}

// session is an Agent session on the machine with the record of its last
// capture; ok is false when no capture looked at it.
type session struct {
	capture.FoundSession
	record capture.Record
	ok     bool
}

// captureStatus prints whether capture works on this machine: each setup
// step as done or not done, the captured repositories, and a summary of
// the Agent sessions on the machine. With --all it lists each Agent
// session. It sends no byte of an Agent session.
func captureStatus(ctx context.Context, args []string, env Env) int {
	all := false
	for _, a := range args {
		switch a {
		case "--all":
			all = true
		case "-h", "--help":
			fmt.Fprintln(env.Stdout, statusUsage)
			return 0
		default:
			fmt.Fprintf(env.Stderr, "solidtrace capture status: unknown argument %q\n%s\n", a, statusUsage)
			return 2
		}
	}
	out := env.Stdout
	now := env.Now()
	plugin := pluginStep(filepath.Dir(env.ClaudeProjectsDir))

	creds, err := env.Source().Credentials()
	if err != nil {
		text := "not signed in"
		if !errors.Is(err, credentials.ErrNotSignedIn) && !errors.Is(err, signin.ErrSignInAgain) {
			text = err.Error()
		}
		steps := []step{
			{name: "Sign-in", text: text, fix: "Run the 'solidtrace login' command from Console: Settings → Agent sessions → Capture setup."},
			plugin,
		}
		fmt.Fprintln(out, "Capture is off: no Agent session uploads from this machine. Do each step marked ✗.")
		printSteps(out, steps)
		return 1
	}
	org := ""
	if creds.Organization != nil {
		org = creds.Organization.Name
	}
	steps := []step{{ok: true, name: "Sign-in", text: fmt.Sprintf("signed in to %s, in the Organization %s", creds.InstanceURL, org)}, plugin}

	client := &agentapi.Client{IngestURL: creds.IngestURL, HTTP: env.HTTP, Tokens: env.Source(), Version: env.Version,
		Warnings: env.Stderr}
	list, err := client.CapturedRepositories(ctx)
	entries := list.Repositories
	listStep := step{name: "Captured repositories"}
	switch {
	case err != nil:
		listStep.text = fmt.Sprintf("the list could not be read: %v", err)
		slug := ""
		if creds.Organization != nil {
			slug = creds.Organization.Slug
		}
		_, cached, at, ok := capture.CachedList(env.ConfigDir, signinName(creds.InstanceURL, creds.IngestURL, slug))
		if ok {
			listStep.fix = fmt.Sprintf("The list kept on this machine is from %s. Capture uses it for %s after that time; then nothing uploads until SolidTrace answers.",
				timeText(at), capture.ListLasts)
		} else {
			listStep.fix = "No list is kept on this machine, so nothing uploads until SolidTrace answers."
		}
		entries = cached
	case len(entries) == 0:
		listStep.text = "the list is empty, so nothing uploads"
		listStep.fix = "An Admin adds repositories in Console: Settings → Agent sessions."
	default:
		listStep.ok = true
		listStep.text = fmt.Sprintf("SolidTrace gave the list: %s", plural(len(entries), "repository", "repositories"))
	}
	if list.Organization.Name != "" {
		org = list.Organization.Name
	}
	steps = append(steps, listStep)

	sessions, sessionsErr := machineSessions(env)
	steps = append(steps, uploadsStep(sessions, sessionsErr, now, filepath.Join(env.ConfigDir, "capture.log")))

	switch {
	case !steps[1].ok || len(entries) == 0:
		fmt.Fprintln(out, "Capture is off: no Agent session uploads from this machine. Do each step marked ✗.")
	case !listStep.ok || !steps[3].ok:
		fmt.Fprintln(out, "Capture has a problem. Do each step marked ✗.")
	default:
		fmt.Fprintf(out, "Capture works: the Agent sessions in the captured repositories of %s upload from this machine.\n", org)
	}
	printSteps(out, steps)

	if entries != nil {
		fmt.Fprintf(out, "\nCaptured repositories of %s:\n", org)
		if len(entries) == 0 {
			fmt.Fprintln(out, "  (none)")
		}
		for _, e := range entries {
			fmt.Fprintf(out, "  %s\n", e)
		}
	}
	fmt.Fprintf(out, "Each member of %s can read the Agent sessions that this machine uploads.\n", org)

	if sessionsErr != nil {
		fmt.Fprintf(out, "\nThe Agent sessions in %s could not be read: %v\n", env.ClaudeProjectsDir, sessionsErr)
		return 0
	}
	if all {
		printEachSession(out, env.ClaudeProjectsDir, sessions)
		return 0
	}
	printSummary(out, env.ClaudeProjectsDir, sessions)
	return 0
}

// pluginStep finds the SolidTrace plugin in Claude Code's config
// directory: installed, and not turned off in the person's settings.
func pluginStep(claudeDir string) step {
	s := step{name: "Claude Code plugin"}
	var installed struct {
		Plugins map[string][]struct {
			Scope       string `json:"scope"`
			Version     string `json:"version"`
			ProjectPath string `json:"projectPath"`
		} `json:"plugins"`
	}
	data, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "installed_plugins.json"))
	if err != nil || json.Unmarshal(data, &installed) != nil || len(installed.Plugins[pluginName]) == 0 {
		s.text = fmt.Sprintf("%s is not installed in %s", pluginName, claudeDir)
		s.fix = "Run the two 'claude plugin' commands from Console: Settings → Agent sessions → Capture setup. Or ask an Admin to push the plugin with managed settings."
		return s
	}
	var settings struct {
		EnabledPlugins map[string]any `json:"enabledPlugins"`
	}
	if data, err := os.ReadFile(filepath.Join(claudeDir, "settings.json")); err == nil && json.Unmarshal(data, &settings) == nil {
		if on, set := settings.EnabledPlugins[pluginName].(bool); set && !on {
			s.text = fmt.Sprintf("%s is installed, but it is turned off", pluginName)
			s.fix = "Turn it on: claude plugin enable " + pluginName
			return s
		}
	}
	copies := installed.Plugins[pluginName]
	p := copies[0]
	for _, c := range copies {
		if c.Scope == "user" {
			p = c
		}
	}
	s.ok = true
	s.text = fmt.Sprintf("%s %s is installed", pluginName, p.Version)
	if p.Scope != "user" && p.ProjectPath != "" {
		s.text += fmt.Sprintf(", for %s only", p.ProjectPath)
	}
	return s
}

// machineSessions are the Agent sessions on the machine, newest first,
// each with the record of its last capture.
func machineSessions(env Env) ([]session, error) {
	found, err := capture.Sessions(env.ClaudeProjectsDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool { return found[i].ModTime.After(found[j].ModTime) })
	sessions := make([]session, len(found))
	for i, f := range found {
		r, ok := capture.RecordOf(env.ConfigDir, f.ID)
		sessions[i] = session{FoundSession: f, record: r, ok: ok}
	}
	return sessions, nil
}

// uploadsStep tells when the hooks last ran capture, and how many Agent
// sessions failed to upload.
func uploadsStep(sessions []session, err error, now time.Time, log string) step {
	s := step{name: "Uploads"}
	if err != nil {
		s.text = "the Agent sessions on this machine could not be read"
		return s
	}
	var last time.Time
	failed := 0
	for _, x := range sessions {
		if !x.ok {
			continue
		}
		if x.record.At.After(last) {
			last = x.record.At
		}
		if x.record.Status == capture.Failed {
			failed++
		}
	}
	switch {
	case len(sessions) == 0:
		s.ok = true
		s.text = "no Agent session is on this machine yet"
	case last.IsZero():
		s.text = "the hooks have not run capture on this machine yet"
		s.fix = "Start Claude Code: the plugin's SessionStart hook checks each Agent session. Problems go to " + log + "."
	case failed > 0:
		s.text = fmt.Sprintf("%s failed; the last capture was %s", plural(failed, "Agent session", "Agent sessions"), ago(now, last))
		s.fix = "See \"Failed\" below and " + log + ". The next SessionStart tries them again."
	default:
		s.ok = true
		s.text = "the last capture was " + ago(now, last)
	}
	return s
}

func printSteps(out io.Writer, steps []step) {
	fmt.Fprintln(out, "\nSetup:")
	width := 0
	for _, s := range steps {
		width = max(width, len(s.name))
	}
	for _, s := range steps {
		mark := "✓"
		if !s.ok {
			mark = "✗"
		}
		fmt.Fprintf(out, "  %s %-*s  %s\n", mark, width, s.name, s.text)
		if s.fix != "" {
			fmt.Fprintf(out, "    %-*s  %s\n", width, "", s.fix)
		}
	}
}

// printSummary counts the Agent sessions on the machine by state, and
// shows the failures and the newest Agent sessions.
func printSummary(out io.Writer, dir string, sessions []session) {
	fmt.Fprintf(out, "\nAgent sessions on this machine (%s): %s\n", dir, number(len(sessions)))
	if len(sessions) == 0 {
		return
	}
	byRepository := map[string]int{}
	byFolder := map[string]int{}
	var uploaded, notCaptured, unchecked int
	var failed []session
	for _, s := range sessions {
		switch {
		case !s.ok:
			unchecked++
		case s.record.Status == capture.Uploaded:
			uploaded++
			byRepository[s.record.Repository]++
		case s.record.Status == capture.NotInList:
			notCaptured++
			byFolder[startDir(s.record)]++
		default:
			failed = append(failed, s)
		}
	}
	rows := [][2]string{
		{number(uploaded), "uploaded"},
		{number(notCaptured), "not captured: they started outside a captured repository, so they stay on this machine"},
		{number(len(failed)), "failed"},
		{number(unchecked), "not checked yet: the next start of Claude Code checks them"},
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r[0]))
	}
	for _, r := range rows {
		fmt.Fprintf(out, "  %*s  %s\n", width, r[0], r[1])
	}

	printCounts(out, "Uploaded, by repository", byRepository, 0)
	printCounts(out, "Not captured, by the folder where Claude Code started", byFolder, 5)

	if len(failed) > 0 {
		fmt.Fprintln(out, "\nFailed, newest first:")
		for i, s := range failed {
			if i == 10 {
				fmt.Fprintf(out, "  and %s more\n", number(len(failed)-10))
				break
			}
			fmt.Fprintf(out, "  %s  %s  %s\n      %s\n", timeText(s.record.At), s.ID, orDash(s.record.Repository), oneLine(s.record.Error))
		}
	}

	fmt.Fprintln(out, "\nNewest Agent sessions:")
	for i, s := range sessions {
		if i == 5 {
			break
		}
		fmt.Fprintf(out, "  %s  %s  %s\n", timeText(s.ModTime), s.ID[:min(8, len(s.ID))], stateText(s, false))
	}
	fmt.Fprintln(out, "\nTo list each Agent session: solidtrace capture status --all")
}

// printCounts prints the counts of a map, most first; limit > 0 shows
// only that many.
func printCounts(out io.Writer, title string, counts map[string]int, limit int) {
	if len(counts) == 0 {
		return
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	fmt.Fprintf(out, "\n%s:\n", title)
	width := len(number(counts[keys[0]]))
	for i, k := range keys {
		if limit > 0 && i == limit {
			fmt.Fprintf(out, "  and %s\n", plural(len(keys)-limit, "other folder", "other folders"))
			break
		}
		fmt.Fprintf(out, "  %*s  %s\n", width, number(counts[k]), k)
	}
}

// printEachSession lists each Agent session on the machine, newest first.
func printEachSession(out io.Writer, dir string, sessions []session) {
	fmt.Fprintf(out, "\nAgent sessions on this machine (%s), newest first:\n", dir)
	if len(sessions) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	for _, s := range sessions {
		fmt.Fprintf(out, "  %s  %s\n", s.ID, stateText(s, true))
	}
}

// stateText is the state of an Agent session from the record of its last
// capture: uploaded, not captured, failed or not checked yet. withTime
// adds the time of that capture.
func stateText(s session, withTime bool) string {
	if !s.ok {
		return "not checked yet"
	}
	r := s.record
	at := ""
	if withTime {
		at = "  checked " + timeText(r.At)
	}
	switch r.Status {
	case capture.Uploaded:
		return fmt.Sprintf("uploaded      %s%s", r.Repository, at)
	case capture.NotInList:
		return fmt.Sprintf("not captured  %s%s", startDir(r), at)
	default:
		return fmt.Sprintf("failed        %s%s: %s", orDash(r.Repository), at, oneLine(r.Error))
	}
}

func startDir(r capture.Record) string {
	if r.StartDir == "" {
		return "no working directory"
	}
	return r.StartDir
}

const timeFormat = "2006-01-02 15:04 UTC"

// timeText is a time in the machine's time zone.
func timeText(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

// ago is how long before now a time was, in words.
func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "less than a minute ago"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute", "minutes") + " ago"
	case d < 48*time.Hour:
		return plural(int(d/time.Hour), "hour", "hours") + " ago"
	default:
		return plural(int(d/(24*time.Hour)), "day", "days") + " ago"
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return number(n) + " " + many
}

// number is n with a comma in each group of three digits.
func number(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
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
