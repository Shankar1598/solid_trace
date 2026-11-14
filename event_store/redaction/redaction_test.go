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
		// Ł and ş are letters, but not word characters of \b: their low
		// bytes (A, _) are.
		`\u0141`: true, `\u015f`: true,
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

// Each default pattern replaces a credential of its shortest length, and
// leaves the same text with one character less. The prefixes are joined at
// run time, so that the source holds no text in a credential's format.
func TestEachDefaultPatternHasItsMinimumLength(t *testing.T) {
	set, err := redaction.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, prefix string
		min          int
	}{
		{"Anthropic", "sk-" + "ant-", 20},
		{"Bitbucket access token", "ATCTT" + "3xFfGN0", 20},
		{"Bitbucket app password", "AT" + "BB", 28},
		{"Atlassian API token", "ATATT" + "3xFfGF0", 20},
	} {
		for n, replaced := range map[int]bool{c.min - 1: false, c.min: true} {
			secret := c.prefix + strings.Repeat("a", n)
			text := []byte("key " + secret + " end")
			set.Unit(text, false)
			want := "key " + secret + " end"
			if replaced {
				want = "key " + strings.Repeat("*", len(secret)) + " end"
			}
			if string(text) != want {
				t.Errorf("%s with %d characters: expected %q, got %q", c.name, n, want, text)
			}
		}
	}
}
