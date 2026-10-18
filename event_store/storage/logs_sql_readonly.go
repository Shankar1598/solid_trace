package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// stateChangingFunctions are the functions a SELECT can call that change
// something: the database, its settings, its logs or profiling, the
// parser, a sequence or the random seed. query and query_table run SQL
// written in a string, and json_execute_serialized_sql a serialized SELECT,
// which the parsed query doesn't show. The list is DuckDB 1.5.6's.
var stateChangingFunctions = map[string]bool{
	"query":                       true,
	"query_table":                 true,
	"json_execute_serialized_sql": true,
	"enable_logging":              true,
	"disable_logging":             true,
	"truncate_duckdb_logs":        true,
	"write_log":                   true,
	"enable_profiling":            true,
	"disable_profiling":           true,
	"enable_peg_parser":           true,
	"disable_peg_parser":          true,
	"checkpoint":                  true,
	"force_checkpoint":            true,
	"nextval":                     true,
	"setseed":                     true,
}

// readOnlyRefusal is the message a SQL query gets when it could change
// something: what isn't allowed.
func readOnlyRefusal(what string) error {
	return &SQLError{fmt.Errorf("SQL queries are read-only: %s isn't allowed", what)}
}

// parsedSQL is what json_serialize_sql answers: the parsed statements, or
// why it couldn't give them.
type parsedSQL struct {
	Error        bool   `json:"error"`
	ErrorType    string `json:"error_type"`
	ErrorMessage string `json:"error_message"`
	// Position is the byte offset of a parser error, which DuckDB 1.5.6
	// writes as a string.
	Position   any   `json:"position"`
	Statements []any `json:"statements"`
}

// checkReadOnly refuses a SQL query, with a *SQLError, unless it is one
// SELECT that calls none of the stateChangingFunctions, and returns the
// SELECT as DuckDB parsed it. DuckDB parses it,
// bound as a parameter so that none of it runs, and nothing of it is
// prepared or run before it passes: duckdb-go runs every statement but the
// last even when it only prepares. SQL that doesn't parse gets DuckDB's
// message, as if it had run. An error that isn't a *SQLError is a failure
// to check it.
func (s *LogStore) checkReadOnly(ctx context.Context, query string) (any, error) {
	var serialized string
	if err := s.db.QueryRowContext(ctx, "SELECT CAST(json_serialize_sql(CAST(? AS VARCHAR)) AS VARCHAR)", query).Scan(&serialized); err != nil {
		return nil, fmt.Errorf("parse the SQL: %w", err)
	}
	var parsed parsedSQL
	if err := json.Unmarshal([]byte(serialized), &parsed); err != nil {
		return nil, fmt.Errorf("read the parsed SQL: %w", err)
	}
	switch {
	case parsed.Error && parsed.ErrorType == "not implemented" && strings.Contains(parsed.ErrorMessage, "Only SELECT"):
		return nil, readOnlyRefusal(statementKind(query))
	case parsed.Error:
		return nil, &SQLError{errors.New(duckDBMessage(query, parsed))}
	case len(parsed.Statements) == 0:
		return nil, &SQLError{errors.New("The SQL has no statement to run.")}
	case len(parsed.Statements) > 1:
		return nil, readOnlyRefusal("more than one statement")
	}
	if name := calledFunction(parsed.Statements[0], stateChangingFunctions); name != "" {
		return nil, readOnlyRefusal(name + "()")
	}
	return parsed.Statements[0], nil
}

// calledFunction is the first of functions, a set of lower-case names, that
// a parsed statement calls, as a scalar, aggregate, window or table
// function, anywhere in it, or "" if it calls none. Every call in DuckDB's
// parsed query is an object with a function_name; its schema and catalog
// don't matter.
func calledFunction(node any, functions map[string]bool) string {
	switch node := node.(type) {
	case map[string]any:
		if name, ok := node["function_name"].(string); ok && functions[strings.ToLower(name)] {
			return strings.ToLower(name)
		}
		for _, key := range slices.Sorted(maps.Keys(node)) {
			if name := calledFunction(node[key], functions); name != "" {
				return name
			}
		}
	case []any:
		for _, child := range node {
			if name := calledFunction(child, functions); name != "" {
				return name
			}
		}
	}
	return ""
}

// selectKeywords are the words a statement that parses as a SELECT may
// start with.
var selectKeywords = map[string]bool{
	"SELECT": true, "WITH": true, "FROM": true, "VALUES": true, "TABLE": true,
	"DESCRIBE": true, "SHOW": true, "SUMMARIZE": true,
}

// statementKind names the statement that isn't a SELECT in SQL that DuckDB
// refused to serialize: the SQL's first keyword, such as DROP, past
// comments and parentheses. When that word starts a SELECT, the statement
// is a later one or follows a WITH, and is named only as not a SELECT.
func statementKind(query string) string {
	rest := query
	for {
		rest = strings.TrimLeftFunc(rest, func(r rune) bool { return unicode.IsSpace(r) || r == '(' })
		switch {
		case strings.HasPrefix(rest, "--"):
			_, rest, _ = strings.Cut(rest, "\n")
			continue
		case strings.HasPrefix(rest, "/*"):
			_, rest, _ = strings.Cut(rest, "*/")
			continue
		}
		break
	}
	end := strings.IndexFunc(rest, func(r rune) bool { return !unicode.IsLetter(r) && r != '_' })
	if end < 0 {
		end = len(rest)
	}
	keyword := strings.ToUpper(rest[:end])
	if keyword == "" || selectKeywords[keyword] {
		return "a statement other than SELECT"
	}
	return keyword
}

// duckDBMessage writes json_serialize_sql's error as DuckDB writes the
// error of a query that fails: its type, its message, and the line it
// points at with a caret under the place.
func duckDBMessage(query string, parsed parsedSQL) string {
	kind := parsed.ErrorType
	if kind != "" {
		kind = strings.ToUpper(kind[:1]) + kind[1:]
	}
	message := fmt.Sprintf("%s Error: %s", kind, parsed.ErrorMessage)
	position := -1
	switch at := parsed.Position.(type) {
	case string:
		if n, err := strconv.Atoi(at); err == nil {
			position = n
		}
	case float64:
		position = int(at)
	}
	if position < 0 || position > len(query) {
		return message
	}
	start := strings.LastIndexByte(query[:position], '\n') + 1
	end := strings.IndexByte(query[position:], '\n')
	if end < 0 {
		end = len(query) - position
	}
	line := strings.Count(query[:start], "\n") + 1
	prefix := fmt.Sprintf("LINE %d: ", line)
	caret := strings.Repeat(" ", len(prefix)+len([]rune(query[start:position]))) + "^"
	return fmt.Sprintf("%s\n\n%s%s\n%s", message, prefix, query[start:position+end], caret)
}
