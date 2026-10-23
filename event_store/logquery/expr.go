package logquery

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/event_store/logquery/parser"
	"github.com/solidtrace/event_store/storage"
)

// valueType is the type of a compiled expression.
type valueType int

const (
	// anyType, as the type asked of an expression, asks for none.
	anyType valueType = iota
	stringType
	numberType
	boolType
	timeType
	timespanType
	// dynamicType is an Attribute read without a type: its value may be a
	// string, a number, a bool or JSON, and differ from Log to Log.
	dynamicType
)

func (t valueType) String() string {
	switch t {
	case stringType:
		return "string"
	case numberType:
		return "number"
	case boolType:
		return "bool"
	case timeType:
		return "datetime"
	case timespanType:
		return "timespan"
	case dynamicType:
		return "dynamic"
	default:
		return "any"
	}
}

// attributeType is the type of Attribute a value of type t is read from.
func (t valueType) attributeType() (storage.AttributeType, bool) {
	switch t {
	case stringType:
		return storage.StringAttribute, true
	case numberType:
		return storage.NumberAttribute, true
	case boolType:
		return storage.BoolAttribute, true
	}
	return "", false
}

// dynamicParts are the types an Attribute read without a type is made of,
// in the order a dynamic value's tuple holds them.
var dynamicParts = []storage.AttributeType{
	storage.StringAttribute, storage.NumberAttribute, storage.BoolAttribute, storage.JSONAttribute,
}

// expr is a compiled, typed expression.
type expr interface {
	typ() valueType
}

// fieldRef is a Log's Field: a built-in field, or an Attribute read as type
// t (dynamicType reads it as whatever type each Log holds).
type fieldRef struct {
	name    string
	builtIn bool
	t       valueType
}

// attributeText is an Attribute read as text by contains: its string, or
// else the JSON text of an array or deep nesting, so that an array is one
// value contains can search.
type attributeText struct{ name string }

// columnRef is a column of a table, or one extend added to Logs.
type columnRef struct {
	name string
	t    valueType
}

// partOf is the part of a dynamic column holding values of type t.
type partOf struct {
	x expr
	t valueType
}

// literal is a value written in a Query, or worked out from one, such as
// ago(1h).
type literal struct {
	Literal
}

// Literal is a value written in a Query: a string, a number, a bool, a time
// or a timespan.
type Literal struct {
	Kind     parser.LiteralKind
	String   string
	Number   float64
	Bool     bool
	Time     time.Time
	Timespan time.Duration
}

// Value is the literal as a Go value.
func (l Literal) Value() any {
	switch l.Kind {
	case parser.NumberLiteral:
		return l.Number
	case parser.BoolLiteral:
		return l.Bool
	case parser.DateTimeLiteral:
		return l.Time
	case parser.TimespanLiteral:
		return l.Timespan
	default:
		return l.String
	}
}

func (l Literal) valueType() valueType {
	switch l.Kind {
	case parser.NumberLiteral:
		return numberType
	case parser.BoolLiteral:
		return boolType
	case parser.DateTimeLiteral:
		return timeType
	case parser.TimespanLiteral:
		return timespanType
	default:
		return stringType
	}
}

// compare is x <op> y. == and != never coerce: values of different types are
// never equal.
type compare struct {
	x  expr
	op compareOp
	y  expr
}

type compareOp string

const (
	opEq compareOp = "=="
	opNE compareOp = "!="
	opLT compareOp = "<"
	opLE compareOp = "<="
	opGT compareOp = ">"
	opGE compareOp = ">="
)

// textMatch tests a string against a pattern written in the Query.
type textMatch struct {
	op            matchOp
	x             expr
	pattern       string
	caseSensitive bool
}

type matchOp int

const (
	matchContains matchOp = iota
	matchStartsWith
	matchRegex
	matchEquals
)

// isEmpty is true for a missing value or an empty string.
type isEmpty struct{ x expr }

// always is true for every row: a comparison with a Dashboard filter set to
// All. A where whose condition is always drops out of the Query.
type always struct{}

func isAlways(x expr) bool {
	_, ok := x.(always)
	return ok
}

type and struct{ x, y expr }
type or struct{ x, y expr }
type not struct{ x expr }

// arith is x <op> y on numbers, or a time plus or minus a timespan.
type arith struct {
	op   string
	x, y expr
	t    valueType
}

// negative is -x.
type negative struct{ x expr }

