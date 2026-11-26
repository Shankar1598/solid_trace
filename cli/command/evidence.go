package command

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/solidtrace/cli/agentapi"
)

// ClaudeSessionEnv is the environment variable in which Claude Code gives
// its Bash tool the id of the Agent session.
const ClaudeSessionEnv = "CLAUDE_CODE_SESSION_ID"

// evidenceLimit is the most bytes of an Evidence file, for a person to
// read.
var evidenceLimit = fmt.Sprintf("%d MiB", agentapi.MaxEvidenceBytes>>20)

// evidence runs `evidence add`: it uploads each file as an Evidence file
// of the Agent session, and prints one line for each file. A file that
// fails does not stop the next one.
func evidence(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 || args[0] != "add" {
		fmt.Fprintln(env.Stderr, "solidtrace evidence: give add FILE... [--caption TEXT] [--session ID]")
		return 2
	}
	flags := flag.NewFlagSet("evidence add", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	caption := flags.String("caption", "", fmt.Sprintf("the caption of each file (at most %d characters)", agentapi.MaxEvidenceCaption))
	session := flags.String("session", "", "the Agent session (default: "+ClaudeSessionEnv+")")
	files, ok := parseInterspersed(flags, args[1:])
	if !ok {
		return 2
	}
	if len(files) == 0 {
		fmt.Fprintln(env.Stderr, "solidtrace evidence add: give one FILE or more")
		return 2
	}
	text := strings.TrimSpace(*caption)
	if n := utf8.RuneCountInString(text); !utf8.ValidString(text) || n > agentapi.MaxEvidenceCaption {
		fmt.Fprintf(env.Stderr, "solidtrace evidence add: a caption is at most %d characters, in UTF-8; this one has %d\n",
			agentapi.MaxEvidenceCaption, n)
		return 2
	}
	sessionID := strings.TrimSpace(*session)
	if sessionID == "" {
		sessionID = strings.TrimSpace(env.ClaudeSessionID)
	}
	if sessionID == "" {
		fmt.Fprintln(env.Stderr, "solidtrace evidence add: no Agent session: run this from Claude Code, or give --session")
		return 1
	}
	source := env.Source()
	creds, err := source.Credentials()
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace evidence add: %v\n", err)
		return 1
	}
	client := &agentapi.Client{IngestURL: creds.IngestURL, HTTP: env.HTTP, Tokens: source, Version: env.Version,
		Warnings: env.Stderr}
	code := 0
	for _, file := range files {
		added, err := addEvidence(ctx, client, sessionID, text, file)
		if err != nil {
			fmt.Fprintf(env.Stderr, "solidtrace evidence add: %s: %s\n", file, evidenceProblem(err))
			code = 1
			continue
		}
		link := added.URL
		if strings.HasPrefix(link, "/") {
			link = strings.TrimRight(creds.InstanceURL, "/") + link
		}
		fmt.Fprintf(env.Stdout, "%s %s %s\n", link, added.ContentType, byteSize(added.ByteSize))
	}
	return code
}

func addEvidence(ctx context.Context, client *agentapi.Client, sessionID, caption, file string) (agentapi.Evidence, error) {
	info, err := os.Stat(file)
	if err != nil {
		return agentapi.Evidence{}, err
	}
	if !info.Mode().IsRegular() {
		return agentapi.Evidence{}, errors.New("not a file")
	}
	if info.Size() > agentapi.MaxEvidenceBytes {
		return agentapi.Evidence{}, fmt.Errorf("an Evidence file is at most %s; this one has %d bytes", evidenceLimit, info.Size())
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return agentapi.Evidence{}, err
	}
	return client.AddEvidence(ctx, sessionID, caption, data)
}

// evidenceProblem is the text of an upload's error, with what to do for
// the refusals of the Evidence files API.
func evidenceProblem(err error) string {
	var refused *agentapi.Error
	if !errors.As(err, &refused) {
		return err.Error()
	}
	switch refused.Status {
	case http.StatusNotFound:
		return "this SolidTrace server is too old for Evidence files: ask the operator to upgrade it"
	case http.StatusUnsupportedMediaType:
		return "SolidTrace keeps only PNG, JPEG, WebP and GIF images and MP4 and WebM videos as Evidence files"
	case http.StatusRequestEntityTooLarge:
		return cmp.Or(refused.Message, "the file is too large: an Evidence file is at most "+evidenceLimit)
	case http.StatusBadRequest, http.StatusForbidden:
		if refused.Message != "" {
			return refused.Message
		}
	}
	return err.Error()
}

// byteSize is a size for a person to read: 512 B, 84 KB, 3.4 MB. A size
// that rounds to 1024 KB is in MB.
func byteSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20-1<<9:
		return fmt.Sprintf("%d KB", (n+1<<9)>>10)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
}
