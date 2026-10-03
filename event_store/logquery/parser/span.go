package parser

import "fmt"

// A Span is a run of bytes of a Query's text: [Start, End).
type Span struct {
	Start int
	End   int
}

func newSpan(start, end int) Span {
	return Span{Start: start, End: end}
}

func nullSpan() Span {
	return Span{Start: -1, End: -1}
}

// IsValid reports whether the span points into the text.
func (s Span) IsValid() bool {
	return s.Start >= 0 && s.End >= s.Start
}

func (s Span) String() string {
	return fmt.Sprintf("[%d,%d)", s.Start, s.End)
}

func unionSpans(spans ...Span) Span {
	u := nullSpan()
	for _, s := range spans {
		switch {
		case !s.IsValid():
		case !u.IsValid():
			u = s
		default:
			u = newSpan(min(u.Start, s.Start), max(u.End, s.End))
		}
	}
	return u
}

// Position is a place in a Query's text as a person counts it: both from 1,
// the column in characters.
type Position struct {
	Line   int
	Column int
}

// PositionOf returns the line and column of the byte offset in source.
func PositionOf(source string, offset int) Position {
	offset = max(0, min(offset, len(source)))
	pos := Position{Line: 1, Column: 1}
	for _, r := range source[:offset] {
		if r == '\n' {
			pos.Line++
			pos.Column = 1
		} else {
			pos.Column++
		}
	}
	return pos
}
