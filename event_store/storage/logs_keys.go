package storage

import (
	"cmp"
	"iter"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/solidtrace/event_store/models"
)

// keyCountsKept is how many keys' counts are kept per Project per day: a
// top-K of the Attribute keys the most Logs carried, which the Fields list
// reads.
const keyCountsKept = 1000

// AttributeType is the type of an Attribute's value. It decides which typed
// subcolumn of attributes holds the value, or attrs_json for JSON.
type AttributeType string

const (
	StringAttribute AttributeType = "string"
	NumberAttribute AttributeType = "number"
	BoolAttribute   AttributeType = "bool"
	// JSONAttribute values (arrays, deep nesting) are JSON text.
	JSONAttribute AttributeType = "json"
)

// countedTypes are the Attribute types whose keys are counted.
var countedTypes = []AttributeType{BoolAttribute, NumberAttribute, StringAttribute}

// LogDay is the UTC day of a Log's time t, as midnight UTC.
func LogDay(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

// dayString is a day as a Date literal, such as 2026-10-23.
func dayString(day time.Time) string {
	return day.UTC().Format(time.DateOnly)
}

// latestTime and earliestTime are the later and the earlier of two times.
func latestTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func earliestTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// typedKey is an Attribute's (type, key) pair, the unit of the key counts.
type typedKey struct {
	Type AttributeType
	Key  string
}

func (k typedKey) compare(other typedKey) int {
	return cmp.Or(cmp.Compare(k.Type, other.Type), cmp.Compare(k.Key, other.Key))
}

// countable reports whether a key is counted: not one with a NUL or invalid
// UTF-8.
func countable(key string) bool {
	return !strings.ContainsRune(key, 0) && utf8.ValidString(key)
}

// logKeys returns a Log's counted (type, key) pairs, by type then key.
func logKeys(log models.Log) []typedKey {
	var keys []typedKey
	for _, t := range countedTypes {
		for _, key := range slices.Sorted(attributeNames(log.Attributes, t)) {
			if countable(key) {
				keys = append(keys, typedKey{t, key})
			}
		}
	}
	return keys
}

// attributeNames iterates the names of a Log's Attributes of type t.
func attributeNames(a models.Attributes, t AttributeType) iter.Seq[string] {
	switch t {
	case StringAttribute:
		return maps.Keys(a.String)
	case NumberAttribute:
		return maps.Keys(a.Number)
	case BoolAttribute:
		return maps.Keys(a.Bool)
	default:
		return maps.Keys(a.JSON)
	}
}

// mergeKeyCounts adds a batch's counts to a day's counts, then keeps only the
// keep most popular keys. It is the Space-Saving top-K: a key new to a full
// top-K starts from the lowest count kept, so a key that keeps coming climbs
// past keys seen once in a burst rather than being evicted each time. It
// returns the keys whose count changed and those evicted.
func mergeKeyCounts(counts, batch map[typedKey]int64, keep int) (changed, evicted []typedKey) {
	var floor int64
	if len(counts) >= keep {
		floor = slices.Min(slices.Collect(maps.Values(counts)))
	}
	for key, n := range batch {
		if current, ok := counts[key]; ok {
			counts[key] = current + n
		} else {
			counts[key] = floor + n
		}
	}

	if len(counts) > keep {
		ranked := slices.SortedFunc(maps.Keys(counts), func(a, b typedKey) int {
			return cmp.Or(cmp.Compare(counts[b], counts[a]), a.compare(b))
		})
		evicted = ranked[keep:]
		for _, key := range evicted {
			delete(counts, key)
		}
	}
	for key := range batch {
		if _, ok := counts[key]; ok {
			changed = append(changed, key)
		}
	}
	return changed, evicted
}
