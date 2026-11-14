package capture

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"time"

	"github.com/solidtrace/cli/agentapi"
)

// SessionStart is the hook event that starts the sweep.
const SessionStart = "SessionStart"

// SettleAfter is how long a file that is not .jsonl must stay the same
// before the sweep sends it whole: a crashed or left Claude Code does not
// end its Agent session, so no SessionEnd comes.
const SettleAfter = time.Minute

// Sweep captures each Agent session under Claude Code's projects
// directory, with the same check of the captured repositories list as a
// hook's call: the Agent sessions that a crashed Claude Code left with
// bytes not sent, the ones of a repository added to the list, and the ones
// whose last upload failed. An Agent session that the last call uploaded
// and that did not change gets no request. A failure that is not of one
// Agent session (no network, the sign-in, the server's version) stops the
// sweep, and each later Agent session with bytes to send gets it as its
// failure. Only one sweep runs at a time.
func (c *Capturer) Sweep(ctx context.Context, projectsDir string) error {
	unlock, err := c.lock(ctx, "sweep", false)
	if errors.Is(err, errBusy) {
		c.Log("sweep: another sweep runs")
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()

	sessions, err := Sessions(projectsDir)
	if err != nil {
		return fmt.Errorf("sweep: read %s: %w", projectsDir, err)
	}
	list, err := c.list(ctx)
	if err != nil {
		return fmt.Errorf("sweep: no captured repositories list, so nothing is sent: %w", err)
	}
	p := &pass{sweep: true, settledBefore: c.Now().Add(-SettleAfter), matches: map[string]match{}}
	for _, s := range sessions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if r, ok := RecordOf(c.Dir, s.ID); ok && r.Status == Uploaded && !r.Pending &&
			r.Fingerprint == fingerprint(filepath.Dir(s.TranscriptPath), s.ID) {
			continue
		}
		in := Input{SessionID: s.ID, TranscriptPath: s.TranscriptPath, HookEventName: SessionStart}
		if err := c.one(ctx, in, list, p); err != nil {
			c.Log("sweep: %s: %v", s.ID, err)
			if p.stopped == nil && stopsTheSweep(err) {
				p.stopped = err
			}
		}
	}
	if p.stopped != nil {
		return fmt.Errorf("sweep stopped: %w", p.stopped)
	}
	return nil
}

// stopsTheSweep is true for a failure that the next Agent session would
// get too: no network, the sign-in, a server failure or its version. A
// refusal of one Agent session, or a file that cannot be read, is not.
func stopsTheSweep(err error) bool {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return false
	}
	var answer *agentapi.Error
	if errors.As(err, &answer) {
		switch answer.Status {
		case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
			return false
		}
	}
	return true
}
