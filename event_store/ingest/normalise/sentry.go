package normalise

import (
	"bytes"
	"encoding/json"

	"github.com/solidtrace/event_store/models"
)

// sentryFields are the keys of a Sentry log record that fill built-in fields.
// See https://develop.sentry.dev/sdk/telemetry/logs/#log-envelope-item-payload
var sentryFields = map[string]field{
	"timestamp": timeField,
	"level":     level,
	"body":      message,
	"trace_id":  traceID,
	"span_id":   spanID,
}

// sentryAttributeFields are the attributes of a Sentry log record that fill
// built-in fields. Like a JSON alias, each is consumed.
var sentryAttributeFields = map[string]field{
	"sentry.environment": environment,
	"service.name":       service,
}

// Sentry normalises one log record of a Sentry envelope's log item. Its
// fields map to the built-in fields by name, with no aliases, so an attribute
// such as msg or env stays an Attribute. Its typed attributes, {"value": …,
// "type": …}, are unwrapped to their values.
func Sentry(record map[string]json.RawMessage, r Receipt) (models.Log, Outcome) {
	var s sent
	values := make(map[string]json.RawMessage, len(record))
	var attributes map[string]json.RawMessage
	for key, raw := range record {
		if isNull(raw) {
			continue
		}
		if f, ok := sentryFields[key]; ok {
			s.fields[f] = raw
			continue
		}
		if key == "attributes" && json.Unmarshal(raw, &attributes) == nil && attributes != nil {
			continue
		}
		values[key] = raw
	}
	for name, raw := range attributes {
		values[name] = unwrap(raw)
	}

	s.attributes = flatten(values)
	for name, f := range sentryAttributeFields {
		if raw, ok := s.attributes[name]; ok {
			s.fields[f] = raw
			delete(s.attributes, name)
		}
	}
	return normaliseSent(s, r)
}

// unwrap returns the value of a typed Sentry attribute, {"value": …, "type":
// …}, without its type or unit. Anything else is returned as it was sent.
func unwrap(raw json.RawMessage) json.RawMessage {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		return raw
	}
	var typed map[string]json.RawMessage
	if json.Unmarshal(raw, &typed) != nil {
		return raw
	}
	value, ok := typed["value"]
	if !ok {
		return raw
	}
	return value
}
