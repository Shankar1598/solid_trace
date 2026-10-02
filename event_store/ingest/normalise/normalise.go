// Package normalise turns what a sender sent as a Log into a normalised Log,
// before Log ingest stores it: the built-in fields filled, every other value an
// Attribute under a flat dotted name with its own type, and the per-Log limits
// applied. It is pure: the receipt time and the Project's Retention come in
// with each call.
package normalise

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/event_store/models"
)

// Receipt is what normalising a Log needs besides the Log itself: when
// SolidTrace received it, and how long its Project keeps Logs.
type Receipt struct {
	// At is when SolidTrace received the Log.
	At time.Time
	// RetentionDays is the Project's Retention, in whole days.
	RetentionDays int
}

// Outcome says what normalising did to a Log.
type Outcome int

const (
	// Kept: the Log is stored whole.
	Kept Outcome = iota
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
// field picked out from the rest, which are its Attributes.
type sent struct {
	fields     [fieldCount]json.RawMessage
	attributes map[string]json.RawMessage
}

// jsonAliases are the keys a JSON Log's built-in fields are read from, best
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

// JSON normalises one JSON object a sender sent as a Log.
func JSON(object map[string]json.RawMessage, r Receipt) (models.Log, Outcome) {
	s := sent{attributes: make(map[string]json.RawMessage, len(object))}
	for name, raw := range object {
		if !isNull(raw) {
			s.attributes[name] = raw
		}
	}
	for f, aliases := range jsonAliases {
		for _, alias := range aliases {
			if raw, ok := s.attributes[alias]; ok {
				s.fields[f] = raw
				delete(s.attributes, alias)
				break
			}
		}
	}
	return normaliseSent(s, r)
}

func normaliseSent(s sent, r Receipt) (models.Log, Outcome) {
	var log models.Log
	for _, name := range slices.Sorted(maps.Keys(s.attributes)) {
		setAttribute(&log.Attributes, name, s.attributes[name])
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
			setAttribute(&log.Attributes, "original."+names[f], raw)
		}
		return value
	}
	log.Level = fill(level, parseLevel)
	log.Message = fill(message, parseString)
	log.Service = fill(service, parseString)
	log.Environment = fill(environment, parseString)
	log.TraceID = fill(traceID, hexID(32))
	log.SpanID = fill(spanID, hexID(16))

	log.Time = r.At
	if raw := s.fields[timeField]; raw != nil {
		if t, ok := parseTime(raw); ok {
			log.Time = t
		} else {
			setAttribute(&log.Attributes, "original.time", raw)
		}
	}
	return log, Kept
}

func parseString(raw json.RawMessage) (string, bool) {
	var s string
	err := json.Unmarshal(raw, &s)
	return s, err == nil
}

// levels maps each spelling of a Level, in lower case, to the Level.
var levels = map[string]string{
	"trace": "trace",
	"debug": "debug",
	"info":  "info",
	"warn":  "warn",
	"error": "error",
	"fatal": "fatal",
}

func parseLevel(raw json.RawMessage) (string, bool) {
	s, ok := parseString(raw)
	if !ok {
		return "", false
	}
	l, ok := levels[strings.ToLower(s)]
	return l, ok
}

// hexID parses a trace or span id: digits hex digits.
func hexID(digits int) func(json.RawMessage) (string, bool) {
	return func(raw json.RawMessage) (string, bool) {
		s, ok := parseString(raw)
		if !ok || len(s) != digits {
			return "", false
		}
		if _, err := hex.DecodeString(s); err != nil {
			return "", false
		}
		return strings.ToLower(s), true
	}
}

func parseTime(raw json.RawMessage) (time.Time, bool) {
	s, ok := parseString(raw)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// setAttribute sets the Attribute name to a JSON value, in the map of the
// value's type. A null is no value, so it sets nothing.
func setAttribute(attrs *models.Attributes, name string, raw json.RawMessage) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || isNull(raw) {
		return
	}
	delete(attrs.String, name)
	delete(attrs.Number, name)
	delete(attrs.Bool, name)
	delete(attrs.JSON, name)

	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			setIn(&attrs.String, name, s)
		}
	case 't', 'f':
		setIn(&attrs.Bool, name, raw[0] == 't')
	case '[', '{':
		var compact bytes.Buffer
		if json.Compact(&compact, raw) == nil {
			setIn(&attrs.JSON, name, compact.String())
		}
	default:
		if n, err := strconv.ParseFloat(string(raw), 64); err == nil {
			setIn(&attrs.Number, name, n)
		} else {
			setIn(&attrs.String, name, string(raw))
		}
	}
}

func setIn[V any](m *map[string]V, name string, value V) {
	if *m == nil {
		*m = make(map[string]V)
	}
	(*m)[name] = value
}
