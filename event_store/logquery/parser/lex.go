package parser

import (
	"fmt"
	"strings"
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
	{"==", TokenEq}, {"!=", TokenNE}, {"<=", TokenLE}, {">=", TokenGE},
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
	kind := TokenIdentifier
	switch value {
	case "and":
		kind = TokenAnd
	case "or":
		kind = TokenOr
	}
	return Token{Kind: kind, Span: newSpan(start, s.pos), Value: value}
}

// number scans digits, an optional fraction and an optional exponent.
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
		for s.pos < len(s.source) && isIdentPart(s.peek()) {
			s.pos += utf8.RuneLen(s.peek())
		}
		return errorToken(newSpan(start, s.pos), "%q isn't a number", s.source[start:s.pos])
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

func isIdentStart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || unicode.IsDigit(r)
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
