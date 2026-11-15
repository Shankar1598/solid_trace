package wrapper

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/solidtrace/cli/agentapi"
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
// harness then starts the Agent session again, with the task.
func (u *uploads) restore(ctx context.Context) (bool, error) {
	if !sessionIDShape(u.task.SessionID) {
		return false, fmt.Errorf("%q is not an Agent session id", u.task.SessionID)
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
			return false, nil
		case errors.As(err, &answer) && answer.Status == http.StatusServiceUnavailable && time.Now().Before(deadline):
			wait := time.Duration(max(answer.RetryAfter, 1)) * time.Second
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(wait):
			}
			continue
		case err != nil:
			return false, err
		}
		defer download.Archive.Close()
		restored, err := extract(download.Archive, u.projectDir, u.log)
		if err != nil {
			return false, err
		}
		if !restored[u.task.SessionID+".jsonl"] {
			fmt.Fprintf(u.log, "wrapper: restore: SolidTrace has no complete transcript of the Agent session %s: "+
				"the harness starts it again\n", u.task.SessionID)
			return false, nil
		}
		return true, nil
	}
}

// sessionIDShape is true for a lower-case UUID.
func sessionIDShape(id string) bool { return sessionFile.MatchString(id + ".jsonl") }

// extract writes each file of the tar archive into dir, and gives the
// paths that it wrote. A file that is partial (Retention dropped its
// start) or cut (it has missing ranges) is not written: its offsets would
// not agree with SolidTrace's, and a resume from it is not promised. A
// path that is not a plain relative path stops the restore.
func extract(archive io.Reader, dir string, log io.Writer) (map[string]bool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	restored := map[string]bool{}
	r := tar.NewReader(archive)
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return restored, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read the download: %w", err)
		}
		// SolidTrace stores files only: no folder, link or device.
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := h.Name
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || name == ".." ||
			strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
			return nil, fmt.Errorf("the download has the path %q, which is not in the Agent session directory", name)
		}
		if flags := h.PAXRecords[agentapi.PAXFlags]; flags != "" {
			fmt.Fprintf(log, "wrapper: restore: %s is %s in SolidTrace: it is not restored\n", name, flags)
			continue
		}
		if err := root.MkdirAll(filepath.Dir(filepath.FromSlash(name)), 0o700); err != nil {
			return nil, err
		}
		f, err := root.OpenFile(filepath.FromSlash(name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(f, r)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, fmt.Errorf("restore %s: %w", name, err)
		}
		restored[name] = true
	}
}
