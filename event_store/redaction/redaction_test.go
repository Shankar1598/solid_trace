package redaction_test

import (
	"errors"
	"strings"
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

// The character before an app password is read as JSON text decodes it.
func TestAppPasswordStartsAWordAfterAJSONEscape(t *testing.T) {
	app := "ATBB" + strings.Repeat("aB3", 10)
	set, err := redaction.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for before, replaced := range map[string]bool{
		`\n`: true, `\r`: true, `\t`: true, `\b`: true, `\f`: true, `\\\n`: true,
		`\u002b`: true, `\u002B`: true, `\u00e9`: true,
		`\\n`: false, `\u0041`: false, `\u005f`: false, `\\u002b`: false, `n`: false,
	} {
		line := []byte(`{"text":"` + before + app + `"}`)
		set.Unit(line, true)
		want := `{"text":"` + before + app + `"}`
		if replaced {
			want = `{"text":"` + before + strings.Repeat("*", len(app)) + `"}`
		}
		if string(line) != want {
			t.Errorf("After %s: expected %s, got %s", before, want, line)
		}
	}
}
