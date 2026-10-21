package storage

import "strings"

// LogFileOrder is a Project's Log file order: the Fields the hourly export
// sorts each Log file by, before time and id, so that the Logs of one source
// sit together in a few row groups. Each is a built-in field (service,
// environment or level) or an Attribute, read as a string. Empty means time
// only.
type LogFileOrder []string

// DefaultLogFileOrder is the Log file order of a Project that sets none.
var DefaultLogFileOrder = LogFileOrder{"service", "file"}

// sortedBuiltIns are the built-in fields a Log file order can name.
var sortedBuiltIns = map[string]bool{"service": true, "environment": true, "level": true}

// String names the order and the time it ends with, such as
// "service, file, time".
func (o LogFileOrder) String() string {
	return strings.Join(append(append([]string{}, o...), "time"), ", ")
}

// orderBy is the ORDER BY of a file of Logs of the day of set: each Field as
// the day holds it, NULLs last, then time and id.
func (o LogFileOrder) orderBy(set PromotedSet) string {
	columns := LogColumns{set: set}
	terms := make([]string, 0, len(o)+2)
	for _, field := range o {
		value := columns.Attribute(StringAttribute, field)
		if sortedBuiltIns[field] {
			value = columns.BuiltIn(field)
		}
		terms = append(terms, value+" NULLS LAST")
	}
	return strings.Join(append(terms, "time", "id"), ", ")
}
