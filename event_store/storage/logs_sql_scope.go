package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/solidtrace/event_store/pkg/sqltext"
)

// EventsView is the name of the view SQL queries read a Project's Events
// from: the events view's columns, the Project's Events of the time range
// only. It hides the global events view of every Project's Events.
const EventsView = "events"

// readNothingTableFunctions are the table functions a SQL query may call:
// they read only their arguments.
var readNothingTableFunctions = map[string]bool{
	"range":           true,
	"generate_series": true,
	"unnest":          true,
}

// catalogFunctions are the scalar functions that read settings, variables
// or the catalogue, which the parsed query shows only as a call: the
// pg_catalog macros read duckdb_views() and duckdb_constraints() when they
// run. The list is DuckDB 1.5.6's.
var catalogFunctions = map[string]bool{
	"current_setting":      true,
	"getvariable":          true,
	"pg_get_viewdef":       true,
	"pg_get_constraintdef": true,
}

// allowedTableRefs are the kinds of table reference that read nothing of
// their own, only what they hold: DuckDB 1.5.6's, but for BASE_TABLE,
// TABLE_FUNCTION and SHOW_REF, which scopeWalk checks.
var allowedTableRefs = map[string]bool{
	"SUBQUERY":        true,
	"JOIN":            true,
	"EXPRESSION_LIST": true,
	"EMPTY":           true,
	"PIVOT":           true,
}

// scopeRefusal is the message a SQL query gets when it reads more than its
// Project's views: what isn't allowed.
func scopeRefusal(what string) error {
	return &SQLError{fmt.Errorf("SQL queries can read only the %s and %s views: %s isn't allowed", LogsView, EventsView, what)}
}

