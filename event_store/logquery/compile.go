// Package logquery compiles Queries, SolidTrace's KQL-style query language
// for Logs, to DuckDB SQL. It is the single path from a Query to SQL.
//
// Compiling runs in three steps, each in its own file:
//   - package parser turns the text into a syntax tree;
//   - Compile (this file) checks the tree against the language's stages,
//     functions, Fields and types, and turns it into a Query of typed stages;
//   - the SQL emitter (sql.go) writes a Query's stages as SQL against one
//     source of Logs, whose columns storage.LogColumns describes.
//
// Builder chips (chips.go) are read off a compiled Query.
package logquery

import (
	"fmt"
	"math"
	"strconv"

	"github.com/solidtrace/event_store/logquery/parser"
)

// Error is a mistake in a Query, at a span of its text.
type Error = parser.Error

// Query is a compiled Query: its stages, each checked and typed.
type Query struct {
	stages []stage
}

// stage is one compiled stage. Every stage so far filters Logs.
type stage interface {
	isStage()
}

// whereStage keeps the Logs its predicate holds for.
type whereStage struct {
	predicate predicate
}

// searchStage keeps the Logs whose message or any string Attribute contains
// term, ignoring case.
type searchStage struct {
	term string
}

func (whereStage) isStage()  {}
func (searchStage) isStage() {}

// predicate is a typed condition on one Log.
type predicate interface {
	isPredicate()
}

// compare is field == value or field != value. Neither coerces: a Field's
// value of another type than value's is never equal to it.
type compare struct {
	field Field
	op    compareOp
	value Literal
}

type compareOp string

const (
	opEq compareOp = "=="
	opNE compareOp = "!="
)

type and struct{ x, y predicate }
type or struct{ x, y predicate }
type not struct{ x predicate }

func (compare) isPredicate() {}
func (and) isPredicate()     {}
func (or) isPredicate()      {}
func (not) isPredicate()     {}

// Field is a Field a Query names: a built-in field or an Attribute.
type Field struct {
	Name    string
	BuiltIn bool
}

// builtInFields are the built-in fields every Log has; all are strings but
// time.
var builtInFields = map[string]bool{
	"time": true, "level": true, "message": true, "service": true,
	"environment": true, "trace_id": true, "span_id": true,
}

func fieldNamed(name string) Field {
	return Field{Name: name, BuiltIn: builtInFields[name]}
}

// Literal is a value written in a Query: a string, a number or a bool.
type Literal struct {
	Kind   parser.LiteralKind
	String string
	Number float64
	Bool   bool
}

// Value is the literal as a Go value: a string, a float64 or a bool.
func (l Literal) Value() any {
	switch l.Kind {
	case parser.NumberLiteral:
		return l.Number
	case parser.BoolLiteral:
		return l.Bool
	default:
		return l.String
	}
}

// Compile parses and checks a Query's text. The error, if any, is an *Error
// with the span of the first mistake.
func Compile(source string) (*Query, error) {
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, err
	}
	q := &Query{}
	for _, s := range tree.Stages {
		compiled, err := compileStage(s)
		if err != nil {
			return nil, err
		}
		q.stages = append(q.stages, compiled)
	}
	return q, nil
}

func errorAt(node parser.Node, format string, args ...any) *Error {
	return &Error{Span: node.Span(), Message: fmt.Sprintf(format, args...)}
}

func compileStage(s parser.Stage) (stage, error) {
	switch s := s.(type) {
	case *parser.WhereStage:
		p, err := compilePredicate(s.Predicate)
		if err != nil {
			return nil, err
		}
		return whereStage{predicate: p}, nil
	case *parser.SearchStage:
		return searchStage{term: s.Term.Value}, nil
	default:
		return nil, errorAt(s, "unknown stage")
	}
}

// compilePredicate compiles an expression that must be a condition.
func compilePredicate(e parser.Expr) (predicate, error) {
	switch e := e.(type) {
	case *parser.ParenExpr:
		return compilePredicate(e.X)
	case *parser.BinaryExpr:
		switch e.Op {
		case parser.TokenAnd, parser.TokenOr:
			x, err := compilePredicate(e.X)
			if err != nil {
				return nil, err
			}
			y, err := compilePredicate(e.Y)
			if err != nil {
				return nil, err
			}
			if e.Op == parser.TokenAnd {
				return and{x, y}, nil
			}
			return or{x, y}, nil
		case parser.TokenEq, parser.TokenNE:
			return compileCompare(e)
		default:
			return nil, &Error{Span: e.OpSpan, Message: "unsupported operator; a condition compares a Field with == or !="}
		}
	case *parser.CallExpr:
		if e.Func.Name != "not" {
			return nil, errorAt(e.Func, "unknown function %q", e.Func.Name)
		}
		if len(e.Args) != 1 {
			return nil, errorAt(e, "not takes one condition")
		}
		x, err := compilePredicate(e.Args[0])
		if err != nil {
			return nil, err
		}
		return not{x}, nil
	default:
		return nil, errorAt(e, "expected a condition, such as level == \"error\"")
	}
}

func compileCompare(e *parser.BinaryExpr) (predicate, error) {
	op := opEq
	if e.Op == parser.TokenNE {
		op = opNE
	}
	ident, ok := e.X.(*parser.Ident)
	if !ok {
		if call, isCall := e.X.(*parser.CallExpr); isCall {
			return nil, errorAt(call.Func, "unknown function %q", call.Func.Name)
		}
		return nil, errorAt(e.X, "expected a Field on the left of %s", op)
	}
	field := fieldNamed(ident.Name)
	value, err := compileLiteral(e.Y)
	if err != nil {
		return nil, err
	}
	if field.Name == "time" {
		return nil, errorAt(e, "time can't be compared yet; the time range sets it")
	}
	return compare{field: field, op: op, value: value}, nil
}

// compileLiteral compiles an expression that must be a literal value.
func compileLiteral(e parser.Expr) (Literal, error) {
	switch e := e.(type) {
	case *parser.BasicLit:
		switch e.Kind {
		case parser.StringLiteral:
			return Literal{Kind: parser.StringLiteral, String: e.Value}, nil
		case parser.BoolLiteral:
			return Literal{Kind: parser.BoolLiteral, Bool: e.Value == "true"}, nil
		default:
			return numberLiteral(e, false)
		}
	case *parser.UnaryExpr:
		if lit, ok := e.X.(*parser.BasicLit); ok && lit.Kind == parser.NumberLiteral {
			return numberLiteral(lit, e.Op == parser.TokenMinus)
		}
	case *parser.CallExpr:
		return Literal{}, errorAt(e.Func, "unknown function %q", e.Func.Name)
	}
	return Literal{}, errorAt(e, "expected a value: a string, a number, true or false")
}

func numberLiteral(lit *parser.BasicLit, negative bool) (Literal, error) {
	n, err := strconv.ParseFloat(lit.Value, 64)
	if err != nil || math.IsInf(n, 0) {
		return Literal{}, errorAt(lit, "%s is too large a number", lit.Value)
	}
	if negative {
		n = -n
	}
	return Literal{Kind: parser.NumberLiteral, Number: n}, nil
}
