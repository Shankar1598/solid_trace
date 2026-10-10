package logquery

import (
	"maps"
	"time"

	"github.com/solidtrace/event_store/logquery/parser"
)

// shape is what a table's stages say about how to chart it, read off the
// tail from its last summarize on: each column's hint, by name, and whether
// a chart may fill the time bins the table has no row for.
type shape struct {
	hints map[string]hint
	// fillGaps is set when the rows are the last summarize's, one for each
	// time bin with Logs, and none have been left out since.
	fillGaps bool
}

// hint is what a column of a table tells a chart: the aggregate it is, such
// as count, or the size of the time bins it holds.
type hint struct {
	agg string
	bin time.Duration
}

// shapeOf works out a table's shape from its tail.
//
// Only the last summarize gives hints: a by column that bins a time gets the
// bins' size, whether it bins the time itself or groups by a column an
// earlier stage binned it into, and an aggregate, not one inside arithmetic,
// its function. A later project, extend or distinct keeps a column's hint
// only as a plain rename, and any other value under that name has none. Any
// stage after the summarize that leaves out rows clears fillGaps, as does
// losing the time bin column.
func shapeOf(tail []stage) shape {
	last := -1
	for i, st := range tail {
		if _, ok := st.(summarizeStage); ok {
			last = i
		}
	}
	var s shape
	for i, st := range tail {
		// Before the last summarize, a column that bins a time may be one
		// it groups by.
		bins := i < last
		switch st := st.(type) {
		case summarizeStage:
			s = summarized(st, s.hints)
		case projectStage:
			s.hints = renamed(st.columns, s.hints, nil, bins)
		case extendStage:
			s.hints = renamed(st.columns, s.hints, s.hints, bins)
		case distinctStage:
			s.hints = renamed(st.columns, s.hints, nil, bins)
			s.fillGaps = false
		case whereStage, takeStage, topStage:
			s.fillGaps = false
		case countStage:
			s = shape{}
		}
	}
	binned := false
	for _, h := range s.hints {
		binned = binned || h.bin > 0
	}
	s.fillGaps = s.fillGaps && binned
	return s
}

// summarized is the shape of a summarize's table, given the hints of the
// columns it reads.
func summarized(st summarizeStage, before map[string]hint) shape {
	hints := map[string]hint{}
	for _, col := range st.by {
		if size, ok := timeBin(col.x, before); ok {
			hints[col.name] = hint{bin: size}
			// One time bin column is the chart's time axis; any other is a
			// group like the rest.
			break
		}
	}
	for _, col := range st.aggregates {
		if a, ok := col.x.(aggregate); ok {
			hints[col.name] = hint{agg: a.fn}
		}
	}
	return shape{hints: hints, fillGaps: true}
}

// timeBin is the bin size of a value that bins a time: bin, or floor, of a
// time, or a column that holds such bins already.
func timeBin(x expr, hints map[string]hint) (time.Duration, bool) {
	switch x := x.(type) {
	case call:
		if (x.fn != "bin" && x.fn != "floor") || x.args[0].typ() != timeType {
			return 0, false
		}
		size, ok := x.args[1].(literal)
		if !ok || size.Kind != parser.TimespanLiteral {
			return 0, false
		}
		return size.Timespan, true
	case columnRef:
		h := hints[x.name]
		return h.bin, h.bin > 0
	}
	return 0, false
}

// renamed are the hints after a project's, an extend's or a distinct's
// columns: a column that is another column, as it is, takes that column's
// hint, and any other has none, unless bins is set and it bins a time. kept
// are the hints of the columns the stage keeps as they were, extend's input;
// nil for the others.
func renamed(columns []namedValue, hints, kept map[string]hint, bins bool) map[string]hint {
	after := maps.Clone(kept)
	if after == nil {
		after = map[string]hint{}
	}
	for _, col := range columns {
		delete(after, col.name)
		if ref, ok := col.x.(columnRef); ok {
			if h, ok := hints[ref.name]; ok {
				after[col.name] = h
			}
		} else if size, ok := timeBin(col.x, hints); ok && bins {
			after[col.name] = hint{bin: size}
		}
	}
	return after
}
