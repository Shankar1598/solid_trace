// Package capture uploads the new bytes of one Agent session from the
// machine to the agent ingest API, when its repository is captured.
//
// The order keeps each byte of another repository on the machine: get the
// captured repositories list (a local cache of at most 10 minutes), match
// the working directory's git remotes, and only then ask what the server
// has and send what is new. A .jsonl file goes as whole lines from the
// server's end offset; another file goes whole, with its hash, when it did
// not change since the last attempt, or at SessionEnd. One lock for each
// Agent session stops two uploads of it at the same time.
package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/solidtrace/cli/agentapi"
)

// Harness is the name of the coding agent that the server records.
const Harness = "claude-code"

// SessionEnd is the hook event at the end of an Agent session.
const SessionEnd = "SessionEnd"

// ListLasts is how long a captured repositories list is used.
const ListLasts = 10 * time.Minute

// LockWait is how long a call waits for another call of the same Agent
// session. The later call then sends what the first one left.
const LockWait = 2 * time.Minute

// Input is what Claude Code gives a hook on stdin.
type Input struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
}

// Capturer uploads Agent sessions with one sign-in.
type Capturer struct {
	API *agentapi.Client
	// Dir is the config directory: it holds the list cache and the state
	// and lock of each Agent session.
	Dir string
	// Signin names the sign-in (instance, ingest URL, Organization): a
	// cached list of another sign-in is not used.
	Signin string
	Now    func() time.Time
	// Log writes one line to the local log.
	Log func(format string, args ...any)
}

var sessionIDShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Capture uploads the new bytes of the hook's Agent session.
func (c *Capturer) Capture(ctx context.Context, in Input) error {
	if !sessionIDShape.MatchString(in.SessionID) {
		return fmt.Errorf("the hook input has no Agent session id (%q)", in.SessionID)
	}
	if filepath.Base(in.TranscriptPath) != in.SessionID+".jsonl" || !filepath.IsAbs(in.TranscriptPath) {
		return fmt.Errorf("the transcript path %q is not <Agent session id>.jsonl", in.TranscriptPath)
	}
	list, err := c.list(ctx)
	if err != nil {
		return fmt.Errorf("no captured repositories list, so nothing is sent: %w", err)
	}
	repo, ok, err := Match(ctx, in.Cwd, list)
	if err != nil {
		return fmt.Errorf("read the git remotes of %s: %w", in.Cwd, err)
	}
	if !ok {
		c.Log("%s: not in the captured repositories (%s), nothing sent", in.SessionID, in.Cwd)
		return nil
	}

	unlock, err := c.lock(ctx, in.SessionID)
	if err != nil {
		return err
	}
	defer unlock()

	u := &upload{c: c, in: in, repo: repo, root: filepath.Dir(in.TranscriptPath)}
	return u.run(ctx)
}

// lock waits until this process alone uploads the Agent session.
func (c *Capturer) lock(ctx context.Context, sessionID string) (func(), error) {
	dir := filepath.Join(c.Dir, "capture")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, sessionID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.NewTimer(LockWait)
	defer deadline.Stop()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", f.Name(), err)
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			f.Close()
			return nil, errors.New("another capture of this Agent session did not stop in time; the next call sends what is left")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (c *Capturer) statePath(sessionID string) string {
	return filepath.Join(c.Dir, "capture", sessionID+".json")
}
