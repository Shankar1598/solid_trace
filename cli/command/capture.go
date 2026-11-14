package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/capture"
)

// LogFile is capture's local log in the config directory.
const LogFile = "capture.log"

// logLimit is the size at which the log moves to capture.log.1.
const logLimit = 1 << 20

// captureHook uploads the new bytes of the Agent session that the hook
// input on stdin names. It is a hook of Claude Code, so it never fails to
// the hook: a failure goes to the local log only, and the exit code is 0.
func captureHook(ctx context.Context, env Env) int {
	log := &localLog{path: filepath.Join(env.ConfigDir, LogFile), now: env}
	if err := runCapture(ctx, env, log); err != nil {
		log.Printf("%v", err)
	}
	return 0
}

func runCapture(ctx context.Context, env Env, log *localLog) error {
	if env.Stdin == nil {
		return fmt.Errorf("capture: no hook input")
	}
	data, err := io.ReadAll(io.LimitReader(env.Stdin, 1<<20))
	if err != nil {
		return fmt.Errorf("capture: read the hook input: %w", err)
	}
	var in capture.Input
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("capture: the hook input is not JSON: %w", err)
	}
	c, err := env.capturer(log)
	if err != nil {
		return fmt.Errorf("capture %s: %w", in.SessionID, err)
	}
	if in.HookEventName == capture.SessionStart {
		if !filepath.IsAbs(in.TranscriptPath) {
			return fmt.Errorf("sweep: the transcript path %q is not absolute", in.TranscriptPath)
		}
		// <projects>/<project>/<id>.jsonl
		return c.Sweep(ctx, filepath.Dir(filepath.Dir(in.TranscriptPath)))
	}
	if err := c.Capture(ctx, in); err != nil {
		return fmt.Errorf("capture %s: %w", in.SessionID, err)
	}
	return nil
}

// capturer is the Capturer of the stored sign-in.
func (env Env) capturer(log *localLog) (*capture.Capturer, error) {
	source := env.Source()
	creds, err := source.Credentials()
	if err != nil {
		return nil, err
	}
	return &capture.Capturer{
		API: &agentapi.Client{IngestURL: creds.IngestURL, HTTP: env.HTTP, Tokens: source, Version: env.Version,
			Warnings: log},
		Dir:    env.ConfigDir,
		Signin: signinName(creds.InstanceURL, creds.IngestURL, creds.Organization.Slug),
		Now:    env.Now,
		Log:    log.Printf,
	}, nil
}

func signinName(instance, ingest, organization string) string {
	return instance + " " + ingest + " " + organization
}

// localLog appends lines to capture's log. It is also the version
// warning's writer.
type localLog struct {
	mu   sync.Mutex
	path string
	now  Env
}

func (l *localLog) Printf(format string, args ...any) {
	_, _ = l.Write([]byte(fmt.Sprintf(format, args...)))
}

func (l *localLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := l.now.Now().UTC().Format("2006-01-02T15:04:05Z") + " " + strings.TrimRight(string(p), "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return 0, err
	}
	if info, err := os.Stat(l.path); err == nil && info.Size() > logLimit {
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return 0, err
	}
	return len(p), nil
}
