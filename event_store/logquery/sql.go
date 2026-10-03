package logquery

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/solidtrace/event_store/logquery/parser"
	"github.com/solidtrace/event_store/pkg/sqltext"
	"github.com/solidtrace/event_store/storage"
)

// fields says where the rows being written against keep each Field: a
// source of Logs (storage.LogColumns), or the Logs a tail reads.
type fields interface {
	BuiltIn(name string) string
	Attribute(t storage.AttributeType, key string) string
}

// Where writes the Query's head, its leading where and search stages, as one
// SQL condition on a source of Logs whose columns cols describes, or "" if
// the head keeps every Log.
//
// Conditions are two-valued, as a person reads them: a comparison with a
// Field a Log lacks is false, and its negation true. So != keeps Logs missing
// the Field, and not(a == b) is a != b.
func (q *Query) Where(cols storage.LogColumns) string {
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
// missing, which a WHERE reads as false, and is kept plain so that DuckDB can
// push it into its scans.
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

func writeTextMatch(m textMatch, f fields) string {
	x := writeValue(m.x, f)
	like := " ILIKE "
	if m.caseSensitive {
		like = " LIKE "
	}
	switch m.op {
	case matchContains:
		return x + like + sqltext.String("%"+escapeLike(m.pattern)+"%") + ` ESCAPE '\'`
	case matchStartsWith:
		return x + like + sqltext.String(escapeLike(m.pattern)+"%") + ` ESCAPE '\'`
	case matchEquals:
		return x + like + sqltext.String(escapeLike(m.pattern)) + ` ESCAPE '\'`
	default:
		return "regexp_matches(" + x + ", " + sqltext.String(m.pattern) + ")"
	}
}

func writeCall(c call, f fields) string {
	arg := func(i int) string { return writeValue(c.args[i], f) }
	switch c.fn {
	case "tostring":
		return writeToString(c.args[0], f)
	case "tolower":
		return "lower(" + arg(0) + ")"
	case "toupper":
		return "upper(" + arg(0) + ")"
	case "strlen":
		return "CAST(length(" + arg(0) + ") AS DOUBLE)"
	case "todouble", "toreal":
		return writeToNumber(c.args[0], f)
	case "toint", "tolong":
		return "trunc(" + writeToNumber(c.args[0], f) + ")"
	case "bin", "floor":
		if c.args[0].typ() == timeType {
			return "time_bucket(" + arg(1) + ", " + arg(0) + ", TIMESTAMP '1970-01-01')"
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
	case timeType:
		return "strftime(" + writeValue(x, f) + ", '%Y-%m-%dT%H:%M:%S.%fZ')"
	case dynamicType:
		if ref, ok := x.(fieldRef); ok {
			return fmt.Sprintf("coalesce(%s, %s, CAST(%s AS VARCHAR), %s)",
				f.Attribute(storage.StringAttribute, ref.name), numberText(f.Attribute(storage.NumberAttribute, ref.name)),
				f.Attribute(storage.BoolAttribute, ref.name), f.Attribute(storage.JSONAttribute, ref.name))
		}
		v := writeValue(x, f)
		return fmt.Sprintf("coalesce(struct_extract(%[1]s, 's'), %[2]s, CAST(struct_extract(%[1]s, 'b') AS VARCHAR), struct_extract(%[1]s, 'j'))",
			v, numberText("struct_extract("+v+", 'n')"))
	}
	return "CAST(" + writeValue(x, f) + " AS VARCHAR)"
}

func numberText(n string) string {
	return fmt.Sprintf("CASE WHEN %[1]s = trunc(%[1]s) AND abs(%[1]s) < 1e15 THEN CAST(CAST(%[1]s AS BIGINT) AS VARCHAR) ELSE CAST(%[1]s AS VARCHAR) END", n)
}

// writeToNumber writes x as a number: a string is read as one, NULL if it
// isn't.
func writeToNumber(x expr, f fields) string {
	switch x.typ() {
	case numberType:
		return writeValue(x, f)
	case dynamicType:
		if ref, ok := x.(fieldRef); ok {
			return fmt.Sprintf("coalesce(%s, TRY_CAST(%s AS DOUBLE), CAST(%s AS DOUBLE))",
				f.Attribute(storage.NumberAttribute, ref.name), f.Attribute(storage.StringAttribute, ref.name),
				f.Attribute(storage.BoolAttribute, ref.name))
		}
		v := writeValue(x, f)
		return fmt.Sprintf("coalesce(struct_extract(%[1]s, 'n'), TRY_CAST(struct_extract(%[1]s, 's') AS DOUBLE), CAST(struct_extract(%[1]s, 'b') AS DOUBLE))", v)
	}
	return "TRY_CAST(" + writeValue(x, f) + " AS DOUBLE)"
}

func writeAggregate(a aggregate, f fields) string {
	switch a.fn {
	case "count":
		return "count(*)"
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
		return "CAST(" + strconv.FormatFloat(l.Number, 'g', -1, 64) + " AS DOUBLE)"
	case parser.BoolLiteral:
		return strconv.FormatBool(l.Bool)
	case parser.DateTimeLiteral:
		return "CAST(" + sqltext.String(l.Time.UTC().Format("2006-01-02 15:04:05.999999")) + " AS TIMESTAMP)"
	case parser.TimespanLiteral:
		return "to_microseconds(" + strconv.FormatInt(l.Timespan.Microseconds(), 10) + ")"
	default:
		return sqltext.String(l.String)
	}
}

// writeSearch writes a case-insensitive contains over message and every
// string Attribute.
func writeSearch(term string, cols storage.LogColumns) string {
	pattern := sqltext.String("%" + escapeLike(term) + "%")
	ilike := func(column string) string {
		return column + " ILIKE " + pattern + ` ESCAPE '\'`
	}
	matches := []string{
		ilike(cols.BuiltIn("message")),
		fmt.Sprintf("coalesce(list_bool_or(list_transform(map_values(%s), lambda v: %s)), false)",
			cols.Map(storage.StringAttribute), ilike("v")),
	}
	for _, column := range cols.Promoted(storage.StringAttribute) {
		matches = append(matches, ilike(column))
	}
	return strings.Join(matches, " OR ")
}

// escapeLike escapes LIKE's wildcards in s, with backslash as the escape.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
