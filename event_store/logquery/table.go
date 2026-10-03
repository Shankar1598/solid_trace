package logquery

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

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
	return sqlIdentifier(name)
}

func (tailFields) Attribute(t storage.AttributeType, key string) string {
	return sqlIdentifier(readColumn(t, key))
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
		list = append(list, cols.Attribute(k.t, k.key)+" AS "+sqlIdentifier(readColumn(k.t, k.key)))
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
	f := tailFields{}
	ctes := []string{"logs AS (\n" + logs + "\n)"}
	prev := "logs"
	ordered, isLogs := false, true
	add := func(format string, args ...any) {
		name := fmt.Sprintf("stage%d", len(ctes))
		ctes = append(ctes, name+" AS ("+fmt.Sprintf(format, args...)+")")
		prev = name
	}
	keepOrder := func(list string) string {
		if ordered {
			return list + ", " + orderColumn
		}
		return list
	}
	newestFirst := `"time" DESC, "id" DESC`
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
	for _, s := range q.tail {
		switch s := s.(type) {
		case whereStage:
			add("SELECT * FROM %s WHERE %s", prev, writeCondition(s.predicate, f, false))
		case projectStage:
			add("SELECT %s FROM %s", keepOrder(selectList(s.columns, f)), prev)
			hasOnly(s.columns)
			isLogs = false
		case extendStage:
			var replaced []string
			for _, col := range s.columns {
				if has[col.name] {
					replaced = append(replaced, sqlIdentifier(col.name))
				}
				has[col.name] = true
			}
			all := "*"
			if len(replaced) > 0 {
				all = "* EXCLUDE (" + strings.Join(replaced, ", ") + ")"
			}
			add("SELECT %s, %s FROM %s", all, selectList(s.columns, f), prev)
		case summarizeStage:
			all := append(append([]namedValue{}, s.by...), s.aggregates...)
			group := ""
			if len(s.by) > 0 {
				positions := make([]string, len(s.by))
				for i := range s.by {
					positions[i] = fmt.Sprint(i + 1)
				}
				group = " GROUP BY " + strings.Join(positions, ", ")
			}
			add("SELECT %s FROM %s%s", selectList(all, f), prev, group)
			hasOnly(all)
			ordered, isLogs = false, false
		case sortStage:
			add("SELECT %s, row_number() OVER (ORDER BY %s) AS %s FROM %s",
				exceptOrder(ordered), orderBy(s.keys, f), orderColumn, prev)
			ordered = true
		case takeStage:
			switch {
			case ordered:
				add("SELECT * FROM %s ORDER BY %s LIMIT %d", prev, orderColumn, s.n)
			case isLogs:
				add("SELECT *, row_number() OVER (ORDER BY %[1]s) AS %[2]s FROM (SELECT * FROM %[3]s ORDER BY %[1]s LIMIT %[4]d)",
					newestFirst, orderColumn, prev, s.n)
				ordered = true
			default:
				add("SELECT * FROM %s LIMIT %d", prev, s.n)
			}
		case topStage:
			order := orderBy([]sortKey{s.key}, f)
			add("SELECT *, row_number() OVER (ORDER BY %[1]s) AS %[2]s FROM (SELECT %[3]s FROM %[4]s ORDER BY %[1]s LIMIT %[5]d)",
				order, orderColumn, exceptOrder(ordered), prev, s.n)
			ordered = true
		case countStage:
			add(`SELECT count(*) AS "Count" FROM %s`, prev)
			has = map[string]bool{"Count": true}
			ordered, isLogs = false, false
		case distinctStage:
			add("SELECT DISTINCT %s FROM %s", selectList(s.columns, f), prev)
			hasOnly(s.columns)
			ordered, isLogs = false, false
		}
	}
	order := "ALL"
	switch {
	case ordered:
		order = orderColumn
	case isLogs:
		order = newestFirst
	}
	return fmt.Sprintf("WITH %s\nSELECT %s FROM %s ORDER BY %s LIMIT %d",
		strings.Join(ctes, ",\n"), selectList(q.output, f), prev, order, limit)
}

func selectList(columns []namedValue, f fields) string {
	list := make([]string, len(columns))
	for i, col := range columns {
		list[i] = writeValue(col.x, f) + " AS " + sqlIdentifier(col.name)
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
				part = "struct_extract(" + writeValue(k.x, f) + ", " + quoteString(dynamicField(t)) + ")"
			}
			terms = append(terms, part+direction+nulls)
		}
	}
	return strings.Join(terms, ", ")
}

// Column is one column of a Query's table: its name, and the type of its
// values: string, number, bool, datetime, timespan, or dynamic for values
// whose type differs from row to row.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Columns are the columns of the Query's table.
func (q *Query) Columns() []Column {
	columns := make([]Column, len(q.output))
	for i, col := range q.output {
		columns[i] = Column{Name: col.name, Type: col.x.typ().String()}
	}
	return columns
}

// Row turns a row of the table as DuckDB returns it into JSON values: times
// as RFC 3339 in UTC, a dynamic value as the value it holds.
func (q *Query) Row(raw []any) []any {
	row := make([]any, len(raw))
	for i, v := range raw {
		row[i] = jsonValue(v)
	}
	return row
}

func jsonValue(v any) any {
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
				return jsonValue(part)
			}
		}
		if j, ok := v["j"].(string); ok && json.Valid([]byte(j)) {
			return json.RawMessage(j)
		}
		return nil
	}
	return v
}
