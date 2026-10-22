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

// Query is a whole Query: an optional source (the leading logs, or the
// Project's slug in brackets), then its stages, each after a pipe.
type Query struct {
	// Source is the leading source, or nil when the Query leaves it out.
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

// SearchStage is search "<term>", or search $name for a Dashboard filter.
type SearchStage struct {
	Pipe    Span // invalid for a Query's first stage
	Keyword Span
	// Term is a string *BasicLit or a *Param.
	Term Expr
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

// Param is a Dashboard filter, $name, which stands for the filter's values.
type Param struct {
	// Name is the filter's name, without the $.
	Name     string
	NameSpan Span
}

func (p *Param) expr() {}

func (p *Param) Span() Span {
	return p.NameSpan
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

// NamedExpr is a column of project, extend, summarize or distinct: an
// expression, named as in name = expr, or not.
type NamedExpr struct {
	Name *Ident // nil when unnamed
	X    Expr
}

func (e *NamedExpr) Span() Span {
	if e.Name == nil {
		return nodeSpan(e.X)
	}
	return unionSpans(e.Name.Span(), nodeSpan(e.X))
}

func namedSpan(exprs []*NamedExpr) Span {
	if len(exprs) == 0 {
		return nullSpan()
	}
	return unionSpans(exprs[0].Span(), exprs[len(exprs)-1].Span())
}

// ProjectStage is project col, name = expr, …: a table of these columns.
type ProjectStage struct {
	Pipe    Span
	Keyword Span
	Columns []*NamedExpr
}

func (s *ProjectStage) stage() {}

func (s *ProjectStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword, namedSpan(s.Columns))
}

// ExtendStage is extend name = expr, …: the rows with these columns added.
type ExtendStage struct {
	Pipe    Span
	Keyword Span
	Columns []*NamedExpr
}

func (s *ExtendStage) stage() {}

func (s *ExtendStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword, namedSpan(s.Columns))
}

// SummarizeStage is summarize agg(…), … by expr, ….
type SummarizeStage struct {
	Pipe       Span
	Keyword    Span
	Aggregates []*NamedExpr
	By         []*NamedExpr
}

func (s *SummarizeStage) stage() {}

func (s *SummarizeStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword, namedSpan(s.Aggregates), namedSpan(s.By))
}

// SortKey is one key of sort or top: an expression, then asc or desc, then
// nulls first or nulls last, each optional.
type SortKey struct {
	X Expr
	// Asc is set for asc; the default is descending.
	Asc bool
	// Nulls is "first", "last", or "" for the default: last when
	// descending, first when ascending.
	Nulls   string
	EndSpan Span
}

func (k *SortKey) Span() Span {
	return unionSpans(nodeSpan(k.X), k.EndSpan)
}

// SortStage is sort by key, … (or order by).
type SortStage struct {
	Pipe    Span
	Keyword Span
	Keys    []*SortKey
}

func (s *SortStage) stage() {}

func (s *SortStage) Span() Span {
	span := unionSpans(s.Pipe, s.Keyword)
	if len(s.Keys) > 0 {
		span = unionSpans(span, s.Keys[len(s.Keys)-1].Span())
	}
	return span
}

// TakeStage is take n (or limit n).
type TakeStage struct {
	Pipe    Span
	Keyword Span
	Count   *BasicLit
}

func (s *TakeStage) stage() {}

func (s *TakeStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword, nodeSpan(s.Count))
}

// TopStage is top n by key.
type TopStage struct {
	Pipe    Span
	Keyword Span
	Count   *BasicLit
	Key     *SortKey
}

func (s *TopStage) stage() {}

func (s *TopStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword, s.Key.Span())
}

// CountStage is count: a table of one row, the number of rows.
type CountStage struct {
	Pipe    Span
	Keyword Span
}

func (s *CountStage) stage() {}

func (s *CountStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword)
}

// DistinctStage is distinct col, …: a table of the columns' distinct
// combinations.
type DistinctStage struct {
	Pipe    Span
	Keyword Span
	Columns []*NamedExpr
}

func (s *DistinctStage) stage() {}

func (s *DistinctStage) Span() Span {
	return unionSpans(s.Pipe, s.Keyword, namedSpan(s.Columns))
}
