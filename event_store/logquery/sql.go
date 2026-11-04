package logquery

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/solidtrace/event_store/logquery/parser"
	"github.com/solidtrace/event_store/pkg/chsql"
	"github.com/solidtrace/event_store/storage"
)

// fields says where the rows being written against keep each Field and
// column: a source of Logs (Columns), or a relation of a tail's chain.
type fields interface {
	// Column is the rows' column of the given name, such as a table's or
	// one extend added.
	Column(name string) string
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
			conditions = append(conditions, writeSearch(s.terms, cols))
		}
	}
	if len(conditions) == 0 {
		return ""
	}
	return "(" + strings.Join(conditions, ") AND (") + ")"
}

// Filter writes the Query's head as Where does, in two parts: the tests that
// read message, search among them, and the rest. A read can test the rest
// first, and read message only for the Logs that pass them: left to itself,
// ClickHouse 26.8 may read all of message first.
func (q *Query) Filter(cols Columns) storage.Filter {
	var rest, message []string
	for _, s := range q.head {
		switch s := s.(type) {
		case whereStage:
			for _, test := range conjuncts(s.predicate) {
				if readsMessage(test) {
					message = append(message, writeCondition(test, cols, false))
				} else {
					rest = append(rest, writeCondition(test, cols, false))
				}
			}
		case searchStage:
			message = append(message, writeSearch(s.terms, cols))
		}
	}
	all := func(conditions []string) string {
		if len(conditions) == 0 {
			return ""
		}
		return "(" + strings.Join(conditions, ") AND (") + ")"
	}
	return storage.Filter{Rest: all(rest), Message: all(message)}
}

// conjuncts are the conditions all of which p is: those it ands.
func conjuncts(p expr) []expr {
	if p, ok := p.(and); ok {
		return append(conjuncts(p.x), conjuncts(p.y)...)
	}
	return []expr{p}
}

// readsMessage reports whether x reads a Log's message.
func readsMessage(x expr) bool {
	switch x := x.(type) {
	case fieldRef:
		return x.builtIn && x.name == "message"
	case partOf:
		return readsMessage(x.x)
	case compare:
		return readsMessage(x.x) || readsMessage(x.y)
	case textMatch:
		return readsMessage(x.x)
	case isEmpty:
		return readsMessage(x.x)
	case and:
		return readsMessage(x.x) || readsMessage(x.y)
	case or:
		return readsMessage(x.x) || readsMessage(x.y)
	case not:
		return readsMessage(x.x)
	case arith:
		return readsMessage(x.x) || readsMessage(x.y)
	case negative:
		return readsMessage(x.x)
	case call:
		for _, arg := range x.args {
			if readsMessage(arg) {
				return true
			}
		}
	case member:
		return readsMessage(x.x)
	case jsonValueText:
		return readsMessage(x.x)
	}
	return false
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
	var test string
	if m, ok := p.(textMatch); ok {
		test = writeTextMatch(m, f)
	} else {
		test = writeValue(p, f)
	}
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
		part := partsOf(x, f)
		s := part(storage.StringAttribute)
		tests := []string{"(" + s + " IS NULL OR " + s + " = '')"}
		for _, t := range dynamicParts[1:] {
			tests = append(tests, part(t)+" IS NULL")
		}
		return "(" + strings.Join(tests, " AND ") + ")"
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
		return f.Column(x.name)
	case attributeText:
		return "coalesce(" + f.Attribute(storage.StringAttribute, x.name) + ", " + f.Attribute(storage.JSONAttribute, x.name) + ")"
	case partOf:
		t, _ := x.t.attributeType()
		return partsOf(x.x, f)(t)
	case member:
		return dynamicOf(partsOf(x, f))
	case jsonValueText:
		part := partsOf(x.x, f)
		return "coalesce(" + part(storage.StringAttribute) + ", " + part(storage.JSONAttribute) + ")"
	case literal:
		return writeLiteral(x.Literal)
	case compare, isEmpty, and, or, not, textMatch:
		// ClickHouse's tests are UInt8s; a value is a Bool.
		return "toBool(coalesce(" + writeCondition(x, f, false) + ", false))"
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
// dynamicParts' order, as one tuple: the part of its type set and the rest
// NULL, or every part NULL where the Log lacks the Attribute, since
// ClickHouse has no NULL tuple.
func dynamicValue(parts []string) string {
	return "tuple(" + strings.Join(parts, ", ") + ")"
}

// dynamicOf writes a dynamic value from its parts by type.
func dynamicOf(part func(storage.AttributeType) string) string {
	parts := make([]string, len(dynamicParts))
	for i, t := range dynamicParts {
		parts[i] = part(t)
	}
	return dynamicValue(parts)
}

// dynamicPart writes the part of v, a dynamic value, that holds values of
// type t.
func dynamicPart(v string, t storage.AttributeType) string {
	for i, part := range dynamicParts {
		if part == t {
			return fmt.Sprintf("tupleElement(%s, %d)", v, i+1)
		}
	}
	panic("logquery: no dynamic part of type " + string(t))
}

// dynamicMissing writes a test, true or false for every row, that v, a
// dynamic value, is missing: no part is set.
func dynamicMissing(v string) string {
	tests := make([]string, len(dynamicParts))
	for i, t := range dynamicParts {
		tests[i] = dynamicPart(v, t) + " IS NULL"
	}
	return "(" + strings.Join(tests, " AND ") + ")"
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
	case "extract":
		return writeExtract(c, f)
	case "substring":
		return writeSubstring(c, f)
	case "round":
		return writeRound(c, f)
	case "parse_json":
		return dynamicOf(partsOf(c, f))
	case "array_length":
		j := jsonText(c.args[0], f)
		return "if(JSONType(" + j + ") = 'Array', toFloat64(JSONLength(" + j + ")), NULL)"
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
		part := partsOf(x, f)
		return fmt.Sprintf("coalesce(%s, %s, toString(%s), %s)", part(storage.StringAttribute),
			numberText(part(storage.NumberAttribute)), part(storage.BoolAttribute), part(storage.JSONAttribute))
	}
	return "toString(" + writeValue(x, f) + ")"
}

