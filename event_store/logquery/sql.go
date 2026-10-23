package logquery

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/solidtrace/event_store/logquery/parser"
	"github.com/solidtrace/event_store/pkg/chsql"
	"github.com/solidtrace/event_store/pkg/sqltext"
	"github.com/solidtrace/event_store/storage"
)

// fields says where the rows being written against keep each Field: a
// source of Logs (Columns), or the Logs a tail reads.
type fields interface {
	BuiltIn(name string) string
	Attribute(t storage.AttributeType, key string) string
}

// Columns says where a source of Logs keeps each Field, as SQL, such as
// storage.ClickHouseColumns for the logs table.
type Columns interface {
	fields
	// SearchText is the text search reads besides message: every string
	// Attribute's value, joined by \0.
	SearchText() string
}

// stringColumns are fields with a column that holds a string Attribute's
// value, or ” for a Log whose Attribute isn't a string, such as the logs
// table's file: a filter on it skips the Logs of other files.
type stringColumns interface {
	StringColumn(key string) (string, bool)
}

// Where writes the Query's head, its leading where and search stages, as one
// ClickHouse SQL condition on a source of Logs whose columns cols describes,
// or "" if the head keeps every Log.
//
// Conditions are two-valued, as a person reads them: a comparison with a
// Field a Log lacks is false, and its negation true. So != keeps Logs missing
// the Field, and not(a == b) is a != b.
func (q *Query) Where(cols Columns) string {
	var conditions []string
	for _, s := range q.head {
		switch s := s.(type) {
		case whereStage:
			conditions = append(conditions, writeCondition(s.predicate, cols, false))
		case searchStage:
			conditions = append(conditions, writeSearch(s.term, cols))
		}
	}
	if len(conditions) == 0 {
		return ""
	}
	return "(" + strings.Join(conditions, ") AND (") + ")"
}

// writeCondition writes a condition, or its negation when negate is set, for
// a WHERE clause. Negations are pushed down to the tests, and each negated
// test is written true or false for every row, so that SQL's NULLs never
// leak out as "unknown". A test not negated may be NULL where a value is
// missing, which a WHERE reads as false, and is kept plain so that ClickHouse
// can use it to skip granules.
func writeCondition(p expr, f fields, negate bool) string {
	switch p := p.(type) {
	case and:
		op := " AND "
		if negate {
			op = " OR "
		}
		return "(" + writeCondition(p.x, f, negate) + op + writeCondition(p.y, f, negate) + ")"
	case or:
		op := " OR "
		if negate {
			op = " AND "
		}
		return "(" + writeCondition(p.x, f, negate) + op + writeCondition(p.y, f, negate) + ")"
	case not:
		return writeCondition(p.x, f, !negate)
	case compare:
		return writeCompare(p, f, negate)
	case isEmpty:
		test := writeIsEmpty(p.x, f)
		if negate {
			return "NOT " + test
		}
		return test
	}
	if p.typ() != boolType {
		panic(fmt.Sprintf("logquery: %T isn't a condition", p))
	}
	test := writeValue(p, f)
	if negate {
		return "NOT coalesce(" + test + ", false)"
	}
	return test
}

// writeCompare writes x <op> y, or its negation. Values of different types
// are never equal: == between them is false, and != true.
func writeCompare(p compare, f fields, negate bool) string {
	equal := p.op == opEq || p.op == opNE
	if equal && p.x.typ() != p.y.typ() {
		return strconv.FormatBool((p.op == opNE) != negate)
	}
	if column, value, ok := stringColumnCompare(p, f); equal && ok {
		if (p.op == opEq) != negate {
			return column + " = " + value
		}
		return column + " != " + value
	}
	x, y := writeValue(p.x, f), writeValue(p.y, f)
	if equal {
		if (p.op == opEq) != negate {
			// NULL, for a missing value, keeps the row out.
			return x + " = " + y
		}
		return x + " IS DISTINCT FROM " + y
	}
	test := x + " " + string(p.op) + " " + y
	if negate {
		return "NOT coalesce(" + test + ", false)"
	}
	return test
}

// stringColumnCompare returns the column that holds a string Attribute
// compared with a string that isn't empty, and the string as SQL, if f has
// one. The column equals the string just when the Attribute does, since a Log
// without the Attribute, or with one of another type, holds ”.
func stringColumnCompare(p compare, f fields) (column, value string, ok bool) {
	columns, ok := f.(stringColumns)
	if !ok {
		return "", "", false
	}
	field, other := p.x, p.y
	if _, isLiteral := field.(literal); isLiteral {
		field, other = other, field
	}
	ref, isRef := field.(fieldRef)
	lit, isLiteral := other.(literal)
	if !isRef || ref.builtIn || ref.t != stringType || !isLiteral || lit.Kind != parser.StringLiteral || lit.String == "" {
		return "", "", false
	}
	column, ok = columns.StringColumn(ref.name)
	return column, chsql.String(lit.String), ok
}

