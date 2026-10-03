package logquery

import (
	"github.com/solidtrace/event_store/logquery/parser"
)

// Problem is a mistake in a Query as validate reports it: its message, and
// where it starts and ends, lines and columns counted from 1, columns in
// characters.
type Problem struct {
	Message   string `json:"message"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
}

// ProblemOf places a compile error in the Query's text.
func ProblemOf(source string, err *Error) Problem {
	start := parser.PositionOf(source, err.Span.Start)
	end := parser.PositionOf(source, err.Span.End)
	return Problem{Message: err.Message, Line: start.Line, Column: start.Column, EndLine: end.Line, EndColumn: end.Column}
}

// Chips are a Query as the Builder shows it: its filters, in order, and its
// search.
type Chips struct {
	Filters []Chip `json:"filters"`
	// Search is the search box's text, or nil for none.
	Search *string `json:"search"`
}

// Chip is one filter: field == value or field != value. Value keeps its type:
// a string, a number or a bool.
type Chip struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// Chips returns the Query's chips, or nil if the Builder can't show it: only
// a Query made of where stages that each compare one Field with a value by ==
// or !=, and at most one search, can be shown.
func (q *Query) Chips() *Chips {
	chips := &Chips{Filters: []Chip{}}
	if len(q.tail) > 0 {
		return nil
	}
	for _, s := range q.head {
		switch s := s.(type) {
		case whereStage:
			chip, ok := chipOf(s.predicate)
			if !ok {
				return nil
			}
			chips.Filters = append(chips.Filters, chip)
		case searchStage:
			if chips.Search != nil {
				return nil
			}
			term := s.term
			chips.Search = &term
		default:
			return nil
		}
	}
	return chips
}

// chipOf returns the chip a where stage's predicate is, if it is one: a Field
// compared with a string, a number or a bool by == or !=.
func chipOf(p expr) (Chip, bool) {
	c, ok := p.(compare)
	if !ok || (c.op != opEq && c.op != opNE) {
		return Chip{}, false
	}
	field, isField := c.x.(fieldRef)
	value, isLiteral := c.y.(literal)
	if !isField || !isLiteral || field.t == timeType {
		return Chip{}, false
	}
	switch value.Kind {
	case parser.StringLiteral, parser.NumberLiteral, parser.BoolLiteral:
		return Chip{Field: field.name, Op: string(c.op), Value: value.Value()}, true
	}
	return Chip{}, false
}
