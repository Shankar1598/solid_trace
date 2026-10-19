package logquery

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/solidtrace/event_store/logquery/parser"
)

// stage is one compiled stage.
type stage interface {
	isStage()
}

// whereStage keeps the rows its predicate holds for.
type whereStage struct {
	predicate expr
}

// searchStage keeps the Logs whose message or any string Attribute contains
// term, ignoring case.
type searchStage struct {
	term string
}

// namedValue is a column a stage makes: its name and value.
type namedValue struct {
	name string
	x    expr
	span parser.Span
}

// projectStage makes a table of its columns.
type projectStage struct{ columns []namedValue }

// extendStage adds columns to the rows, replacing any of the same name.
type extendStage struct{ columns []namedValue }

// summarizeStage makes a table of its by columns, then its aggregates, with
// one row per distinct by.
type summarizeStage struct{ by, aggregates []namedValue }

// sortKey is one key of a sort: descending unless asc, NULLs last when
// descending and first when ascending unless said otherwise.
type sortKey struct {
	x          expr
	asc        bool
	nullsFirst bool
}

// sortStage sorts the rows.
type sortStage struct{ keys []sortKey }

// takeStage keeps the first n rows: the first in order if the rows are
// sorted, else the newest Logs, else any.
type takeStage struct{ n int }

// topStage keeps the first n rows sorted by key.
type topStage struct {
	n   int
	key sortKey
}

// countStage makes a table of one row: Count, the number of rows.
type countStage struct{}

// distinctStage makes a table of its columns' distinct combinations.
type distinctStage struct{ columns []namedValue }

func (whereStage) isStage()     {}
func (searchStage) isStage()    {}
func (projectStage) isStage()   {}
func (extendStage) isStage()    {}
func (summarizeStage) isStage() {}
func (sortStage) isStage()      {}
func (takeStage) isStage()      {}
func (topStage) isStage()       {}
func (countStage) isStage()     {}
func (distinctStage) isStage()  {}

// stage compiles the next stage of q, whose tail so far says what a
// summarize's by columns bin.
func (c *compiler) stage(s parser.Stage, q *Query) (stage, error) {
	switch s := s.(type) {
	case *parser.WhereStage:
		p, err := c.condition(s.Predicate)
		if err != nil {
			return nil, err
		}
		return whereStage{predicate: p}, nil
	case *parser.SearchStage:
		if c.tail {
			return nil, errorAt(s, "search only comes before other stages; after them, use where … contains")
		}
		return searchStage{term: s.Term.Value}, nil
	case *parser.ProjectStage:
		columns, err := c.columns(s.Columns, nil)
		if err != nil {
			return nil, err
		}
		c.scope = scope{columns: columnsOf(columns)}
		return projectStage{columns: columns}, nil
	case *parser.ExtendStage:
		columns, err := c.columns(s.Columns, nil)
		if err != nil {
			return nil, err
		}
		for _, col := range columnsOf(columns) {
			c.scope.columns = slices.DeleteFunc(c.scope.columns, func(old column) bool { return old.name == col.name })
			c.scope.columns = append(c.scope.columns, col)
		}
		return extendStage{columns: columns}, nil
	case *parser.SummarizeStage:
		by, err := c.columns(s.By, nil)
		if err != nil {
			return nil, err
		}
		basis := q.rateBasisOf(c.scope, by, len(q.tail))
		aggregates, err := c.columns(s.Aggregates, &basis)
		if err != nil {
			return nil, err
		}
		for i, a := range aggregates {
			if !hasAggregate(a.x, "") {
				return nil, errorAt(s.Aggregates[i].X, "expected a function such as count(), dcount(x) or avg(x) before by")
			}
		}
		if len(by) == 0 && len(aggregates) == 0 {
			return nil, errorAt(s, "summarize needs a function such as count(), or by and columns")
		}
		all := append(slices.Clone(by), aggregates...)
		if err := uniqueNames(all); err != nil {
			return nil, err
		}
		c.scope = scope{columns: columnsOf(all)}
		return summarizeStage{by: by, aggregates: aggregates}, nil
	case *parser.SortStage:
		var keys []sortKey
		for _, k := range s.Keys {
			key, err := c.sortKey(k)
			if err != nil {
				return nil, err
			}
			keys = append(keys, key)
		}
		return sortStage{keys: keys}, nil
	case *parser.TakeStage:
		n, err := rowCount(s.Count)
		if err != nil {
			return nil, err
		}
		return takeStage{n: n}, nil
	case *parser.TopStage:
		n, err := rowCount(s.Count)
		if err != nil {
			return nil, err
		}
		key, err := c.sortKey(s.Key)
		if err != nil {
			return nil, err
		}
		return topStage{n: n, key: key}, nil
	case *parser.CountStage:
		c.scope = scope{columns: []column{{name: "Count", t: numberType}}}
		return countStage{}, nil
	case *parser.DistinctStage:
		columns, err := c.columns(s.Columns, nil)
		if err != nil {
			return nil, err
		}
		c.scope = scope{columns: columnsOf(columns)}
		return distinctStage{columns: columns}, nil
	default:
		return nil, errorAt(s, "unknown stage")
	}
}

