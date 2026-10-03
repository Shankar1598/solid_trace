// Package parser turns a Query's text into a syntax tree. It knows the
// language's grammar only: which stages, functions and Fields exist, and what
// types they take, is the compiler's business (package logquery).
//
// Its structure follows github.com/runreveal/pql's parser: a scanner, a tree
// of nodes that each know their span, and a recursive-descent parser with
// precedence climbing for binary operators.
package parser

import (
	"fmt"
)

// Error is a mistake in a Query's text, at Span.
type Error struct {
	Span    Span
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

// Parse parses a Query's text. It stops at the first mistake.
func Parse(source string) (*Query, error) {
	p := &parser{source: source, tokens: Scan(source)}
	query, err := p.query()
	if err != nil {
		return nil, err
	}
	return query, nil
}

type parser struct {
	source string
	tokens []Token
	pos    int
}

// next returns the next token and moves past it, or reports that there are
// none left.
func (p *parser) next() (Token, bool) {
	tok, ok := p.peek()
	if ok {
		p.pos++
	}
	return tok, ok
}

func (p *parser) peek() (Token, bool) {
	if p.pos >= len(p.tokens) {
		return Token{Span: newSpan(len(p.source), len(p.source))}, false
	}
	return p.tokens[p.pos], true
}

func (p *parser) errorf(span Span, format string, args ...any) *Error {
	return &Error{Span: span, Message: fmt.Sprintf(format, args...)}
}

// describe names a token for an error message.
func (p *parser) describe(tok Token, ok bool) string {
	if !ok {
		return "the end of the Query"
	}
	return fmt.Sprintf("%q", p.source[tok.Span.Start:tok.Span.End])
}

// query parses [logs |] stage { | stage }.
func (p *parser) query() (*Query, error) {
	for _, tok := range p.tokens {
		if tok.Kind == TokenError {
			return nil, p.errorf(tok.Span, "%s", tok.Value)
		}
	}
	q := &Query{}
	if tok, ok := p.peek(); ok && tok.Kind == TokenIdentifier && tok.Value == "logs" {
		p.pos++
		q.Source = &Ident{Name: tok.Value, NameSpan: tok.Span}
		next, ok := p.next()
		if !ok {
			return q, nil
		}
		if next.Kind != TokenPipe {
			return nil, p.errorf(next.Span, "expected | after logs, got %s", p.describe(next, ok))
		}
		stage, err := p.stage(next.Span)
		if err != nil {
			return nil, err
		}
		q.Stages = append(q.Stages, stage)
	} else if ok {
		stage, err := p.stage(nullSpan())
		if err != nil {
			return nil, err
		}
		q.Stages = append(q.Stages, stage)
	}
	for {
		tok, ok := p.next()
		if !ok {
			return q, nil
		}
		if tok.Kind != TokenPipe {
			return nil, p.errorf(tok.Span, "expected | or the end of the Query, got %s", p.describe(tok, ok))
		}
		stage, err := p.stage(tok.Span)
		if err != nil {
			return nil, err
		}
		q.Stages = append(q.Stages, stage)
	}
}

// stage parses one stage, after its pipe if it has one.
func (p *parser) stage(pipe Span) (Stage, error) {
	keyword, ok := p.next()
	if !ok || keyword.Kind != TokenIdentifier {
		return nil, p.errorf(keyword.Span, "expected a stage, such as where, got %s", p.describe(keyword, ok))
	}
	switch keyword.Value {
	case "where":
		if _, ok := p.peek(); !ok {
			return nil, p.errorf(keyword.Span, "where needs a condition, such as where level == \"error\"")
		}
		predicate, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		return &WhereStage{Pipe: pipe, Keyword: keyword.Span, Predicate: predicate}, nil
	case "search":
		term, ok := p.next()
		if !ok || term.Kind != TokenString {
			span := term.Span
			if !ok {
				span = keyword.Span
			}
			return nil, p.errorf(span, "search needs a quoted string, such as search \"timeout\"")
		}
		return &SearchStage{
			Pipe:    pipe,
			Keyword: keyword.Span,
			Term:    &BasicLit{ValueSpan: term.Span, Kind: StringLiteral, Value: term.Value},
		}, nil
	default:
		return nil, p.errorf(keyword.Span, "unknown stage %q", keyword.Value)
	}
}

// precedence is how tightly a binary operator binds, or -1 for a token that
// isn't one.
func precedence(kind TokenKind) int {
	switch kind {
	case TokenStar, TokenSlash, TokenMod:
		return 4
	case TokenPlus, TokenMinus:
		return 3
	case TokenEq, TokenNE, TokenLT, TokenLE, TokenGT, TokenGE, TokenEqCI, TokenNECI,
		TokenContains, TokenNotContains, TokenContainsCS, TokenNotContainsCS,
		TokenStartsWith, TokenNotStartsWith, TokenStartsWithCS, TokenNotStartsWithCS,
		TokenMatchesRegex, TokenIn, TokenNotIn:
		return 2
	case TokenAnd:
		return 1
	case TokenOr:
		return 0
	default:
		return -1
	}
}

// wordOperators are the operators written as words, and their negations
// written with a leading !.
var wordOperators = map[string][2]TokenKind{
	"contains":      {TokenContains, TokenNotContains},
	"contains_cs":   {TokenContainsCS, TokenNotContainsCS},
	"startswith":    {TokenStartsWith, TokenNotStartsWith},
	"startswith_cs": {TokenStartsWithCS, TokenNotStartsWithCS},
	"in":            {TokenIn, TokenNotIn},
}

// binaryOperator reads the binary operator at the parser's position, if
// there is one, without moving: its kind, its span and how many tokens it
// takes.
func (p *parser) binaryOperator() (TokenKind, Span, int) {
	tok, ok := p.peek()
	if !ok {
		return 0, tok.Span, 0
	}
	switch tok.Kind {
	case TokenIdentifier:
		if kinds, ok := wordOperators[tok.Value]; ok {
			return kinds[0], tok.Span, 1
		}
		if tok.Value == "matches" && p.pos+1 < len(p.tokens) {
			if next := p.tokens[p.pos+1]; next.Kind == TokenIdentifier && next.Value == "regex" {
				return TokenMatchesRegex, unionSpans(tok.Span, next.Span), 2
			}
		}
		return 0, tok.Span, 0
	case TokenBang:
		if p.pos+1 < len(p.tokens) {
			next := p.tokens[p.pos+1]
			if kinds, ok := wordOperators[next.Value]; ok && next.Kind == TokenIdentifier && next.Span.Start == tok.Span.End {
				return kinds[1], unionSpans(tok.Span, next.Span), 2
			}
		}
		return 0, tok.Span, 0
	}
	return tok.Kind, tok.Span, 1
}

// expr parses an expression whose binary operators bind at least as tightly
// as minPrecedence.
func (p *parser) expr(minPrecedence int) (Expr, error) {
	x, err := p.unaryExpr()
	if err != nil {
		return nil, err
	}
	for {
		kind, span, width := p.binaryOperator()
		prec := precedence(kind)
		if width == 0 || prec < 0 || prec < minPrecedence {
			return x, nil
		}
		p.pos += width
		opText := fmt.Sprintf("%q", p.source[span.Start:span.End])
		if _, ok := p.peek(); !ok {
			return nil, p.errorf(span, "expected an expression after %s", opText)
		}
		var y Expr
		if kind == TokenIn || kind == TokenNotIn {
			y, err = p.list(opText)
		} else {
			y, err = p.expr(prec + 1)
		}
		if err != nil {
			return nil, err
		}
		x = &BinaryExpr{X: x, OpSpan: span, Op: kind, Y: y}
	}
}

// list parses (value, …), after in or !in.
func (p *parser) list(op string) (*ListExpr, error) {
	lparen, ok := p.next()
	if !ok || lparen.Kind != TokenLParen {
		return nil, p.errorf(lparen.Span, "expected a list in parentheses after %s, such as (\"a\", \"b\")", op)
	}
	list := &ListExpr{Lparen: lparen.Span}
	for {
		elem, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		list.Elems = append(list.Elems, elem)
		tok, ok := p.next()
		switch {
		case ok && tok.Kind == TokenComma:
		case ok && tok.Kind == TokenRParen:
			list.Rparen = tok.Span
			return list, nil
		default:
			return nil, p.errorf(tok.Span, "expected , or ), got %s", p.describe(tok, ok))
		}
	}
}

func (p *parser) unaryExpr() (Expr, error) {
	tok, ok := p.peek()
	if ok && (tok.Kind == TokenMinus || tok.Kind == TokenPlus) {
		p.pos++
		x, err := p.primaryExpr()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{OpSpan: tok.Span, Op: tok.Kind, X: x}, nil
	}
	return p.primaryExpr()
}

func (p *parser) primaryExpr() (Expr, error) {
	tok, ok := p.next()
	if !ok {
		return nil, p.errorf(tok.Span, "expected an expression, got the end of the Query")
	}
	switch tok.Kind {
	case TokenNumber:
		return &BasicLit{ValueSpan: tok.Span, Kind: NumberLiteral, Value: tok.Value}, nil
	case TokenString:
		return &BasicLit{ValueSpan: tok.Span, Kind: StringLiteral, Value: tok.Value}, nil
	case TokenTimespan:
		return &BasicLit{ValueSpan: tok.Span, Kind: TimespanLiteral, Value: tok.Value}, nil
	case TokenDateTime:
		return &BasicLit{ValueSpan: tok.Span, Kind: DateTimeLiteral, Value: tok.Value}, nil
	case TokenQuotedIdentifier:
		return &Ident{Name: tok.Value, NameSpan: tok.Span, Quoted: true}, nil
	case TokenIdentifier:
		return p.identExpr(tok)
	case TokenLParen:
		x, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		end, ok := p.next()
		if !ok || end.Kind != TokenRParen {
			return nil, p.errorf(end.Span, "expected ), got %s", p.describe(end, ok))
		}
		return &ParenExpr{Lparen: tok.Span, X: x, Rparen: end.Span}, nil
	default:
		return nil, p.errorf(tok.Span, "expected an expression, got %s", p.describe(tok, ok))
	}
}

// identExpr parses what starts with a name: true or false, a call, or a
// Field's name, dotted parts and all.
func (p *parser) identExpr(first Token) (Expr, error) {
	next, ok := p.peek()
	adjacent := ok && next.Span.Start == first.Span.End
	switch {
	case adjacent && next.Kind == TokenDot:
		return p.dottedName(first)
	case ok && next.Kind == TokenLParen:
		return p.call(first)
	case first.Value == "true" || first.Value == "false":
		return &BasicLit{ValueSpan: first.Span, Kind: BoolLiteral, Value: first.Value}, nil
	case first.Value == "not":
		return nil, p.errorf(first.Span, "not takes its condition in parentheses: not(…)")
	}
	return &Ident{Name: first.Value, NameSpan: first.Span}, nil
}

// dottedName parses a name such as http.request.method into one Ident. The
// parts must touch their dots.
func (p *parser) dottedName(first Token) (*Ident, error) {
	name, span := first.Value, first.Span
	for {
		dot, ok := p.peek()
		if !ok || dot.Kind != TokenDot || dot.Span.Start != span.End {
			return &Ident{Name: name, NameSpan: span}, nil
		}
		p.pos++
		part, ok := p.next()
		isName := part.Kind == TokenIdentifier || part.Kind == TokenAnd || part.Kind == TokenOr
		if !ok || !isName || part.Span.Start != dot.Span.End {
			return nil, p.errorf(unionSpans(span, dot.Span), "expected a name after the dot; quote names like this as ['%s.…']", name)
		}
		name += "." + p.source[part.Span.Start:part.Span.End]
		span = unionSpans(span, part.Span)
	}
}

// call parses name(arg, …).
func (p *parser) call(name Token) (*CallExpr, error) {
	lparen, _ := p.next()
	call := &CallExpr{Func: &Ident{Name: name.Value, NameSpan: name.Span}, Lparen: lparen.Span}
	if tok, ok := p.peek(); ok && tok.Kind == TokenRParen {
		p.pos++
		call.Rparen = tok.Span
		return call, nil
	}
	for {
		arg, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)
		tok, ok := p.next()
		switch {
		case ok && tok.Kind == TokenComma:
		case ok && tok.Kind == TokenRParen:
			call.Rparen = tok.Span
			return call, nil
		default:
			return nil, p.errorf(tok.Span, "expected , or ), got %s", p.describe(tok, ok))
		}
	}
}
