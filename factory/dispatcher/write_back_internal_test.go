package dispatcher

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

func TestThePROpenedCommentListsEachCheckAndSaysNoEvidenceWithNoPassedCheck(t *testing.T) {
	const pr, run = "https://bitbucket.org/example/shop/pull-requests/1", "Run: https://solidtrace.example.com/example/runs/r-1"
	with := prOpenedComment(pr, run, "Fixed the total.", []checkLine{{name: "unit", passed: true}, {name: "lint"}})
	for _, part := range []string{pr, run, "Fixed the total.", "- passed: unit", "- failed: lint"} {
		if !strings.Contains(with, part) {
			t.Errorf("the comment does not have %q:\n%s", part, with)
		}
	}
	if strings.Contains(with, "no Evidence") {
		t.Errorf("a PR with a passed check says no Evidence:\n%s", with)
	}

	for _, checks := range [][]checkLine{nil, {{name: "lint"}}} {
		if got := prOpenedComment(pr, run, "Fixed.", checks); !strings.Contains(got, "no Evidence") {
			t.Errorf("a PR with the checks %v does not say no Evidence:\n%s", checks, got)
		}
	}
}

// writeBackRun is a Run of the ticket T-1 in a new store.
func writeBackRun(t *testing.T) *store {
	t.Helper()
	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	run := storedRun{Run: dispatcherapi.Run{ID: "r-1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: at}, organizationID: "7", projectID: "42"}
	if err := s.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE runs SET ticket_connection_id = 'c-1', ticket_id = 'T-1' WHERE id = 'r-1'`); err != nil {
		t.Fatal(err)
	}
	return s
}

// queueStep sets the Run's state and PR, and queues the write-back step of
// the run event kind of the Execution.
func queueStep(t *testing.T, s *store, kind, execution, state, prURL string) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE runs SET state = ?, pr_url = ?, summary = 'Fixed.' WHERE id = 'r-1'`, state,
		prURL); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := queueWriteBack(tx, time.Now(), "r-1", kind, map[string]any{"execution.id": execution}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// queuedSteps are the Run's write-back steps, oldest first, as
// "step status comment".
func queuedSteps(t *testing.T, s *store) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT step, status, comment FROM ticket_writes WHERE run_id = 'r-1' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var steps []string
	for rows.Next() {
		var step, status string
		var comment bool
		if err := rows.Scan(&step, &status, &comment); err != nil {
			t.Fatal(err)
		}
		steps = append(steps, fmt.Sprintf("%s %s %t", step, status, comment))
	}
	return steps
}

// A person answers a question, and the Run gets a second Execution: only
// the first Execution says that the Run started.
func TestOnlyTheFirstExecutionOfARunPostsTheStartedStep(t *testing.T) {
	s := writeBackRun(t)
	queueStep(t, s, eventExecutionStarted, "e-1", dispatcherapi.StateRunning, "")
	queueStep(t, s, eventExecutionEnded, "e-1", dispatcherapi.StateWaitingOnPerson, "")
	queueStep(t, s, eventExecutionStarted, "e-2", dispatcherapi.StateRunning, "")

	want := []string{"started working true", "needs_a_person waiting true"}
	if got := queuedSteps(t, s); !slices.Equal(got, want) {
		t.Errorf("the steps are %q, want %q", got, want)
	}
}

// A second done of the same PR sets the review status again and posts no
// second PR-opened comment; a new PR gets its own comment.
func TestEachPRGetsOnePROpenedComment(t *testing.T) {
	const first, second = "https://bitbucket.org/example/shop/pull-requests/1",
		"https://bitbucket.org/example/shop/pull-requests/2"
	s := writeBackRun(t)
	queueStep(t, s, eventExecutionEnded, "e-1", dispatcherapi.StateDone, first)
	queueStep(t, s, eventExecutionEnded, "e-2", dispatcherapi.StateWaitingOnPerson, first)
	queueStep(t, s, eventExecutionEnded, "e-3", dispatcherapi.StateDone, first)
	queueStep(t, s, eventExecutionEnded, "e-4", dispatcherapi.StateDone, second)

	want := []string{"pr_opened review true", "needs_a_person waiting true", "pr_opened review false",
		"pr_opened  true"}
	if got := queuedSteps(t, s); !slices.Equal(got, want) {
		t.Errorf("the steps are %q, want %q", got, want)
	}
}
