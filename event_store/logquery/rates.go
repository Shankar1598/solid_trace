package logquery

import (
	"fmt"
	"strings"
	"time"

	"github.com/solidtrace/event_store/logquery/parser"
)

// rate() and peak_rate() count a summarize's rows per second of its time
// bin: the first of its by columns that bins the Logs' own time, or, without
// one, the Query's whole range.
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

// secondsColumn holds, on each row a summarize reads, the seconds of the
// row's bin that lie in the range; secondColumn, the rows of its group in
// its second.
const (
	secondsColumn = `"#seconds"`
	secondColumn  = `"#second"`
)

// perSecond is what a summarize counts peak_rate() in: the size of its time
// bin, 0 for none, and whether its rows have the Logs' own time.
type perSecond struct {
	bin     time.Duration
	logTime bool
}

// perSecondOf is what a summarize at index at of the tail, reading rows of
// scope s, counts per second in.
func (q *Query) perSecondOf(s scope, by []namedValue, at int) perSecond {
	var p perSecond
	if _, size, ok := q.rateBin(by, at); ok {
		p.bin = size
	}
	if col, ok := s.lookup("time"); ok {
		p.logTime = q.isLogTime(columnRef{name: col.name, t: col.t}, at)
	} else {
		p.logTime = s.logs
	}
	return p
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

// perSecondColumns writes the columns a summarize at index at of the tail
// adds to the rows it reads for its rates, as a select list after *, or ""
// if it has no rates.
func (q *Query) perSecondColumns(s summarizeStage, at int, f fields) string {
	var columns []string
	if usesAggregate(s.aggregates, "rate") {
		seconds := writeLiteral(Literal{Kind: parser.NumberLiteral, Number: q.to.Sub(q.from).Seconds()})
		if bin, size, ok := q.rateBin(s.by, at); ok {
			start := writeValue(bin.x, f)
			seconds = fmt.Sprintf("(epoch_us(least(%[1]s + %[2]s, %[3]s)) - epoch_us(greatest(%[1]s, %[4]s))) / 1000000",
				start, writeLiteral(Literal{Kind: parser.TimespanLiteral, Timespan: size}),
				writeLiteral(Literal{Kind: parser.DateTimeLiteral, Time: q.to}),
				writeLiteral(Literal{Kind: parser.DateTimeLiteral, Time: q.from}))
		}
		columns = append(columns, seconds+" AS "+secondsColumn)
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
		columns = append(columns, "count(*) OVER (PARTITION BY "+strings.Join(partition, ", ")+") AS "+secondColumn)
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
