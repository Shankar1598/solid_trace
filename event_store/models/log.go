package models

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
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

// MarshalJSONTo writes the Attributes as one JSON object, each value with its
// own type, so a reader can't tell which map holds it. Names are sorted, as
// encoding/json sorts a map's keys. It writes straight to the encoder, so
// that a page of Logs builds no map per Log; json.Marshal calls it too.
func (a Attributes) MarshalJSONTo(enc *jsontext.Encoder) error {
	names := make([]string, 0, len(a.String)+len(a.Number)+len(a.Bool)+len(a.JSON))
	names = slices.AppendSeq(names, maps.Keys(a.String))
	names = slices.AppendSeq(names, maps.Keys(a.Number))
	names = slices.AppendSeq(names, maps.Keys(a.Bool))
	names = slices.AppendSeq(names, maps.Keys(a.JSON))
	slices.Sort(names)

	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, name := range slices.Compact(names) {
		if err := enc.WriteToken(jsontext.String(name)); err != nil {
			return err
		}
		var err error
		// A name is in one map; were it in more, the last one here wins.
		if value, ok := a.JSON[name]; ok {
			err = enc.WriteValue(jsontext.Value(value))
		} else if value, ok := a.Bool[name]; ok {
			err = enc.WriteToken(jsontext.Bool(value))
		} else if value, ok := a.Number[name]; ok {
			err = enc.WriteToken(jsontext.Float(value))
		} else {
			err = enc.WriteToken(jsontext.String(a.String[name]))
		}
		if err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}
