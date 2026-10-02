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
