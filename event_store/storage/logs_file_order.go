package storage

import (
	"fmt"
	"slices"
	"strings"

	"github.com/solidtrace/event_store/pkg/logger"
)

// LogFileOrder is a Project's Log file order: the Fields the hourly export
// sorts each Log file by, before time and id, so that the Logs of one Service's
// log file sit together in a few row groups. Each is a built-in field (service,
// environment or level) or an Attribute, read as a string. Empty means time
// only.
type LogFileOrder []string

// DefaultLogFileOrder is the Log file order of a Project that sets none.
var DefaultLogFileOrder = LogFileOrder{"service", "file"}

// LogFileOrders gives each Project's Log file order. Console's settings are
// the production one (ConsoleSettings.LogFileOrder).
type LogFileOrders interface {
	LogFileOrder(projectID uint32) (LogFileOrder, error)
}

// UseLogFileOrders has the hourly export sort each Project's files by its
// order from orders. Without it, every Project's files get the default.
func (s *LogStore) UseLogFileOrders(orders LogFileOrders) {
	s.orders = orders
}

// logFileOrder is a Project's Log file order, or the default, with a warning,
// when it can't be read: the export never fails over it.
func (s *LogStore) logFileOrder(projectID uint32) LogFileOrder {
	if s.orders == nil {
		return DefaultLogFileOrder
	}
	order, err := s.orders.LogFileOrder(projectID)
	if err != nil {
		logger.L.Warn("Failed to read a Project's Log file order; exporting its Logs in the default order",
			"project_id", projectID, "default", DefaultLogFileOrder.String(), "error", err)
		return DefaultLogFileOrder
	}
	return order
}

// sortedBuiltIns are the built-in fields a Log file order can name. Console
// has the same list (Project::LOG_FILE_ORDER_BUILT_INS).
var sortedBuiltIns = map[string]bool{"service": true, "environment": true, "level": true}

// maxLogFileOrder is how many Fields a Log file order can name. Console has
// the same limit (Project::MAX_LOG_FILE_ORDER).
const maxLogFileOrder = 3

// unsortedFields are the Fields a Log file order can't name: time and id
// always end it, and the rest are near unique. Console has the same list
// (Project::UNSORTED_FIELDS).
var unsortedFields = []string{"time", "_time", "id", "message", "trace_id", "span_id"}

// check refuses an order Console's validations would refuse: more than
// maxLogFileOrder Fields, an empty name, an unsorted Field or a repeat.
func (o LogFileOrder) check() error {
	if len(o) > maxLogFileOrder {
		return fmt.Errorf("it names more than %d Fields", maxLogFileOrder)
	}
	for i, field := range o {
		switch {
		case field == "":
			return fmt.Errorf("it names an empty Field")
		case slices.Contains(unsortedFields, field):
			return fmt.Errorf("it names %s", field)
		case slices.Contains(o[:i], field):
			return fmt.Errorf("it names %s twice", field)
		}
	}
	return nil
}

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
		var value string
		if sortedBuiltIns[field] {
			value = columns.BuiltIn(field)
		} else {
			value = columns.Attribute(StringAttribute, field)
		}
		terms = append(terms, value+" NULLS LAST")
	}
	return strings.Join(append(terms, "time", "id"), ", ")
}