// call is a scalar function.
type call struct {
	fn   string
	args []expr
	t    valueType
}

// aggregate is one of summarize's functions.
type aggregate struct {
	fn string
	x  expr // nil for count()
	// percent is percentile's second argument.
	percent float64
	t       valueType
}

func (e fieldRef) typ() valueType    { return e.t }
func (e columnRef) typ() valueType   { return e.t }
func (attributeText) typ() valueType { return stringType }
func (e partOf) typ() valueType      { return e.t }
func (e literal) typ() valueType     { return e.valueType() }
func (compare) typ() valueType       { return boolType }
func (textMatch) typ() valueType     { return boolType }
func (isEmpty) typ() valueType       { return boolType }
func (always) typ() valueType        { return boolType }
func (and) typ() valueType           { return boolType }
func (or) typ() valueType            { return boolType }
func (not) typ() valueType           { return boolType }
func (e arith) typ() valueType       { return e.t }
func (e negative) typ() valueType    { return e.x.typ() }
func (e call) typ() valueType        { return e.t }
func (e aggregate) typ() valueType   { return e.t }

// condition compiles an expression that must be a condition.
func (c *compiler) condition(e parser.Expr) (expr, error) {
	x, err := c.expr(e, anyType, nil)
	if err != nil {
		return nil, err
	}
	if x.typ() != boolType {
		return nil, errorAt(e, "expected a condition, such as level == \"error\"")
	}
	return x, nil
}

// expr compiles an expression. want is the type its context asks for, which
// decides the type an Attribute is read as; anyType reads it as dynamic.
// aggregates, if not nil, allows summarize's functions, and is what its
// rates are counted in.
func (c *compiler) expr(e parser.Expr, want valueType, aggregates *rateBasis) (expr, error) {
	switch e := e.(type) {
	case *parser.Ident:
		return c.name(e, want)
	case *parser.BasicLit:
		l, err := c.literal(e)
		return literal{l}, err
	case *parser.ParenExpr:
		return c.expr(e.X, want, aggregates)
	case *parser.UnaryExpr:
		return c.unary(e, aggregates)
	case *parser.BinaryExpr:
		return c.binary(e, aggregates)
	case *parser.CallExpr:
		return c.call(e, want, aggregates)
	case *parser.ListExpr:
		return nil, errorAt(e, "a list only follows in or !in")
	case *parser.Param:
		return nil, c.misplaced(e)
	}
	return nil, errorAt(e, "unexpected expression")
}

// misplaced is the mistake of a Dashboard filter where it can't go.
func (c *compiler) misplaced(p *parser.Param) *Error {
	if c.inNot {
		return errorAt(p, "$%s can't go inside not(…), where All would keep nothing; exclude with != instead", p.Name)
	}
	return p.Misplaced()
}

// name compiles a name: a column, a built-in field or an Attribute.
func (c *compiler) name(id *parser.Ident, want valueType) (expr, error) {
	name := id.Name
	if alias, ok := fieldAliases[name]; ok && !id.Quoted {
		name = alias
	}
	if col, ok := c.scope.lookup(name); ok {
		ref := columnRef{name: col.name, t: col.t}
		if col.t == dynamicType {
			if _, ok := want.attributeType(); ok {
				return partOf{x: ref, t: want}, nil
			}
		}
		return ref, nil
	}
	if !c.scope.logs {
		return nil, errorAt(id, "unknown column %q; the columns here are %s", id.Name, strings.Join(c.scope.names(), ", "))
	}
	if t, ok := builtInFields[name]; ok {
		return fieldRef{name: name, builtIn: true, t: t}, nil
	}
	c.named(name)
	if at, ok := want.attributeType(); ok {
		c.read(at, name)
		return fieldRef{name: name, t: want}, nil
	}
	for _, t := range dynamicParts {
		c.read(t, name)
	}
	return fieldRef{name: name, t: dynamicType}, nil
}

