package logquery

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/solidtrace/event_store/logquery/parser"
	"github.com/solidtrace/event_store/pkg/chsql"
)

// rate() and peak_rate() count a summarize's rows per second of its time
// bin: the first of its by columns that bins the Logs' own time, or, with no
// time bin at all, the Query's whole range. rate() refuses a summarize by
// another time and none of the Logs' own, whose bins it can't size.
//
// rate() is a group's count over the seconds of its bin that lie in the
// Query's range, so a bin cut by either end of the range still reads a true
// rate. peak_rate() is the count of the group's busiest second, as
// bin(time, 1s) puts them, so it needs bins of whole seconds and rows that
// have the Logs' time.
//
// Both are worked out inside their summarize, so a Query using them is still
// made bin by bin (Bins). A bin the bin cache keeps lies wholly in the range,
// so its rate is its count over its size, whatever the range.

// binSecondsColumn holds, on each row a summarize reads, the seconds of the
// row's bin that lie in the range; secondCountColumn, the rows of its group
// in its second.
const (
	binSecondsColumn  = "#bin_seconds"
	secondCountColumn = "#second_count"
)

// rateBasis is what a summarize's rates are counted in. bin is the size of
// its bin of the Logs' own time, 0 for none. otherTime is whether, with
// none, it groups by another time, whose bins rate() can't divide by.
// logTime is whether its rows have the Logs' own time, whose seconds
// peak_rate() counts rows in.
type rateBasis struct {
	bin       time.Duration
	otherTime bool
	logTime   bool
}

// rateBasisOf is the rate basis of a summarize at index at of the tail by
// these columns, reading rows of scope s.
func (q *Query) rateBasisOf(s scope, by []namedValue, at int) rateBasis {
	var b rateBasis
	if _, size, ok := q.rateBin(by, at); ok {
		b.bin = size
	} else {
		b.otherTime = slices.ContainsFunc(by, func(col namedValue) bool { return col.x.typ() == timeType })
	}
	if col, ok := s.lookup("time"); ok {
		b.logTime = q.isLogTime(columnRef{name: col.name, t: col.t}, at)
	} else {
		b.logTime = s.logs
	}
	return b
}

// rateArgs refuses arguments to rate() or peak_rate(), which count the rows
// a where leaves.
func rateArgs(e *parser.CallExpr) error {
	if len(e.Args) != 0 {
		return errorAt(e, "%[1]s takes no arguments; filter with where first, as in where path == \"/\" | summarize %[1]s()", e.Func.Name)
	}
	return nil
}

// rateBin is the time bin of a summarize at index at of the tail by these
// columns: the first that bins the Logs' own time, and its size.
func (q *Query) rateBin(by []namedValue, at int) (namedValue, time.Duration, bool) {
	for _, col := range by {
		if size, ok := q.binOfLogTime(col.x, at); ok {
			return col, size, true
		}
	}
	return namedValue{}, 0, false
}

// rateColumns writes the columns a summarize at index at of the tail adds to
// the rows it reads for its rates, whose fields f reads, as a select list
// after *, or "" if it has no rates.
func (q *Query) rateColumns(s summarizeStage, at int, f fields) string {
	var columns []string
	if usesAggregate(s.aggregates, "rate") {
		seconds := writeLiteral(Literal{Kind: parser.NumberLiteral, Number: q.to.Sub(q.from).Seconds()})
		if bin, size, ok := q.rateBin(s.by, at); ok {
			start := writeValue(bin.x, f)
			end := fmt.Sprintf("least(%s + %s, %s)", start, writeLiteral(Literal{Kind: parser.TimespanLiteral, Timespan: size}),
				writeLiteral(Literal{Kind: parser.DateTimeLiteral, Time: q.to}))
			begin := fmt.Sprintf("greatest(%s, %s)", start, writeLiteral(Literal{Kind: parser.DateTimeLiteral, Time: q.from}))
			seconds = fmt.Sprintf("(toUnixTimestamp64Micro(%s) - toUnixTimestamp64Micro(%s)) / 1000000", end, begin)
		}
		columns = append(columns, seconds+" AS "+chsql.Identifier(binSecondsColumn))
	}
	if usesAggregate(s.aggregates, "peak_rate") {
		var partition []string
		for _, col := range s.by {
			partition = append(partition, writeValue(col.x, f))
		}
		second := call{fn: "bin", t: timeType, args: []expr{
			fieldRef{name: "time", builtIn: true, t: timeType},
			literal{Literal{Kind: parser.TimespanLiteral, Timespan: time.Second}},
		}}
		partition = append(partition, writeCall(second, f))
		columns = append(columns, "count() OVER (PARTITION BY "+strings.Join(partition, ", ")+") AS "+chsql.Identifier(secondCountColumn))
	}
	return strings.Join(columns, ", ")
}

// usesAggregate reports whether any of the columns has an aggregate of
// function fn.
func usesAggregate(columns []namedValue, fn string) bool {
	for _, col := range columns {
		if hasAggregate(col.x, fn) {
			return true
		}
	}
	return false
}
