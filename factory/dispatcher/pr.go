package dispatcher

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

// maxPRTitle is the most runes of a PR's title.
const maxPRTitle = 255

// pr is the wrapper's pr call: it opens the Run's PR from the Run's
// branch to its base branch through the Project's Forge, or updates the
// title and description of the PR that the Run has. The Run records the
// PR at once, with a PR opened event. It never merges. The wrapper pushed
// the Run's branch before the call.
func (d *Dispatcher) pr(w http.ResponseWriter, r *http.Request, e storedExecution) {
	var text dispatcherapi.PRText
	if !decode(w, r, &text, "the body must be a JSON object with title and description") {
		return
	}
	text.Title = strings.TrimSpace(text.Title)
	text.Description = strings.TrimSpace(text.Description)
	if problem := checkPRText(text); problem != "" {
		writeProblem(w, http.StatusBadRequest, problem)
		return
	}
	if e.state != dispatcherapi.ExecutionRunning {
		writeProblem(w, http.StatusConflict, "the Execution is not running: "+errEnded.Error()+" or has not started")
		return
	}
	// One pr call of a Run at a time: two first calls must not open two
	// PRs. The pr calls of other Runs do not wait.
	defer d.prLocks.lock(e.runID)()
	// The forge call and its record do not stop when the wrapper goes:
	// a PR that the forge opened is always recorded on the Run.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), dispatcherapi.PRBudget)
	defer cancel()
	target, err := d.store.prTarget(e.runID)
	if err != nil {
		d.serverError(w, "read the Run's PR", err)
		return
	}
	settings, err := d.store.settings(e.organizationID, e.projectID)
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusConflict, "the Project has no Factory settings")
		return
	}
	if err != nil {
		d.serverError(w, "read the Factory settings", err)
		return
	}
	if target.repository == "" {
		target.repository = settings.Repository
	}
	f, err := d.projectForge(e.organizationID, settings.Connection)
	if err != nil {
		writeProblem(w, http.StatusConflict, err.Error())
		return
	}
	if target.prID != 0 {
		got, err := f.UpdatePR(ctx, target.repository, target.prID,
			forge.PRText{Title: text.Title, Description: text.Description})
		if err != nil {
			writeProblem(w, http.StatusBadGateway, err.Error())
			return
		}
		link := got.Link
		if link == "" {
			link = target.prURL
		}
		writeJSON(w, http.StatusOK, dispatcherapi.PR{ID: target.prID, URL: link})
		return
	}
	if text.Title == "" {
		writeProblem(w, http.StatusBadRequest, "the Run has no PR yet: the first pr needs a title")
		return
	}
	got, err := f.OpenPR(ctx, target.repository, forge.NewPR{Source: dispatcherapi.RunBranch(e.runID),
		Destination: target.baseBranch, Title: text.Title, Description: text.Description})
	if err != nil {
		writeProblem(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := d.store.savePR(e.runID, e.id, got, d.now()); err != nil {
		d.serverError(w, "record the Run's PR", err)
		return
	}
	writeJSON(w, http.StatusOK, dispatcherapi.PR{ID: got.ID, URL: got.Link, Opened: true})
}

// runLocks holds one lock for each Run that has a pr call, and forgets
// it when the last call ends.
type runLocks struct {
	mu   sync.Mutex
	runs map[string]*runLock
}

type runLock struct {
	sync.Mutex
	users int
}

// lock locks the Run and gives the unlock.
func (l *runLocks) lock(runID string) (unlock func()) {
	l.mu.Lock()
	if l.runs == nil {
		l.runs = map[string]*runLock{}
	}
	rl := l.runs[runID]
	if rl == nil {
		rl = &runLock{}
		l.runs[runID] = rl
	}
	rl.users++
	l.mu.Unlock()
	rl.Lock()
	return func() {
		rl.Unlock()
		l.mu.Lock()
		if rl.users--; rl.users == 0 {
			delete(l.runs, runID)
		}
		l.mu.Unlock()
	}
}

func checkPRText(t dispatcherapi.PRText) string {
	switch {
	case t.Title == "" && t.Description == "":
		return "give a title or a description"
	case !utf8.ValidString(t.Title) || !utf8.ValidString(t.Description):
		return "a title and a description must be UTF-8 text"
	case strings.ContainsAny(t.Title, "\r\n"):
		return "a PR's title is one line"
	case utf8.RuneCountInString(t.Title) > maxPRTitle:
		return "a PR's title can have at most 255 characters"
	case len(t.Description) > maxReportText:
		return "a PR's description can have at most 64 KiB"
	}
	return ""
}

// prTarget is where a Run's PR goes, and the PR that it has (prID 0 =
// none).
type prTarget struct {
	repository, baseBranch, prURL string
	prID                          int
}

func (s *store) prTarget(runID string) (prTarget, error) {
	var t prTarget
	err := s.db.QueryRow(`SELECT repository, base_branch, pr_id, pr_url FROM runs WHERE id = ?`, runID).
		Scan(&t.repository, &t.baseBranch, &t.prID, &t.prURL)
	return t, err
}

// savePR records the PR that the Execution's pr call opened on its Run,
// with the PR opened event.
func (s *store) savePR(runID, executionID string, pr forge.PR, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE runs SET pr_id = ?, pr_url = ? WHERE id = ? AND pr_id = 0`, pr.ID, pr.Link, runID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		if err == nil {
			err = sql.ErrNoRows
		}
		return err
	}
	if err := recordEvent(tx, at, runID, eventPROpened, map[string]any{"execution.id": executionID,
		"pr.id": pr.ID}); err != nil {
		return err
	}
	return s.commit(tx)
}
