package dispatcher

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The trigger word of the comments that continue a Run. Ticket 76 adds
// the same two functions in ticket_comments.go: when both are in one
// branch, keep one copy.

// triggers is true when text starts with the word: the word alone, or
// the word and then a space.
func triggers(text, word string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), word)
	if !ok {
		return false
	}
	next, _ := utf8.DecodeRuneInString(rest)
	return rest == "" || unicode.IsSpace(next)
}

// messageText is a comment's text as a message: at most
// dispatcherapi.MaxMessage bytes, cut at a whole rune.
func messageText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= dispatcherapi.MaxMessage {
		return text
	}
	cut := dispatcherapi.MaxMessage
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
