// Package capture uploads the new bytes of one Agent session from the
// machine to the agent ingest API, when its repository is captured.
//
// The order keeps each byte of another repository on the machine: get the
// captured repositories list (a local cache of at most 10 minutes), match
// the git remotes of the directory where the Agent session started (not
// the hook's cwd, which follows each cd), and only then ask what the server
// has and send what is new. A .jsonl file goes as whole lines from the
// server's end offset; another file goes whole, with its hash, when it did
// not change since the last attempt, or at SessionEnd. One lock for each
// Agent session stops two uploads of it at the same time.
package capture

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"
	"unicode/utf16"

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
		err = fmt.Errorf("no captured repositories list, so nothing is sent: %w", err)
		c.recordFailed(in.SessionID, Record{}, err)
		return err
	}
	return c.one(ctx, in, list, &pass{})
}

// pass is one call of capture: a hook's call for its Agent session, or a
// sweep of each Agent session on the machine.
type pass struct {
	sweep bool
	// settledBefore: in a sweep, a file that did not change since then is
	// sent whole, as at SessionEnd.
	settledBefore time.Time
	// matches keeps the match of each start directory in a sweep.
	matches map[string]match
	// stopped is the failure that stopped a sweep: each later Agent session
	// with bytes to send gets it, with no request.
	stopped error
}

type match struct {
	repo Repository
	ok   bool
	err  error
}

func (p *pass) match(ctx context.Context, start string, list []string) (Repository, bool, error) {
	if p.matches == nil {
		repo, ok, err := Match(ctx, start, list)
		return repo, ok, err
	}
	m, seen := p.matches[start]
	if !seen {
		m.repo, m.ok, m.err = Match(ctx, start, list)
		p.matches[start] = m
	}
	return m.repo, m.ok, m.err
}

// one captures one Agent session with the list in hand and keeps its
// record. Nothing is sent before the match passes.
func (c *Capturer) one(ctx context.Context, in Input, list []string, p *pass) error {
	start := StartDir(in.TranscriptPath, in.Cwd)
	repo, ok, err := p.match(ctx, start, list)
	if err != nil {
		err = fmt.Errorf("read the git remotes of %s: %w", start, err)
		c.recordFailed(in.SessionID, Record{StartDir: start}, err)
		return err
	}
	if !ok {
		if !p.sweep {
			c.Log("%s: not in the captured repositories (%s), nothing sent", in.SessionID, start)
		}
		c.record(in.SessionID, Record{Status: NotInList, StartDir: start})
		return nil
	}
	rec := Record{Repository: repo.Name, StartDir: start}
	if p.stopped != nil {
		c.recordFailed(in.SessionID, rec, p.stopped)
		return nil
	}

	unlock, err := c.lock(ctx, in.SessionID, !p.sweep)
	if errors.Is(err, errBusy) {
		// A hook's call uploads it now.
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()

	root := filepath.Dir(in.TranscriptPath)
	rec.Fingerprint = fingerprint(root, in.SessionID)
	u := &upload{c: c, in: in, repo: repo, root: root, settledBefore: p.settledBefore}
	if err := u.run(ctx); err != nil {
		c.recordFailed(in.SessionID, rec, err)
		return err
	}
	rec.Status, rec.Pending = Uploaded, u.pending
	c.record(in.SessionID, rec)
	return nil
}

// startLines and startBytes limit how much of <id>.jsonl StartDir reads.
const (
	startLines = 64
	startBytes = 8 << 20
)

// StartDir is the working directory where the Agent session started: the
// cwd of the first line of the transcript that has one, when that folder
// is on this machine and its Claude Code project folder holds the
// transcript. The hook's cwd follows each cd of the agent. A fork of a
// download keeps the cwd of the first machine, which can be another
// folder, or no folder, on this machine. Then StartDir is the hook's cwd
// or the nearest folder above it whose project folder holds the
// transcript. Else it is the hook's cwd.
func StartDir(transcriptPath, hookCwd string) string {
	project := filepath.Base(filepath.Dir(transcriptPath))
	if cwd := firstCwd(transcriptPath); cwd != "" && ClaudeProjectFolder(cwd) == project {
		if info, err := os.Stat(cwd); err == nil && info.IsDir() {
			return cwd
		}
	}
	if filepath.IsAbs(hookCwd) {
		for dir := filepath.Clean(hookCwd); ; dir = filepath.Dir(dir) {
			if ClaudeProjectFolder(dir) == project {
				return dir
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	return hookCwd
}

// firstCwd is the cwd of the first line of the transcript that has one, ""
// when no line has one.
func firstCwd(transcriptPath string) string {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, startBytes))
	for range startLines {
		line, err := r.ReadBytes('\n')
		var l struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &l) == nil && filepath.IsAbs(l.Cwd) {
			return l.Cwd
		}
		if err != nil {
			break
		}
	}
	return ""
}

// ClaudeProjectFolder is the name of the folder under Claude Code's
// projects folder that holds the transcripts of the Agent sessions that
// start in the folder at path. Claude Code changes each UTF-16 code unit
// that is not an ASCII letter or digit to "-". A name of more than 200
// characters keeps its first 200, then "-" and the absolute value of
// Java's String.hashCode of path, in base 36.
func ClaudeProjectFolder(path string) string {
	units := utf16.Encode([]rune(path))
	name := make([]byte, len(units))
	var hash int32
	for i, u := range units {
		hash = 31*hash + int32(u)
		if u < 128 && (u >= 'a' && u <= 'z' || u >= 'A' && u <= 'Z' || u >= '0' && u <= '9') {
			name[i] = byte(u)
		} else {
			name[i] = '-'
		}
	}
	if len(name) <= 200 {
		return string(name)
	}
	return string(name[:200]) + "-" + strconv.FormatInt(max(int64(hash), -int64(hash)), 36)
}

// errBusy is the answer of a lock that does not wait, when another
// process holds it.
var errBusy = errors.New("another capture holds the lock")

// lock waits until this process alone uploads the Agent session. With
// wait false it answers errBusy at once when another process holds it.
func (c *Capturer) lock(ctx context.Context, name string, wait bool) (func(), error) {
	dir := filepath.Join(c.Dir, "capture")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
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
		if !wait && errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, errBusy
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
