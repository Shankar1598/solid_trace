package storage

import (
	"maps"
	"slices"
	"strings"

	"github.com/solidtrace/event_store/pkg/sqltext"
)

// slowTextMatch is a text match DuckDB runs several times slower on message
// than its string functions: its operator, and whether it counts case.
type slowTextMatch struct {
	operator      string
	caseSensitive bool
}

// slowTextMatches are the slow text matches by the function name DuckDB
// 1.5.6 parses them as: ILIKE, and LIKE with ESCAPE. A plain LIKE isn't
// one, since DuckDB itself rewrites LIKE '%x%' as contains.
var slowTextMatches = map[string]slowTextMatch{
	"~~*":              {"ILIKE", false},
	"!~~*":             {"NOT ILIKE", false},
	"ilike_escape":     {"ILIKE", false},
	"not_ilike_escape": {"NOT ILIKE", false},
	"like_escape":      {"LIKE", true},
	"not_like_escape":  {"NOT LIKE", true},
}

// sqlNotice is the advice a SQL query's answer carries when it matches the
// message column slowly: the first of slowTextMatches it makes on message,
// with a pattern that starts with %, and what to write instead. "" when it
// makes none. A pattern that starts otherwise fails fast on most messages,
// and % alone matches every one.
// statement is the SELECT as DuckDB parsed it (checkReadOnly).
func sqlNotice(statement any) string {
	switch node := statement.(type) {
	case map[string]any:
		if notice := textMatchNotice(node); notice != "" {
			return notice
		}
		for _, key := range slices.Sorted(maps.Keys(node)) {
			if notice := sqlNotice(node[key]); notice != "" {
				return notice
			}
		}
	case []any:
		for _, child := range node {
			if notice := sqlNotice(child); notice != "" {
				return notice
			}
		}
	}
	return ""
}

// textMatchNotice is sqlNotice's advice for one parsed call, or "" unless it
// is a slow text match on message.
func textMatchNotice(node map[string]any) string {
	name, _ := node["function_name"].(string)
	match, ok := slowTextMatches[strings.ToLower(name)]
	children, _ := node["children"].([]any)
	if !ok || len(children) < 2 || !isMessageColumn(children[0]) {
		return ""
	}
	pattern, ok := constantString(children[1])
	if !ok || !strings.HasPrefix(pattern, "%") || pattern == "%" {
		return ""
	}
	slow := "message " + match.operator + " " + sqltext.String(pattern)
	escape := ""
	if len(children) > 2 {
		escape, _ = constantString(children[2])
		slow += " ESCAPE " + sqltext.String(escape)
	}
	not := ""
	if strings.HasPrefix(match.operator, "NOT ") {
		not = "NOT "
	}

	// %term%, with no wildcard or escape in term, is contains.
	term := strings.TrimSuffix(strings.TrimPrefix(pattern, "%"), "%")
	if strings.HasSuffix(pattern, "%") && !strings.ContainsAny(term, "%_"+escape) {
		fast := "contains(lower(message), " + sqltext.String(strings.ToLower(term)) + ")"
		if match.caseSensitive {
			fast = "contains(message, " + sqltext.String(term) + ")"
		}
		return slow + " is slow on Logs: " + not + fast + " matches the same Logs several times faster."
	}
	flags := "(?i)"
	if match.caseSensitive {
		flags = ""
	}
	return slow + " is slow on Logs: " + not + "regexp_matches(message, '" + flags + "…'), with . for _ and .* for %, is faster."
}

// isMessageColumn is whether a parsed expression is the message column,
// qualified or not.
func isMessageColumn(node any) bool {
	ref, _ := node.(map[string]any)
	if ref["class"] != "COLUMN_REF" {
		return false
	}
	names, _ := ref["column_names"].([]any)
	if len(names) == 0 {
		return false
	}
	last, _ := names[len(names)-1].(string)
	return strings.EqualFold(last, "message")
}

// constantString is a parsed expression's value when it is a VARCHAR
// constant that isn't NULL.
func constantString(node any) (string, bool) {
	constant, _ := node.(map[string]any)
	if constant["class"] != "CONSTANT" {
		return "", false
	}
	value, _ := constant["value"].(map[string]any)
	s, ok := value["value"].(string)
	return s, ok && value["is_null"] != true
}
