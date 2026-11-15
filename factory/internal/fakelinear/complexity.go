package fakelinear

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// MaxComplexity is the most complexity points that Linear accepts for
// one query. Linear refuses a query above it.
const MaxComplexity = 10_000

// Complexity gives the complexity points of a GraphQL operation by
// Linear's documented rules: a property costs 0.1 point, an object costs
// 1 point, and a connection multiplies the points of its children by its
// first argument (50 when it has none). A field with a nodes child is a
// connection. The answer is in tenths of a point, to keep it exact.
func Complexity(query string) (int, error) {
	p := &parser{tokens: tokens(query)}
	// Skip the operation's name and variables, to its selection set.
	for depth := 0; ; {
		t, ok := p.next()
		if !ok {
			return 0, fmt.Errorf("the query has no selection set")
		}
		switch t {
		case "(":
			depth++
		case ")":
			depth--
		}
		if t == "{" && depth == 0 {
			break
		}
	}
	cost, _, err := p.set()
	return cost, err
}

type parser struct {
	tokens []string
	at     int
}

func (p *parser) next() (string, bool) {
	if p.at >= len(p.tokens) {
		return "", false
	}
	p.at++
	return p.tokens[p.at-1], true
}

func (p *parser) peek() string {
	if p.at >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.at]
}

// set reads a selection set after its "{", up to its "}". It gives the
// points (in tenths) and the names of its fields.
func (p *parser) set() (int, []string, error) {
	cost := 0
	var names []string
	for {
		name, ok := p.next()
		if !ok {
			return 0, nil, fmt.Errorf("a selection set has no end")
		}
		if name == "}" {
			return cost, names, nil
		}
		if p.peek() == ":" { // an alias
			p.next()
			if name, ok = p.next(); !ok {
				return 0, nil, fmt.Errorf("an alias has no field")
			}
		}
		names = append(names, name)
		first := -1
		if p.peek() == "(" {
			p.next()
			var err error
			if first, err = p.arguments(); err != nil {
				return 0, nil, err
			}
		}
		if p.peek() != "{" {
			cost++ // a property: 0.1 point
			continue
		}
		p.next()
		children, childNames, err := p.set()
		if err != nil {
			return 0, nil, err
		}
		multiplier := 1
		switch {
		case first >= 0:
			multiplier = first
		case contains(childNames, "nodes"):
			multiplier = 50
		}
		cost += 10 + multiplier*children // an object: 1 point
	}
}

// arguments reads a field's arguments after its "(", up to its ")". It
// gives the value of first (-1 is none).
func (p *parser) arguments() (int, error) {
	first := -1
	for depth := 0; ; {
		t, ok := p.next()
		if !ok {
			return 0, fmt.Errorf("the arguments have no end")
		}
		switch t {
		case "(", "{", "[":
			depth++
		case "]", "}":
			depth--
		case ")":
			if depth == 0 {
				return first, nil
			}
			depth--
		case "first":
			if depth == 0 && p.peek() == ":" {
				p.next()
				value, _ := p.next()
				n, err := strconv.Atoi(value)
				if err != nil {
					return 0, fmt.Errorf("first is %q, not a number", value)
				}
				first = n
			}
		}
	}
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// tokens splits a query into names, numbers, variables and punctuation.
// Commas are white space in GraphQL; a string is one token.
func tokens(query string) []string {
	var out []string
	runes := []rune(query)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.IsSpace(r) || r == ',':
			i++
		case r == '"':
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				if runes[j] == '\\' {
					j++
				}
				j++
			}
			out = append(out, string(runes[i:min(j+1, len(runes))]))
			i = j + 1
		case r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-':
			j := i
			for j < len(runes) && (runes[j] == '_' || runes[j] == '$' || unicode.IsLetter(runes[j]) ||
				unicode.IsDigit(runes[j]) || runes[j] == '-' || runes[j] == '.') {
				j++
			}
			out = append(out, string(runes[i:j]))
			i = j
		default:
			out = append(out, string(r))
			i++
		}
	}
	return out
}

// complexityError is Linear's refusal of a query above MaxComplexity.
func complexityError(tenths int) string {
	return fmt.Sprintf("Query too complex: the complexity is %s points, and the most is %d",
		strings.TrimSuffix(strconv.FormatFloat(float64(tenths)/10, 'f', 1, 64), ".0"), MaxComplexity)
}
