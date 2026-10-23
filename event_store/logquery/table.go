package logquery

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/solidtrace/event_store/pkg/chsql"
	"github.com/solidtrace/event_store/storage"
)

// readColumn is the name of the column that holds the Attribute key of type
// t in the Logs a tail reads.
func readColumn(t storage.AttributeType, key string) string {
	return "@" + string(t) + ":" + key
}

// tailFields are the columns of rel, one relation of a tail's chain: a
// table's columns and the built-in fields by name, and each Attribute the
// tail reads under its readColumn. Each is written as rel's own column, since
// ClickHouse lets an alias in a SELECT stand for a column of the same name.
type tailFields struct{ rel string }

func (f tailFields) Column(name string) string {
	return f.rel + "." + chsql.Identifier(name)
}

func (f tailFields) BuiltIn(name string) string {
	return f.Column(name)
}

func (f tailFields) Attribute(t storage.AttributeType, key string) string {
	return f.Column(readColumn(t, key))
}

// tailOn are the fields of the relation rel of a run's chain.
func tailOn(rel string) fields {
	return tailFields{rel: rel}
}

// Select is the select list a source of Logs, whose columns cols describes,
// is read with for the Query's tail: id, the built-in fields, and each
// Attribute the tail reads.
func (q *Query) Select(cols Columns) string {
	list := []string{cols.BuiltIn("id")}
	for _, name := range builtInOrder {
		list = append(list, cols.BuiltIn(name))
	}
	for _, k := range q.reads {
		list = append(list, cols.Attribute(k.t, k.key)+" AS "+chsql.Identifier(readColumn(k.t, k.key)))
	}
	return strings.Join(list, ", ")
}

// orderColumn numbers the rows of a sorted relation in their order, since
// SQL keeps no order from one query to the next.
const orderColumn = "#order"

// sourceRelation is the CTE of the Logs that pass a Query's head, which its
// tail reads. takenRelation is a subquery's, whose rows a stage numbers.
const (
	sourceRelation = "matched"
	takenRelation  = "taken"
)

// TableSQL writes the Query's tail as ClickHouse SQL over logs, a query of
// the Logs that pass its head read with Select, returning its table's rows,
// at most limit of them. Each stage is one CTE.
func (q *Query) TableSQL(logs string, limit int) string {
	c := &chain{prev: sourceRelation, isLogs: true}
	ctes := []string{sourceRelation + " AS (\n" + logs + "\n)"}
	q.writeTail(c, tailOn)
	for _, cte := range c.ctes {
		ctes = append(ctes, cte.name+" AS ("+cte.sql+")")
	}
	last := tailOn(c.prev)
	return fmt.Sprintf("WITH %s\nSELECT %s FROM %s ORDER BY %s LIMIT %d",
		strings.Join(ctes, ",\n"), selectList(q.output, last), c.prev, c.order(last), limit)
}

// chain is a Query written as a chain of CTEs, one per stage, each reading
// the one before.
type chain struct {
	ctes []cte
	// prev is the relation the next stage reads.
	prev string
	// ordered is set when prev's rows are numbered in their order, by
	// orderColumn; isLogs while they are Logs, newest first unless ordered.
	ordered, isLogs bool
}

type cte struct {
	name, sql string
}

// add adds a stage, reading prev.
func (c *chain) add(format string, args ...any) {
	name := fmt.Sprintf("stage%d", len(c.ctes)+1)
	c.ctes = append(c.ctes, cte{name: name, sql: fmt.Sprintf(format, args...)})
	c.prev = name
}

// order is what the chain's last relation, whose columns f reads, is
// ordered by.
func (c *chain) order(f fields) string {
	switch {
	case c.ordered:
		return f.Column(orderColumn)
	case c.isLogs:
		return newestFirstOrder(f)
	}
	return "ALL"
}

// newestFirstOrder orders Logs, whose fields f reads, newest first: by
// time, then by id in the order of its bytes.
func newestFirstOrder(f fields) string {
	return f.BuiltIn("time") + " DESC, " + chsql.UUIDOrder(f.BuiltIn("id")) + " DESC"
}