// columns compiles the columns of project, extend, summarize or distinct,
// naming those not named. aggregates is as expr takes it.
func (c *compiler) columns(exprs []*parser.NamedExpr, aggregates *rateBasis) ([]namedValue, error) {
	columns := make([]namedValue, 0, len(exprs))
	for i, e := range exprs {
		x, err := c.expr(e.X, anyType, aggregates)
		if err != nil {
			return nil, err
		}
		name := ""
		switch {
		case e.Name != nil:
			name = e.Name.Name
		case aggregates != nil:
			name = aggregateName(x)
		default:
			name = fieldName(x)
		}
		if name == "" {
			name = fmt.Sprintf("Column%d", i+1)
		}
		columns = append(columns, namedValue{name: name, x: x, span: e.Span()})
	}
	if err := uniqueNames(columns); err != nil {
		return nil, err
	}
	return columns, nil
}

// aggregateName is the name KQL gives an unnamed aggregate, such as count_,
// avg_duration or percentile_duration_95.
func aggregateName(x expr) string {
	a, ok := x.(aggregate)
	if !ok {
		return ""
	}
	name := a.fn + "_" + fieldName(a)
	if a.fn == "percentile" {
		name += "_" + strconv.FormatFloat(a.percent, 'f', -1, 64)
	}
	return name
}

// hasAggregate reports whether x has an aggregate of function fn, or of any
// function if fn is "".
func hasAggregate(x expr, fn string) bool {
	switch x := x.(type) {
	case aggregate:
		return fn == "" || x.fn == fn
	case arith:
		return hasAggregate(x.x, fn) || hasAggregate(x.y, fn)
	case negative:
		return hasAggregate(x.x, fn)
	case call:
		return slices.ContainsFunc(x.args, func(arg expr) bool { return hasAggregate(arg, fn) })
	}
	return false
}

// uniqueNames refuses columns of the same name, at the second.
func uniqueNames(columns []namedValue) error {
	seen := map[string]bool{}
	for _, col := range columns {
		if seen[col.name] {
			return &Error{Span: col.span, Message: fmt.Sprintf("two columns are named %q; name one, as in %s2 = …", col.name, col.name)}
		}
		seen[col.name] = true
	}
	return nil
}

func columnsOf(values []namedValue) []column {
	columns := make([]column, len(values))
	for i, v := range values {
		columns[i] = column{name: v.name, t: v.x.typ()}
	}
	return columns
}

func (c *compiler) sortKey(k *parser.SortKey) (sortKey, error) {
	x, err := c.expr(k.X, anyType, nil)
	if err != nil {
		return sortKey{}, err
	}
	nullsFirst := k.Asc
	if k.Nulls != "" {
		nullsFirst = k.Nulls == "first"
	}
	return sortKey{x: x, asc: k.Asc, nullsFirst: nullsFirst}, nil
}

