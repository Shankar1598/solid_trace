package parser

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// TokenKind is the kind of a Token.
type TokenKind int

const (
	// TokenIdentifier is a name: a stage, a function, a Field or a keyword
	// such as true. Its Value is the name. A dotted name is several of these
	// joined by TokenDot.
	TokenIdentifier TokenKind = 1 + iota
	// TokenQuotedIdentifier is a name in brackets, ['…'] or ["…"], for names
	// that aren't plain identifiers. Its Value is the name, unquoted.
	TokenQuotedIdentifier
	// TokenNumber is a number such as 200, 1.5 or 1e3. Its Value is as
	// written.
	TokenNumber
	// TokenString is a string in single or double quotes. Its Value is the
	// string, its escapes resolved.
	TokenString
	// TokenTimespan is a number with a unit of time, such as 1h or 90s. Its
	// Value is as written.
	TokenTimespan
	// TokenDateTime is datetime(…), a point in time. Its Value is what the
	// parentheses hold, unquoted.
	TokenDateTime
	// TokenParam is a Dashboard filter, $ and its name, such as $service.
	// Its Value is the name, without the $.
	TokenParam

	TokenAnd // the keyword and
	TokenOr  // the keyword or

	TokenPipe     // |
	TokenDot      // .
	TokenComma    // ,
	TokenLParen   // (
	TokenRParen   // )
	TokenLBracket // [
	TokenRBracket // ]
	TokenPlus     // +
	TokenMinus    // -
	TokenStar     // *
	TokenSlash    // /
	TokenMod      // %
	TokenAssign   // =
	TokenEq       // ==
	TokenNE       // !=
	TokenLT       // <
	TokenLE       // <=
	TokenGT       // >
	TokenGE       // >=
	TokenEqCI     // =~
	TokenNECI     // !~
	TokenBang     // !, before a word operator such as !in

	// Word operators. The scanner gives them as TokenIdentifier (and
	// TokenBang); the parser reads them into a BinaryExpr's Op.
	TokenContains        // contains
	TokenNotContains     // !contains
	TokenContainsCS      // contains_cs
	TokenNotContainsCS   // !contains_cs
	TokenStartsWith      // startswith
	TokenNotStartsWith   // !startswith
	TokenStartsWithCS    // startswith_cs
	TokenNotStartsWithCS // !startswith_cs
	TokenMatchesRegex    // matches regex
	TokenIn              // in
	TokenNotIn           // !in

	// TokenError is text that isn't a token. Its Value says why.
	TokenError TokenKind = -1
)

// Token is one token of a Query's text.
type Token struct {
	Kind  TokenKind
	Span  Span
	Value string
}

var symbols = []struct {
	text string
	kind TokenKind
}{
	// Longest first.
	{"==", TokenEq}, {"!=", TokenNE}, {"=~", TokenEqCI}, {"!~", TokenNECI}, {"<=", TokenLE}, {">=", TokenGE},
	{"!", TokenBang},
	{"|", TokenPipe}, {".", TokenDot}, {",", TokenComma}, {"(", TokenLParen}, {")", TokenRParen},
	{"]", TokenRBracket}, {"+", TokenPlus}, {"-", TokenMinus}, {"*", TokenStar},
	{"/", TokenSlash}, {"%", TokenMod}, {"=", TokenAssign}, {"<", TokenLT}, {">", TokenGT},
}

// Scan splits a Query's text into tokens. Text that isn't a token becomes a
// TokenError, and scanning goes on after it.
func Scan(source string) []Token {
	s := &scanner{source: source}
	var tokens []Token
	for {
		s.skipSpaceAndComments()
		if s.pos >= len(source) {
			return tokens
		}
		tokens = append(tokens, s.token())
	}
}

type scanner struct {
	source string
	pos    int
}

func (s *scanner) peek() rune {
	r, _ := utf8.DecodeRuneInString(s.source[s.pos:])
	return r
}

func (s *scanner) skipSpaceAndComments() {
	for s.pos < len(s.source) {
		rest := s.source[s.pos:]
		r, size := utf8.DecodeRuneInString(rest)
		switch {
		case unicode.IsSpace(r):
			s.pos += size
		case strings.HasPrefix(rest, "//"):
			// A comment runs to the end of its line.
			if end := strings.IndexByte(rest, '\n'); end >= 0 {
				s.pos += end + 1
			} else {
				s.pos = len(s.source)
			}
		default:
			return
		}
	}
}

