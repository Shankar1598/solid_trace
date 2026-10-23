package logquery

import (
	"fmt"
	"strings"

	"github.com/solidtrace/event_store/pkg/chsql"
	"github.com/solidtrace/event_store/storage"
)

// readableLogs is the table a Query's Readable SQL reads: SQL mode's logs,
// which holds a Project's Logs as the logs table does, with their Attributes
// in attributes and attrs_json.
const readableLogs = "logs"

// ReadableSQL writes the Query as ClickHouse SQL against SQL mode's logs
// table, for a person to read and change: its head as one condition, then
// one CTE per stage, every Attribute read from attributes or attrs_json. Run
// in SQL mode over the same Project and range, it returns the Query's rows:
// a Query of Log rows gives the table's rows, newest first; a table gives its
// columns, a value of no fixed type as JSON text.
//
// It reads no range of its own, since SQL mode reads only the run's; a where
// on time stays in it. Unlike a run, a table isn't cut at a number of rows.
func (q *Query) ReadableSQL() string {
	on := func(rel string) fields { return storage.ClickHouseColumns{Table: rel} }
	where := q.Where(storage.ClickHouseColumns{})
	if !q.table {
		// The tail is only takes and newest-first sorts.
		final := "SELECT * FROM " + readableLogs
		if where != "" {
			final += "\nWHERE " + where
		}
		final += "\nORDER BY " + newestFirstOrder(storage.ClickHouseColumns{})
		if q.hasTake {
			final += fmt.Sprintf(" LIMIT %d", q.take)
		}
		return final
	}

	c := &chain{prev: readableLogs, isLogs: true}
	if where != "" {
		c.add("SELECT * FROM %s WHERE %s", c.prev, where)
	}
	q.writeTail(c, on)
	last := on(c.prev)
	list := make([]string, len(q.output))
	for i, col := range q.output {
		value := writeValue(col.x, last)
		if col.x.typ() == dynamicType {
			value = dynamicJSON(col.x, last)
		}
		list[i] = value + " AS " + chsql.Identifier(col.name)
	}
	ctes := make([]string, len(c.ctes))
	for i, cte := range c.ctes {
		ctes[i] = cte.name + " AS (\n  " + cte.sql + "\n)"
	}
	return "WITH\n" + strings.Join(ctes, ",\n") + fmt.Sprintf("\nSELECT %s\nFROM %s\nORDER BY %s",
		strings.Join(list, ", "), c.prev, c.order(last))
}

// dynamicJSON writes a value of no fixed type as JSON text: the value of its
// type it holds, or NULL.
func dynamicJSON(x expr, f fields) string {
	part := partsOf(x, f)
	var cases []string
	for _, t := range dynamicParts[:len(dynamicParts)-1] {
		cases = append(cases, "isNotNull("+part(t)+"), toJSONString("+part(t)+")")
	}
	return "multiIf(" + strings.Join(cases, ", ") + ", " + part(storage.JSONAttribute) + ")"
}
