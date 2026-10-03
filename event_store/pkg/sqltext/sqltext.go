// Package sqltext writes values into DuckDB SQL text.
package sqltext

import "strings"

// String quotes s as a SQL string literal.
func String(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Identifier quotes name as a SQL identifier.
func Identifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
