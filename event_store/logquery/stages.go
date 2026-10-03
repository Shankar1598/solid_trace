package logquery

import (
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

func (whereStage) isStage()  {}
func (searchStage) isStage() {}

func (c *compiler) stage(s parser.Stage) (stage, error) {
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
	default:
		return nil, errorAt(s, "unknown stage")
	}
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

// finish works out what the Query returns.
func (q *Query) finish(c *compiler) {
}

// outputColumn is one column of a Query's table.
type outputColumn struct {
	name string
	t    valueType
}
