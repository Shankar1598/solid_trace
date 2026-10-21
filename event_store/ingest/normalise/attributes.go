package normalise

import (
	"bytes"
	"encoding/json"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/solidtrace/event_store/models"
)

// flatten turns nested objects into dotted names: {"a": {"b": 1}} becomes
// {"a.b": 1}. Arrays are values, not flattened, and a null is no value. An
// object nested past maxDepth levels is kept whole, as a value.
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

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// ReservedPrefix starts the names of Attributes SolidTrace sets.
const ReservedPrefix = "solidtrace."

// truncatedAttribute marks a Log that a limit cut.
const truncatedAttribute = ReservedPrefix + "truncated"

// unreserved renames a sender's Attribute out of SolidTrace's reserved names.
func unreserved(name string) string {
	if strings.HasPrefix(name, ReservedPrefix) {
		return "original." + name
	}
	return name
}

// setAttribute sets the Attribute name to a JSON value, in the map of the
// value's type. A null is no value, so it sets nothing. A name over
// maxNameBytes is dropped, and a string cut to maxStringBytes.
func (n *normaliser) setAttribute(name string, raw json.RawMessage) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || isNull(raw) {
		return
	}
	if len(name) > maxNameBytes {
		n.truncated = true
		return
	}
	attrs := &n.log.Attributes
	deleteAttribute(attrs, name)

	switch raw[0] {
	case '"':
		if s, ok := parseString(raw); ok {
			setIn(&attrs.String, name, n.cut(s))
		}
	case 't', 'f':
		setIn(&attrs.Bool, name, raw[0] == 't')
	case '[', '{':
		var compact bytes.Buffer
		if json.Compact(&compact, raw) == nil {
			setIn(&attrs.JSON, name, compact.String())
		}
	default:
		if d, ok := double(string(raw)); ok {
			setIn(&attrs.Number, name, d)
		} else {
			setIn(&attrs.String, name, n.cut(string(raw)))
		}
	}
}

func deleteAttribute(attrs *models.Attributes, name string) {
	delete(attrs.String, name)
	delete(attrs.Number, name)
	delete(attrs.Bool, name)
	delete(attrs.JSON, name)
}

func setIn[V any](m *map[string]V, name string, value V) {
	if *m == nil {
		*m = make(map[string]V)
	}
	(*m)[name] = value
}

// double returns a JSON number as a DOUBLE, unless a DOUBLE can't hold it: a
// whole number it can't hold exactly, or a number out of its range.
func double(number string) (float64, bool) {
	// A DOUBLE holds every whole number of up to 15 digits exactly, and none
	// of over 309.
	if digits := len(strings.TrimPrefix(number, "-")); digits > 15 && !strings.ContainsAny(number, ".eE") {
		if digits > 309 {
			return 0, false
		}
		whole, ok := new(big.Int).SetString(number, 10)
		if !ok {
			return 0, false
		}
		d, accuracy := new(big.Float).SetInt(whole).Float64()
		return d, accuracy == big.Exact
	}
	d, err := strconv.ParseFloat(number, 64)
	return d, err == nil
}

// cut returns s, cut to maxStringBytes without splitting a character.
func (n *normaliser) cut(s string) string {
	if len(s) <= maxStringBytes {
		return s
	}
	n.truncated = true
	end := maxStringBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// limitAttributes keeps the first maxAttributes Attributes in name order and
// drops the rest, so the same Log always keeps the same ones.
func (n *normaliser) limitAttributes() {
	attrs := &n.log.Attributes
	count := len(attrs.String) + len(attrs.Number) + len(attrs.Bool) + len(attrs.JSON)
	if count <= maxAttributes {
		return
	}
	n.truncated = true
	names := make([]string, 0, count)
	names = slices.AppendSeq(names, maps.Keys(attrs.String))
	names = slices.AppendSeq(names, maps.Keys(attrs.Number))
	names = slices.AppendSeq(names, maps.Keys(attrs.Bool))
	names = slices.AppendSeq(names, maps.Keys(attrs.JSON))
	slices.Sort(names)
	for _, name := range names[maxAttributes:] {
		deleteAttribute(attrs, name)
	}
}

// Size is about how many bytes a Log holds: its built-in fields, and its
// Attributes' names and values.
func Size(log models.Log) int {
	total := len(log.Level) + len(log.Message) + len(log.Service) + len(log.Environment) + len(log.TraceID) + len(log.SpanID)
	for name, value := range log.Attributes.String {
		total += len(name) + len(value)
	}
	for name := range log.Attributes.Number {
		total += len(name) + 8
	}
	for name := range log.Attributes.Bool {
		total += len(name) + 1
	}
	for name, value := range log.Attributes.JSON {
		total += len(name) + len(value)
	}
	return total
}
