package wrapper

import (
	"fmt"
	"slices"
	"strings"
	"testing"
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
