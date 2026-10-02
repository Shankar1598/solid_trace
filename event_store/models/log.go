package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Log is one normalised Log: its built-in fields and its Attributes. An empty
// Level, Service, Environment, TraceID or SpanID means the Log has none;
// Message is always present, possibly empty.
//
// The msgpack tags are the format of Log batches in Pebble. A batch can
// outlive the process that wrote it, so don't rename them.
type Log struct {
	ID          uuid.UUID  `json:"id" msgpack:"id"`
	Time        time.Time  `json:"time" msgpack:"t"`
	Level       string     `json:"level,omitempty" msgpack:"l,omitempty"`
	Message     string     `json:"message" msgpack:"m"`
	Service     string     `json:"service,omitempty" msgpack:"s,omitempty"`
	Environment string     `json:"environment,omitempty" msgpack:"e,omitempty"`
	TraceID     string     `json:"trace_id,omitempty" msgpack:"ti,omitempty"`
	SpanID      string     `json:"span_id,omitempty" msgpack:"si,omitempty"`
	Attributes  Attributes `json:"attributes" msgpack:"a"`
}

// Attributes are a Log's Attributes, one map per value type, as a hot table
// stores them. A name is in at most one of the maps.
type Attributes struct {
	String map[string]string  `msgpack:"s,omitempty"`
	Number map[string]float64 `msgpack:"n,omitempty"`
	Bool   map[string]bool    `msgpack:"b,omitempty"`
	// JSON holds arrays and objects as JSON text.
	JSON map[string]string `msgpack:"j,omitempty"`
}

// MarshalJSON writes the Attributes as one JSON object, each value with its
// own type, so a reader can't tell which map holds it.
func (a Attributes) MarshalJSON() ([]byte, error) {
	merged := make(map[string]any, len(a.String)+len(a.Number)+len(a.Bool)+len(a.JSON))
	for name, value := range a.String {
		merged[name] = value
	}
	for name, value := range a.Number {
		merged[name] = value
	}
	for name, value := range a.Bool {
		merged[name] = value
	}
	for name, value := range a.JSON {
		merged[name] = json.RawMessage(value)
	}
	return json.Marshal(merged)
}
