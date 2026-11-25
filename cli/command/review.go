package command

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/solidtrace/cli/review"
)

// sessionIDPattern keeps an Agent session's id from making a glob or a
// path of another folder.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func reviewCommand(ctx context.Context, args []string, env Env) int {
	if len(args) > 0 {
		switch args[0] {
		case "digest":
			return reviewDigest(args[1:], env)
		case "sessions":
			return reviewSessions(ctx, args[1:], env)
		case "check":
			return reviewCheck(args[1:], env)
		case "fixed":
			return reviewFixed(ctx, args[1:], env)
		}
	}
	fmt.Fprintln(env.Stderr, "solidtrace review: give digest, sessions, check or fixed")
	return 2
}

// stringList is a flag that can be given more than once.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(s string) error { *l = append(*l, s); return nil }

func reviewDigest(args []string, env Env) int {
	flags := flag.NewFlagSet("review digest", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	var sessions, files stringList
	flags.Var(&sessions, "session", "the id of an Agent session (more than once for more sessions)")
	flags.Var(&files, "file", "the path of a main transcript (more than once for more transcripts)")
	skills := flags.String("skills", review.DefaultSkills, "a regular expression that matches the names of the review skills")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) > 0 || len(sessions)+len(files) == 0 {
		fmt.Fprintln(env.Stderr, "solidtrace review digest: give --session ID or --file PATH (one or more)")
		return 2
	}
	pattern, err := regexp.Compile(*skills)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace review digest: --skills: %v\n", err)
		return 2
	}
	type source struct{ id, path string }
	var sources []source
	for _, id := range sessions {
		path, err := sessionTranscript(env.ClaudeProjectsDir, id)
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace review digest: %v\n", err)
			return 1
		}
		sources = append(sources, source{id, path})
	}
	for _, path := range files {
		sources = append(sources, source{trimExt(filepath.Base(path)), path})
	}
	var digests []review.Digest
	for _, s := range sources {
		d, err := readDigest(s.path, s.id, pattern)
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace review digest: %v\n", err)
			return 1
		}
		digests = append(digests, d)
	}
	return printJSON(env, "review digest", review.Merge(digests...))
}

func readDigest(path, id string, skills *regexp.Regexp) (review.Digest, error) {
	f, err := os.Open(path)
	if err != nil {
		return review.Digest{}, err
	}
	defer f.Close()
	d, err := review.Read(f, id, skills)
	if err != nil {
		return review.Digest{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

func printJSON(env Env, verb string, v any) int {
	out := json.NewEncoder(env.Stdout)
	// The texts hold code: keep <, > and & as they are.
	out.SetEscapeHTML(false)
	out.SetIndent("", "  ")
	if err := out.Encode(v); err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace %s: %v\n", verb, err)
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
