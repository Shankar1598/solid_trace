package normalise_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
)

var receivedAt = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// receipt is a Log received at receivedAt for a Project that keeps Logs for
// 30 days.
var receipt = normalise.Receipt{At: receivedAt, RetentionDays: 30}

// normaliseJSON normalises the JSON object text as the JSON route's sender
// sent it.
func normaliseJSON(t *testing.T, text string, r normalise.Receipt) (models.Log, normalise.Outcome) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &object); err != nil {
		t.Fatalf("Bad test object %s: %v", text, err)
	}
	return normalise.JSON(object, r)
}

// attributes returns a Log's Attributes as a reader sees them: one object,
// each value with its own type.
func attributes(t *testing.T, log models.Log) map[string]any {
	t.Helper()
	raw, err := json.Marshal(log.Attributes)
	if err != nil {
		t.Fatalf("Marshal Attributes: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal Attributes %s: %v", raw, err)
	}
	return out
}

func TestMessageComesFromTheFirstAliasPresent(t *testing.T) {
	log, outcome := normaliseJSON(t, `{"body": "from body", "msg": "from msg", "log": "from log"}`, receipt)

	if outcome != normalise.Kept {
		t.Errorf("Expected the Log kept, got %v", outcome)
	}
	if log.Message != "from msg" {
		t.Errorf("Expected the message from msg, got %q", log.Message)
	}
	// The aliases ranked lower stay Attributes; the one used is consumed.
	want := map[string]any{"log": "from log", "body": "from body"}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}
}

// builtIns is a Log's built-in fields, to compare in one go.
type builtIns struct {
	Time                                                  time.Time
	Level, Message, Service, Environment, TraceID, SpanID string
}

func builtInsOf(log models.Log) builtIns {
	return builtIns{log.Time, log.Level, log.Message, log.Service, log.Environment, log.TraceID, log.SpanID}
}

func TestEachBuiltInFieldIsFilledFromAnyOfItsAliases(t *testing.T) {
	const traceID, spanID = "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"
	at := time.Date(2026, 10, 2, 11, 59, 0, 0, time.UTC)
	// Every Log has a time, so it is in each expectation.
	cases := []struct {
		object string
		want   builtIns
	}{
		{`{"message": "m"}`, builtIns{Time: receivedAt, Message: "m"}},
		{`{"msg": "m"}`, builtIns{Time: receivedAt, Message: "m"}},
		{`{"log": "m"}`, builtIns{Time: receivedAt, Message: "m"}},
		{`{"body": "m"}`, builtIns{Time: receivedAt, Message: "m"}},
		{`{"time": "2026-10-02T11:59:00Z"}`, builtIns{Time: at}},
		{`{"timestamp": "2026-10-02T11:59:00Z"}`, builtIns{Time: at}},
		{`{"ts": "2026-10-02T11:59:00Z"}`, builtIns{Time: at}},
		{`{"@timestamp": "2026-10-02T11:59:00Z"}`, builtIns{Time: at}},
		{`{"_time": "2026-10-02T11:59:00Z"}`, builtIns{Time: at}},
		{`{"level": "info"}`, builtIns{Time: receivedAt, Level: "info"}},
		{`{"severity": "info"}`, builtIns{Time: receivedAt, Level: "info"}},
		{`{"lvl": "info"}`, builtIns{Time: receivedAt, Level: "info"}},
		{`{"log.level": "info"}`, builtIns{Time: receivedAt, Level: "info"}},
		{`{"service": "web"}`, builtIns{Time: receivedAt, Service: "web"}},
		{`{"service.name": "web"}`, builtIns{Time: receivedAt, Service: "web"}},
		{`{"environment": "prod"}`, builtIns{Time: receivedAt, Environment: "prod"}},
		{`{"env": "prod"}`, builtIns{Time: receivedAt, Environment: "prod"}},
		{`{"deployment.environment": "prod"}`, builtIns{Time: receivedAt, Environment: "prod"}},
		{`{"trace_id": "` + traceID + `"}`, builtIns{Time: receivedAt, TraceID: traceID}},
		{`{"traceId": "` + traceID + `"}`, builtIns{Time: receivedAt, TraceID: traceID}},
		{`{"span_id": "` + spanID + `"}`, builtIns{Time: receivedAt, SpanID: spanID}},
		{`{"spanId": "` + spanID + `"}`, builtIns{Time: receivedAt, SpanID: spanID}},
	}
	for _, c := range cases {
		t.Run(c.object, func(t *testing.T) {
			log, _ := normaliseJSON(t, c.object, receipt)
			if got := builtInsOf(log); got != c.want {
				t.Errorf("Expected %+v, got %+v", c.want, got)
			}
			// The alias is consumed.
			if got := attributes(t, log); len(got) != 0 {
				t.Errorf("Expected no Attributes, got %v", got)
			}
		})
	}
}

func TestLevelsAreMatchedInAnyCaseWithCommonSpellingsMapped(t *testing.T) {
	cases := map[string]string{
		"trace": "trace", "DEBUG": "debug", "Info": "info", "warn": "warn", "error": "error", "FATAL": "fatal",
		"warning": "warn", "WARNING": "warn", "err": "error", "critical": "fatal", "crit": "fatal",
		"panic": "fatal", "Emergency": "fatal", "notice": "info",
	}
	for sent, want := range cases {
		t.Run(sent, func(t *testing.T) {
			log, _ := normaliseJSON(t, `{"level": "`+sent+`"}`, receipt)
			if log.Level != want {
				t.Errorf("Expected Level %q, got %q", want, log.Level)
			}
			if got := attributes(t, log); len(got) != 0 {
				t.Errorf("Expected no Attributes, got %v", got)
			}
		})
	}
}

