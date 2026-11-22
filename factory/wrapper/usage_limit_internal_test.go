package wrapper

import (
	"testing"
	"time"
)

func rateLimitEvent(status string) map[string]any {
	return map[string]any{"type": "rate_limit_event", "rate_limit_info": map[string]any{"status": status, "resetsAt": float64(1763150400)}}
}

func errorResult(text string) map[string]any {
	return map[string]any{"type": "result", "is_error": true, "result": text}
}

// A rate_limit_event with a status other than "rejected" (a warning that
// the account nears its limit) is not a usage limit: a turn that fails
// for another reason after it does not move the Run.
func TestARateLimitEventThatIsNotRejectedIsNoUsageLimit(t *testing.T) {
	var u usageLimit
	u.event(rateLimitEvent("allowed_warning"))
	if u.result(errorResult("the tool failed")) {
		t.Error("an error result after a warning is a usage limit")
	}
	if u.resetsAt != nil {
		t.Errorf("the warning gave a reset time %v", u.resetsAt)
	}
}

// A rejected event is a usage limit at the error result, with the reset
// time of the event.
func TestARejectedRateLimitEventBeforeAnErrorResultIsAUsageLimitWithItsResetTime(t *testing.T) {
	var u usageLimit
	u.event(rateLimitEvent("rejected"))
	if !u.result(errorResult("the turn stopped")) {
		t.Fatal("an error result after a rejected event is not a usage limit")
	}
	if want := time.Unix(1763150400, 0).UTC(); u.resetsAt == nil || !u.resetsAt.Equal(want) {
		t.Errorf("reset time %v, want %v", u.resetsAt, want)
	}
}

// A result that is not an error shows that the turn worked, so the signals
// before it are gone: a later error result with no limit sign of its own
// is not a usage limit.
func TestAResultThatIsNotAnErrorClearsTheLimitSignalsOfTheTurn(t *testing.T) {
	var u usageLimit
	u.event(rateLimitEvent("rejected"))
	if u.result(map[string]any{"type": "result", "is_error": false, "result": "done"}) {
		t.Fatal("a result that is not an error is a usage limit")
	}
	if u.result(errorResult("the tool failed")) {
		t.Error("an error result of the next turn is a usage limit from the signals of the turn before")
	}
}
