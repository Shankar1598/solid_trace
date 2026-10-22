package logquery

import (
	"strconv"
	"strings"

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

// Chips are a Query as the Builder shows it: its filters, in order, its
// search, and the summarize, sort and take after them.
type Chips struct {
	Filters []Chip `json:"filters"`
	// Search is the search box's text, or nil for none.
	Search *string `json:"search"`
	// SearchFilter is the Dashboard filter the search box is, as in
	// search $q, or nil for none.
	SearchFilter *string `json:"search_filter"`
	// Summarize is nil when the Query has none.
	Summarize *Summarize `json:"summarize"`
	Sort      []SortChip `json:"sort"`
	// Take is the most rows the Query keeps, or nil for no take.
	Take *int `json:"take"`
}

// Chip is one filter: a Field, an operator and a value. Value keeps its
// type: a string, a number or a bool; a list of them for in and !in; nil
// for exists and missing, which are isnotnull(field) and isnull(field); a
// FilterValue for a Dashboard filter.
type Chip struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// FilterValue is a chip's value that is a Dashboard filter, $name.
type FilterValue struct {
	Filter string `json:"filter"`
}

// Summarize is a summarize the Builder can show: functions of one Field
// each, by Fields and time bins.
type Summarize struct {
	Aggregates []AggregateChip `json:"aggregates"`
	By         []GroupChip     `json:"by"`
}

// AggregateChip is one of summarize's functions: count(), rate() and
// peak_rate() have no Field; percentile has its percentage.
type AggregateChip struct {
	Fn      string  `json:"fn"`
	Field   string  `json:"field,omitempty"`
	Percent float64 `json:"percent,omitempty"`
}

// GroupChip is one of summarize's by columns: a Field, or time in bins of
// Bin as written (such as 5m), or "auto" for bin_auto(time).
type GroupChip struct {
	Field string `json:"field"`
	Bin   string `json:"bin,omitempty"`
}

// SortChip is one key of a sort: a column, descending unless Asc.
type SortChip struct {
	Column string `json:"column"`
	Asc    bool   `json:"asc"`
}

// Chips returns what the Builder shows of the Query, or nil if it can't
// show it all. It shows a Query of where stages that each test one Field
// against values, at most one search among them, then at most one each of
// summarize, sort and take, in that order.
func (q *Query) Chips() *Chips {
	chips, shown := q.readBuilder()
	for _, ok := range shown {
		if !ok {
			return nil
		}
	}
	return chips
}

// Stage is where one stage of a Query sits in its text, from its keyword to
// its end, and what it is to the explorer: Filter when it is one of the
// leading filters (the stages before the first that isn't a where or a
// search), Builder when the Builder can show it where it stands.
// The explorer names the stages the Builder can't show, and adds a pivot's
// where after the last filter, from these places.
type Stage struct {
	Line      int  `json:"line"`
	Column    int  `json:"column"`
	EndLine   int  `json:"end_line"`
	EndColumn int  `json:"end_column"`
	Filter    bool `json:"filter"`
	Builder   bool `json:"builder"`
}

// Stages places each of the Query's stages, in order.
func (q *Query) Stages() []Stage {
	_, shown := q.readBuilder()
	stages := make([]Stage, 0, len(q.spans))
	for i, span := range q.spans {
		st := Stage{Filter: i < q.heads, Builder: shown[i]}
		// A stage's span starts at its pipe; the stage starts at its keyword.
		text := q.text[span.Start:span.End]
		start := span.Start + len(text) - len(strings.TrimLeft(text, "| \t\r\n"))
		from, to := parser.PositionOf(q.text, start), parser.PositionOf(q.text, span.End)
		st.Line, st.Column, st.EndLine, st.EndColumn = from.Line, from.Column, to.Line, to.Column
		stages = append(stages, st)
	}
	return stages
}

// Where the Builder's stages come, in order: filters, then summarize, sort
// and take, each once.
const (
	inFilters = iota
	afterSummarize
	afterSort
	afterTake
)

// readBuilder reads the Builder's chips off the Query's stages, and says of
// each stage whether the Builder can show it where it stands.
func (q *Query) readBuilder() (*Chips, []bool) {
	chips := &Chips{Filters: []Chip{}, Sort: []SortChip{}}
	shown := make([]bool, len(q.tree.Stages))
	at := inFilters
	for i, s := range q.tree.Stages {
		switch s := s.(type) {
		case *parser.WhereStage:
			chip, ok := chipOf(s.Predicate)
			if shown[i] = ok && at == inFilters; shown[i] {
				chips.Filters = append(chips.Filters, chip)
			}
		case *parser.SearchStage:
			if shown[i] = chips.Search == nil && chips.SearchFilter == nil && at == inFilters; shown[i] {
				switch term := s.Term.(type) {
				case *parser.Param:
					name := term.Name
					chips.SearchFilter = &name
				case *parser.BasicLit:
					text := term.Value
					chips.Search = &text
				}
			}
		case *parser.SummarizeStage:
			summarize, ok := summarizeOf(s)
			if shown[i] = ok && at < afterSummarize; shown[i] {
				chips.Summarize, at = summarize, afterSummarize
			}
		case *parser.SortStage:
			keys, ok := sortOf(s)
			if shown[i] = ok && at < afterSort; shown[i] {
				chips.Sort, at = keys, afterSort
			}
		case *parser.TakeStage:
			n, err := rowCount(s.Count)
			if shown[i] = err == nil && at < afterTake; shown[i] {
				chips.Take, at = &n, afterTake
			}
		}
	}
	return chips, shown
}

// textChipOps are the word operators a chip can have.
var textChipOps = map[parser.TokenKind]string{
	parser.TokenContains: "contains", parser.TokenNotContains: "!contains",
	parser.TokenStartsWith: "startswith", parser.TokenNotStartsWith: "!startswith",
}

var compareChipOps = map[parser.TokenKind]string{
	parser.TokenEq: "==", parser.TokenNE: "!=",
	parser.TokenLT: "<", parser.TokenLE: "<=", parser.TokenGT: ">", parser.TokenGE: ">=",
}

// chipOf returns the chip a where stage's predicate is, if it is one: a
// Field other than time compared with a value, tested with contains or
// startswith, in or !in a list of values, or isnotnull or isnull.
func chipOf(p parser.Expr) (Chip, bool) {
	switch p := p.(type) {
	case *parser.BinaryExpr:
		field, ok := chipField(p.X)
		if !ok {
			return Chip{}, false
		}
		if op, ok := compareChipOps[p.Op]; ok {
			value, ok := chipValue(p.Y)
			return Chip{Field: field, Op: op, Value: value}, ok
		}
		if op, ok := textChipOps[p.Op]; ok {
			if param, ok := p.Y.(*parser.Param); ok {
				return Chip{Field: field, Op: op, Value: FilterValue{Filter: param.Name}}, true
			}
			lit, ok := p.Y.(*parser.BasicLit)
			if !ok || lit.Kind != parser.StringLiteral {
				return Chip{}, false
			}
			return Chip{Field: field, Op: op, Value: lit.Value}, true
		}
		if p.Op == parser.TokenIn || p.Op == parser.TokenNotIn {
			list := p.Y.(*parser.ListExpr)
			values := make([]any, len(list.Elems))
			for i, elem := range list.Elems {
				if values[i], ok = chipValue(elem); !ok {
					return Chip{}, false
				}
			}
			op := "in"
			if p.Op == parser.TokenNotIn {
				op = "!in"
			}
			return Chip{Field: field, Op: op, Value: values}, true
		}
	case *parser.CallExpr:
		if len(p.Args) != 1 {
			return Chip{}, false
		}
		field, ok := chipField(p.Args[0])
		switch {
		case ok && p.Func.Name == "isnotnull":
			return Chip{Field: field, Op: "exists"}, true
		case ok && p.Func.Name == "isnull":
			return Chip{Field: field, Op: "missing"}, true
		}
	}
	return Chip{}, false
}

// chipField is the Field a chip tests: a name, but not time, which the time
// range filters on.
func chipField(x parser.Expr) (string, bool) {
	id, ok := x.(*parser.Ident)
	if !ok {
		return "", false
	}
	name := fieldNameOf(id)
	return name, name != "time"
}

// fieldNameOf is the Field a name means, with aliases such as _time read.
func fieldNameOf(id *parser.Ident) string {
	if alias, ok := fieldAliases[id.Name]; ok && !id.Quoted {
		return alias
	}
	return id.Name
}

// chipValue reads a value written in the Query: a string, a number (a
// negative one too), a bool, or a Dashboard filter.
func chipValue(x parser.Expr) (any, bool) {
	if param, ok := x.(*parser.Param); ok {
		return FilterValue{Filter: param.Name}, true
	}
	negative := false
	if u, ok := x.(*parser.UnaryExpr); ok && u.Op == parser.TokenMinus {
		negative, x = true, u.X
	}
	lit, ok := x.(*parser.BasicLit)
	if !ok {
		return nil, false
	}
	switch {
	case lit.Kind == parser.NumberLiteral:
		n, err := strconv.ParseFloat(lit.Value, 64)
		if negative {
			n = -n
		}
		return n, err == nil
	case negative:
		return nil, false
	case lit.Kind == parser.StringLiteral:
		return lit.Value, true
	case lit.Kind == parser.BoolLiteral:
		return lit.Value == "true", true
	}
	return nil, false
}

// builderAggregates are the functions the Builder offers in summarize.
var builderAggregates = map[string]bool{
	"count": true, "rate": true, "peak_rate": true, "dcount": true, "sum": true, "avg": true, "min": true, "max": true, "percentile": true,
}

// withoutField are the Builder's functions that take no Field.
var withoutField = map[string]bool{"count": true, "rate": true, "peak_rate": true}

// summarizeOf reads a summarize the Builder can show: unnamed functions of
// one Field each, and by Fields or time bins.
func summarizeOf(s *parser.SummarizeStage) (*Summarize, bool) {
	summarize := &Summarize{Aggregates: []AggregateChip{}, By: []GroupChip{}}
	for _, a := range s.Aggregates {
		call, ok := a.X.(*parser.CallExpr)
		if a.Name != nil || !ok || !builderAggregates[call.Func.Name] {
			return nil, false
		}
		chip := AggregateChip{Fn: call.Func.Name}
		args := call.Args
		if chip.Fn == "percentile" {
			if len(args) != 2 {
				return nil, false
			}
			lit, ok := args[1].(*parser.BasicLit)
			if !ok || lit.Kind != parser.NumberLiteral {
				return nil, false
			}
			chip.Percent, _ = strconv.ParseFloat(lit.Value, 64)
			args = args[:1]
		}
		if len(args) == 1 {
			id, ok := args[0].(*parser.Ident)
			if !ok {
				return nil, false
			}
			chip.Field = fieldNameOf(id)
		}
		if withoutField[chip.Fn] != (chip.Field == "") {
			return nil, false
		}
		summarize.Aggregates = append(summarize.Aggregates, chip)
	}
	for _, b := range s.By {
		group, ok := groupOf(b)
		if !ok {
			return nil, false
		}
		summarize.By = append(summarize.By, group)
	}
	return summarize, true
}

// groupOf reads a by column: a Field, bin(time, size) or bin_auto(time).
func groupOf(b *parser.NamedExpr) (GroupChip, bool) {
	if b.Name != nil {
		return GroupChip{}, false
	}
	switch x := b.X.(type) {
	case *parser.Ident:
		return GroupChip{Field: fieldNameOf(x)}, true
	case *parser.CallExpr:
		if len(x.Args) == 0 {
			return GroupChip{}, false
		}
		id, ok := x.Args[0].(*parser.Ident)
		if !ok || fieldNameOf(id) != "time" {
			return GroupChip{}, false
		}
		switch {
		case x.Func.Name == "bin_auto" && len(x.Args) == 1:
			return GroupChip{Field: "time", Bin: "auto"}, true
		case x.Func.Name == "bin" && len(x.Args) == 2:
			size, ok := x.Args[1].(*parser.BasicLit)
			if ok && size.Kind == parser.TimespanLiteral {
				return GroupChip{Field: "time", Bin: size.Value}, true
			}
		}
	}
	return GroupChip{}, false
}

// sortOf reads a sort the Builder can show: by names, each asc or desc,
// with NULLs where they fall by default.
func sortOf(s *parser.SortStage) ([]SortChip, bool) {
	keys := make([]SortChip, 0, len(s.Keys))
	for _, k := range s.Keys {
		id, ok := k.X.(*parser.Ident)
		if !ok || k.Nulls != "" {
			return nil, false
		}
		keys = append(keys, SortChip{Column: fieldNameOf(id), Asc: k.Asc})
	}
	return keys, true
}