// partsOf returns the parts of x, a dynamic value, by type: an Attribute's
// typed values, those of the JSON value parse_json reads or a path reaches,
// or the elements of a dynamic column's tuple.
func partsOf(x expr, f fields) func(storage.AttributeType) string {
	switch x := x.(type) {
	case fieldRef:
		return func(t storage.AttributeType) string { return f.Attribute(t, x.name) }
	case member:
		v := writeMember(x, f)
		return jsonParts(v, v)
	case call:
		if x.fn == "parse_json" {
			return parsedParts(writeValue(x.args[0], f))
		}
	}
	v := writeValue(x, f)
	return func(t storage.AttributeType) string { return dynamicPart(v, t) }
}

// jsonParts returns the parts by type of the value that v, JSON text, holds:
// its string, number or bool, or raw, the JSON text of an array or object.
// The others are NULL, and all are where v holds null or isn't JSON. Each
// is read only once its type is known, since ClickHouse reads a string such
// as "12" as a number.
func jsonParts(v, raw string) func(storage.AttributeType) string {
	is := "JSONType(" + v + ")"
	return func(t storage.AttributeType) string {
		switch t {
		case storage.StringAttribute:
			return "if(" + is + " = 'String', JSONExtractString(" + v + "), NULL)"
		case storage.NumberAttribute:
			return "if(" + is + " IN ('Int64', 'UInt64', 'Double'), JSONExtractFloat(" + v + "), NULL)"
		case storage.BoolAttribute:
			return "if(" + is + " = 'Bool', toBool(JSONExtractBool(" + v + ")), NULL)"
		}
		return "if(" + is + " IN ('Array', 'Object'), " + raw + ", NULL)"
	}
}

// parsedParts returns the parts by type of parse_json(s): those of the JSON
// value s holds, an array or object as its JSON text without spaces, or, as
// KQL has it, s itself as a string where s isn't JSON.
func parsedParts(s string) func(storage.AttributeType) string {
	parts := jsonParts(s, "JSONExtractRaw("+s+")")
	return func(t storage.AttributeType) string {
		if t == storage.StringAttribute {
			return "if(isValidJSON(" + s + "), " + parts(t) + ", " + s + ")"
		}
		return parts(t)
	}
}

// jsonText writes the JSON text of x, a dynamic value, that a path reaches
// into: the text parse_json reads, the value a path reached, or an array's
// or object's JSON text. Anything else holds no member or element.
func jsonText(x expr, f fields) string {
	switch x := x.(type) {
	case call:
		if x.fn == "parse_json" {
			return writeValue(x.args[0], f)
		}
	case member:
		return writeMember(x, f)
	}
	return partsOf(x, f)(storage.JSONAttribute)
}

// writeMember writes the JSON text of the value at m's path, or ” where
// there is none. Each name and index on the path is one argument of
// ClickHouse's JSON functions, never part of a path's text. An index reaches
// only into an array, as ClickHouse's would take an object's nth member too.
func writeMember(m member, f fields) string {
	args := []string{jsonText(m.x, f)}
	var arrays []string
	for _, step := range m.path {
		if !step.element {
			args = append(args, chsql.String(step.key))
			continue
		}
		arrays = append(arrays, "JSONType("+strings.Join(args, ", ")+") = 'Array'")
		// ClickHouse counts elements from 1, and from the end from -1.
		index := step.index
		if index >= 0 {
			index++
		}
		args = append(args, strconv.Itoa(index))
	}
	raw := "JSONExtractRaw(" + strings.Join(args, ", ") + ")"
	if len(arrays) == 0 {
		return raw
	}
	return "if(" + strings.Join(arrays, " AND ") + ", " + raw + ", '')"
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
		part := partsOf(x, f)
		return fmt.Sprintf("coalesce(%s, toFloat64OrNull(%s), toFloat64(%s))",
			part(storage.NumberAttribute), part(storage.StringAttribute), part(storage.BoolAttribute))
	}
	return "toFloat64OrNull(toString(" + writeValue(x, f) + "))"
}

