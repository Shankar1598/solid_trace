package redaction_test

import (
	"errors"
	"testing"

	"github.com/solidtrace/event_store/redaction"
)

// The default patterns follow the rule that Console applies to the extra
// ones, so no default replacement can break a JSON line.
func TestEachDefaultPatternPassesTheCheck(t *testing.T) {
	for _, p := range redaction.Defaults {
		if err := redaction.Check(p.Expr); err != nil {
			t.Errorf("%s (%s): %v", p.Name, p.Expr, err)
		}
	}
}

func TestFollowerFailsWhenTheExtraPatternsCannotBeRead(t *testing.T) {
	extra, readErr := []string{`acme_[a-z]{8}`}, error(nil)
	f := redaction.NewFollower(func() ([]string, error) { return extra, readErr })
	if _, err := f.Current(); err != nil {
		t.Fatalf("Expected a Set, got %v", err)
	}
	readErr = errors.New("database is locked")
	if _, err := f.Current(); err == nil {
		t.Errorf("Expected an error when the patterns cannot be read")
	}
	readErr, extra = nil, []string{`acme_"`}
	if _, err := f.Current(); err == nil {
		t.Errorf("Expected an error for a pattern that the check refuses")
	}
}
