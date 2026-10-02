package ingest

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/solidtrace/event_store/models"
)

// normaliseLog turns one JSON object into a Log, before it is stored.
//
// Each built-in field is filled from the key of its own name. A value that
// can't fill its field is kept as the Attribute original.<field>. A Log
// without a usable time gets receivedAt. Every other key is an Attribute,
// typed by its JSON value; arrays and objects are kept as JSON text.
func normaliseLog(object map[string]json.RawMessage, receivedAt time.Time) models.Log {
	var log models.Log
	for name, raw := range object {
		if !isBuiltInField(name) {
			setAttribute(&log.Attributes, name, raw)
		}
	}

	// After the sender's own keys, so original.<field> is never overwritten
	// by one of them.
	log.Message = stringField(object, "message", &log.Attributes)
	log.Level = stringField(object, "level", &log.Attributes)
	log.Service = stringField(object, "service", &log.Attributes)
	log.Environment = stringField(object, "environment", &log.Attributes)
	log.TraceID = stringField(object, "trace_id", &log.Attributes)
	log.SpanID = stringField(object, "span_id", &log.Attributes)
	log.Time = timeField(object, &log.Attributes)
	if log.Time.IsZero() {
		log.Time = receivedAt
	}
	return log
}

func isBuiltInField(name string) bool {
	switch name {
	case "message", "time", "level", "service", "environment", "trace_id", "span_id":
		return true
	}
	return false
}

// stringField returns the string at name, or "" if there is none. Any other
// value is kept as original.<name>.
func stringField(object map[string]json.RawMessage, name string, attrs *models.Attributes) string {
	raw, ok := object[name]
	if !ok || isNull(raw) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		setAttribute(attrs, "original."+name, raw)
		return ""
	}
	return s
}

// timeField returns the RFC 3339 time at "time", in UTC, or the zero time if
// there is none. Any other value is kept as original.time.
func timeField(object map[string]json.RawMessage, attrs *models.Attributes) time.Time {
	raw, ok := object["time"]
	if !ok || isNull(raw) {
		return time.Time{}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC()
		}
	}
	setAttribute(attrs, "original.time", raw)
	return time.Time{}
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
			// Out of a DOUBLE's range: kept as written.
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
