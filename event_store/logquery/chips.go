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
	for _, s := range q.stages {
		switch s := s.(type) {
		case whereStage:
			c, ok := s.predicate.(compare)
			if !ok {
				return nil
			}
			chips.Filters = append(chips.Filters, Chip{Field: c.field.Name, Op: string(c.op), Value: c.value.Value()})
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
