package normalise_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
)

// normaliseSentry normalises the JSON object text as one record of a Sentry
// envelope's log item.
func normaliseSentry(t *testing.T, text string, r normalise.Receipt) (models.Log, normalise.Outcome) {
	t.Helper()
	var record map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &record); err != nil {
		t.Fatalf("Bad test record %s: %v", text, err)
	}
	return normalise.Sentry(record, r)
}

// A record as sentry-ruby 7.1 sends it for
// Sentry.logger.info("User %s has logged in!", "ada"): LogEvent#to_h, with the
// attributes Scope#apply_to_telemetry adds.
const rubyRecord = `{
	"level": "info",
	"timestamp": 1790942340.25,
	"trace_id": "5b8efff798038103d269b633813fc60c",
	"span_id": "b0e6f15b45c36b12",
	"body": "User ada has logged in!",
	"attributes": {
		"sentry.message.template": {"value": "User %s has logged in!", "type": "string"},
		"sentry.message.parameter.0": {"value": "ada", "type": "string"},
		"sentry.sdk.name": {"value": "sentry.ruby", "type": "string"},
		"sentry.sdk.version": {"value": "7.1.0", "type": "string"},
		"sentry.environment": {"value": "production", "type": "string"},
		"sentry.release": {"value": "shop@1.4.2", "type": "string"},
		"server.address": {"value": "web-1", "type": "string"}
	}
}`

func TestASentryRecordFillsTheBuiltInFieldsAndUnwrapsItsAttributes(t *testing.T) {
	log, outcome := normaliseSentry(t, rubyRecord, receipt)

	if outcome != normalise.Kept {
		t.Errorf("Expected the Log kept, got %v", outcome)
	}
	want := builtIns{
		Time:        time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC),
		Level:       "info",
		Message:     "User ada has logged in!",
		Environment: "production",
		TraceID:     "5b8efff798038103d269b633813fc60c",
		SpanID:      "b0e6f15b45c36b12",
	}
	if got := builtInsOf(log); got != want {
		t.Errorf("Expected %+v, got %+v", want, got)
	}
	// sentry.environment is consumed; the other sentry.* keys stay.
	wantAttributes := map[string]any{
		"sentry.message.template":    "User %s has logged in!",
		"sentry.message.parameter.0": "ada",
		"sentry.sdk.name":            "sentry.ruby",
		"sentry.sdk.version":         "7.1.0",
		"sentry.release":             "shop@1.4.2",
		"server.address":             "web-1",
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, wantAttributes) {
		t.Errorf("Expected Attributes %v, got %v", wantAttributes, got)
	}
}

// A record as sentry-javascript 11.4 sends it (core/src/logs/internal.ts),
// with attributes of every type a Sentry SDK sends, some of them named like
// JSON aliases.
const javascriptRecord = `{
	"timestamp": 1790942340.25,
	"level": "warn",
	"body": "Cart total over limit",
	"trace_id": "5b8efff798038103d269b633813fc60c",
	"severity_number": 13,
	"attributes": {
		"duration": {"value": 12, "type": "integer", "unit": "millisecond"},
		"cart.total": {"value": 1250.75, "type": "double"},
		"retried": {"value": false, "type": "boolean"},
		"skus": {"value": ["sku-1", "sku-2"], "type": "array"},
		"order.id": {"value": 9007199254740993, "type": "integer"},
		"service.name": {"value": "checkout", "type": "string"},
		"msg": {"value": "not the message", "type": "string"},
		"env": {"value": "not the environment", "type": "string"},
		"ts": {"value": 1, "type": "integer"},
		"sentry.environment": {"value": "staging", "type": "string"},
		"sentry.sdk.name": {"value": "sentry.javascript.node", "type": "string"},
		"sentry.timestamp.sequence": {"value": 0, "type": "integer"},
		"sentry.trace.parent_span_id": {"value": "b0e6f15b45c36b12", "type": "string"}
	}
}`

