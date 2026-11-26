package command

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/review"
)

// reviewFinal checks the Review record in a file and writes it to the
// folder of its Agent session, where the capture uploads it with the
// session's other files (ADR 0024).
func reviewFinal(args []string, env Env) int {
	flags := flag.NewFlagSet("review final", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	session := flags.String("session", "", "the Agent session (default: "+ClaudeSessionEnv+")")
	flags.Usage = func() {
		fmt.Fprintln(env.Stderr, "Usage: solidtrace review final FILE [--session ID]\n\nCheck the Review record in FILE and write it to the folder of the Agent session, where the capture uploads it.")
		flags.PrintDefaults()
	}
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) != 1 {
		fmt.Fprintln(env.Stderr, "solidtrace review final: give the record's file: review final FILE [--session ID]")
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(env.Stderr, "solidtrace review final: %v\n", err)
		return 1
	}
	data, err := readRecordFile(positional[0])
	if err != nil {
		return fail(err)
	}
	record, err := review.ReadFinal(data)
	if err != nil {
		return fail(err)
	}
	sessionID := cmp.Or(strings.TrimSpace(*session), strings.TrimSpace(env.ClaudeSessionID))
	if sessionID == "" {
		return fail(errors.New("no Agent session: run this from Claude Code, or give --session"))
	}
	transcript, err := sessionTranscript(env.ClaudeProjectsDir, sessionID)
	if err != nil {
		return fail(err)
	}
	project := filepath.Dir(transcript)
	path := filepath.Join(project, sessionID, "solidtrace", "review-records", recordFileName(record))
	if err := writeRecord(project, path, data); err != nil {
		return fail(err)
	}
	fixed := 0
	for _, f := range record.Findings {
		if f.Outcome == "fixed" {
			fixed++
		}
	}
	fmt.Fprintf(env.Stdout, "record: round %d, %d findings, %d fixed, kept in %s\n", record.Round, len(record.Findings), fixed, path)
	return 0
}

// readRecordFile reads a record of at most capture.UnitLimit bytes: the
// capture cuts a larger file, and a cut file is no record.
func readRecordFile(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, capture.UnitLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > capture.UnitLimit {
		return nil, fmt.Errorf("%s is more than %d bytes", name, capture.UnitLimit)
	}
	return data, nil
}

// recordFileName is round-<n>-<h>.json, with <h> the first 12 hex
// characters of the SHA-256 of the record's pr ("" for none): a round of a
// PR written again replaces its file.
func recordFileName(r review.Record) string {
	var pr string
	if r.PR != nil {
		pr = *r.PR
	}
	sum := sha256.Sum256([]byte(pr))
	return fmt.Sprintf("round-%d-%s.json", r.Round, hex.EncodeToString(sum[:])[:12])
}

// writeRecord writes data to path with a rename, so that the capture never
// reads a part of it. The temporary file is in the project folder, outside
// the Agent session's folder, so that the capture does not upload it.
func writeRecord(project, path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(project, ".solidtrace-review-record-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
