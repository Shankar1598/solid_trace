package capture

import (
	"context"
	"fmt"
	"path/filepath"
)

// RunUpload is one upload of a Run's Agent session, by the wrapper of its
// Execution.
type RunUpload struct {
	SessionID string
	// ProjectDir is the Claude project directory: it holds <id>.jsonl and
	// the <id>/ directory.
	ProjectDir string
	// Repository is the Run's repository, from its Factory settings.
	Repository Repository
	// RunID and Project name the Run and its Project (slug).
	RunID, Project string
	// Extra are directories under ProjectDir whose files go with this
	// Agent session, such as the Run's evidence/.
	Extra []string
	// Final sends each file that is not .jsonl at once, as at SessionEnd.
	Final bool
}

// UploadRun sends the new bytes of a Run's Agent session with capture's
// rules: whole lines of each .jsonl file from the server's end offset, and
// each other file whole, when it did not change since the last upload or
// the upload is Final. It has no captured repositories check: a Run's
// repository comes from its Factory settings, and its token reaches only
// the Run's Agent sessions. Files open inside ProjectDir only.
func (c *Capturer) UploadRun(ctx context.Context, r RunUpload) error {
	if !sessionIDShape.MatchString(r.SessionID) {
		return fmt.Errorf("%q is not an Agent session id", r.SessionID)
	}
	in := Input{SessionID: r.SessionID, TranscriptPath: filepath.Join(r.ProjectDir, r.SessionID+".jsonl")}
	if r.Final {
		in.HookEventName = SessionEnd
	}
	unlock, err := c.lock(ctx, r.SessionID, true)
	if err != nil {
		return err
	}
	defer unlock()
	u := &upload{c: c, in: in, repo: r.Repository, root: r.ProjectDir, forRun: &r}
	return u.run(ctx)
}