func TestTypedSentryAttributesAreUnwrappedToValuesOfTheirOwnType(t *testing.T) {
	log, _ := normaliseSentry(t, javascriptRecord, receipt)

	want := builtIns{
		Time:        time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC),
		Level:       "warn",
		Message:     "Cart total over limit",
		Service:     "checkout",
		Environment: "staging",
		TraceID:     "5b8efff798038103d269b633813fc60c",
	}
	if got := builtInsOf(log); got != want {
		t.Errorf("Expected %+v, got %+v", want, got)
	}
	// Each value keeps its type; the unit is dropped with the type. A whole
	// number a DOUBLE can't hold is kept exactly, as a string. JSON aliases
	// mean nothing here, and a record's other fields are Attributes too.
	wantAttributes := map[string]any{
		"duration":                    12.0,
		"cart.total":                  1250.75,
		"retried":                     false,
		"skus":                        []any{"sku-1", "sku-2"},
		"order.id":                    "9007199254740993",
		"msg":                         "not the message",
		"env":                         "not the environment",
		"ts":                          1.0,
		"sentry.sdk.name":             "sentry.javascript.node",
		"sentry.timestamp.sequence":   0.0,
		"sentry.trace.parent_span_id": "b0e6f15b45c36b12",
		"severity_number":             13.0,
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, wantAttributes) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", wantAttributes, got)
	}
}

func TestASentryTimestampInRFC3339FillsTheTime(t *testing.T) {
	// sentry-go 0.49 marshals Log.Timestamp, a time.Time, as RFC 3339.
	log, _ := normaliseSentry(t, `{
		"timestamp": "2026-10-02T11:59:00.25Z",
		"trace_id": "5b8efff798038103d269b633813fc60c",
		"level": "error",
		"severity_number": 17,
		"body": "payment failed",
		"attributes": {"sentry.sdk.name": {"value": "sentry.go", "type": "string"}}
	}`, receipt)

	if want := time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC); !log.Time.Equal(want) {
		t.Errorf("Expected time %v, got %v", want, log.Time)
	}
}

func TestSentryRecordsFollowTheSameRulesAsJSONLogs(t *testing.T) {
	log, outcome := normaliseSentry(t, `{
		"timestamp": 1780000000,
		"trace_id": "not-a-trace-id",
		"level": "verbose",
		"body": "`+strings.Repeat("x", 70_000)+`",
		"attributes": {
			"solidtrace.truncated": {"value": false, "type": "boolean"},
			"sentry.environment": {"value": 7, "type": "integer"}
		}
	}`, receipt)

	if outcome != normalise.Truncated {
		t.Errorf("Expected the Log truncated, got %v", outcome)
	}
	// A time older than the Retention becomes the receipt time.
	if !log.Time.Equal(receivedAt) {
		t.Errorf("Expected the receipt time, got %v", log.Time)
	}
	if log.Level != "" || log.TraceID != "" || log.Environment != "" {
		t.Errorf("Expected no Level, trace id or environment, got %+v", builtInsOf(log))
	}
	if len(log.Message) != 64<<10 {
		t.Errorf("Expected the message cut to 64 KB, got %d bytes", len(log.Message))
	}
	want := map[string]any{
		"original.time":                 1780000000.0,
		"original.trace_id":             "not-a-trace-id",
		"original.level":                "verbose",
		"original.environment":          7.0,
		"original.solidtrace.truncated": false,
		"solidtrace.truncated":          true,
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", want, got)
	}
}

func TestAnAttributeWinsOverARecordFieldOfTheSameName(t *testing.T) {
	log, _ := normaliseSentry(t, `{
		"body": "m",
		"severity_number": 13,
		"attributes": {"severity_number": {"value": 9, "type": "integer"}}
	}`, receipt)

	if got, want := attributes(t, log), map[string]any{"severity_number": 9.0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}
}
