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

// otlpLevels are the Levels of the OTLP severity_number values 1 to 24, four
// values to each Level.
var otlpLevels = [...]string{"trace", "debug", "info", "warn", "error", "fatal"}

// otlpAliases are the Attributes that an OTLP log record's built-in fields
// are read from, best first. The highest layer that has one of a field's
// names fills the field, from its best name present. That name is consumed,
// and any others stay Attributes. The other built-in fields come from the
// record's own fields.
var otlpAliases = [fieldCount][]string{
	service:     {"service.name"},
	environment: {"deployment.environment.name", "deployment.environment"},
}

// OTLP normalises one OTLP log record, with the resource and the
// instrumentation scope it was sent under, by the rules JSON follows (ADR
// 0002), and by the OTLP rules below.
//
// The resource, scope and record attributes are Attributes in one flat
// namespace, with no prefix. When two layers give the same name, the record
// wins over the scope, and the scope wins over the resource. The scope's name
// and version are otel.scope.name and otel.scope.version, and the record's
// event_name is event.name. In one layer, these fields win over an attribute
// of the same name, in the dotted or the nested form. An attribute named as a
// built-in field is kept as original.<field>.
//
// The time is the record's time, else its observed time. The Level comes from
// severity_number 1 to 24, else from severity_text. A body that isn't a
// string is kept as original.message, and bytes are base64. The flags, the
// dropped attributes counts, the schema URLs, and severity_text when
// severity_number gives the Level, aren't kept.
func OTLP(resource pcommon.Resource, scope pcommon.InstrumentationScope, record plog.LogRecord, r Receipt) (models.Log, Outcome) {
	// The event name goes on top of the flattened attributes, so that it wins
	// over both forms of an attribute.
	recordAttributes := flatten(members(record.Attributes()))
	if name := record.EventName(); name != "" {
		recordAttributes["event.name"] = quote(name)
	}
	s := otlpSent(resource, scope, recordAttributes, nil)

	t := record.Timestamp()
	if t == 0 {
		t = record.ObservedTimestamp()
	}
	if t != 0 {
		s.fields[timeField] = strconv.AppendUint(nil, uint64(t), 10)
	}
	// A severity_number out of the range 1 to 24, with no severity_text, is
	// kept as original.level.
	switch number := record.SeverityNumber(); {
	case number >= 1 && number <= 24:
		s.fields[level] = quote(otlpLevels[(number-1)/4])
	case record.SeverityText() != "":
		s.fields[level] = quote(record.SeverityText())
	case number != 0:
		s.fields[level] = strconv.AppendInt(nil, int64(number), 10)
	}
	switch body := record.Body(); body.Type() {
	case pcommon.ValueTypeEmpty:
	case pcommon.ValueTypeBytes:
		// Bytes aren't text, so they are no message. They are set after the
		// rename, so they win over an attribute named message, as a body of
		// another type does.
		s.attributes["original.message"] = appendValue(nil, body)
	default:
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

// otlpSent merges the resource, the scope and own into one flat namespace of
// Attributes. own is the flattened attributes of a log record or a metric
// data point, with its protocol fields set on top. It renames an attribute
// named as a built-in field, or with one of the names in ours, to
// original.<name>, and picks out the service and the environment. ours are
// the names of Attributes that the caller sets after this.
func otlpSent(resource pcommon.Resource, scope pcommon.InstrumentationScope, own map[string]json.RawMessage, ours []string) sent {
	// The scope's fields go on top of its flattened attributes, so that they
	// win over both forms of an attribute.
	scopeAttributes := flatten(members(scope.Attributes()))
	if name := scope.Name(); name != "" {
		scopeAttributes["otel.scope.name"] = quote(name)
	}
	if version := scope.Version(); version != "" {
		scopeAttributes["otel.scope.version"] = quote(version)
	}

	// Each layer is flattened by itself, so a record's {"http": {"method":
	// …}} still wins over a resource's "http.method".
	s := sent{attributes: flatten(members(resource.Attributes()))}
	maps.Copy(s.attributes, scopeAttributes)
	maps.Copy(s.attributes, own)

	// The rename wins over a sender's own original.<field>, as on the JSON
	// route.
	for _, reserved := range [...][]string{names[:], ours} {
		for _, name := range reserved {
			if raw, ok := s.attributes[name]; ok {
				delete(s.attributes, name)
				s.attributes["original."+name] = raw
			}
		}
	}
	// The merged Attributes stand for the resource layer: when own and the
	// scope have none of a field's names, the names in it came from the
	// resource. The merged value of the name found is that layer's value, as
	// no higher layer has the name.
	layers := [...]map[string]json.RawMessage{own, scopeAttributes, s.attributes}
	for f, aliases := range otlpAliases {
	search:
		for _, layer := range layers {
			for _, name := range aliases {
				if _, ok := layer[name]; ok {
					s.fields[f] = s.attributes[name]
					delete(s.attributes, name)
					break search
				}
			}
		}
	}
	return s
}

// members returns the attributes m as the members of a JSON object.
func members(m pcommon.Map) map[string]json.RawMessage {
	object := make(map[string]json.RawMessage, m.Len())
	for name, value := range m.All() {
		object[name] = appendValue(nil, value)
	}
	return object
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
		b = appendDouble(b, v.Double())
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

// appendDouble appends d as JSON: a number, or a string when JSON can't hold
// it (NaN or an infinity).
func appendDouble(b []byte, d float64) []byte {
	if math.IsNaN(d) || math.IsInf(d, 0) {
		b, _ = jsontext.AppendQuote(b, strconv.FormatFloat(d, 'g', -1, 64))
		return b
	}
	return strconv.AppendFloat(b, d, 'g', -1, 64)
}

// quote returns s as a JSON string.
func quote(s string) json.RawMessage {
	b, _ := jsontext.AppendQuote(nil, s)
	return b
}
