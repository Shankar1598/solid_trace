// Package redaction removes credentials from the units of Agent sessions'
// files before EventStore stores them (ADR 0020). It replaces each byte that
// a Redaction pattern matches with '*'. So a stored unit has the length of
// the original, each offset is the client's offset, each line stays valid
// JSON, and a resume of the Agent session still works.
//
// The patterns are Go regular expressions (RE2 syntax): the default list,
// which is always on, and the extra patterns that an Instance admin adds on
// Settings → Instance. No pattern can match a quote or a backslash (Check),
// so that a replacement cannot end a JSON string or start an escape.
package redaction

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// Pattern is a default Redaction pattern and the credential it finds.
type Pattern struct {
	Name, Expr string
}

// Defaults are the credential patterns that are always on.
//
// The Atlassian formats start with a fixed prefix of 12 characters, which
// random text (a base64 image, say) almost never holds, because a
// replacement there would break the image for a resume. The app password's
// prefix is short, so it must also start and end at a word boundary.
var Defaults = []Pattern{
	// sk-ant-api03-, sk-ant-admin01-, sk-ant-oat01- (CLAUDE_CODE_OAUTH_TOKEN),
	// sk-ant-ort01- and any later kind of Anthropic key.
	{"Anthropic API keys and Claude OAuth tokens", `sk-ant-[A-Za-z0-9_-]{20,}`},
	// Repository, project and workspace access tokens.
	{"Bitbucket access tokens", `ATCTT3xFfGN0[A-Za-z0-9_=-]{20,}`},
	{"Bitbucket app passwords", `\bATBB[A-Za-z0-9]{28,}\b`},
	// Atlassian API tokens, which the Bitbucket API also takes.
	{"Atlassian API tokens", `ATATT3xFfGF0[A-Za-z0-9_=-]{20,}`},
}

// The limits on the extra patterns.
const (
	MaxPatterns      = 100
	MaxPatternLength = 1024
)

// Check returns what is wrong with an extra pattern, or nil: it must be a
// valid Go regular expression of at most MaxPatternLength bytes, and it
// must not be able to match a quote or a backslash.
func Check(expr string) error {
	switch {
	case strings.TrimSpace(expr) == "":
		return errors.New("The pattern is empty.")
	case len(expr) > MaxPatternLength:
		return fmt.Errorf("The pattern is longer than %d bytes.", MaxPatternLength)
	}
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return fmt.Errorf("This is not a valid pattern (Go regular expression syntax): %v.", err)
	}
	if _, err := regexp.Compile(expr); err != nil {
		return fmt.Errorf("This is not a valid pattern (Go regular expression syntax): %v.", err)
	}
	switch {
	case canMatch(re, '"'):
		return errors.New(`The pattern can match a quote ("), so a replacement could break a JSON line. ` +
			`Use a character class that leaves it out, as [A-Za-z0-9_-] does.`)
	case canMatch(re, '\\'):
		return errors.New(`The pattern can match a backslash (\), so a replacement could break a JSON line. ` +
			`Use a character class that leaves it out, as [A-Za-z0-9_-] does.`)
	}
	return nil
}

// canMatch reports whether some part of re matches the character r. It
// looks at each part, reachable or not, so it can refuse a pattern that
// never matches r, but never passes one that does.
func canMatch(re *syntax.Regexp, r rune) bool {
	switch re.Op {
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true
	case syntax.OpLiteral:
		for _, lit := range re.Rune {
			if lit == r || (re.Flags&syntax.FoldCase != 0 && foldsTo(lit, r)) {
				return true
			}
		}
	case syntax.OpCharClass:
		for i := 0; i+1 < len(re.Rune); i += 2 {
			if re.Rune[i] <= r && r <= re.Rune[i+1] {
				return true
			}
		}
	}
	for _, sub := range re.Sub {
		if canMatch(sub, r) {
			return true
		}
	}
	return false
}

