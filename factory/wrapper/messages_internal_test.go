package wrapper

import (
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The Dispatcher can send a command again (after a lost answer). A message
// that the wrapper already has is not written again: the agent would get it
// two times.
func TestAMessageThatTheWrapperAlreadyHasIsNotWrittenAgain(t *testing.T) {
	f := startFollow(t)
	e := f.e
	first := dispatcherapi.Message{ID: "m-1", Text: "Use the new API"}
	e.takeMessage(first, 1)
	e.takeMessage(first, 1)
	e.takeMessage(dispatcherapi.Message{ID: "m-2", Text: "And keep the old one"}, 3)

	for _, id := range []string{"m-1", "m-2"} {
		if line := f.next(t, "the message "+id); !strings.Contains(line, `"uuid":"`+id+`"`) {
			t.Fatalf("stdin got %s, want the message %s", line, id)
		}
	}
	if n := len(e.inbox.all); n != 2 {
		t.Errorf("the wrapper has %d messages, want 2", n)
	}
}
