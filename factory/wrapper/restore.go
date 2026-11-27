package wrapper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/dispatcherapi"
)

// The restore of the Agent session directory. An Execution that resumes
// the Run's Agent session (Task.Resume) downloads the directory from
// SolidTrace into the fixed project directory before the harness starts,
// so that --resume finds the transcript on any machine. The download is
// the highest version of each path as SolidTrace stored it; the uploads
// then go on from its end offsets.

// restoreWait is the longest time that the restore waits for SolidTrace
// to have the last uploads ready (its 503 with Retry-After).
const restoreWait = 2 * time.Minute

// restoreTimeout is the longest time of one download. It is the server's
// own limit of one download.
const restoreTimeout = 10 * time.Minute

// errNoAgentSession is a download of an Agent session that SolidTrace
// does not have: the Run's earlier Executions did not start the harness,
// or Retention dropped it.
var errNoAgentSession = errors.New("SolidTrace has no Agent session")

// restore downloads the Run's current Agent session into the project
// directory. It is true when the transcript (<session id>.jsonl) is back,
// so that the harness can resume it. It is false, with no error, when
// SolidTrace has no Agent session or no complete transcript of it: the
// harness then starts an Agent session with the task, under the session
// id that restore gives. With no Agent session in SolidTrace, that is the
// Run's current id. With a partial or cut transcript, SolidTrace keeps the
// old transcript's end offsets, so a new Agent session joins the Run: the
// Dispatcher gives a new id and makes it the Run's current one. It writes
// through the open project directory (prepare), and gives each file and
// folder that it makes to the run user.
func (u *uploads) restore(ctx context.Context, r runUser) (resumed bool, session string, err error) {
	session = u.task.SessionID
	found, resumed, err := u.download(ctx, r)
	if err != nil || resumed || !found {
		return resumed, session, err
	}
	if err := u.askFor(ctx, dispatcherapi.TokenRequest{NewSession: true}); err != nil {
		return false, "", fmt.Errorf("get a new Agent session id for the Run: %w", err)
	}
	u.mu.Lock()
	session = u.token.SessionID
	u.mu.Unlock()
	if !sessionIDShape(session) || session == u.task.SessionID {
		return false, "", fmt.Errorf("the Dispatcher gave %q as the new Agent session id", session)
	}
	fmt.Fprintf(u.log, "wrapper: restore: the new Agent session %s joins the Run\n", session)
	return false, session, nil
}

// download downloads the Run's current Agent session into the project
// directory. found is true when SolidTrace has the Agent session, and
// restored when its complete transcript is back.
func (u *uploads) download(ctx context.Context, r runUser) (found, restored bool, err error) {
	if !sessionIDShape(u.task.SessionID) {
		return false, false, fmt.Errorf("%q is not an Agent session id", u.task.SessionID)
	}
	ctx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()
	hc := &http.Client{Transport: u.capturer.API.HTTP.Transport}
	deadline := time.Now().Add(restoreWait)
	for {
		download, err := u.capturer.API.DownloadAgentSession(ctx, hc, u.task.SessionID)
		var answer *agentapi.Error
		switch {
		case errors.As(err, &answer) && answer.Status == http.StatusNotFound:
			fmt.Fprintf(u.log, "wrapper: restore: %v %s: the harness starts it again\n", errNoAgentSession,
				u.task.SessionID)
			return false, false, nil
		case errors.As(err, &answer) && answer.Status == http.StatusServiceUnavailable && time.Now().Before(deadline):
			wait := time.Duration(max(answer.RetryAfter, 1)) * time.Second
			select {
			case <-ctx.Done():
				return false, false, ctx.Err()
			case <-time.After(wait):
			}
			continue
		case err != nil:
			return false, false, err
		}
		defer download.Archive.Close()
		files, err := agentapi.Extract(download.Archive, u.dir, "", func(name string) error { return r.giveIn(u.dir, name) })
		if err != nil {
			return false, false, err
		}
		transcript := false
		for _, f := range files {
			if f.Flags != "" {
				fmt.Fprintf(u.log, "wrapper: restore: %s is %s in SolidTrace: it is not restored\n", f.Name, f.Flags)
			} else if f.Name == u.task.SessionID+".jsonl" {
				transcript = true
			}
		}
		if !transcript {
			fmt.Fprintf(u.log, "wrapper: restore: SolidTrace has no complete transcript of the Agent session %s\n",
				u.task.SessionID)
			return true, false, nil
		}
		return true, true, nil
	}
}

// sessionIDShape is true for a lower-case UUID.
func sessionIDShape(id string) bool { return sessionFile.MatchString(id + ".jsonl") }

// giveIn makes the path in root the run user's.
func (r runUser) giveIn(root *os.Root, name string) error {
	if !r.changes() {
		return nil
	}
	return root.Lchown(name, r.uid, r.gid)
}