func (s *scanner) token() Token {
	start := s.pos
	rest := s.source[start:]
	r := s.peek()
	switch {
	case r == '$':
		return s.param()
	case isIdentStart(r):
		return s.identifier()
	case isDigit(r):
		return s.number()
	case r == '"' || r == '\'':
		return s.string()
	case strings.HasPrefix(rest, "['") || strings.HasPrefix(rest, `["`):
		return s.quotedIdentifier()
	case r == '[':
		s.pos++
		return Token{Kind: TokenLBracket, Span: newSpan(start, s.pos)}
	}
	for _, sym := range symbols {
		if strings.HasPrefix(rest, sym.text) {
			s.pos += len(sym.text)
			return Token{Kind: sym.kind, Span: newSpan(start, s.pos)}
		}
	}
	_, size := utf8.DecodeRuneInString(rest)
	s.pos += size
	return errorToken(newSpan(start, s.pos), "unexpected %q", rest[:size])
}

func errorToken(span Span, format string, args ...any) Token {
	return Token{Kind: TokenError, Span: span, Value: fmt.Sprintf(format, args...)}
}

func (s *scanner) identifier() Token {
	start := s.pos
	for s.pos < len(s.source) && isIdentPart(s.peek()) {
		s.pos += utf8.RuneLen(s.peek())
	}
	value := s.source[start:s.pos]
	if value == "datetime" {
		if tok, ok := s.dateTime(start); ok {
			return tok
		}
	}
	kind := TokenIdentifier
	switch value {
	case "and":
		kind = TokenAnd
	case "or":
		kind = TokenOr
	}
	return Token{Kind: kind, Span: newSpan(start, s.pos), Value: value}
}

// param scans a Dashboard filter, $name. The name is read as an identifier
// is.
func (s *scanner) param() Token {
	start := s.pos
	s.pos++
	if s.pos >= len(s.source) || !isIdentStart(s.peek()) {
		return errorToken(newSpan(start, s.pos), "expected a filter's name after $, such as $service")
	}
	for s.pos < len(s.source) && isIdentPart(s.peek()) {
		s.pos += utf8.RuneLen(s.peek())
	}
	return Token{Kind: TokenParam, Span: newSpan(start, s.pos), Value: s.source[start+1 : s.pos]}
}

// dateTime scans the parentheses of datetime(…), whose text is a time
// unquoted, as in datetime(2026-10-02T10:00:00Z), or quoted. It reports false,
// moving nothing, when no parenthesis follows.
func (s *scanner) dateTime(start int) (Token, bool) {
	open := s.pos
	for open < len(s.source) && (s.source[open] == ' ' || s.source[open] == '\t') {
		open++
	}
	if open >= len(s.source) || s.source[open] != '(' {
		return Token{}, false
	}
	end := strings.IndexByte(s.source[open:], ')')
	if end < 0 {
		s.pos = len(s.source)
		return errorToken(newSpan(start, s.pos), "expected ) after datetime(…"), true
	}
	s.pos = open + end + 1
	text := strings.TrimSpace(s.source[open+1 : open+end])
	if len(text) >= 2 && (text[0] == '"' || text[0] == '\'') && text[len(text)-1] == text[0] {
		text = text[1 : len(text)-1]
	}
	return Token{Kind: TokenDateTime, Span: newSpan(start, s.pos), Value: text}, true
}

// timespanUnits are the units a timespan can be written in, as KQL spells
// them.
var timespanUnits = map[string]time.Duration{
	"d": 24 * time.Hour, "day": 24 * time.Hour, "days": 24 * time.Hour,
	"h": time.Hour, "hr": time.Hour, "hrs": time.Hour, "hour": time.Hour, "hours": time.Hour,
	"m": time.Minute, "min": time.Minute, "minute": time.Minute, "minutes": time.Minute,
	"s": time.Second, "sec": time.Second, "second": time.Second, "seconds": time.Second,
	"ms": time.Millisecond, "milli": time.Millisecond, "millis": time.Millisecond,
	"millisecond": time.Millisecond, "milliseconds": time.Millisecond,
	"microsecond": time.Microsecond, "microseconds": time.Microsecond,
	"tick": 100 * time.Nanosecond, "ticks": 100 * time.Nanosecond,
}