// writeTail adds the Query's tail to a chain over Logs, whose relations' Fields
// on reads.
func (q *Query) writeTail(c *chain, on func(rel string) fields) {
	order := chsql.Identifier(orderColumn)
	keepOrder := func(list string, f fields) string {
		if c.ordered {
			return list + ", " + f.Column(orderColumn) + " AS " + order
		}
		return list
	}
	// has are the columns of the relation so far, but for the order.
	has := map[string]bool{"id": true}
	for _, name := range builtInOrder {
		has[name] = true
	}
	hasOnly := func(columns []namedValue) {
		has = map[string]bool{}
		for _, col := range columns {
			has[col.name] = true
		}
	}
	taken := on(takenRelation)
	for i, s := range q.tail {
		f := on(c.prev)
		switch s := s.(type) {
		case whereStage:
			c.add("SELECT * FROM %s WHERE %s", c.prev, writeCondition(s.predicate, f, false))
		case projectStage:
			c.add("SELECT %s FROM %s", keepOrder(selectList(s.columns, f), f), c.prev)
			hasOnly(s.columns)
			c.isLogs = false
		case extendStage:
			var replaced []string
			for _, col := range s.columns {
				if has[col.name] {
					replaced = append(replaced, chsql.Identifier(col.name))
				}
				has[col.name] = true
			}
			all := "*"
			if len(replaced) > 0 {
				all = "* EXCEPT (" + strings.Join(replaced, ", ") + ")"
			}
			c.add("SELECT %s, %s FROM %s", all, selectList(s.columns, f), c.prev)
		case summarizeStage:
			if rates := q.rateColumns(s, i, f); rates != "" {
				c.add("SELECT *, %s FROM %s", rates, c.prev)
				f = on(c.prev)
			}
			all := append(append([]namedValue{}, s.by...), s.aggregates...)
			group := ""
			if len(s.by) > 0 {
				positions := make([]string, len(s.by))
				for i := range s.by {
					positions[i] = fmt.Sprint(i + 1)
				}
				group = " GROUP BY " + strings.Join(positions, ", ")
			}
			c.add("SELECT %s FROM %s%s", selectList(all, f), c.prev, group)
			hasOnly(all)
			c.ordered, c.isLogs = false, false
		case sortStage:
			c.add("SELECT %s, row_number() OVER (ORDER BY %s) AS %s FROM %s",
				exceptOrder(c.ordered), orderBy(s.keys, f), order, c.prev)
			c.ordered = true
		case takeStage:
			switch {
			case c.ordered:
				c.add("SELECT * FROM %s ORDER BY %s LIMIT %d", c.prev, f.Column(orderColumn), s.n)
			case c.isLogs:
				c.add("SELECT *, row_number() OVER (ORDER BY %s) AS %s FROM (SELECT * FROM %s ORDER BY %s LIMIT %d) AS %s",
					newestFirstOrder(taken), order, c.prev, newestFirstOrder(f), s.n, takenRelation)
				c.ordered = true
			default:
				c.add("SELECT * FROM %s LIMIT %d", c.prev, s.n)
			}
		case topStage:
			keys := []sortKey{s.key}
			c.add("SELECT *, row_number() OVER (ORDER BY %s) AS %s FROM (SELECT %s FROM %s ORDER BY %s LIMIT %d) AS %s",
				orderBy(keys, taken), order, exceptOrder(c.ordered), c.prev, orderBy(keys, f), s.n, takenRelation)
			c.ordered = true
		case countStage:
			c.add("SELECT count() AS %s FROM %s", chsql.Identifier("Count"), c.prev)
			has = map[string]bool{"Count": true}
			c.ordered, c.isLogs = false, false
		case distinctStage:
			c.add("SELECT DISTINCT %s FROM %s", selectList(s.columns, f), c.prev)
			hasOnly(s.columns)
			c.ordered, c.isLogs = false, false
		}
	}
}

func selectList(columns []namedValue, f fields) string {
	list := make([]string, len(columns))
	for i, col := range columns {
		list[i] = writeValue(col.x, f) + " AS " + chsql.Identifier(col.name)
	}
	return strings.Join(list, ", ")
}

func exceptOrder(ordered bool) string {
	if ordered {
		return "* EXCEPT (" + chsql.Identifier(orderColumn) + ")"
	}
	return "*"
}

// orderBy writes sort keys. A value of no fixed type sorts numbers first,
// then strings, then bools, then JSON.
func orderBy(keys []sortKey, f fields) string {
	var terms []string
	for _, k := range keys {
		direction := " DESC"
		if k.asc {
			direction = " ASC"
		}
		nulls := " NULLS LAST"
		if k.nullsFirst {
			nulls = " NULLS FIRST"
		}
		if k.x.typ() != dynamicType {
			terms = append(terms, writeValue(k.x, f)+direction+nulls)
			continue
		}
		for _, t := range []storage.AttributeType{storage.NumberAttribute, storage.StringAttribute, storage.BoolAttribute, storage.JSONAttribute} {
			var part string
			if ref, ok := k.x.(fieldRef); ok {
				part = f.Attribute(t, ref.name)
			} else {
				part = dynamicPart(writeValue(k.x, f), t)
			}
			terms = append(terms, part+direction+nulls)
		}
	}
	return strings.Join(terms, ", ")
}

// Column is one column of a Query's table: its name, and the type of its
// values: string, number, bool, datetime, timespan, or dynamic for values
// whose type differs from row to row. A column of the Query's last
// summarize, or a plain rename of one, says how to chart it: Agg is the
// aggregate it is (count, countif, dcount, sum, avg, min, max, percentile,
// rate or peak_rate), and BinSeconds the size of the time bins it holds.
// Only one column has BinSeconds, the table's time axis: the first that
// holds time bins.
type Column struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Agg        string  `json:"agg,omitempty"`
	BinSeconds float64 `json:"bin_seconds,omitempty"`
}

// Columns are the columns of the Query's table.
func (q *Query) Columns() []Column {
	columns := make([]Column, len(q.output))
	for i, col := range q.output {
		h := q.shape.hints[col.name]
		columns[i] = Column{Name: col.name, Type: col.x.typ().String(), Agg: h.agg, BinSeconds: h.bin.Seconds()}
	}
	return columns
}

// Row turns a row of the table, as storage's ScanLogTable returns it, into
// JSON values: times as RFC 3339 in UTC, a dynamic value as the value it
// holds.
func (q *Query) Row(raw []any) []any {
	row := make([]any, len(raw))
	for i, v := range raw {
		row[i] = tableCell(v)
	}
	return row
}

// tableCell is a cell of a Query's table as a JSON value: a time as RFC 3339
// in UTC, NaN and infinities as null, and a dynamic value, a tuple with one
// part set, as the value of that part.
func tableCell(v any) any {
	switch v := v.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return v
	case float32:
		return tableCell(float64(v))
	case []any:
		// A dynamic value: one part set, the rest NULL, in dynamicParts'
		// order.
		for i, t := range dynamicParts {
			if i >= len(v) || v[i] == nil {
				continue
			}
			if t != storage.JSONAttribute {
				return tableCell(v[i])
			}
			if j, ok := v[i].(string); ok && json.Valid([]byte(j)) {
				return json.RawMessage(j)
			}
		}
		return nil
	}
	return v
}