// rowCount reads take's or top's number of rows.
func rowCount(lit *parser.BasicLit) (int, error) {
	n, err := strconv.Atoi(lit.Value)
	if err != nil || n < 0 {
		return 0, errorAt(lit, "expected a whole number of rows")
	}
	return n, nil
}

// narrow narrows the Query's range by the conditions on time that every Log
// of its head must meet: those of its where stages joined by and.
func (q *Query) narrow() {
	if q.from.IsZero() && q.to.IsZero() {
		return
	}
	for _, s := range q.head {
		if w, ok := s.(whereStage); ok {
			q.narrowBy(w.predicate)
		}
	}
	if q.to.Before(q.from) {
		q.to = q.from
	}
}

func (q *Query) narrowBy(p expr) {
	switch p := p.(type) {
	case and:
		q.narrowBy(p.x)
		q.narrowBy(p.y)
	case compare:
		op := p.op
		field, value := p.x, p.y
		if _, ok := field.(literal); ok {
			field, value = value, field
			op = flipped[op]
		}
		ref, ok := field.(fieldRef)
		lit, isLit := value.(literal)
		if !ok || !ref.builtIn || ref.name != "time" || !isLit || lit.Kind != parser.DateTimeLiteral {
			return
		}
		at := lit.Time.UTC()
		after := at.Add(time.Microsecond)
		switch op {
		case opGT:
			q.from = latest(q.from, after)
		case opGE:
			q.from = latest(q.from, at)
		case opLT:
			q.to = earliest(q.to, at)
		case opLE:
			q.to = earliest(q.to, after)
		case opEq:
			q.from = latest(q.from, at)
			q.to = earliest(q.to, after)
		}
	}
}

// flipped is the operator that keeps x <op> y true with its sides swapped.
var flipped = map[compareOp]compareOp{opEq: opEq, opNE: opNE, opLT: opGT, opLE: opGE, opGT: opLT, opGE: opLE}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func earliest(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// finish works out what the Query returns. A tail of take stages, and sorts
// newest first, keeps Log rows; any other stage makes a table. A table of
// Logs, as top gives, has the built-in fields, then the Attributes the Query
// names, then the columns extend added.
func (q *Query) finish(c *compiler) {
	for _, s := range q.tail {
		switch s := s.(type) {
		case takeStage:
			if !q.hasTake || s.n < q.take {
				q.take, q.hasTake = s.n, true
			}
		case sortStage:
			if !newestFirst(s.keys) {
				q.table = true
			}
		default:
			q.table = true
		}
	}
	if !q.table {
		return
	}
	if c.scope.logs {
		for _, name := range builtInOrder {
			if col, ok := c.scope.lookup(name); ok {
				q.output = append(q.output, namedValue{name: name, x: columnRef{name: name, t: col.t}})
			} else {
				q.output = append(q.output, namedValue{name: name, x: fieldRef{name: name, builtIn: true, t: builtInFields[name]}})
			}
		}
		for _, name := range c.names {
			if _, ok := c.scope.lookup(name); ok {
				continue
			}
			for _, t := range dynamicParts {
				c.read(t, name)
			}
			q.output = append(q.output, namedValue{name: name, x: fieldRef{name: name, t: dynamicType}})
		}
		for _, col := range c.scope.columns {
			if _, builtIn := builtInFields[col.name]; !builtIn {
				q.output = append(q.output, namedValue{name: col.name, x: columnRef{name: col.name, t: col.t}})
			}
		}
	} else {
		for _, col := range c.scope.columns {
			q.output = append(q.output, namedValue{name: col.name, x: columnRef{name: col.name, t: col.t}})
		}
	}
	q.reads = c.readOrder
	q.shape = shapeOf(q.tail, q.output)
}

// newestFirst reports whether a sort keeps Logs newest first, as they come.
func newestFirst(keys []sortKey) bool {
	if len(keys) != 1 || keys[0].asc {
		return false
	}
	ref, ok := keys[0].x.(fieldRef)
	return ok && ref.builtIn && ref.name == "time"
}
