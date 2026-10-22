package logquery

import (
	"fmt"
	"strings"

	"github.com/solidtrace/event_store/pkg/sqltext"
	"github.com/solidtrace/event_store/storage"
)

// ReadableSQL writes the Query as SQL against the logs view
// (storage.LogsView), for a person to read and change: one CTE per stage,
// every Attribute read from its map. Run through Run SQL over the same Project
// and range, it returns the Query's rows: a Query of Log rows gives the view's
// rows, newest first; a table gives its columns, a value of no fixed type as
// JSON.
//
// It reads no range of its own, since the view holds only the run's; a where
// on time stays in it. Unlike a run, a table isn't cut at a number of rows.
func (q *Query) ReadableSQL() string {
	cols := storage.LogViewColumns()
	c := &chain{prev: storage.LogsView, isLogs: true}
	for _, s := range q.head {
		switch s := s.(type) {
		case whereStage:
			c.add("SELECT * FROM %s WHERE %s", c.prev, writeCondition(s.predicate, cols, false))
		case searchStage:
			c.add("SELECT * FROM %s WHERE %s", c.prev, writeSearch(s.terms, cols))
		}
	}

	var final string
	if !q.table {
		// The tail is only takes and newest-first sorts.
		final = fmt.Sprintf("SELECT * FROM %s ORDER BY %s", c.prev, newestFirstOrder)
		if q.hasTake {
			final += fmt.Sprintf(" LIMIT %d", q.take)
		}
	} else {
		q.writeTail(c, cols)
		list := make([]string, len(q.output))
		for i, col := range q.output {
			value := writeValue(col.x, cols)
			if col.x.typ() == dynamicType {
				value = dynamicJSON(col.x, cols)
			}
			list[i] = value + " AS " + sqltext.Identifier(col.name)
		}
		final = fmt.Sprintf("SELECT %s\nFROM %s\nORDER BY %s", strings.Join(list, ", "), c.prev, c.order())
	}
	if len(c.ctes) == 0 {
		return final
	}
	ctes := make([]string, len(c.ctes))
	for i, cte := range c.ctes {
		ctes[i] = cte.name + " AS (\n  " + cte.sql + "\n)"
	}
	return "WITH\n" + strings.Join(ctes, ",\n") + "\n" + final
}

// dynamicJSON writes a value of no fixed type as JSON: the value of its type
// it holds, or NULL.
func dynamicJSON(x expr, f fields) string {
	parts := make([]string, len(dynamicParts))
	for i, t := range dynamicParts {
		var part string
		if ref, ok := x.(fieldRef); ok {
			part = f.Attribute(t, ref.name)
		} else {
			part = "struct_extract(" + writeValue(x, f) + ", " + sqltext.String(dynamicField(t)) + ")"
		}
		if t == storage.JSONAttribute {
			parts[i] = "CAST(" + part + " AS JSON)"
		} else {
			parts[i] = "to_json(" + part + ")"
		}
	}
	return "CAST(coalesce(" + strings.Join(parts, ", ") + ") AS JSON)"
}