// writeIsEmpty writes a test, true or false for every row, that x is missing
// or an empty string.
func writeIsEmpty(x expr, f fields) string {
	switch x.typ() {
	case stringType:
		v := writeValue(x, f)
		return "(" + v + " IS NULL OR " + v + " = '')"
	case dynamicType:
		if ref, ok := x.(fieldRef); ok {
			s := f.Attribute(storage.StringAttribute, ref.name)
			tests := []string{"(" + s + " IS NULL OR " + s + " = '')"}
			for _, t := range dynamicParts[1:] {
				tests = append(tests, f.Attribute(t, ref.name)+" IS NULL")
			}
			return "(" + strings.Join(tests, " AND ") + ")"
		}
		v := writeValue(x, f)
		return "(" + v + " IS NULL OR struct_extract(" + v + ", 's') = '')"
	}
	return "(" + writeValue(x, f) + " IS NULL)"
}

// writeValue writes an expression as a value. A condition's value is true or
// false, never NULL.
func writeValue(x expr, f fields) string {
	switch x := x.(type) {
	case fieldRef:
		if x.builtIn {
			return f.BuiltIn(x.name)
		}
		if t, ok := x.t.attributeType(); ok {
			return f.Attribute(t, x.name)
		}
		parts := make([]string, len(dynamicParts))
		for i, t := range dynamicParts {
			parts[i] = f.Attribute(t, x.name)
		}
		return dynamicValue(parts)
	case columnRef:
		return sqltext.Identifier(x.name)
	case attributeText:
		return "coalesce(" + f.Attribute(storage.StringAttribute, x.name) + ", " + f.Attribute(storage.JSONAttribute, x.name) + ")"
	case partOf:
		t, _ := x.t.attributeType()
		return "struct_extract(" + writeValue(x.x, f) + ", " + sqltext.String(dynamicField(t)) + ")"
	case literal:
		return writeLiteral(x.Literal)
	case compare, isEmpty, and, or, not:
		return "coalesce(" + writeCondition(x, f, false) + ", false)"
	case textMatch:
		return writeTextMatch(x, f)
	case arith:
		return "(" + writeValue(x.x, f) + " " + x.op + " " + writeValue(x.y, f) + ")"
	case negative:
		return "(-" + writeValue(x.x, f) + ")"
	case call:
		return writeCall(x, f)
	case aggregate:
		return writeAggregate(x, f)
	}
	panic(fmt.Sprintf("logquery: unknown expression %T", x))
}

// dynamicValue writes the parts of an Attribute read without a type, in
// dynamicParts' order, as one struct value: NULL where the Log lacks the
// Attribute, else the part of its type set and the rest NULL.
func dynamicValue(parts []string) string {
	missing := make([]string, len(parts))
	fields := make([]string, len(parts))
	for i, part := range parts {
		missing[i] = part + " IS NULL"
		fields[i] = sqltext.String(dynamicField(dynamicParts[i])) + ": " + part
	}
	return fmt.Sprintf("CASE WHEN %s THEN NULL ELSE {%s} END", strings.Join(missing, " AND "), strings.Join(fields, ", "))
}

// writeTextMatch writes a text match with ClickHouse's string functions.
// Where case doesn't count, both sides are lowercased by lowerUTF8, or the
// pattern is looked for with a case-insensitive position.
func writeTextMatch(m textMatch, f fields) string {
	x, term := writeValue(m.x, f), chsql.String(m.pattern)
	switch {
	case m.op == matchRegex:
		return "match(" + x + ", " + term + ")"
	case m.op == matchContains && m.caseSensitive:
		return "position(" + x + ", " + term + ") > 0"
	case m.op == matchContains:
		return caseInsensitivePosition(x, m.pattern) + " > 0"
	}
	if !m.caseSensitive {
		x, term = "lowerUTF8("+x+")", "lowerUTF8("+term+")"
	}
	switch m.op {
	case matchStartsWith:
		return "startsWith(" + x + ", " + term + ")"
	case matchEquals:
		return x + " = " + term
	}
	panic(fmt.Sprintf("logquery: unknown text match %d", m.op))
}

// caseInsensitivePosition writes where pattern starts in x, ignoring case,
// from 1, or 0 if x doesn't hold it. An ASCII pattern takes the ASCII form,
// which is faster: only ASCII bytes can match it.
func caseInsensitivePosition(x, pattern string) string {
	for _, r := range pattern {
		if r >= utf8.RuneSelf {
			return "positionCaseInsensitiveUTF8(" + x + ", " + chsql.String(pattern) + ")"
		}
	}
	return "positionCaseInsensitive(" + x + ", " + chsql.String(pattern) + ")"
}

