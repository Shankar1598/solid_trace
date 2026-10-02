package normalise

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"strings"
	"time"
)

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