func TestAValueThatCannotFillItsFieldIsKeptAsOriginal(t *testing.T) {
	cases := []struct {
		object string
		want   map[string]any
	}{
		{`{"level": "verbose"}`, map[string]any{"original.level": "verbose"}},
		{`{"severity": 30}`, map[string]any{"original.level": 30.0}},
		{`{"trace_id": "not-a-trace-id"}`, map[string]any{"original.trace_id": "not-a-trace-id"}},
		{`{"traceId": "4bf92f3577b34da6a3ce929d0e0e47"}`, map[string]any{"original.trace_id": "4bf92f3577b34da6a3ce929d0e0e47"}},
		{`{"span_id": "00f067aa0ba902bz"}`, map[string]any{"original.span_id": "00f067aa0ba902bz"}},
		{`{"msg": 42}`, map[string]any{"original.message": 42.0}},
		{`{"service": ["a", "b"]}`, map[string]any{"original.service": []any{"a", "b"}}},
		{`{"env": true}`, map[string]any{"original.environment": true}},
		// SolidTrace's original.<field> wins over a sender's own.
		{`{"level": "loud", "original.level": "mine"}`, map[string]any{"original.level": "loud"}},
	}
	for _, c := range cases {
		t.Run(c.object, func(t *testing.T) {
			log, _ := normaliseJSON(t, c.object, receipt)
			if got := builtInsOf(log); got != (builtIns{Time: receivedAt}) {
				t.Errorf("Expected no built-in field but the receipt time, got %+v", got)
			}
			if got := attributes(t, log); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Expected Attributes %v, got %v", c.want, got)
			}
		})
	}
}

func TestTraceAndSpanIDsAreStoredInLowerCase(t *testing.T) {
	log, _ := normaliseJSON(t, `{"trace_id": "4BF92F3577B34DA6A3CE929D0E0E4736", "span_id": "00F067AA0BA902B7"}`, receipt)
	if log.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || log.SpanID != "00f067aa0ba902b7" {
		t.Errorf("Expected lower-case ids, got %q and %q", log.TraceID, log.SpanID)
	}
}

func TestTimeIsReadFromISO8601OrAnEpochInAnyUnit(t *testing.T) {
	at := time.Date(2026, 10, 2, 11, 59, 30, 0, time.UTC)
	cases := map[string]time.Time{
		`"2026-10-02T11:59:30Z"`:           at,
		`"2026-10-02T11:59:30.123456789Z"`: at.Add(123456789),
		`"2026-10-02T12:59:30+01:00"`:      at,
		`"2026-10-02T12:59:30.5+0100"`:     at.Add(500 * time.Millisecond),
		`"2026-10-02 11:59:30Z"`:           at,
		`"2026-10-02t11:59:30z"`:           at,
		`"2026-10-02T11:59:30"`:            at, // no zone is UTC
		`"2026-10-02 11:59:30.25"`:         at.Add(250 * time.Millisecond),
		`1790942370`:                       at,
		`1790942370.25`:                    at.Add(250 * time.Millisecond),
		`1790942370250`:                    at.Add(250 * time.Millisecond),
		`1790942370250000`:                 at.Add(250 * time.Millisecond),
		`1790942370250000123`:              at.Add(250*time.Millisecond + 123),
		`1.79094237e9`:                     at,
		`1790942370123.456`:                at.Add(123456 * time.Microsecond),
	}
	for sent, want := range cases {
		t.Run(sent, func(t *testing.T) {
			log, _ := normaliseJSON(t, `{"time": `+sent+`}`, receipt)
			if !log.Time.Equal(want) || log.Time.Location() != time.UTC {
				t.Errorf("Expected %v, got %v", want, log.Time)
			}
			if got := attributes(t, log); len(got) != 0 {
				t.Errorf("Expected no Attributes, got %v", got)
			}
		})
	}
}

func TestATimeOutsideRetentionOrAnHourAheadBecomesTheReceiptTime(t *testing.T) {
	cases := []struct {
		time     string
		days     int
		replaced bool
	}{
		{`"2026-09-02T12:00:00Z"`, 30, false}, // 30 days back, exactly
		{`"2026-09-02T11:59:59Z"`, 30, true},
		{`"2026-09-02T11:59:59Z"`, 31, false},
		{`"2026-09-25T11:59:59Z"`, 7, true},
		{`"2026-10-02T13:00:00Z"`, 30, false}, // 1 hour ahead, exactly
		{`"2026-10-02T13:00:01Z"`, 30, true},
		{`0`, 30, true},
		{`-1790942370`, 30, true},
		{`"9999-12-31T23:59:59Z"`, 30, true},
		{`"yesterday"`, 30, true},
		{`true`, 30, true},
		{`{"seconds": 1}`, 30, true},
	}
	for _, c := range cases {
		t.Run(c.time, func(t *testing.T) {
			r := normalise.Receipt{At: receivedAt, RetentionDays: c.days}
			log, _ := normaliseJSON(t, `{"ts": `+c.time+`}`, r)
			var original any
			json.Unmarshal([]byte(c.time), &original)
			got := attributes(t, log)
			if c.replaced {
				if !log.Time.Equal(receivedAt) {
					t.Errorf("Expected the receipt time, got %v", log.Time)
				}
				if want := map[string]any{"original.time": original}; !reflect.DeepEqual(got, want) {
					t.Errorf("Expected Attributes %v, got %v", want, got)
				}
			} else {
				if log.Time.Equal(receivedAt) {
					t.Errorf("Expected the Log's own time kept, got the receipt time")
				}
				if len(got) != 0 {
					t.Errorf("Expected no Attributes, got %v", got)
				}
			}
		})
	}
}
