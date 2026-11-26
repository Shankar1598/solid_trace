package command

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/review"
)

// reviewFinal checks the Review record in a file and writes it to the
// folder of its Agent session, where the capture uploads it with the
// session's other files (ADR 0024).
func reviewFinal(args []string, env Env) int {
	flags := flag.NewFlagSet("review final", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	session := sessionFlag(flags)
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
	sessionID, ok := agentSessionID(*session, env)
	if !ok {
		return fail(errors.New(noAgentSession))
	}
	transcript, err := sessionTranscript(env.ClaudeProjectsDir, sessionID)
	if several := (severalTranscripts{}); errors.As(err, &several) {
		transcript, err = newest(several.paths)
	}
	if err != nil {
		return fail(err)
	}
	projectFolder := filepath.Dir(transcript)
	path := filepath.Join(projectFolder, sessionID, capture.WrittenWholeFolder, "review-records", recordFileName(record))
	if err := writeRecord(projectFolder, path, data); err != nil {
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

// newest is the path of the file that changed last: of the copies of a
// transcript, the one that Claude Code writes now.
func newest(paths []string) (string, error) {
	var last string
	var at time.Time
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		if last == "" || info.ModTime().After(at) {
			last, at = p, info.ModTime()
		}
	}
	return last, nil
}

var recordLimit = fmt.Sprintf("%d MiB", capture.UnitLimit>>20)

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
// reads a part of it. The temporary file is in the Claude project folder,
// outside the Agent session's folder, so that the capture does not upload
// it.
func writeRecord(projectFolder, path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(projectFolder, ".solidtrace-review-record-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// The bytes go to the disk before the rename, so that a power loss
	// leaves the old file or the whole new one, not an empty file.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
