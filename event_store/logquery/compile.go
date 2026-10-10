// Package logquery compiles Queries, SolidTrace's KQL-style query language
// for Logs, to DuckDB SQL. It is the single path from a Query to SQL.
//
// Compiling runs in three steps, each in its own files:
//   - package parser turns the text into a syntax tree;
//   - Compile (this file, expr.go and stages.go) checks the tree against the
//     language's stages, functions, Fields and types, and turns it into a
//     Query of typed stages;
//   - the SQL emitter (sql.go) writes a Query as SQL: its leading filters
//     against one source of Logs, whose columns storage.LogColumns describes,
//     and the rest as a chain of stages over the Logs that pass them.
//
// What the Builder shows (chips.go) is read off a compiled Query's syntax
// tree.
package logquery

import (
	"fmt"
	"time"

	"github.com/solidtrace/event_store/logquery/parser"
	"github.com/solidtrace/event_store/storage"
)

// Error is a mistake in a Query, at a span of its text.
type Error = parser.Error

// Bounds are what a Query runs within: the request's time range, [From, To),
// and the time it runs at, which ago() and now() read. A Query can narrow the
// range but never widen it. Zero From and To, as validate has, mean no range.
type Bounds struct {
	From, To time.Time
	Now      time.Time
}

// Query is a compiled Query.
//
// Its stages split in two: the head, its leading where and search stages,
// which filter Logs; and the tail, every stage after them. The head is
// written as one condition on each source of Logs (Where); the tail as a
// chain of stages over the Logs that pass it (TableSQL). A Query whose tail
// keeps Logs newest first, as take does, returns Log rows; any other returns
// a table.
type Query struct {
	head []stage
	tail []stage
	// text is the Query's text, and spans each stage's place in it, head
	// then tail.
	text  string
	spans []parser.Span
	// tree is the Query's syntax tree, which the Builder is read from.
	tree *parser.Query

	from, to time.Time
	// table is set when the Query returns a table rather than Log rows.
	table bool
	// take is how many Log rows a Query of Log rows returns at most, if
	// hasTake.
	take    int
	hasTake bool
	// reads are the Attributes, by type, that the tail reads from each Log.
	reads []attrKey
	// output is the table's columns, over the tail's last stage.
	output []namedValue
	// shape is how to chart the table.
	shape shape
}

// Compile parses and checks a Query's text for the given bounds. The error,
// if any, is an *Error with the span of the first mistake.
func Compile(source string, bounds Bounds) (*Query, error) {
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, err
	}
	c := &compiler{text: source, bounds: bounds, scope: logScope(), reads: map[attrKey]bool{}}
	q := &Query{from: bounds.From.UTC(), to: bounds.To.UTC(), text: source, tree: tree}
	inHead := true
	for _, s := range tree.Stages {
		if inHead {
			switch s.(type) {
			case *parser.WhereStage, *parser.SearchStage:
			default:
				inHead = false
				c.tail = true
			}
		}
		compiled, err := c.stage(s)
		if err != nil {
			return nil, err
		}
		if inHead {
			q.head = append(q.head, compiled)
		} else {
			q.tail = append(q.tail, compiled)
		}
		q.spans = append(q.spans, s.Span())
	}
	q.narrow()
	q.finish(c)
	return q, nil
}

// Range is the time range the Query reads: the request's, narrowed by any
// leading where on time.
func (q *Query) Range() (from, to time.Time) {
	return q.from, q.to
}

// Table reports whether the Query returns a table rather than Log rows.
func (q *Query) Table() bool {
	return q.table
}

// Take is how many Log rows a Query of Log rows returns at most, if it says.
func (q *Query) Take() (int, bool) {
	return q.take, q.hasTake
}

// FillGaps reports whether a chart of the Query's table may fill the time
// bins it has no row for: when its rows are its last summarize's by a time
// bin, and no stage since has left any out. A table cut short at a run's
// limit of rows lacks bins too, which the run must check itself.
func (q *Query) FillGaps() bool {
	return q.shape.fillGaps
}

func errorAt(node parser.Node, format string, args ...any) *Error {
	return &Error{Span: node.Span(), Message: fmt.Sprintf(format, args...)}
}

// compiler holds what a Query's stages are compiled against: the bounds, the
// columns of the rows at the current stage, and what the tail reads.
type compiler struct {
	text   string
	bounds Bounds
	scope  scope
	// tail is set once past the leading filter stages.
	tail bool
	// reads are the Attributes, by type, the tail reads from Logs; names
	// are the Attributes it names, in order.
	reads     map[attrKey]bool
	readOrder []attrKey
	names     []string
}

// read notes that the tail reads the Attribute key of type t.
func (c *compiler) read(t storage.AttributeType, key string) {
	if !c.tail {
		return
	}
	k := attrKey{t: t, key: key}
	if !c.reads[k] {
		c.reads[k] = true
		c.readOrder = append(c.readOrder, k)
	}
}

// named notes that the Query names the Attribute key.
func (c *compiler) named(key string) {
	for _, name := range c.names {
		if name == key {
			return
		}
	}
	c.names = append(c.names, key)
}

// scope is what names mean at a stage: the columns of its rows.
type scope struct {
	// logs is set while the rows are Logs: a name that isn't a column is a
	// built-in field or an Attribute.
	logs bool
	// columns are a table's columns, or the columns extend added to Logs.
	columns []column
}

type column struct {
	name string
	t    valueType
}

func logScope() scope {
	return scope{logs: true}
}

func (s scope) lookup(name string) (column, bool) {
	for _, col := range s.columns {
		if col.name == name {
			return col, true
		}
	}
	return column{}, false
}

func (s scope) names() []string {
	names := make([]string, len(s.columns))
	for i, col := range s.columns {
		names[i] = col.name
	}
	return names
}

// builtInFields are the built-in fields every Log has, and their types.
var builtInFields = map[string]valueType{
	"time": timeType, "level": stringType, "message": stringType, "service": stringType,
	"environment": stringType, "trace_id": stringType, "span_id": stringType,
}

// builtInOrder is the order of the built-in fields in a table of Logs.
var builtInOrder = []string{"time", "level", "message", "service", "environment", "trace_id", "span_id"}

// fieldAliases are other names for built-in fields, so that pasted Axiom
// queries work.
var fieldAliases = map[string]string{"_time": "time"}

// attrKey is an Attribute of one type.
type attrKey struct {
	t   storage.AttributeType
	key string
}