// ParseTimespan reads a timespan as TokenTimespan holds it, such as 1.5h.
func ParseTimespan(text string) (time.Duration, error) {
	i := strings.IndexFunc(text, func(r rune) bool { return !isDigit(r) && r != '.' })
	if i <= 0 {
		return 0, fmt.Errorf("%q isn't a timespan", text)
	}
	unit, ok := timespanUnits[text[i:]]
	if !ok {
		return 0, fmt.Errorf("%q isn't a timespan; use a unit such as d, h, m, s or ms", text)
	}
	n, err := strconv.ParseFloat(text[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("%q isn't a timespan", text)
	}
	return time.Duration(n * float64(unit)), nil
}

// number scans, an optional fraction and an optional exponent.
func (s *scanner) number() Token {
	start := s.pos
	s.digits()
	if s.pos+1 < len(s.source) && s.source[s.pos] == '.' && isDigit(rune(s.source[s.pos+1])) {
		s.pos++
		s.digits()
	}
	if s.pos < len(s.source) && (s.source[s.pos] == 'e' || s.source[s.pos] == 'E') {
		save := s.pos
		s.pos++
		if s.pos < len(s.source) && (s.source[s.pos] == '+' || s.source[s.pos] == '-') {
			s.pos++
		}
		if s.pos < len(s.source) && isDigit(rune(s.source[s.pos])) {
			s.digits()
		} else {
			s.pos = save
		}
	}
	if s.pos < len(s.source) && isIdentPart(s.peek()) {
		unitStart := s.pos
		for s.pos < len(s.source) && isIdentPart(s.peek()) {
			s.pos += utf8.RuneLen(s.peek())
		}
		text := s.source[start:s.pos]
		if _, ok := timespanUnits[s.source[unitStart:s.pos]]; ok && !strings.ContainsAny(text[:unitStart-start], "eE") {
			return Token{Kind: TokenTimespan, Span: newSpan(start, s.pos), Value: text}
		}
		return errorToken(newSpan(start, s.pos), "%q isn't a number", text)
	}
	return Token{Kind: TokenNumber, Span: newSpan(start, s.pos), Value: s.source[start:s.pos]}
}

func (s *scanner) digits() {
	for s.pos < len(s.source) && isDigit(rune(s.source[s.pos])) {
		s.pos++
	}
}

// string scans a string in single or double quotes, with backslash escapes.
func (s *scanner) string() Token {
	start := s.pos
	value, end, msg := unquote(s.source, start)
	s.pos = end
	if msg != "" {
		return errorToken(newSpan(start, end), "%s", msg)
	}
	return Token{Kind: TokenString, Span: newSpan(start, end), Value: value}
}

// quotedIdentifier scans ['name'] or ["name"].
func (s *scanner) quotedIdentifier() Token {
	start := s.pos
	value, end, msg := unquote(s.source, start+1)
	s.pos = end
	if msg != "" {
		return errorToken(newSpan(start, end), "%s", msg)
	}
	if end >= len(s.source) || s.source[end] != ']' {
		return errorToken(newSpan(start, end), "expected ] after the quoted name")
	}
	s.pos++
	return Token{Kind: TokenQuotedIdentifier, Span: newSpan(start, s.pos), Value: value}
}

// unquote reads the quoted string that starts at source[start], and returns
// its value and the offset just past its closing quote, or a message saying
// what is wrong with it.
func unquote(source string, start int) (string, int, string) {
	quote := source[start]
	var b strings.Builder
	pos := start + 1
	for pos < len(source) {
		c := source[pos]
		switch {
		case c == quote:
			return b.String(), pos + 1, ""
		case c == '\n':
			return "", pos, "a string can't span lines"
		case c == '\\':
			if pos+1 >= len(source) {
				return "", len(source), "unterminated string"
			}
			switch e := source[pos+1]; e {
			case '\\', '\'', '"':
				b.WriteByte(e)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				return "", pos + 2, fmt.Sprintf("unknown escape \\%c", e)
			}
			pos += 2
		default:
			b.WriteByte(c)
			pos++
		}
	}
	return "", len(source), "unterminated string"
}

// isIdentStart reports whether r can start a name. $ can't, as it starts a
// Dashboard filter; a Field whose name starts with $ is written ['$type'].
func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || r == '$' || unicode.IsDigit(r)
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
