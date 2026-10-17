package logquery

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/solidtrace/event_store/logquery/parser"
	"github.com/solidtrace/event_store/storage"
)

// Bins is how a Query's table splits into time bins, for the bin cache: Size
// is the bins' size, each starting a whole number of sizes after 1970, and
// Key identifies the Query's SQL, so that two Queries with the same Key make
// the same rows from the same Logs.
type Bins struct {
	Size time.Duration
	Key  string
}

// Bins reports whether the Query's table is made bin by bin, and how. It is
// when the last stage is a summarize whose first by column bins the Log's
// own time, the table's time axis, and each stage before it keeps or drops
// each Log on its own (where, extend and project). Then each row comes from
// the Logs of one bin alone, and rows come in order of their bin first, so
// a table over a time range is its bins' rows, one bin after another.
func (q *Query) Bins() (Bins, bool) {
	if !q.table || len(q.tail) == 0 || len(q.output) == 0 {
		return Bins{}, false
	}
	last := len(q.tail) - 1
	s, ok := q.tail[last].(summarizeStage)
	if !ok || len(s.by) == 0 || s.by[0].name != q.output[0].name || q.shape.hints[s.by[0].name].bin == 0 {
		return Bins{}, false
	}
	for _, st := range q.tail[:last] {
		switch st.(type) {
		case whereStage, extendStage, projectStage:
		default:
			return Bins{}, false
		}
	}
	size, ok := q.binOfLogTime(s.by[0].x, last)
	if !ok || size <= 0 || size%time.Microsecond != 0 || size != q.shape.hints[s.by[0].name].bin {
		return Bins{}, false
	}
	sum := sha256.New()
	for _, part := range []string{q.Where(storage.LogColumns{}), q.Select(storage.LogColumns{}), q.TableSQL("", 0)} {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return Bins{Size: size, Key: hex.EncodeToString(sum.Sum(nil)[:16])}, true
}

// binOfLogTime returns the size of the bins x puts Logs in, read at the
// stage at index at of the tail, if x is bin or floor of the Log's own time
// by a timespan, or a column that holds such bins.
func (q *Query) binOfLogTime(x expr, at int) (time.Duration, bool) {
	switch x := x.(type) {
	case columnRef:
		def, before, ok := q.definition(x.name, at)
		if !ok {
			return 0, false
		}
		return q.binOfLogTime(def, before)
	case call:
		if x.fn != "bin" && x.fn != "floor" {
			return 0, false
		}
		size, ok := x.args[1].(literal)
		if !ok || size.Kind != parser.TimespanLiteral || !q.isLogTime(x.args[0], at) {
			return 0, false
		}
		return size.Timespan, true
	}
	return 0, false
}

// isLogTime reports whether x, read at the stage at index at of the tail, is
// the Log's own time.
func (q *Query) isLogTime(x expr, at int) bool {
	switch x := x.(type) {
	case fieldRef:
		return x.builtIn && x.name == "time"
	case columnRef:
		def, before, ok := q.definition(x.name, at)
		return ok && q.isLogTime(def, before)
	}
	return false
}

// definition finds the column name as the stage at index at of the tail
// reads it: the value the latest extend or project before it gave the
// column, and that stage's index, which the value is read at.
func (q *Query) definition(name string, at int) (expr, int, bool) {
	for i := at - 1; i >= 0; i-- {
		var columns []namedValue
		switch st := q.tail[i].(type) {
		case extendStage:
			columns = st.columns
		case projectStage:
			columns = st.columns
		default:
			continue
		}
		for _, col := range columns {
			if col.name == name {
				return col.x, i, true
			}
		}
		if _, ok := q.tail[i].(projectStage); ok {
			return nil, 0, false
		}
	}
	return nil, 0, false
}
