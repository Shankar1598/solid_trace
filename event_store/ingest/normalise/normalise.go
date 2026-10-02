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
	"math"
	"math/big"
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
	s := sent{attributes: flatten(object)}
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

// maxDepth is how many levels of nested objects are flattened. An object
// deeper than that is kept whole, as JSON, under the name of its level.
const maxDepth = 10

// flatten turns nested objects into dotted names: {"a": {"b": 1}} becomes
// {"a.b": 1}. Arrays are values, not flattened, and a null is no value.
//
// When two forms name the same Attribute, the more literal one wins: the one
// nested fewer levels deep, and between equals, the one whose outermost keys
// are longer. So {"a.b": 1} beats {"a": {"b": 2}} (ADR 0002).
func flatten(object map[string]json.RawMessage) map[string]json.RawMessage {
	f := flattener{values: make(map[string]json.RawMessage, len(object)), paths: make(map[string][]int)}
	f.add("", nil, object)
	return f.values
}

type flattener struct {
	values map[string]json.RawMessage
	// paths holds, for each name, the lengths of the keys it was made of.
	paths map[string][]int
}

func (f *flattener) add(prefix string, path []int, object map[string]json.RawMessage) {
	for key, raw := range object {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || isNull(raw) {
			continue
		}
		name := prefix + key
		keyPath := append(slices.Clip(path), len(key))
		if raw[0] == '{' && len(keyPath) < maxDepth {
			var inner map[string]json.RawMessage
			if json.Unmarshal(raw, &inner) == nil {
				f.add(name+".", keyPath, inner)
				continue
			}
		}
		if existing, ok := f.paths[name]; ok && !moreLiteral(keyPath, existing) {
			continue
		}
		f.values[name] = raw
		f.paths[name] = keyPath
	}
}

// moreLiteral tells whether a name made of keys of lengths a is a more literal
// form than one made of keys of lengths b.
func moreLiteral(a, b []int) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
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

	// A Log without a time it can use is placed at its receipt.
	log.Time = r.At
	if raw := s.fields[timeField]; raw != nil {
		if t, ok := parseTime(raw); ok && r.inWindow(t) {
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

	"warning":   "warn",
	"err":       "error",
	"critical":  "fatal",
	"crit":      "fatal",
	"panic":     "fatal",
	"emergency": "fatal",
	"notice":    "info",
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

// maxAhead is how far past its receipt a Log's time may be.
const maxAhead = time.Hour

// inWindow tells whether a Log's own time is believable: no older than the
// Project's Retention and at most maxAhead in the future.
func (r Receipt) inWindow(t time.Time) bool {
	oldest := r.At.AddDate(0, 0, -r.RetentionDays)
	return !t.Before(oldest) && !t.After(r.At.Add(maxAhead))
}

// parseTime parses an ISO 8601 time, or a numeric epoch.
func parseTime(raw json.RawMessage) (time.Time, bool) {
	if s, ok := parseString(raw); ok {
		return parseISO8601(s)
	}
	return parseEpoch(string(raw))
}

// iso8601Layouts are the ISO 8601 forms a Log's time is read in: RFC 3339,
// with a space for the T, an offset without its colon or without minutes, or
// no offset at all, which is UTC. Fractional seconds, after a point or a
// comma, are read in every one.
var iso8601Layouts = []string{
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05Z07",
	"2006-01-02T15:04:05",
}

func parseISO8601(s string) (time.Time, bool) {
	// RFC 3339 allows a lower-case t and z, and a space for the T.
	s = strings.ToUpper(s)
	if len(s) > 10 && s[10] == ' ' {
		s = s[:10] + "T" + s[11:]
	}
	for _, layout := range iso8601Layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// Epoch units, in nanoseconds, by the magnitude that marks them: below 1e11 an
// epoch is in seconds (up to the year 5138), below 1e14 in milliseconds, below
// 1e17 in microseconds, and above that in nanoseconds.
var epochUnits = []struct {
	below float64
	unit  *big.Rat
}{
	{1e11, big.NewRat(int64(time.Second), 1)},
	{1e14, big.NewRat(int64(time.Millisecond), 1)},
	{1e17, big.NewRat(int64(time.Microsecond), 1)},
	{math.Inf(1), big.NewRat(1, 1)},
}

// parseEpoch parses a JSON number as a Unix epoch, its unit inferred from its
// magnitude. It reads the number exactly, so a fraction of a second or a
// nanosecond epoch past a DOUBLE's precision isn't rounded.
func parseEpoch(number string) (time.Time, bool) {
	exact, ok := new(big.Rat).SetString(number)
	if !ok {
		return time.Time{}, false
	}
	magnitude, _ := new(big.Rat).Abs(exact).Float64()
	for _, e := range epochUnits {
		if magnitude < e.below {
			exact.Mul(exact, e.unit)
			break
		}
	}
	nanos := new(big.Int).Quo(exact.Num(), exact.Denom())
	if !nanos.IsInt64() {
		return time.Time{}, false
	}
	return time.Unix(0, nanos.Int64()).UTC(), true
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