func foldsTo(lit, r rune) bool {
	for f := lit; ; {
		f = unicode.SimpleFold(f)
		if f == lit {
			return false
		}
		if f == r {
			return true
		}
	}
}

// Set is the default patterns and the extra ones, compiled.
type Set struct {
	patterns []*regexp.Regexp
}

// New compiles the default patterns and extra. It refuses an extra pattern
// that Check refuses, naming it.
func New(extra []string) (*Set, error) {
	if len(extra) > MaxPatterns {
		return nil, fmt.Errorf("%d extra patterns, more than %d", len(extra), MaxPatterns)
	}
	set := &Set{}
	for _, p := range Defaults {
		set.patterns = append(set.patterns, regexp.MustCompile(p.Expr))
	}
	for i, expr := range extra {
		if err := Check(expr); err != nil {
			return nil, fmt.Errorf("extra pattern %d (%q): %w", i+1, expr, err)
		}
		set.patterns = append(set.patterns, regexp.MustCompile(expr))
	}
	return set, nil
}

// Unit replaces in place each byte of unit that a pattern matches with '*'.
// A unit of a .jsonl file (jsonl) is whole lines, and each line is
// redacted alone; another file is redacted whole.
//
// In a piece (a line, or the whole file) that is valid JSON, only the text
// of its strings changes: a match on an escape (the n of \n, say) or
// outside a string (in a number) keeps those bytes, so the piece stays
// valid JSON. In a piece that is not JSON, each matched byte changes.
func (s *Set) Unit(unit []byte, jsonl bool) {
	if !jsonl {
		s.piece(unit)
		return
	}
	for len(unit) > 0 {
		end := slices.Index(unit, '\n')
		if end < 0 {
			end = len(unit)
		}
		s.piece(unit[:end])
		unit = unit[min(end+1, len(unit)):]
	}
}

func (s *Set) piece(piece []byte) {
	var spans [][]int
	for _, re := range s.patterns {
		for _, m := range re.FindAllIndex(piece, -1) {
			if m[1] > m[0] {
				spans = append(spans, m)
			}
		}
	}
	if len(spans) == 0 {
		return
	}
	marked := make([]bool, len(piece))
	for _, m := range spans {
		for i := m[0]; i < m[1]; i++ {
			marked[i] = true
		}
	}
	if !json.Valid(piece) {
		for i, m := range marked {
			if m {
				piece[i] = '*'
			}
		}
		return
	}
	// Valid JSON: walk it, and change only the bytes of string text, not
	// a quote, an escape or a byte outside a string.
	inString := false
	for i := 0; i < len(piece); i++ {
		switch b := piece[i]; {
		case !inString:
			inString = b == '"'
		case b == '"':
			inString = false
		case b == '\\':
			if piece[i+1] == 'u' {
				i += 5
			} else {
				i++
			}
		case marked[i]:
			piece[i] = '*'
		}
	}
}

// Source gives the extra patterns as Console stores them
// (storage.ConsoleSettings.RedactionPatterns).
type Source func() ([]string, error)

// Follower gives the current Set: it reads the extra patterns on each call,
// so a pattern saved on Settings → Instance applies from the next upload,
// and compiles them again only when they changed.
type Follower struct {
	source Source

	mu   sync.Mutex
	key  string
	set  *Set
	read bool
}

// NewFollower returns a Follower of source.
func NewFollower(source Source) *Follower {
	return &Follower{source: source}
}

// Current returns the Set of the default patterns and the extra patterns as
// they are now. It fails when the extra patterns cannot be read or
// compiled: a unit must then not be stored, as a secret could stay in it.
func (f *Follower) Current() (*Set, error) {
	extra, err := f.source()
	if err != nil {
		return nil, err
	}
	key := strings.Join(extra, "\x00")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.read && key == f.key {
		return f.set, nil
	}
	set, err := New(extra)
	if err != nil {
		return nil, err
	}
	f.key, f.set, f.read = key, set, true
	return set, nil
}
