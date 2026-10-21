// Package normalise turns what a sender sent as a Log into a normalised Log,
// before Log ingest stores it: the built-in fields filled, every other value an
// Attribute under a flat dotted name with its own type, and the per-Log limits
// applied. What a Log transform returns is normalised by the same rules. It
// is pure: the receipt time and the Project's Retention come in with each
// call.
package normalise

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/solidtrace/event_store/models"
)

// Receipt is what normalising a Log needs besides the Log itself: when
// SolidTrace received it, and how long its Project keeps Logs.
type Receipt struct {
	// At is when SolidTrace received the Log. A Log without a usable time of
	// its own is placed at it.
	At time.Time
	// RetentionDays is the Project's Retention, in whole days. A Log's time
	// older than that before At isn't used.
	RetentionDays int
}

// Outcome says what normalising did to a Log.
type Outcome int

const (
	// Kept: the Log is stored whole.
	Kept Outcome = iota
	// Truncated: the Log is stored, but a limit cut a string or dropped an
	// Attribute, and it carries solidtrace.truncated = true.
	Truncated
	// Dropped: the Log is over maxLogBytes even after the limits, and isn't
	// stored.
	Dropped
)

// The per-Log limits. A Log over one is cut and kept, except past
// maxLogBytes, when it is dropped.
const (
	// maxStringBytes caps every string, the message included.
	maxStringBytes = 64 << 10
	// maxNameBytes caps an Attribute's name; a longer one is dropped.
	maxNameBytes = 256
	// maxAttributes caps a Log's Attributes; the excess is dropped.
	maxAttributes = 1000
	// maxDepth is how many levels of nested objects are flattened. An object
	// deeper than that is kept whole, as JSON, under the name of its level.
	maxDepth = 10
	// maxLogBytes caps a whole Log, after the limits above.
	maxLogBytes = 1 << 20
	// maxAhead is how far past its receipt a Log's own time may be.
	maxAhead = time.Hour
)

// field is one of a Log's built-in fields.
type field int

const (
	timeField field = iota
	level
	message
	service
	environment
	traceID
	spanID
	fieldCount
)

// names are the built-in fields' names, as an original.<field> Attribute uses
// them.
var names = [fieldCount]string{
	timeField:   "time",
	level:       "level",
	message:     "message",
	service:     "service",
	environment: "environment",
	traceID:     "trace_id",
	spanID:      "span_id",
}

// sent is a Log as its sender sent it, with the value given for each built-in
// field picked out from the rest, which are its Attributes under flat names.
type sent struct {
	fields     [fieldCount]json.RawMessage
	attributes map[string]json.RawMessage
}

// jsonAliases are the names a JSON Log's built-in fields are read from, best
// first. The first one present fills the field and is consumed; any others
// present stay Attributes.
var jsonAliases = [fieldCount][]string{
	timeField:   {"time", "timestamp", "ts", "@timestamp", "_time"},
	level:       {"level", "severity", "lvl", "log.level"},
	message:     {"message", "msg", "log", "body"},
	service:     {"service", "service.name"},
	environment: {"environment", "env", "deployment.environment"},
	traceID:     {"trace_id", "traceId"},
	spanID:      {"span_id", "spanId"},
}

// JSON normalises one JSON object a sender sent as a Log. Its aliases are
// looked up after flattening, so {"log": {"level": "info"}} fills the Level as
// {"log.level": "info"} does.
func JSON(object map[string]json.RawMessage, r Receipt) (models.Log, Outcome) {
	return normaliseObject(object, jsonAliases, r)
}

// ownNames read each built-in field only under its own name.
var ownNames = func() (aliases [fieldCount][]string) {
	for f, name := range names {
		aliases[f] = []string{name}
	}
	return aliases
}()

// Transformed normalises the object a Log transform returned for a Log, by
// the rules JSON follows for a sender's Log, except that each built-in field
// is read only under its own name, where the program saw it. So a program
// that deletes .message stores a Log without one, even if the Log has a msg
// Attribute. A value is one a Log transform returns: a string, int64,
// float64, bool, nil, time.Time, []any or map[string]any. One that JSON
// can't hold, such as an infinite number, is an error.
func Transformed(object map[string]any, r Receipt) (models.Log, Outcome, error) {
	raw := make(map[string]json.RawMessage, len(object))
	for name, value := range object {
		var b bytes.Buffer
		encoder := json.NewEncoder(&b)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return models.Log{}, Dropped, fmt.Errorf("%s can't be stored: %w", name, err)
		}
		raw[name] = b.Bytes()
	}
	log, outcome := normaliseObject(raw, ownNames, r)
	return log, outcome, nil
}

// normaliseObject normalises a Log given as a JSON object, after flattening
// it, filling each built-in field from the first of its aliases present.
// That alias is consumed; any others present stay Attributes.
func normaliseObject(object map[string]json.RawMessage, aliases [fieldCount][]string, r Receipt) (models.Log, Outcome) {
	s := sent{attributes: flatten(object)}
	for f, names := range aliases {
		for _, name := range names {
			if raw, ok := s.attributes[name]; ok {
				s.fields[f] = raw
				delete(s.attributes, name)
				break
			}
		}
	}
	return normaliseSent(s, r)
}

// normaliser builds one Log, and notes whether a limit cut it.
type normaliser struct {
	log       models.Log
	truncated bool
}

// normaliseSent fills a Log's built-in fields from the values sent for them,
// keeps a value that can't fill its field as original.<field>, and applies the
// limits.
func normaliseSent(s sent, r Receipt) (models.Log, Outcome) {
	var n normaliser
	for _, name := range slices.Sorted(maps.Keys(s.attributes)) {
		n.setAttribute(unreserved(name), s.attributes[name])
	}

	// After the sender's own Attributes, so an original.<field> SolidTrace
	// sets is never overwritten by one of them.
	fill := func(f field, parse func(json.RawMessage) (string, bool)) string {
		raw := s.fields[f]
		if raw == nil {
			return ""
		}
		value, ok := parse(raw)
		if !ok {
			n.setAttribute("original."+names[f], raw)
		}
		return n.cut(value)
	}
	n.log.Level = fill(level, parseLevel)
	n.log.Message = fill(message, parseString)
	n.log.Service = fill(service, parseString)
	n.log.Environment = fill(environment, parseString)
	n.log.TraceID = fill(traceID, hexID(32))
	n.log.SpanID = fill(spanID, hexID(16))

	n.log.Time = r.At
	if raw := s.fields[timeField]; raw != nil {
		if t, ok := parseTime(raw); ok && r.inWindow(t) {
			n.log.Time = t
		} else {
			n.setAttribute("original.time", raw)
		}
	}

	n.limitAttributes()
	if Size(n.log) > maxLogBytes {
		return models.Log{}, Dropped
	}
	if n.truncated {
		setIn(&n.log.Attributes.Bool, truncatedAttribute, true)
		return n.log, Truncated
	}
	return n.log, Kept
}
