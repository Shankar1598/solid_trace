package normalise

import (
	"encoding/base64"
	"encoding/json"
	"encoding/json/jsontext"
	"maps"
	"math"
	"strconv"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/solidtrace/event_store/models"
)

// otlpLevels are the Levels of the OTLP severity numbers 1 to 24, four
// numbers to each Level.
var otlpLevels = [...]string{"trace", "debug", "info", "warn", "error", "fatal"}

// otlpAliases are the Attributes an OTLP log record's built-in fields are
// read from, best first, as for a JSON Log: the first one present fills the
// field and is consumed, and any others present stay Attributes. The other
// built-in fields come from the record's own fields.
var otlpAliases = [fieldCount][]string{
	service:     {"service.name"},
	environment: {"deployment.environment.name", "deployment.environment"},
}

// OTLP normalises one OTLP log record, with the resource and the
// instrumentation scope it was sent under, by the rules JSON follows (ADR
// 0002).
//
// The resource, scope and record attributes are Attributes in one flat
// namespace, with no prefix. When two layers give the same name, the record
// wins over the scope, and the scope wins over the resource. In one layer, an
// attribute wins over a field of the same name, as in a Sentry record. The
// scope's name and version are otel.scope.name and otel.scope.version, and the
// record's event name is event.name. An attribute named as a built-in field
// is kept as original.<field>.
//
// The time is the record's time, else its observed time. The Level comes from
// the severity number, else from the severity text. A body that isn't a
// string is kept as original.message. The flags, the dropped attributes
// counts, the schema URLs, and the severity text when the severity number
// gives the Level, aren't kept.
func OTLP(resource pcommon.Resource, scope pcommon.InstrumentationScope, record plog.LogRecord, r Receipt) (models.Log, Outcome) {
	scopeFields := make(map[string]json.RawMessage, 2)
	if name := scope.Name(); name != "" {
		scopeFields["otel.scope.name"] = quote(name)
	}
	if version := scope.Version(); version != "" {
		scopeFields["otel.scope.version"] = quote(version)
	}
	recordFields := make(map[string]json.RawMessage, 1)
	if name := record.EventName(); name != "" {
		recordFields["event.name"] = quote(name)
	}

	// Each layer is flattened by itself, so a record's {"http": {"method":
	// …}} still wins over a resource's "http.method".
	s := sent{attributes: flatten(members(resource.Attributes(), nil))}
	maps.Copy(s.attributes, flatten(members(scope.Attributes(), scopeFields)))
	maps.Copy(s.attributes, flatten(members(record.Attributes(), recordFields)))

	for _, name := range names {
		if raw, ok := s.attributes[name]; ok {
			delete(s.attributes, name)
			s.attributes["original."+name] = raw
		}
	}
	for f, aliases := range otlpAliases {
		for _, name := range aliases {
			if raw, ok := s.attributes[name]; ok {
				s.fields[f] = raw
				delete(s.attributes, name)
				break
			}
		}
	}

	t := record.Timestamp()
	if t == 0 {
		t = record.ObservedTimestamp()
	}
	if t != 0 {
		s.fields[timeField] = strconv.AppendUint(nil, uint64(t), 10)
	}
	// A severity number out of the range, with no text, is kept as
	// original.level.
	switch number := record.SeverityNumber(); {
	case number >= 1 && number <= 24:
		s.fields[level] = quote(otlpLevels[(number-1)/4])
	case record.SeverityText() != "":
		s.fields[level] = quote(record.SeverityText())
	case number != 0:
		s.fields[level] = strconv.AppendInt(nil, int64(number), 10)
	}
	if body := record.Body(); body.Type() != pcommon.ValueTypeEmpty {
		s.fields[message] = appendValue(nil, body)
	}
	if id := record.TraceID(); !id.IsEmpty() {
		s.fields[traceID] = quote(id.String())
	}
	if id := record.SpanID(); !id.IsEmpty() {
		s.fields[spanID] = quote(id.String())
	}
	return normaliseSent(s, r)
}

// members returns the members of a JSON object that holds the attributes m,
// added to fields. An attribute wins over a field of the same name.
func members(m pcommon.Map, fields map[string]json.RawMessage) map[string]json.RawMessage {
	if fields == nil {
		fields = make(map[string]json.RawMessage, m.Len())
	}
	for name, value := range m.All() {
		fields[name] = appendValue(nil, value)
	}
	return fields
}

// appendValue appends an OTLP value as JSON. An int64 is its exact digits,
// bytes are a base64 string, an empty value is null, and a double that JSON
// can't hold (NaN or an infinity) is a string.
func appendValue(b []byte, v pcommon.Value) []byte {
	switch v.Type() {
	case pcommon.ValueTypeStr:
		b, _ = jsontext.AppendQuote(b, v.Str())
	case pcommon.ValueTypeInt:
		b = strconv.AppendInt(b, v.Int(), 10)
	case pcommon.ValueTypeDouble:
		if d := v.Double(); math.IsNaN(d) || math.IsInf(d, 0) {
			b, _ = jsontext.AppendQuote(b, strconv.FormatFloat(d, 'g', -1, 64))
		} else {
			b = strconv.AppendFloat(b, d, 'g', -1, 64)
		}
	case pcommon.ValueTypeBool:
		b = strconv.AppendBool(b, v.Bool())
	case pcommon.ValueTypeBytes:
		b = append(b, '"')
		b = base64.StdEncoding.AppendEncode(b, v.Bytes().AsRaw())
		b = append(b, '"')
	case pcommon.ValueTypeSlice:
		b = append(b, '[')
		for i, item := range v.Slice().All() {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendValue(b, item)
		}
		b = append(b, ']')
	case pcommon.ValueTypeMap:
		b = append(b, '{')
		first := true
		for name, item := range v.Map().All() {
			if !first {
				b = append(b, ',')
			}
			first = false
			b, _ = jsontext.AppendQuote(b, name)
			b = append(b, ':')
			b = appendValue(b, item)
		}
		b = append(b, '}')
	default:
		b = append(b, "null"...)
	}
	return b
}

func quote(s string) json.RawMessage {
	b, _ := jsontext.AppendQuote(nil, s)
	return b
}
