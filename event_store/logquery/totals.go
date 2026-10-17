package logquery

// Totals is the Query of the totals of q's time chart, if q has one: when
// its last stage is a summarize by a time bin, the table's time axis, it is
// q with that bin dropped from the summarize's by. Its table has a row for
// each group, the other by columns, with its aggregates over the whole range,
// each worked out from the Logs rather than from the bins, so an avg or a
// dcount is exact. A summarize by the bin alone has one row. Rows go by the
// first aggregate, largest first, then by the group columns.
func (q *Query) Totals() (*Query, bool) {
	if !q.table || len(q.tail) == 0 {
		return nil, false
	}
	last := len(q.tail) - 1
	s, ok := q.tail[last].(summarizeStage)
	if !ok {
		return nil, false
	}
	bin := -1
	for i, col := range s.by {
		if q.shape.hints[col.name].bin > 0 {
			bin = i
			break
		}
	}
	// A summarize by the bin alone and no aggregates has no totals.
	if bin < 0 || len(s.by) == 1 && len(s.aggregates) == 0 {
		return nil, false
	}
	by := append(append([]namedValue{}, s.by[:bin]...), s.by[bin+1:]...)
	var keys []sortKey
	if len(s.aggregates) > 0 {
		first := s.aggregates[0]
		keys = append(keys, sortKey{x: columnRef{name: first.name, t: first.x.typ()}})
	}
	for _, col := range by {
		keys = append(keys, sortKey{x: columnRef{name: col.name, t: col.x.typ()}, asc: true})
	}
	tail := append(append([]stage{}, q.tail[:last]...), summarizeStage{by: by, aggregates: s.aggregates})
	if len(keys) > 0 {
		tail = append(tail, sortStage{keys: keys})
	}
	var output []namedValue
	for _, col := range q.output {
		if col.name != s.by[bin].name {
			output = append(output, col)
		}
	}
	totals := *q
	totals.tail, totals.output = tail, output
	totals.shape = shapeOf(tail, output)
	return &totals, true
}
