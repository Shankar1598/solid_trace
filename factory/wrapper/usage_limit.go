package wrapper

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// usageLimit reads the harness's output stream for the usage limit of
// the Execution's account. Claude Code shows it in three ways: a
// rate_limit_event with the status "rejected" and the reset time
// (resetsAt, Unix seconds), an assistant message with the error
// "rate_limit", and an error result whose text names the usage limit
// (older versions end the text with "|<reset time in Unix seconds>").
// The result ends the turn: at it, the Execution ends with the reason
// EndUsageLimit.
type usageLimit struct {
	// seen is true after a rejected rate_limit_event or an assistant
	// rate_limit error.
	seen bool
	// resetsAt is the reset time that the stream gave (nil: none yet).
	resetsAt *time.Time
}

// resetSuffix is the reset time at the end of an older limit text, such
// as "Claude AI usage limit reached|1763150400".
var resetSuffix = regexp.MustCompile(`\|(\d{9,11})\s*$`)

// limitWords are the words of a result text that reports the usage
// limit.
var limitWords = []string{"usage limit", "hit your limit"}

// event reads one event of the stream that is not a result.
func (u *usageLimit) event(ev map[string]any) {
	switch {
	case ev["type"] == "rate_limit_event":
		info, _ := ev["rate_limit_info"].(map[string]any)
		if info == nil || info["status"] != "rejected" {
			return
		}
		u.seen = true
		if at, ok := info["resetsAt"].(float64); ok && at > 0 {
			t := time.Unix(int64(at), 0).UTC()
			u.resetsAt = &t
		}
	case ev["type"] == "assistant" && ev["error"] == "rate_limit":
		u.seen = true
		message, _ := ev["message"].(map[string]any)
		content, _ := message["content"].([]any)
		for _, c := range content {
			if block, _ := c.(map[string]any); block["type"] == "text" {
				text, _ := block["text"].(string)
				u.text(text)
			}
		}
	}
}

// text keeps the reset time at the end of a limit text, if the stream
// gave none before.
func (u *usageLimit) text(text string) {
	if u.resetsAt != nil {
		return
	}
	m := resetSuffix.FindStringSubmatch(text)
	if m == nil {
		return
	}
	if at, err := strconv.ParseInt(m[1], 10, 64); err == nil {
		t := time.Unix(at, 0).UTC()
		u.resetsAt = &t
	}
}

// result is true when the result ends the turn at the usage limit.
func (u *usageLimit) result(ev map[string]any) bool {
	text, _ := ev["result"].(string)
	if !u.seen && ev["is_error"] == true {
		lower := strings.ToLower(text)
		for _, w := range limitWords {
			u.seen = u.seen || strings.Contains(lower, w)
		}
	}
	if u.seen {
		u.text(text)
	}
	return u.seen
}