// literal compiles a literal as written.
func (c *compiler) literal(lit *parser.BasicLit) (Literal, error) {
	switch lit.Kind {
	case parser.StringLiteral:
		return Literal{Kind: parser.StringLiteral, String: lit.Value}, nil
	case parser.BoolLiteral:
		return Literal{Kind: parser.BoolLiteral, Bool: lit.Value == "true"}, nil
	case parser.TimespanLiteral:
		d, err := parser.ParseTimespan(lit.Value)
		if err != nil {
			return Literal{}, errorAt(lit, "%s", err)
		}
		return Literal{Kind: parser.TimespanLiteral, Timespan: d}, nil
	case parser.DateTimeLiteral:
		t, ok := parseDateTime(lit.Value)
		if !ok {
			return Literal{}, errorAt(lit, "%q isn't a time; write it as 2026-10-02T10:00:00Z or 2026-10-02", lit.Value)
		}
		return Literal{Kind: parser.DateTimeLiteral, Time: t}, nil
	default:
		return numberLiteral(lit, false)
	}
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

// dateTimeLayouts are the ways a datetime(…) can be written. A time without
// a zone is UTC.
var dateTimeLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04", time.DateOnly,
}

func parseDateTime(text string) (time.Time, bool) {
	for _, layout := range dateTimeLayouts {
		if t, err := time.Parse(layout, text); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (c *compiler) unary(e *parser.UnaryExpr, aggregates *rateBasis) (expr, error) {
	if lit, ok := e.X.(*parser.BasicLit); ok && lit.Kind == parser.NumberLiteral {
		l, err := numberLiteral(lit, e.Op == parser.TokenMinus)
		return literal{l}, err
	}
	x, err := c.expr(e.X, numberType, aggregates)
	if err != nil {
		return nil, err
	}
	if x.typ() == timespanType {
		if l, ok := x.(literal); ok && e.Op == parser.TokenMinus {
			l.Timespan = -l.Timespan
			return l, nil
		}
	}
	if x.typ() != numberType && x.typ() != timespanType {
		return nil, errorAt(e, "%s needs a number, not a %s", opText(e.Op), x.typ())
	}
	if e.Op == parser.TokenPlus {
		return x, nil
	}
	return negative{x}, nil
}

func opText(op parser.TokenKind) string {
	switch op {
	case parser.TokenPlus:
		return "+"
	case parser.TokenMinus:
		return "-"
	case parser.TokenStar:
		return "*"
	case parser.TokenSlash:
		return "/"
	case parser.TokenMod:
		return "%"
	}
	return "this operator"
}

var compareOps = map[parser.TokenKind]compareOp{
	parser.TokenEq: opEq, parser.TokenNE: opNE,
	parser.TokenLT: opLT, parser.TokenLE: opLE, parser.TokenGT: opGT, parser.TokenGE: opGE,
}

// textOp is an operator that tests a string against a pattern: what it
// tests, whether case counts, and whether it is negated.
type textOp struct {
	op            matchOp
	caseSensitive bool
	negated       bool
}

// match is x tested against pattern.
func (t textOp) match(x expr, pattern string) expr {
	var m expr = textMatch{op: t.op, x: x, pattern: pattern, caseSensitive: t.caseSensitive}
	if t.negated {
		return not{m}
	}
	return m
}

// textOps are the operators that test a string against a pattern.
var textOps = map[parser.TokenKind]textOp{
	parser.TokenContains:        {matchContains, false, false},
	parser.TokenNotContains:     {matchContains, false, true},
	parser.TokenContainsCS:      {matchContains, true, false},
	parser.TokenNotContainsCS:   {matchContains, true, true},
	parser.TokenStartsWith:      {matchStartsWith, false, false},
	parser.TokenNotStartsWith:   {matchStartsWith, false, true},
	parser.TokenStartsWithCS:    {matchStartsWith, true, false},
	parser.TokenNotStartsWithCS: {matchStartsWith, true, true},
	parser.TokenMatchesRegex:    {matchRegex, true, false},
	parser.TokenEqCI:            {matchEquals, false, false},
	parser.TokenNECI:            {matchEquals, false, true},
}

func (c *compiler) binary(e *parser.BinaryExpr, aggregates *rateBasis) (expr, error) {
	switch e.Op {
	case parser.TokenAnd, parser.TokenOr:
		x, err := c.condition(e.X)
		if err != nil {
			return nil, err
		}
		y, err := c.condition(e.Y)
		if err != nil {
			return nil, err
		}
		if e.Op == parser.TokenAnd {
			return and{x, y}, nil
		}
		return or{x, y}, nil
	case parser.TokenIn, parser.TokenNotIn:
		return c.in(e, aggregates)
	case parser.TokenPlus, parser.TokenMinus, parser.TokenStar, parser.TokenSlash, parser.TokenMod:
		return c.arith(e, aggregates)
	}
	if op, ok := compareOps[e.Op]; ok {
		return c.compare(e, op, aggregates)
	}
	if text, ok := textOps[e.Op]; ok {
		x, err := c.textOperand(e.X, e.OpSpan, text, aggregates)
		if err != nil {
			return nil, err
		}
		if p, ok := e.Y.(*parser.Param); ok {
			return nil, c.misplaced(p)
		}
		lit, ok := e.Y.(*parser.BasicLit)
		if !ok || lit.Kind != parser.StringLiteral {
			return nil, errorAt(e.Y, "expected a quoted string after %s", c.source(e.OpSpan))
		}
		if text.op == matchRegex {
			if _, err := regexp.Compile(lit.Value); err != nil {
				return nil, errorAt(lit, "invalid regular expression: %s", strings.TrimPrefix(err.Error(), "error parsing regexp: "))
			}
		}
		return text.match(x, lit.Value), nil
	}
	return nil, &Error{Span: e.OpSpan, Message: "unsupported operator"}
}

// textOperand compiles the string a text operator tests. contains reads an
// Attribute as text, arrays and deep nesting as JSON.
func (c *compiler) textOperand(e parser.Expr, opSpan parser.Span, text textOp, aggregates *rateBasis) (expr, error) {
	x, err := c.expr(e, stringType, aggregates)
	if err != nil {
		return nil, err
	}
	if x.typ() != stringType {
		return nil, errorAt(e, "expected a string on the left of %s, not a %s", c.source(opSpan), x.typ())
	}
	if ref, ok := x.(fieldRef); ok && !ref.builtIn && text.op == matchContains {
		c.read(storage.JSONAttribute, ref.name)
		x = attributeText{name: ref.name}
	}
	return x, nil
}

// predicate compiles a where's condition, in which a Dashboard filter may be
// compared with ==, !=, =~, !~ and the text operators but matches regex,
// joined by and and or. A comparison with a filter set to All is always, and
// and and or fold it away.
func (c *compiler) predicate(e parser.Expr) (expr, error) {
	switch e := e.(type) {
	case *parser.ParenExpr:
		return c.predicate(e.X)
	case *parser.BinaryExpr:
		switch e.Op {
		case parser.TokenAnd, parser.TokenOr:
			x, err := c.predicate(e.X)
			if err != nil {
				return nil, err
			}
			y, err := c.predicate(e.Y)
			if err != nil {
				return nil, err
			}
			switch {
			case e.Op == parser.TokenOr && (isAlways(x) || isAlways(y)):
				return always{}, nil
			case e.Op == parser.TokenOr:
				return or{x, y}, nil
			case isAlways(x):
				return y, nil
			case isAlways(y):
				return x, nil
			}
			return and{x, y}, nil
		}
		if p, side, ok := filterSides(e); ok {
			return c.filter(e, p, side)
		}
	}
	return c.condition(e)
}

// filterOps are the operators a Dashboard filter can be compared with, each
// set to whether the filter can go on either side of it.
var filterOps = map[parser.TokenKind]bool{
	parser.TokenEq: true, parser.TokenNE: true, parser.TokenEqCI: true, parser.TokenNECI: true,
	parser.TokenContains: false, parser.TokenNotContains: false, parser.TokenContainsCS: false, parser.TokenNotContainsCS: false,
	parser.TokenStartsWith: false, parser.TokenNotStartsWith: false, parser.TokenStartsWithCS: false, parser.TokenNotStartsWithCS: false,
}

// filterSides splits a comparison with a Dashboard filter into the filter and
// what it is compared with. The filter is on the right, or on either side of
// ==, !=, =~ and !~.
func filterSides(e *parser.BinaryExpr) (*parser.Param, parser.Expr, bool) {
	eitherSide, ok := filterOps[e.Op]
	if !ok {
		return nil, nil, false
	}
	if p, ok := e.Y.(*parser.Param); ok {
		return p, e.X, true
	}
	if p, ok := e.X.(*parser.Param); ok && eitherSide {
		return p, e.Y, true
	}
	return nil, nil, false
}

// filter compiles side <op> $name, a comparison with a Dashboard filter. One
// value compares as if written there as a string; with several, a positive
// operator matches any of them and a negated one none. With none, the
// comparison is always, and what compiling side noted is forgotten, so that
// the Query compiles as if the comparison weren't there.
//
// A value is text. Compared with an Attribute, or a column of no fixed
// type, it matches the text or the number or bool it reads as; with a
// column of another type, the value it reads as, or else nothing.
func (c *compiler) filter(e *parser.BinaryExpr, p *parser.Param, side parser.Expr) (expr, error) {
	if _, ok := side.(*parser.Param); ok {
		return nil, errorAt(p, "compare $%s with a field, as in service == $%s", p.Name, p.Name)
	}
	values := c.valuesOf(p)
	forget := c.mark()
	text, isText := textOps[e.Op]
	var x expr
	var err error
	if isText {
		x, err = c.textOperand(side, e.OpSpan, text, nil)
	} else {
		x, err = c.expr(side, stringType, nil)
	}
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		forget()
		return always{}, nil
	}
	negated := e.Op == parser.TokenNE || text.negated
	op := opEq
	if negated {
		op = opNE
	}
	join := func(m, alt expr) expr {
		if negated {
			return and{m, alt}
		}
		return or{m, alt}
	}
	var all expr
	for _, v := range values {
		var m expr
		if isText {
			m = text.match(x, v)
		} else {
			m = compare{x: x, op: op, y: literal{valueAs(x.typ(), v)}}
		}
		if untyped(x) && (!isText || text.op == matchEquals) {
			if l, ok := readsAs(v); ok {
				y, err := c.expr(side, l.valueType(), nil)
				if err != nil {
					return nil, err
				}
				m = join(m, compare{x: y, op: op, y: literal{l}})
			}
		}
		if all == nil {
			all = m
		} else {
			all = join(all, m)
		}
	}
	return all, nil
}

// valuesOf notes that the Query names the Dashboard filter p, and returns
// its values, leaving out empty ones: none for All.
func (c *compiler) valuesOf(p *parser.Param) []string {
	if !slices.Contains(c.parameters, p.Name) {
		c.parameters = append(c.parameters, p.Name)
	}
	var values []string
	for _, v := range c.values[p.Name] {
		if v != "" {
			values = append(values, v)
		}
	}
	return values
}

// untyped reports whether x, read as a string, is a value of no fixed type:
// an Attribute, or a dynamic column.
func untyped(x expr) bool {
	switch x := x.(type) {
	case fieldRef:
		return !x.builtIn
	case partOf:
		return true
	}
	return false
}

// readsAs is the number or bool a filter's text value reads as, if any.
func readsAs(v string) (Literal, bool) {
	if n, err := strconv.ParseFloat(v, 64); err == nil && !math.IsInf(n, 0) && !math.IsNaN(n) {
		return Literal{Kind: parser.NumberLiteral, Number: n}, true
	}
	if v == "true" || v == "false" {
		return Literal{Kind: parser.BoolLiteral, Bool: v == "true"}, true
	}
	return Literal{}, false
}

// valueAs is a filter's text value as a value of type t, when it reads as
// one, or else as text.
func valueAs(t valueType, v string) Literal {
	if l, ok := readsAs(v); ok && l.valueType() == t {
		return l
	}
	return Literal{Kind: parser.StringLiteral, String: v}
}

// source quotes the Query's text at span, for messages.
func (c *compiler) source(span parser.Span) string {
	return fmt.Sprintf("%q", c.text[span.Start:span.End])
}

// compare compiles x <op> y. The side with a type decides the type the
// other is read as.
func (c *compiler) compare(e *parser.BinaryExpr, op compareOp, aggregates *rateBasis) (expr, error) {
	y, err := c.expr(e.Y, anyType, aggregates)
	if err != nil {
		return nil, err
	}
	x, err := c.expr(e.X, concrete(y.typ()), aggregates)
	if err != nil {
		return nil, err
	}
	if y.typ() == dynamicType && x.typ() != dynamicType {
		if y, err = c.expr(e.Y, x.typ(), aggregates); err != nil {
			return nil, err
		}
	}
	if x.typ() == dynamicType && y.typ() == dynamicType {
		if _, isIdent := e.X.(*parser.Ident); isIdent {
			if _, isIdent := e.Y.(*parser.Ident); isIdent && c.scope.logs {
				return nil, errorAt(e.Y, "expected a value: a string, a number, true or false")
			}
		}
		return nil, errorAt(e, "can't compare two values whose types aren't known; convert one, as in tostring(a) == tostring(b)")
	}
	if op != opEq && op != opNE {
		if x.typ() != y.typ() {
			return nil, errorAt(e, "can't compare a %s with a %s", x.typ(), y.typ())
		}
		if x.typ() == boolType {
			return nil, errorAt(e, "bools can only be compared with == and !=")
		}
	}
	return compare{x: x, op: op, y: y}, nil
}

// concrete is the type to ask of the other side of a comparison with a
// value of type t.
func concrete(t valueType) valueType {
	if t == dynamicType {
		return anyType
	}
	return t
}

// in compiles x in (a, b, …) as x == a or x == b …, and !in as its negation,
// so !in keeps Logs missing the Field.
func (c *compiler) in(e *parser.BinaryExpr, aggregates *rateBasis) (expr, error) {
	list := e.Y.(*parser.ListExpr)
	var match expr
	for _, elem := range list.Elems {
		value, err := c.expr(elem, anyType, aggregates)
		if err != nil {
			return nil, err
		}
		if _, ok := value.(literal); !ok {
			return nil, errorAt(elem, "expected a value: a string, a number, true or false")
		}
		x, err := c.expr(e.X, value.typ(), aggregates)
		if err != nil {
			return nil, err
		}
		var eq expr = compare{x: x, op: opEq, y: value}
		if match != nil {
			eq = or{match, eq}
		}
		match = eq
	}
	if e.Op == parser.TokenNotIn {
		return not{match}, nil
	}
	return match, nil
}

func (c *compiler) arith(e *parser.BinaryExpr, aggregates *rateBasis) (expr, error) {
	x, err := c.expr(e.X, numberType, aggregates)
	if err != nil {
		return nil, err
	}
	want := numberType
	if x.typ() == timeType {
		want = timespanType
	}
	y, err := c.expr(e.Y, want, aggregates)
	if err != nil {
		return nil, err
	}
	op := opText(e.Op)
	switch {
	case x.typ() == numberType && y.typ() == numberType:
		return arith{op: op, x: x, y: y, t: numberType}, nil
	case x.typ() == timeType && y.typ() == timespanType && (op == "+" || op == "-"):
		if xl, ok := x.(literal); ok {
			if yl, ok := y.(literal); ok {
				// Worked out now, so that a range can be narrowed by it.
				d := yl.Timespan
				if op == "-" {
					d = -d
				}
				return literal{Literal{Kind: parser.DateTimeLiteral, Time: xl.Time.Add(d)}}, nil
			}
		}
		return arith{op: op, x: x, y: y, t: timeType}, nil
	case x.typ() == timespanType && y.typ() == timespanType && (op == "+" || op == "-"):
		return arith{op: op, x: x, y: y, t: timespanType}, nil
	}
	return nil, errorAt(e, "can't apply %s to a %s and a %s", op, x.typ(), y.typ())
}

// function describes a scalar function: its arguments' types, and its
// result's type given theirs.
type function struct {
	args   []valueType
	result func(args []expr) valueType
}

func fixed(t valueType) func([]expr) valueType {
	return func([]expr) valueType { return t }
}

var scalarFunctions = map[string]function{
	"tostring": {args: []valueType{anyType}, result: fixed(stringType)},
	"tolower":  {args: []valueType{stringType}, result: fixed(stringType)},
	"toupper":  {args: []valueType{stringType}, result: fixed(stringType)},
	"strlen":   {args: []valueType{stringType}, result: fixed(numberType)},
	"toint":    {args: []valueType{anyType}, result: fixed(numberType)},
	"tolong":   {args: []valueType{anyType}, result: fixed(numberType)},
	"todouble": {args: []valueType{anyType}, result: fixed(numberType)},
	"toreal":   {args: []valueType{anyType}, result: fixed(numberType)},
	"bin":      {args: []valueType{numberType, anyType}, result: func(args []expr) valueType { return args[0].typ() }},
	"floor":    {args: []valueType{numberType, anyType}, result: func(args []expr) valueType { return args[0].typ() }},
}

var aggregateFunctions = map[string]bool{
	"count": true, "countif": true, "dcount": true, "sum": true, "avg": true, "min": true, "max": true, "percentile": true,
	"rate": true, "peak_rate": true,
}

func (c *compiler) call(e *parser.CallExpr, want valueType, aggregates *rateBasis) (expr, error) {
	name := e.Func.Name
	arity := func(n int, usage string) error {
		if len(e.Args) != n {
			return errorAt(e, "%s takes %s", name, usage)
		}
		return nil
	}
	switch name {
	case "not":
		if err := arity(1, "one condition"); err != nil {
			return nil, err
		}
		inNot := c.inNot
		c.inNot = true
		x, err := c.condition(e.Args[0])
		c.inNot = inNot
		if err != nil {
			return nil, err
		}
		return not{x}, nil
	case "isempty", "isnotempty", "isnull", "isnotnull":
		if err := arity(1, "one value"); err != nil {
			return nil, err
		}
		x, err := c.expr(e.Args[0], anyType, aggregates)
		if err != nil {
			return nil, err
		}
		var test expr = isEmpty{x}
		if strings.HasPrefix(name, "isnot") {
			test = not{test}
		}
		return test, nil
	case "now":
		if err := arity(0, "no arguments"); err != nil {
			return nil, err
		}
		return literal{Literal{Kind: parser.DateTimeLiteral, Time: c.now()}}, nil
	case "ago":
		if err := arity(1, "a timespan, such as ago(1h)"); err != nil {
			return nil, err
		}
		span, err := c.timespan(e.Args[0])
		if err != nil {
			return nil, err
		}
		return literal{Literal{Kind: parser.DateTimeLiteral, Time: c.now().Add(-span)}}, nil
	case "bin_auto":
		if err := arity(1, "time, as in bin_auto(time)"); err != nil {
			return nil, err
		}
		x, err := c.expr(e.Args[0], anyType, aggregates)
		if err != nil {
			return nil, err
		}
		if x.typ() != timeType {
			return nil, errorAt(e.Args[0], "bin_auto takes time")
		}
		size := literal{Literal{Kind: parser.TimespanLiteral, Timespan: autoBin(c.bounds.To.Sub(c.bounds.From))}}
		return call{fn: "bin", args: []expr{x, size}, t: timeType}, nil
	}
	if aggregateFunctions[name] {
		if aggregates == nil {
			return nil, errorAt(e.Func, "%s() only works in summarize", name)
		}
		return c.aggregate(e, *aggregates)
	}
	fn, ok := scalarFunctions[name]
	if !ok {
		return nil, errorAt(e.Func, "unknown function %q", name)
	}
	if len(e.Args) != len(fn.args) {
		return nil, errorAt(e, "%s takes %d arguments", name, len(fn.args))
	}
	args := make([]expr, len(e.Args))
	for i, arg := range e.Args {
		x, err := c.expr(arg, fn.args[i], aggregates)
		if err != nil {
			return nil, err
		}
		args[i] = x
	}
	if err := checkCall(name, e, args); err != nil {
		return nil, err
	}
	return call{fn: name, args: args, t: fn.result(args)}, nil
}

// checkCall checks the types of a scalar function's arguments where its
// table doesn't say all.
func checkCall(name string, e *parser.CallExpr, args []expr) error {
	switch name {
	case "tolower", "toupper", "strlen":
		if args[0].typ() != stringType {
			return errorAt(e.Args[0], "%s takes a string, not a %s", name, args[0].typ())
		}
	case "bin", "floor":
		switch {
		case args[0].typ() == timeType && args[1].typ() == timespanType:
		case args[0].typ() == numberType && args[1].typ() == numberType:
		default:
			return errorAt(e, "%s takes a time and a timespan, as in bin(time, 5m), or two numbers", name)
		}
		if l, ok := args[1].(literal); !ok || (l.Timespan <= 0 && l.Number <= 0) {
			return errorAt(e.Args[1], "%s needs a size above zero written in the Query", name)
		}
	}
	return nil
}

// timespan compiles an argument that must be a timespan written in the
// Query.
func (c *compiler) timespan(e parser.Expr) (time.Duration, error) {
	x, err := c.expr(e, timespanType, nil)
	if err != nil {
		return 0, err
	}
	l, ok := x.(literal)
	if !ok || l.Kind != parser.TimespanLiteral {
		return 0, errorAt(e, "expected a timespan, such as 1h or 30m")
	}
	return l.Timespan, nil
}

func (c *compiler) now() time.Time {
	if c.bounds.Now.IsZero() {
		return time.Now().UTC()
	}
	return c.bounds.Now.UTC()
}

// autoBin is the bin size bin_auto picks for a time range: the smallest of
// a ladder of sizes that splits it into at most about 100 bins.
func autoBin(span time.Duration) time.Duration {
	ladder := []time.Duration{
		time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second,
		time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute,
		time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour,
	}
	if span <= 0 {
		return time.Hour
	}
	for _, size := range ladder {
		if span/size <= 100 {
			return size
		}
	}
	return ladder[len(ladder)-1]
}

// aggregate compiles one of summarize's functions, whose rates are counted
// in basis.
func (c *compiler) aggregate(e *parser.CallExpr, basis rateBasis) (expr, error) {
	name := e.Func.Name
	switch name {
	case "count":
		if len(e.Args) != 0 {
			return nil, errorAt(e, "count takes no arguments; use dcount(x) for distinct values")
		}
		return aggregate{fn: name, t: numberType}, nil
	case "rate":
		if err := rateArgs(e); err != nil {
			return nil, err
		}
		if basis.otherTime {
			return nil, errorAt(e, "rate() needs a time bin of the Logs' own time, such as bin(time, 1m)")
		}
		return aggregate{fn: name, t: numberType}, nil
	case "peak_rate":
		if err := rateArgs(e); err != nil {
			return nil, err
		}
		if !basis.logTime {
			return nil, errorAt(e, "peak_rate() counts rows in each second of the Logs' own time, which these rows don't have")
		}
		if basis.bin%time.Second != 0 {
			return nil, errorAt(e, "peak_rate() needs time bins of whole seconds, not %s", basis.bin)
		}
		return aggregate{fn: name, t: numberType}, nil
	case "countif":
		if len(e.Args) != 1 {
			return nil, errorAt(e, "countif takes one condition")
		}
		x, err := c.condition(e.Args[0])
		if err != nil {
			return nil, err
		}
		return aggregate{fn: name, x: x, t: numberType}, nil
	case "percentile":
		if len(e.Args) != 2 {
			return nil, errorAt(e, "percentile takes a value and a percentage, as in percentile(duration, 95)")
		}
		x, err := c.number(e.Args[0], name)
		if err != nil {
			return nil, err
		}
		p, err := c.expr(e.Args[1], numberType, nil)
		if err != nil {
			return nil, err
		}
		l, ok := p.(literal)
		if !ok || l.Kind != parser.NumberLiteral || l.Number < 0 || l.Number > 100 {
			return nil, errorAt(e.Args[1], "expected a percentage from 0 to 100")
		}
		return aggregate{fn: name, x: x, percent: l.Number, t: numberType}, nil
	}
	if len(e.Args) != 1 {
		return nil, errorAt(e, "%s takes one value", name)
	}
	switch name {
	case "sum", "avg":
		x, err := c.number(e.Args[0], name)
		if err != nil {
			return nil, err
		}
		return aggregate{fn: name, x: x, t: numberType}, nil
	case "min", "max":
		x, err := c.expr(e.Args[0], anyType, nil)
		if err != nil {
			return nil, err
		}
		if x.typ() == dynamicType {
			// An Attribute's least or greatest value is a number's.
			if x, err = c.number(e.Args[0], name); err != nil {
				return nil, err
			}
		}
		if x.typ() == boolType {
			return nil, errorAt(e.Args[0], "%s takes a number, a string or a time, not a bool", name)
		}
		return aggregate{fn: name, x: x, t: x.typ()}, nil
	default: // dcount
		x, err := c.expr(e.Args[0], anyType, nil)
		if err != nil {
			return nil, err
		}
		return aggregate{fn: name, x: x, t: numberType}, nil
	}
}

// number compiles an argument that must be a number.
func (c *compiler) number(e parser.Expr, fn string) (expr, error) {
	x, err := c.expr(e, numberType, nil)
	if err != nil {
		return nil, err
	}
	if x.typ() != numberType {
		return nil, errorAt(e, "%s takes a number, not a %s", fn, x.typ())
	}
	return x, nil
}

// fieldName is the name of the first Field or column an expression reads,
// for naming the columns of summarize and project.
func fieldName(x expr) string {
	switch x := x.(type) {
	case fieldRef:
		return x.name
	case columnRef:
		return x.name
	case partOf:
		return fieldName(x.x)
	case call:
		for _, arg := range x.args {
			if name := fieldName(arg); name != "" {
				return name
			}
		}
	case aggregate:
		if x.x != nil {
			return fieldName(x.x)
		}
	case arith:
		if name := fieldName(x.x); name != "" {
			return name
		}
		return fieldName(x.y)
	case negative:
		return fieldName(x.x)
	}
	return ""
}
