package command

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/solidtrace/cli/review"
)

// sessionIDPattern keeps an Agent session's id from making a glob or a
// path of another folder.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func reviewCommand(_ context.Context, args []string, env Env) int {
	if len(args) == 0 || args[0] != "digest" {
		fmt.Fprintln(env.Stderr, "solidtrace review: give digest --session ID or digest --file PATH")
		return 2
	}
	flags := flag.NewFlagSet("review digest", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	session := flags.String("session", "", "the Agent session's id")
	file := flags.String("file", "", "the path of a transcript")
	skills := flags.String("skills", review.DefaultSkills, "a regular expression that matches the names of the review skills")
	positional, ok := parseInterspersed(flags, args[1:])
	if !ok {
		return 2
	}
	if len(positional) > 0 || (*session == "") == (*file == "") {
		fmt.Fprintln(env.Stderr, "solidtrace review digest: give --session ID or --file PATH (one of the two)")
		return 2
	}
	pattern, err := regexp.Compile(*skills)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review digest: --skills: %v\n", err)
		return 2
	}
	path := *file
	if *session != "" {
		if path, err = sessionTranscript(env.ClaudeProjectsDir, *session); err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace review digest: %v\n", err)
			return 1
		}
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review digest: %v\n", err)
		return 1
	}
	defer f.Close()
	digest, err := review.Read(f, pattern)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review digest: %s: %v\n", path, err)
		return 1
	}
	if digest.Session == "" {
		digest.Session = *session
	}
	if digest.Session == "" {
		digest.Session = trimExt(filepath.Base(path))
	}
	out := json.NewEncoder(env.Stdout)
	// The texts hold code: keep <, > and & as they are.
	out.SetEscapeHTML(false)
	out.SetIndent("", "  ")
	if err := out.Encode(digest); err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review digest: %v\n", err)
		return 1
	}
	return 0
}

// sessionTranscript is the main transcript of the Agent session id: the
// one <projects>/<folder>/<id>.jsonl.
func sessionTranscript(projects, id string) (string, error) {
	if !sessionIDPattern.MatchString(id) {
		return "", fmt.Errorf("%q is not an Agent session id", id)
	}
	if projects == "" {
		return "", fmt.Errorf("no Claude Code projects folder: set CLAUDE_CONFIG_DIR or HOME")
	}
	matches, err := filepath.Glob(filepath.Join(projects, "*", id+".jsonl"))
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no transcript of the Agent session %s in %s", id, projects)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%d transcripts of the Agent session %s in %s: give --file with one of %v", len(matches), id, projects, matches)
	}
}

func trimExt(name string) string {
	return name[:len(name)-len(filepath.Ext(name))]
}
