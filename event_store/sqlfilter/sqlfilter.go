// Package sqlfilter finds the Dashboard filters a SQL query names and writes
// their values as ClickHouse query parameters (ADR 0025).
//
// SQL names a filter as a query parameter, {region:Array(String)}, and
// ClickHouse binds the value on the server, so no value goes into the SQL
// text. The type says how the filter's values are written:
//
//   - Array(String): every value, as an array. All is [].
//   - String: the values joined by one space. All is ”.
//   - Nullable(String): as String, but All is NULL.
//
// A name the request gives no values for is All.
package sqlfilter

import (
	"fmt"
	"strings"

	"github.com/solidtrace/event_store/pkg/chsql"
)

// Type is a type SQL may give a Dashboard filter.
type Type string

// The types of a Dashboard filter in SQL.
const (
	Array    Type = "Array(String)"
	Text     Type = "String"
	Nullable Type = "Nullable(String)"
)

// typesText lists the types for an error.
const typesText = "Array(String), String or Nullable(String)"

// Filter is a Dashboard filter that a SQL query names, and its type there.
type Filter struct {
	Name string
	Type Type
}

// Scan returns the Dashboard filters that sql names, in the order of first
// use. Parameters inside string literals, quoted identifiers, heredocs and
// comments are not parameters. A parameter of another type, or a name with
// two types, is an error that says what to write.
func Scan(sql string) ([]Filter, error) {
	var filters []Filter
	types := map[string]Type{}
	for i := 0; i < len(sql); {
		if end := skip(sql, i); end > i {
			i = end
			continue
		}
		if sql[i] != '{' {
			i++
			continue
		}
		name, kind, end, ok := parameter(sql, i)
		if !ok {
			i++
			continue
		}
		i = end
		t := Type(kind)
		if t != Array && t != Text && t != Nullable {
			return nil, fmt.Errorf("{%s:%s}: a Dashboard filter's type must be %s", name, kind, typesText)
		}
		if seen, ok := types[name]; ok {
			if seen != t {
				return nil, fmt.Errorf("%s is named as %s and as %s: a Dashboard filter has one type in a SQL query, %s", name, seen, t, typesText)
			}
			continue
		}
		types[name] = t
		filters = append(filters, Filter{Name: name, Type: t})
	}
	return filters, nil
}

// Names are the names of filters, in order.
func Names(filters []Filter) []string {
	names := make([]string, len(filters))
	for i, f := range filters {
		names[i] = f.Name
	}
	return names
}

// Parameters writes each filter's values, by name, as the text of its type
// that ClickHouse reads for a query parameter. A filter with no values is
// All. Values of names that no filter has are not used.
func Parameters(filters []Filter, values map[string][]string) map[string]string {
	parameters := make(map[string]string, len(filters))
	for _, f := range filters {
		v := values[f.Name]
		switch {
		case f.Type == Array:
			quoted := make([]string, len(v))
			for i, value := range v {
				quoted[i] = chsql.String(value)
			}
			parameters[f.Name] = "[" + strings.Join(quoted, ",") + "]"
		case f.Type == Nullable && len(v) == 0:
			parameters[f.Name] = `\N`
		default:
			parameters[f.Name] = escaped.Replace(strings.Join(v, " "))
		}
	}
	return parameters
}

// escaped writes a String parameter in ClickHouse's escaped text form, so
// that a backslash, a tab or a new line stays as typed, and \N is text, not
// NULL.
var escaped = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`, "\x00", `\0`)

// skip returns where the string literal, quoted identifier, heredoc or
// comment that starts at i ends, or i when none starts there.
func skip(sql string, i int) int {
	switch c := sql[i]; {
	case c == '\'' || c == '"' || c == '`':
		return quoted(sql, i, c)
	case strings.HasPrefix(sql[i:], "--") || strings.HasPrefix(sql[i:], "# ") || strings.HasPrefix(sql[i:], "#!"):
		if end := strings.IndexByte(sql[i:], '\n'); end >= 0 {
			return i + end + 1
		}
		return len(sql)
	case strings.HasPrefix(sql[i:], "/*"):
		return comment(sql, i)
	case c == '$':
		return heredoc(sql, i)
	}
	return i
}

// quoted returns where the text quoted by q that starts at i ends. A
// backslash escapes the next character, and two quotes are one quote.
func quoted(sql string, i int, q byte) int {
	for j := i + 1; j < len(sql); j++ {
		switch sql[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(sql)
}

// comment returns where the /* comment that starts at i ends. Comments
// nest, as ClickHouse reads them.
func comment(sql string, i int) int {
	depth := 0
	for j := i; j < len(sql)-1; j++ {
		switch sql[j : j+2] {
		case "/*":
			depth++
			j++
		case "*/":
			depth--
			j++
			if depth == 0 {
				return j + 1
			}
		}
	}
	return len(sql)
}

// heredoc returns where the heredoc, $tag$…$tag$, that starts at i ends, or
// i when no heredoc starts there.
func heredoc(sql string, i int) int {
	end := strings.IndexByte(sql[i+1:], '$')
	if end < 0 {
		return i
	}
	tag := sql[i : i+end+2]
	for _, c := range tag[1 : len(tag)-1] {
		if !isWord(byte(c)) {
			return i
		}
	}
	close := strings.Index(sql[i+len(tag):], tag)
	if close < 0 {
		return len(sql)
	}
	return i + len(tag) + close + len(tag)
}

// parameter reads the query parameter {name:Type} that starts at i: its
// name, its type with no spaces, and where it ends. ok is false when the {
// starts no parameter, as in a map literal.
func parameter(sql string, i int) (name, kind string, end int, ok bool) {
	j := spaces(sql, i+1)
	start := j
	for j < len(sql) && isWord(sql[j]) {
		j++
	}
	if j == start || isDigit(sql[start]) {
		return "", "", 0, false
	}
	name = sql[start:j]
	j = spaces(sql, j)
	if j >= len(sql) || sql[j] != ':' {
		return "", "", 0, false
	}
	var b strings.Builder
	depth := 0
	for j++; j < len(sql); j++ {
		c := sql[j]
		switch {
		case c == '}' && depth == 0:
			return name, b.String(), j + 1, true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			continue
		}
		b.WriteByte(c)
	}
	return "", "", 0, false
}

func spaces(sql string, i int) int {
	for i < len(sql) && (sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r') {
		i++
	}
	return i
}

func isWord(c byte) bool {
	return c == '_' || isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
