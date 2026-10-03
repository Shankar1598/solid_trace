package parser

// Node is a node of a Query's syntax tree.
type Node interface {
	Span() Span
}

func nodeSpan(n Node) Span {
	if n == nil {
		return nullSpan()
	}
	return n.Span()
}

// Query is a whole Query: an optional source (the leading logs), then its
// stages, each after a pipe.
type Query struct {
	// Source is the leading logs, or nil when the Query leaves it out.
	Source *Ident
	Stages []Stage
}

// Stage is one stage of a Query, such as where or search.
type Stage interface {
	Node
	stage()
}

// WhereStage is where <predicate>.
type WhereStage struct {
	Pipe      Span // invalid for a Query's first stage
	Keyword   Span
	Predicate Expr
}

func (s *WhereStage) stage() {}

func (s *WhereStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword, nodeSpan(s.Predicate))
}

// SearchStage is search "<term>".
type SearchStage struct {
	Pipe    Span // invalid for a Query's first stage
	Keyword Span
	Term    *BasicLit
}

func (s *SearchStage) stage() {}

func (s *SearchStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword, nodeSpan(s.Term))
}

// Expr is an expression.
type Expr interface {
	Node
	expr()
}

// Ident is a name: a Field, or a function's name in a CallExpr. A dotted
// name, such as http.status, is one Ident: Attribute names are flat
// (ADR 0002).
type Ident struct {
	Name     string
	NameSpan Span
	// Quoted is set for a name written as ['…'].
	Quoted bool
}

func (id *Ident) expr() {}

func (id *Ident) Span() Span {
	return id.NameSpan
}

// LiteralKind is the type of a BasicLit.
type LiteralKind int

const (
	StringLiteral LiteralKind = iota + 1
	NumberLiteral
	BoolLiteral
	// TimespanLiteral is a length of time, such as 1h. Its Value is as
	// written; ParseTimespan reads it.
	TimespanLiteral
	// DateTimeLiteral is datetime(…). Its Value is the time as written.
	DateTimeLiteral
)

// BasicLit is a literal: a string, a number, true or false, a timespan or a
// datetime.
type BasicLit struct {
	ValueSpan Span
	Kind      LiteralKind
	// Value is a string's value, a number as written, or "true" or "false".
	Value string
}

func (l *BasicLit) expr() {}

func (l *BasicLit) Span() Span {
	return l.ValueSpan
}

// BinaryExpr is X <op> Y, such as status == 200, a and b, or path contains
// "api". For in and !in, Y is a *ListExpr.
type BinaryExpr struct {
	X      Expr
	OpSpan Span
	Op     TokenKind
	Y      Expr
}

func (e *BinaryExpr) expr() {}

func (e *BinaryExpr) Span() Span {
	return unionSpans(nodeSpan(e.X), e.OpSpan, nodeSpan(e.Y))
}

// UnaryExpr is -X or +X.
type UnaryExpr struct {
	OpSpan Span
	Op     TokenKind
	X      Expr
}

func (e *UnaryExpr) expr() {}

func (e *UnaryExpr) Span() Span {
	return unionSpans(e.OpSpan, nodeSpan(e.X))
}

// ParenExpr is (X).
type ParenExpr struct {
	Lparen Span
	X      Expr
	Rparen Span
}

func (e *ParenExpr) expr() {}

func (e *ParenExpr) Span() Span {
	return unionSpans(e.Lparen, nodeSpan(e.X), e.Rparen)
}

// CallExpr is a function call, such as not(x).
type CallExpr struct {
	Func   *Ident
	Lparen Span
	Args   []Expr
	Rparen Span
}

func (e *CallExpr) expr() {}

func (e *CallExpr) Span() Span {
	return unionSpans(e.Func.Span(), e.Lparen, e.Rparen)
}

// ListExpr is a parenthesised list of values, (a, b, …), the right side of
// in and !in.
type ListExpr struct {
	Lparen Span
	Elems  []Expr
	Rparen Span
}

func (e *ListExpr) expr() {}

func (e *ListExpr) Span() Span {
	return unionSpans(e.Lparen, e.Rparen)
}
