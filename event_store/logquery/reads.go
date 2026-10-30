package logquery

import (
	"slices"
	"strings"

	"github.com/solidtrace/event_store/logquery/parser"
)

// What a Query reads, for sizing it before it runs from the counts that
// Source counts and the counts by minute keep: which Services and files its
// leading filters keep, and whether it matches text in Logs' messages.

// ReadScope is the Services and files a Query's leading where stages keep,
// as their conditions joined by and pick them with == and in. Services and
// Files are nil for any; empty, the Query keeps none. No other condition
// narrows it, as ClickHouse reads the rows to test them.
type ReadScope struct {
	Services, Files []string
}

// Keeps reports whether the scope keeps a Source's Logs.
func (s ReadScope) Keeps(service, file string) bool {
	return (s.Services == nil || slices.Contains(s.Services, service)) &&
		(s.Files == nil || slices.Contains(s.Files, file))
}

// OnFile is the scope narrowed to one file.
func (s ReadScope) OnFile(file string) ReadScope {
	s.Files = intersect(s.Files, []string{file})
	return s
}

// Scope is the Services and files the Query's leading filters keep.
func (q *Query) Scope() ReadScope {
	var scope ReadScope
	for _, s := range q.head {
		w, ok := s.(whereStage)
		if !ok {
			continue
		}
		for _, condition := range conjuncts(w.predicate) {
			if values, ok := equalsAny(condition, "service", true); ok {
				scope.Services = intersect(scope.Services, values)
			} else if values, ok := equalsAny(condition, "file", false); ok {
				scope.Files = intersect(scope.Files, values)
			}
		}
	}
	return scope
}

// equalsAny reads a condition that keeps the Logs whose Field name, built in
// or an Attribute, is one of some strings: name == "a", or such comparisons
// joined by or, as in compiles to. ok is false for any other condition.
func equalsAny(p expr, name string, builtIn bool) (values []string, ok bool) {
	switch p := p.(type) {
	case or:
		x, ok := equalsAny(p.x, name, builtIn)
		if !ok {
			return nil, false
		}
		y, ok := equalsAny(p.y, name, builtIn)
		return append(x, y...), ok
	case compare:
		field, value := p.x, p.y
		if _, ok := field.(literal); ok {
			field, value = value, field
		}
		ref, isRef := field.(fieldRef)
		lit, isLit := value.(literal)
		if p.op != opEq || !isRef || ref.name != name || ref.builtIn != builtIn || !isLit || lit.Kind != parser.StringLiteral {
			return nil, false
		}
		return []string{lit.String}, true
	}
	return nil, false
}

// intersect is the values in both a and b, nil standing for any value.
func intersect(a, b []string) []string {
	if a == nil {
		return slices.Clone(b)
	}
	if b == nil {
		return a
	}
	both := []string{}
	for _, v := range a {
		if slices.Contains(b, v) && !slices.Contains(both, v) {
			both = append(both, v)
		}
	}
	return both
}

// MatchesText reports whether the Query matches text in Logs' messages, so
// that it reads the text of every Log it scans: it has a search, or tests
// message with contains, startswith, =~, matches regex or their negations,
// anywhere in the Query.
func (q *Query) MatchesText() bool {
	for _, s := range slices.Concat(q.head, q.tail) {
		var exprs []expr
		switch s := s.(type) {
		case searchStage:
			return true
		case whereStage:
			exprs = []expr{s.predicate}
		case projectStage:
			exprs = valuesOf(s.columns)
		case extendStage:
			exprs = valuesOf(s.columns)
		case summarizeStage:
			exprs = valuesOf(slices.Concat(s.by, s.aggregates))
		case distinctStage:
			exprs = valuesOf(s.columns)
		case sortStage:
			for _, k := range s.keys {
				exprs = append(exprs, k.x)
			}
		case topStage:
			exprs = []expr{s.key.x}
		}
		for _, x := range exprs {
			if anyOf(x, isMessageMatch) {
				return true
			}
		}
	}
	return false
}

func valuesOf(columns []namedValue) []expr {
	exprs := make([]expr, len(columns))
	for i, col := range columns {
		exprs[i] = col.x
	}
	return exprs
}

// isMessageMatch reports whether x tests message against a pattern.
func isMessageMatch(x expr) bool {
	m, ok := x.(textMatch)
	return ok && readsMessage(m.x)
}

// anyOf reports whether f holds for x or any expression within it.
func anyOf(x expr, f func(expr) bool) bool {
	if x == nil {
		return false
	}
	if f(x) {
		return true
	}
	switch x := x.(type) {
	case compare:
		return anyOf(x.x, f) || anyOf(x.y, f)
	case textMatch:
		return anyOf(x.x, f)
	case isEmpty:
		return anyOf(x.x, f)
	case and:
		return anyOf(x.x, f) || anyOf(x.y, f)
	case or:
		return anyOf(x.x, f) || anyOf(x.y, f)
	case not:
		return anyOf(x.x, f)
	case arith:
		return anyOf(x.x, f) || anyOf(x.y, f)
	case negative:
		return anyOf(x.x, f)
	case partOf:
		return anyOf(x.x, f)
	case call:
		return slices.ContainsFunc(x.args, func(arg expr) bool { return anyOf(arg, f) })
	case aggregate:
		return anyOf(x.x, f)
	}
	return false
}

// OnFile is a Query's text with a where file == "<file>" first, after its
// leading source if it has one, so that it reads only that file's Logs. The
// text must compile.
func OnFile(text, file string) string {
	filter := "where file == " + quote(file)
	tree, err := parser.Parse(text)
	if err == nil && tree.Source != nil {
		// The source's span is all of it: logs, or ['slug'].
		end := tree.Source.NameSpan.End
		return text[:end] + " | " + filter + text[end:]
	}
	rest := strings.TrimSpace(text)
	switch {
	case rest == "":
		return filter
	case strings.HasPrefix(rest, "|"):
		return filter + " " + rest
	}
	return filter + " | " + rest
}

// quote writes s as a string a Query reads back as s.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
