package logquery

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/solidtrace/event_store/pkg/sqltext"
	"github.com/solidtrace/event_store/storage"
)

// readColumn is the name of the column that holds the Attribute key of type
// t in the Logs a tail reads.
func readColumn(t storage.AttributeType, key string) string {
	return "@" + string(t) + ":" + key
}

// tailFields are the columns of the Logs a tail reads: the built-in fields
// by name, and each Attribute it reads under its readColumn.
type tailFields struct{}

func (tailFields) BuiltIn(name string) string {
	return sqltext.Identifier(name)
}

func (tailFields) Attribute(t storage.AttributeType, key string) string {
	return sqltext.Identifier(readColumn(t, key))
}

// Select is the select list a source of Logs, whose columns cols describes,
// is read with for the Query's tail: id, the built-in fields, and each
// Attribute the tail reads.
func (q *Query) Select(cols storage.LogColumns) string {
	list := []string{cols.BuiltIn("id")}
	for _, name := range builtInOrder {
		list = append(list, cols.BuiltIn(name))
	}
	for _, k := range q.reads {
		list = append(list, cols.Attribute(k.t, k.key)+" AS "+sqltext.Identifier(readColumn(k.t, k.key)))
	}
	return strings.Join(list, ", ")
}

// orderColumn numbers the rows of a sorted relation in their order, since
// SQL keeps no order from one query to the next.
const orderColumn = `"#order"`

// TableSQL writes the Query's tail as SQL over logs, the Logs that pass its
// head read with Select, returning its table's rows, at most limit of them.
// Each stage is one CTE.
func (q *Query) TableSQL(logs string, limit int) string {
	c := &chain{prev: "logs", isLogs: true}
	ctes := []string{"logs AS (\n" + logs + "\n)"}
	q.writeTail(c, tailFields{})
	for _, cte := range c.ctes {
		ctes = append(ctes, cte.name+" AS ("+cte.sql+")")
	}
	return fmt.Sprintf("WITH %s\nSELECT %s FROM %s ORDER BY %s LIMIT %d",
		strings.Join(ctes, ",\n"), selectList(q.output, tailFields{}), c.prev, c.order(), limit)
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

// order is what the chain's last relation is ordered by.
func (c *chain) order() string {
	switch {
	case c.ordered:
		return orderColumn
	case c.isLogs:
		return newestFirstOrder
	}
	return "ALL"
}

// newestFirstOrder orders Logs newest first.
const newestFirstOrder = `"time" DESC, "id" DESC`

// writeTail adds the Query's tail to a chain over Logs, whose Fields f reads.
func (q *Query) writeTail(c *chain, f fields) {
	keepOrder := func(list string) string {
		if c.ordered {
			return list + ", " + orderColumn
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
	for i, s := range q.tail {
		switch s := s.(type) {
		case whereStage:
			c.add("SELECT * FROM %s WHERE %s", c.prev, writeCondition(s.predicate, f, false))
		case projectStage:
			c.add("SELECT %s FROM %s", keepOrder(selectList(s.columns, f)), c.prev)
			hasOnly(s.columns)
			c.isLogs = false
		case extendStage:
			var replaced []string
			for _, col := range s.columns {
				if has[col.name] {
					replaced = append(replaced, sqltext.Identifier(col.name))
				}
				has[col.name] = true
			}
			all := "*"
			if len(replaced) > 0 {
				all = "* EXCLUDE (" + strings.Join(replaced, ", ") + ")"
			}
			c.add("SELECT %s, %s FROM %s", all, selectList(s.columns, f), c.prev)
		case summarizeStage:
			if rates := q.perSecondColumns(s, i, f); rates != "" {
				c.add("SELECT *, %s FROM %s", rates, c.prev)
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
				exceptOrder(c.ordered), orderBy(s.keys, f), orderColumn, c.prev)
			c.ordered = true
		case takeStage:
			switch {
			case c.ordered:
				c.add("SELECT * FROM %s ORDER BY %s LIMIT %d", c.prev, orderColumn, s.n)
			case c.isLogs:
				c.add("SELECT *, row_number() OVER (ORDER BY %[1]s) AS %[2]s FROM (SELECT * FROM %[3]s ORDER BY %[1]s LIMIT %[4]d)",
					newestFirstOrder, orderColumn, c.prev, s.n)
				c.ordered = true
			default:
				c.add("SELECT * FROM %s LIMIT %d", c.prev, s.n)
			}
		case topStage:
			order := orderBy([]sortKey{s.key}, f)
			c.add("SELECT *, row_number() OVER (ORDER BY %[1]s) AS %[2]s FROM (SELECT %[3]s FROM %[4]s ORDER BY %[1]s LIMIT %[5]d)",
				order, orderColumn, exceptOrder(c.ordered), c.prev, s.n)
			c.ordered = true
		case countStage:
			c.add(`SELECT count(*) AS "Count" FROM %s`, c.prev)
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
		list[i] = writeValue(col.x, f) + " AS " + sqltext.Identifier(col.name)
	}
	return strings.Join(list, ", ")
}

func exceptOrder(ordered bool) string {
	if ordered {
		return "* EXCLUDE (" + orderColumn + ")"
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
				part = "struct_extract(" + writeValue(k.x, f) + ", " + sqltext.String(dynamicField(t)) + ")"
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
// aggregate it is (count, countif, dcount, sum, avg, min, max or
// percentile), and BinSeconds the size of the time bins it holds. Only one
// column has BinSeconds, the table's time axis: the first that holds time
// bins.
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

// Row turns a row of the table as DuckDB returns it into JSON values: times
// as RFC 3339 in UTC, a dynamic value as the value it holds.
func (q *Query) Row(raw []any) []any {
	row := make([]any, len(raw))
	for i, v := range raw {
		row[i] = tableCell(v)
	}
	return row
}

// tableCell is a cell of a Query's table as a JSON value: a time as RFC 3339
// in UTC, NaN and infinities as null, and a dynamic value, a struct with one
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
	case map[string]any:
		// A dynamic value: one part set, the rest NULL.
		for _, field := range []string{"s", "n", "b"} {
			if part := v[field]; part != nil {
				return tableCell(part)
			}
		}
		if j, ok := v["j"].(string); ok && json.Valid([]byte(j)) {
			return json.RawMessage(j)
		}
		return nil
	}
	return v
}