func writeCall(c call, f fields) string {
	arg := func(i int) string { return writeValue(c.args[i], f) }
	switch c.fn {
	case "tostring":
		return writeToString(c.args[0], f)
	case "tolower":
		return "lowerUTF8(" + arg(0) + ")"
	case "toupper":
		return "upperUTF8(" + arg(0) + ")"
	case "strlen":
		return "toFloat64(lengthUTF8(" + arg(0) + "))"
	case "todouble", "toreal":
		return writeToNumber(c.args[0], f)
	case "toint", "tolong":
		return "trunc(" + writeToNumber(c.args[0], f) + ")"
	case "bin", "floor":
		if c.args[0].typ() == timeType {
			// Bins of microseconds start at the epoch, whatever their size.
			return "toStartOfInterval(" + arg(0) + ", " + arg(1) + ")"
		}
		return "(floor(" + arg(0) + " / " + arg(1) + ") * " + arg(1) + ")"
	}
	panic("logquery: unknown function " + c.fn)
}

// writeToString writes x as a string, as KQL's tostring does: a number
// without a fraction has no decimal point.
func writeToString(x expr, f fields) string {
	switch x.typ() {
	case stringType:
		return writeValue(x, f)
	case numberType:
		return numberText(writeValue(x, f))
	case boolType:
		return "if(" + writeValue(x, f) + ", 'true', 'false')"
	case timeType:
		return "formatDateTime(" + writeValue(x, f) + ", '%Y-%m-%dT%H:%i:%S.%fZ')"
	case dynamicType:
		if ref, ok := x.(fieldRef); ok {
			return fmt.Sprintf("coalesce(%s, %s, toString(%s), %s)",
				f.Attribute(storage.StringAttribute, ref.name), numberText(f.Attribute(storage.NumberAttribute, ref.name)),
				f.Attribute(storage.BoolAttribute, ref.name), f.Attribute(storage.JSONAttribute, ref.name))
		}
		v := writeValue(x, f)
		return fmt.Sprintf("coalesce(struct_extract(%[1]s, 's'), %[2]s, CAST(struct_extract(%[1]s, 'b') AS VARCHAR), struct_extract(%[1]s, 'j'))",
			v, numberText("struct_extract("+v+", 'n')"))
	}
	return "toString(" + writeValue(x, f) + ")"
}

func numberText(n string) string {
	return fmt.Sprintf("if(%[1]s = trunc(%[1]s) AND abs(%[1]s) < 1e15, toString(toInt64(%[1]s)), toString(%[1]s))", n)
}

// writeToNumber writes x as a number: a string is read as one, NULL if it
// isn't.
func writeToNumber(x expr, f fields) string {
	switch x.typ() {
	case numberType:
		return writeValue(x, f)
	case stringType:
		return "toFloat64OrNull(" + writeValue(x, f) + ")"
	case boolType:
		return "toFloat64(" + writeValue(x, f) + ")"
	case dynamicType:
		if ref, ok := x.(fieldRef); ok {
			return fmt.Sprintf("coalesce(%s, toFloat64OrNull(%s), toFloat64(%s))",
				f.Attribute(storage.NumberAttribute, ref.name), f.Attribute(storage.StringAttribute, ref.name),
				f.Attribute(storage.BoolAttribute, ref.name))
		}
		v := writeValue(x, f)
		return fmt.Sprintf("coalesce(struct_extract(%[1]s, 'n'), TRY_CAST(struct_extract(%[1]s, 's') AS DOUBLE), CAST(struct_extract(%[1]s, 'b') AS DOUBLE))", v)
	}
	return "toFloat64OrNull(toString(" + writeValue(x, f) + "))"
}

func writeAggregate(a aggregate, f fields) string {
	switch a.fn {
	case "count":
		return "count(*)"
	case "rate":
		// Every row of a group is in the same bin, so min() reads the
		// bin's seconds in range.
		return "coalesce(count(*) / min(" + binSecondsColumn + "), 0)"
	case "peak_rate":
		return "coalesce(max(" + secondCountColumn + "), 0)"
	case "countif":
		return "count_if(" + writeValue(a.x, f) + ")"
	case "dcount":
		return "approx_count_distinct(" + writeValue(a.x, f) + ")"
	case "percentile":
		return "approx_quantile(" + writeValue(a.x, f) + ", " + strconv.FormatFloat(a.percent/100, 'g', -1, 64) + ")"
	}
	return a.fn + "(" + writeValue(a.x, f) + ")"
}

func writeLiteral(l Literal) string {
	switch l.Kind {
	case parser.NumberLiteral:
		return "toFloat64(" + strconv.FormatFloat(l.Number, 'g', -1, 64) + ")"
	case parser.BoolLiteral:
		return strconv.FormatBool(l.Bool)
	case parser.DateTimeLiteral:
		return chsql.Time(l.Time)
	case parser.TimespanLiteral:
		return "toIntervalMicrosecond(" + strconv.FormatInt(l.Timespan.Microseconds(), 10) + ")"
	default:
		return chsql.String(l.String)
	}
}

// writeSearch writes a case-insensitive contains over message and every
// string Attribute's value.
func writeSearch(term string, cols Columns) string {
	return caseInsensitivePosition(cols.BuiltIn("message"), term) + " > 0 OR " +
		caseInsensitivePosition(cols.SearchText(), term) + " > 0"
}