// writeExtract writes extract(regex, group, source): what the group matched
// at the regex's first match. No match, an empty one, and a group the regex
// doesn't have give no value, as KQL's null, which for a string is its
// empty string.
func writeExtract(c call, f fields) string {
	regex, group := c.args[0].(literal).String, c.args[1].(literal).Number
	if group > float64(regexp.MustCompile(regex).NumSubexp()) {
		return "CAST(NULL AS Nullable(String))"
	}
	return fmt.Sprintf("nullIf(regexpExtract(%s, %s, %d), '')", writeValue(c.args[2], f), chsql.String(regex), int(group))
}

// writeSubstring writes substring(source, start [, length]) in characters.
// KQL's start counts from 0, or back from the end when negative, and never
// before the first character; ClickHouse's counts from 1. A range past the
// end stops there, and a length below zero takes nothing.
func writeSubstring(c call, f fields) string {
	s := writeValue(c.args[0], f)
	var from string
	if l, ok := c.args[1].(literal); ok && l.Number >= 0 && l.Number < 1<<53 {
		from = strconv.FormatInt(int64(l.Number)+1, 10)
	} else {
		start := "toInt64(" + writeValue(c.args[1], f) + ")"
		from = fmt.Sprintf("if(%[2]s < 0, greatest(toInt64(lengthUTF8(%[1]s)) + %[2]s, 0), %[2]s) + 1", s, start)
	}
	if len(c.args) == 2 {
		return "substringUTF8(" + s + ", " + from + ")"
	}
	return "substringUTF8(" + s + ", " + from + ", greatest(toInt64(" + writeValue(c.args[2], f) + "), 0))"
}

// writeRound writes round(number [, precision]) as KQL rounds: to precision
// digits after the point, or before it when negative, with halves away from
// zero, where ClickHouse's round takes a Float64's halves to even. The
// number is scaled by an exact power of ten, multiplied for digits after the
// point and divided for digits before it, so that 2.15 to one digit scales
// to 21.5 and rounds to 2.2, as in KQL. Scaled to 2^52 or more, it has no
// fraction left to round.
func writeRound(c call, f fields) string {
	x, digits := writeValue(c.args[0], f), "0"
	if len(c.args) == 2 {
		digits = "trunc(" + writeValue(c.args[1], f) + ")"
	}
	scale := "pow(10, least(abs(" + digits + "), 308))"
	scaled := fmt.Sprintf("if(%[1]s < 0, %[2]s / %[3]s, %[2]s * %[3]s)", digits, x, scale)
	rounded := fmt.Sprintf("(trunc(%[1]s) + sign(%[1]s) * (abs(%[1]s - trunc(%[1]s)) >= 0.5))", scaled)
	return fmt.Sprintf("if(abs(%[1]s) >= 4503599627370496, %[2]s, if(%[3]s < 0, %[4]s * %[5]s, %[4]s / %[5]s))",
		scaled, x, digits, rounded, scale)
}

// writeAggregate writes one of summarize's functions. Over no rows, or no
// values, sum, avg, min, max and percentile are NULL, as SQL has them, rather
// than ClickHouse's 0 or 1970: their -OrNull forms. dcount counts the
// values present, approximately past some thousands; percentile is a
// t-digest's estimate.
func writeAggregate(a aggregate, f fields) string {
	switch a.fn {
	case "count":
		return "count()"
	case "rate":
		// Every row of a group is in the same bin, so min() reads the
		// bin's seconds in range: none in an empty range.
		return "coalesce(count() / nullIf(minOrNull(" + f.Column(binSecondsColumn) + "), 0), 0)"
	case "peak_rate":
		return "coalesce(maxOrNull(" + f.Column(secondCountColumn) + "), 0)"
	case "countif":
		return "countIf(" + writeValue(a.x, f) + ")"
	case "dcount":
		v := writeValue(a.x, f)
		if a.x.typ() == dynamicType {
			return "uniqIf(" + v + ", NOT " + dynamicMissing(v) + ")"
		}
		return "uniq(" + v + ")"
	case "percentile":
		return "toFloat64(quantileTDigestOrNull(" + strconv.FormatFloat(a.percent/100, 'g', -1, 64) + ")(" + writeValue(a.x, f) + "))"
	}
	return a.fn + "OrNull(" + writeValue(a.x, f) + ")"
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

// writeSearch writes a case-insensitive contains of any of terms over
// message and every string Attribute's value.
func writeSearch(terms []string, cols Columns) string {
	var matches []string
	for _, term := range terms {
		matches = append(matches,
			caseInsensitivePosition(cols.BuiltIn("message"), term)+" > 0",
			caseInsensitivePosition(cols.SearchText(), term)+" > 0")
	}
	return strings.Join(matches, " OR ")
}
