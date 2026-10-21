package logtransform

import (
	"encoding/json"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/solidtrace/event_store/models"
)

// view is a Log as a program sees it as `.`. The built-in fields are at the
// top level, each absent when the Log has none, except .message, which is
// always a string; .time is a timestamp. The Attributes are nested by their
// dots, as Vector shows them, each with its type: a JSON Attribute is an
// array or object. An Attribute that can't nest, because a shorter name
// already holds a value, stays at the top level under its dotted name. The
// Log's id isn't shown.
func view(log models.Log) map[string]any {
	v := map[string]any{"time": log.Time, "message": log.Message}
	for name, value := range map[string]string{
		"level":       log.Level,
		"service":     log.Service,
		"environment": log.Environment,
		"trace_id":    log.TraceID,
		"span_id":     log.SpanID,
	} {
		if value != "" {
			v[name] = value
		}
	}

	values := attributeValues(log.Attributes)
	// Sorted, a name comes after every name it extends, so "a" is placed
	// before "a.b" finds it.
	objects := make(map[string]bool) // the dotted names of objects made here
	for _, name := range slices.Sorted(maps.Keys(values)) {
		place(v, objects, name, values[name])
	}
	return v
}

// place puts an Attribute's value in v, nested by the dots in its name, or at
// the top level under its whole name when it can't nest.
func place(v map[string]any, objects map[string]bool, name string, value any) {
	keys := strings.Split(name, ".")
	if slices.Contains(keys, "") {
		v[name] = value
		return
	}
	object := v
	for i, key := range keys[:len(keys)-1] {
		path := strings.Join(keys[:i+1], ".")
		inner, ok := object[key]
		if !ok {
			made := make(map[string]any)
			object[key] = made
			objects[path] = true
			object = made
			continue
		}
		if !objects[path] {
			v[name] = value
			return
		}
		object = inner.(map[string]any)
	}
	last := keys[len(keys)-1]
	if _, ok := object[last]; ok {
		v[name] = value
		return
	}
	object[last] = value
}

// attributeValues are a Log's Attributes by name, as a program sees each
// value. A whole Number is an integer, as a sender's JSON integer is in
// Vector.
func attributeValues(a models.Attributes) map[string]any {
	values := make(map[string]any, len(a.String)+len(a.Number)+len(a.Bool)+len(a.JSON))
	for name, s := range a.String {
		values[name] = s
	}
	for name, n := range a.Number {
		values[name] = number(n)
	}
	for name, b := range a.Bool {
		values[name] = b
	}
	for name, text := range a.JSON {
		values[name] = fromJSON(text)
	}
	return values
}

// maxExactInteger is the largest whole number a float64 holds exactly, with
// every whole number below it.
const maxExactInteger = 1 << 53

func number(f float64) any {
	if f == math.Trunc(f) && math.Abs(f) <= maxExactInteger {
		return int64(f)
	}
	return f
}

// fromJSON decodes a JSON Attribute, its numbers int64 when they are whole
// and float64 otherwise. Text that isn't JSON, which normalising never
// stores, stays a string.
func fromJSON(text string) any {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return text
	}
	return numbers(value)
}

func numbers(value any) any {
	switch value := value.(type) {
	case json.Number:
		if i, err := value.Int64(); err == nil {
			return i
		}
		f, _ := value.Float64()
		return f
	case []any:
		for i, x := range value {
			value[i] = numbers(x)
		}
	case map[string]any:
		for k, x := range value {
			value[k] = numbers(x)
		}
	}
	return value
}