// checkScope refuses a SQL query, with a *SQLError, unless all it reads is
// the logs and events views, unqualified (ADR 0003): every table it names
// is one of them or a WITH part in scope, every table function reads
// nothing, and it calls none of the catalogFunctions. It also refuses a
// WITH name that isn't a plain identifier, which DuckDB could read as a
// file, or that is a table or view in the database, the global events view
// among them, so that a slip in following scope can't reach the real one. statement is the SELECT as
// DuckDB parsed it (checkReadOnly), which shows the user's SQL, not the
// views', so the views' own reads aren't checked. It says whether the query
// reads the events view.
func (s *LogStore) checkScope(ctx context.Context, statement any) (bool, error) {
	if name := calledFunction(statement, catalogFunctions); name != "" {
		return false, scopeRefusal(name + "()")
	}
	w := scopeWalk{withNames: map[string]string{}}
	if what := w.walk(statement, nil); what != "" {
		return false, scopeRefusal(what)
	}
	if len(w.withNames) == 0 {
		return w.events, nil
	}
	names := make([]string, 0, len(w.withNames))
	for _, name := range slices.Sorted(maps.Keys(w.withNames)) {
		names = append(names, sqltext.String(name))
	}
	var taken string
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT lower(name) FROM (
			SELECT table_name AS name FROM duckdb_tables()
			UNION ALL SELECT view_name FROM duckdb_views()
		) WHERE lower(name) IN (%s) ORDER BY 1 LIMIT 1`, strings.Join(names, ", "))).Scan(&taken)
	switch {
	case err == nil:
		return false, scopeRefusal(w.withNames[taken])
	case !errors.Is(err, sql.ErrNoRows):
		return false, fmt.Errorf("look up the SQL's WITH names: %w", err)
	}
	return w.events, nil
}

// scopeWalk walks a parsed query for checkScope.
type scopeWalk struct {
	// events is set when the query reads the events view.
	events bool
	// withNames are the query's WITH names in lower case, each as written.
	withNames map[string]string
}

// walk returns the first thing node reads that isn't allowed, as the
// refusal names it, or "". scope holds the WITH names in scope, in lower
// case. Every query node in DuckDB's parse has a cte_map, every expression
// a class, and every table reference a type, an alias and a sample.
func (w *scopeWalk) walk(node any, scope map[string]bool) string {
	switch node := node.(type) {
	case map[string]any:
		// A recursive WITH part's body names itself.
		if name, ok := node["cte_name"].(string); ok {
			scope = withName(scope, name)
		}
		if ctes, ok := node["cte_map"].(map[string]any); ok {
			var what string
			if scope, what = w.walkWith(ctes, scope); what != "" {
				return what
			}
		}
		if isTableRef(node) {
			if what := w.tableRef(node, scope); what != "" {
				return what
			}
		}
		for _, key := range slices.Sorted(maps.Keys(node)) {
			if key == "cte_map" {
				continue
			}
			if what := w.walk(node[key], scope); what != "" {
				return what
			}
		}
	case []any:
		for _, child := range node {
			if what := w.walk(child, scope); what != "" {
				return what
			}
		}
	}
	return ""
}

// walkWith walks a query node's WITH parts as DuckDB binds them, each with
// only the WITH names before it in scope: a part that names itself or a
// later one names a table instead. It returns the scope of the node's body,
// which has every WITH name in it, or what isn't allowed.
func (w *scopeWalk) walkWith(ctes map[string]any, scope map[string]bool) (map[string]bool, string) {
	entries, _ := ctes["map"].([]any)
	for _, entry := range entries {
		entry, _ := entry.(map[string]any)
		name, _ := entry["key"].(string)
		if !plainIdentifier.MatchString(name) {
			return nil, name
		}
		if what := w.walk(entry["value"], scope); what != "" {
			return nil, what
		}
		scope = withName(scope, name)
		w.withNames[strings.ToLower(name)] = name
	}
	return scope, ""
}

// plainIdentifier is a WITH name that DuckDB can't read as a file or URL.
var plainIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// withName is scope with a WITH name added, in lower case.
func withName(scope map[string]bool, name string) map[string]bool {
	inner := maps.Clone(scope)
	if inner == nil {
		inner = map[string]bool{}
	}
	inner[strings.ToLower(name)] = true
	return inner
}

// isTableRef says whether a node of DuckDB's parse is a table reference.
func isTableRef(node map[string]any) bool {
	_, alias := node["alias"]
	_, sample := node["sample"]
	_, class := node["class"]
	_, modifiers := node["modifiers"]
	return alias && sample && !class && !modifiers
}

// tableRef returns what a table reference reads that isn't allowed, or "".
// What it holds, such as a subquery, is walked after it.
func (w *scopeWalk) tableRef(node map[string]any, scope map[string]bool) string {
	kind, _ := node["type"].(string)
	switch kind {
	case "BASE_TABLE":
		catalog, _ := node["catalog_name"].(string)
		schema, _ := node["schema_name"].(string)
		table, _ := node["table_name"].(string)
		if catalog == "" && schema == "" {
			name := strings.ToLower(table)
			switch {
			case scope[name]:
				return ""
			case name == LogsView:
				return ""
			case name == EventsView:
				w.events = true
				return ""
			}
		}
		var parts []string
		for _, part := range []string{catalog, schema, table} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, ".")
	case "TABLE_FUNCTION":
		function, _ := node["function"].(map[string]any)
		name, _ := function["function_name"].(string)
		if readNothingTableFunctions[strings.ToLower(name)] {
			return ""
		}
		return strings.ToLower(name) + "()"
	case "SHOW_REF":
		// DESCRIBE, SHOW and SUMMARIZE of a table or a query hold it as a
		// query. SHOW TABLES and SHOW ALL TABLES name a table instead.
		if table, _ := node["table_name"].(string); table != "" || node["query"] == nil {
			return "SHOW TABLES"
		}
		return ""
	}
	if allowedTableRefs[kind] {
		return ""
	}
	return "a " + kind + " table reference"
}

// createEventsView creates the events view on a SQL query's connection:
// the global events view's Events of the Project in [from, to). The global
// view is named with its database, as the temporary view hides it.
func createEventsView(ctx context.Context, conn *sql.Conn, projectID uint32, from, to time.Time) error {
	var database string
	if err := conn.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, fmt.Sprintf(
		"CREATE TEMPORARY VIEW %s AS SELECT * FROM %s.main.events WHERE project_id = %d AND timestamp >= %s AND timestamp < %s",
		EventsView, sqltext.Identifier(database), projectID, timestampLiteral(from), timestampLiteral(to)))
	return err
}
