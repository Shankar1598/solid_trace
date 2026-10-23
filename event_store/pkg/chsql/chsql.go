// Package chsql writes values into ClickHouse SQL text.
package chsql

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// escaper escapes what ClickHouse reads as an escape or an end inside a
// quoted string or identifier.
var escaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`, "`", "\\`", "\x00", `\0`)

// String quotes s as a string literal.
func String(s string) string {
	return "'" + escaper.Replace(s) + "'"
}

// Identifier quotes name as an identifier, such as one part of a JSON path.
func Identifier(name string) string {
	return "`" + escaper.Replace(name) + "`"
}

// Time writes t as a DateTime64 of microseconds in UTC. A column of time is
// compared only with such a value: 26.8.34 skips a boundary second when one
// is compared with a DateTime or an integer (ClickHouse #123999).
func Time(t time.Time) string {
	return "toDateTime64(" + String(t.UTC().Format("2006-01-02 15:04:05.000000")) + ", 6, 'UTC')"
}

// UUID writes id as a UUID.
func UUID(id uuid.UUID) string {
	return "toUUID('" + id.String() + "')"
}

// UUIDOrder writes x, a UUID, as a value that orders UUIDs by their bytes,
// as they are made, which ClickHouse's own order of UUIDs doesn't: it
// compares their second halves first.
func UUIDOrder(x string) string {
	return "toUInt128(" + x + ")"
}
