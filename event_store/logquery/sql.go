package logquery

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/solidtrace/event_store/logquery/parser"
	"github.com/solidtrace/event_store/storage"
)

// Where writes the Query's stages as one SQL condition on a source of Logs
// whose columns cols describes, or "" if the Query keeps every Log.
//
// Conditions are two-valued, as a person reads them: a comparison with a
// Field a Log lacks is false, and its negation true. So != keeps Logs missing
// the Field, and not(a == b) is a != b.
func (q *Query) Where(cols storage.LogColumns) string {
	var conditions []string
	for _, s := range q.stages {
		switch s := s.(type) {
		case whereStage:
			conditions = append(conditions, writePredicate(s.predicate, cols, false))
		case searchStage:
			conditions = append(conditions, writeSearch(s.term, cols))
		}
	}
	if len(conditions) == 0 {
		return ""
	}
	return "(" + strings.Join(conditions, ") AND (") + ")"
}

// writePredicate writes p, or its negation when negate is set. Negations are
// pushed down to the comparisons, so that SQL's NULLs never leak out as
// "unknown": every comparison is written true or false for every Log.
func writePredicate(p predicate, cols storage.LogColumns, negate bool) string {
	switch p := p.(type) {
	case and:
		op := " AND "
		if negate {
			op = " OR "
		}
		return "(" + writePredicate(p.x, cols, negate) + op + writePredicate(p.y, cols, negate) + ")"
	case or:
		op := " OR "
		if negate {
			op = " AND "
		}
		return "(" + writePredicate(p.x, cols, negate) + op + writePredicate(p.y, cols, negate) + ")"
	case not:
		return writePredicate(p.x, cols, !negate)
	case compare:
		equal := (p.op == opEq) != negate
		return writeCompare(p.field, p.value, cols, equal)
	}
	panic(fmt.Sprintf("logquery: unknown predicate %T", p))
}

// writeCompare writes field == value when equal is set, else field != value.
// A Field's values of another type than value's never equal it.
func writeCompare(field Field, value Literal, cols storage.LogColumns, equal bool) string {
	var column string
	if field.BuiltIn {
		// Every built-in field but time (which can't be compared) is a
		// string.
		if value.Kind != parser.StringLiteral {
			return strconv.FormatBool(!equal)
		}
		column = cols.BuiltIn(field.Name)
	} else {
		column = cols.Attribute(attributeType(value.Kind), field.Name)
	}
	if equal {
		// NULL, for a Log without the Field, keeps the Log out.
		return column + " = " + writeLiteral(value)
	}
	return column + " IS DISTINCT FROM " + writeLiteral(value)
}

// attributeType is the type of Attribute a literal can equal.
func attributeType(kind parser.LiteralKind) storage.AttributeType {
	switch kind {
	case parser.NumberLiteral:
		return storage.NumberAttribute
	case parser.BoolLiteral:
		return storage.BoolAttribute
	default:
		return storage.StringAttribute
	}
}

func writeLiteral(l Literal) string {
	switch l.Kind {
	case parser.NumberLiteral:
		return "CAST(" + strconv.FormatFloat(l.Number, 'g', -1, 64) + " AS DOUBLE)"
	case parser.BoolLiteral:
		return strconv.FormatBool(l.Bool)
	default:
		return quoteString(l.String)
	}
}

// writeSearch writes a case-insensitive contains over message and every
// string Attribute.
func writeSearch(term string, cols storage.LogColumns) string {
	pattern := quoteString("%" + escapeLike(term) + "%")
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

// quoteString quotes s as a SQL string literal.
func quoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
