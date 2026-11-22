package wrapper

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The WIP commit's message names the files that the save left out, and
// the restore reads the same names back, also a name with a new line.
func TestTheLeftOutFilesOfASaveComeBackFromItsMessage(t *testing.T) {
	skipped := []string{"build/", "big.bin", "odd\nname.txt", `"quoted".txt`, `"a"`}
	message := "WIP [skip ci]: saved by the factory at 2026-11-21T03:00:00Z, Run r1, Execution e1, reason checkpoint. Not reviewed." +
		leftOutText(skipped)
	subject, body, _ := strings.Cut(message, "\n")
	if !wipSubject.MatchString(subject) {
		t.Errorf("the first line %q is not a WIP subject", subject)
	}
	if got := leftOut(body); !slices.Equal(got, skipped) {
		t.Errorf("the names from the message: %q, want %q", got, skipped)
	}
	if leftOutText(nil) != "" {
		t.Error("a save that left out no file has a list in its message")
	}
}

func TestTheMessageNamesAtMost200LeftOutFiles(t *testing.T) {
	var skipped []string
	for i := range maxSkipped + 5 {
		skipped = append(skipped, fmt.Sprintf("f%d.bin", i))
	}
	got := leftOut(leftOutText(skipped))
	if len(got) != maxSkipped+1 || got[maxSkipped] != "(and 5 more)" {
		t.Errorf("%d names, the last %q", len(got), got[len(got)-1])
	}
}

// The restore uses only a save of the Run that restores and of an Execution
// that the Dispatcher names: a WIP commit of another Run, or of an
// Execution that is not in the list, is not the Run's last work.
func TestTheRestoreFindsOnlyASaveOfThisRunAndOfTheNamedExecutions(t *testing.T) {
	e, _ := savingExecution(t, map[string]string{"a.txt": "one\n"}, nil)
	writeFiles(t, e.checkout, map[string]string{"work.txt": "work\n"})
	if _, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted); err != nil {
		t.Fatalf("the save: %v", err)
	}
	ref := "refs/agent-wip/" + saveRunID
	find := func(runID string, executions ...string) *foundSave {
		t.Helper()
		e.task.RunID = runID
		save, err := e.findSave(context.Background(), e.environ, ref, executions)
		if err != nil {
			t.Fatalf("findSave(%s, %v): %v", runID, executions, err)
		}
		return save
	}

	if save := find(saveRunID, "ex1"); save == nil || save.reason != dispatcherapi.EndPreempted {
		t.Errorf("the save of this Run and of ex1 is not found: %+v", save)
	}
	if save := find("run2", "ex1"); save != nil {
		t.Errorf("the save of another Run is found: %+v", save)
	}
	if save := find(saveRunID, "ex2"); save != nil {
		t.Errorf("the save of an Execution that is not named is found: %+v", save)
	}
}
