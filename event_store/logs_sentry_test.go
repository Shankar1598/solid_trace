package main_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// The fixtures below are envelopes as current Sentry SDKs send them: the
// framing of each SDK's envelope serialiser, and each SDK's log records.
//   - sentry-ruby 7.1: Transport#serialize_envelope joins the headers and
//     items with "\n", with no item length; LogEvent#to_h and
//     Scope#apply_to_telemetry make the record.
//   - sentry-javascript 11.4: serializeEnvelope, also with no item length;
//     createLogEnvelope and _INTERNAL_captureLog make the item and records.
//   - sentry-python 2.71 and sentry-go 0.49: each item carries its length and
//     ends in "\n". sentry-go marshals a record's timestamp as RFC 3339.

// sentryItem frames one envelope item without a length, as sentry-ruby and
// sentry-javascript do.
func sentryItem(header, payload string) string {
	return "\n" + header + "\n" + payload
}

// sentryItemWithLength frames one envelope item with its length, as
// sentry-python and sentry-go do.
func sentryItemWithLength(header, payload string) string {
	return "\n" + strings.TrimSuffix(header, "}") + fmt.Sprintf(`,"length":%d}`, len(payload)) + "\n" + payload
}

// logItemHeader is a log item's header for n records.
func logItemHeader(n int) string {
	return fmt.Sprintf(`{"type":"log","item_count":%d,"content_type":"application/vnd.sentry.items.log+json"}`, n)
}

// postEnvelope sends an envelope to Project 123 as a Sentry SDK does, and
// expects 200.
func postEnvelope(t *testing.T, env *TestEnv, envelope string) {
	t.Helper()
	req, _ := http.NewRequest("POST", "/api/123/envelope", bytes.NewBufferString(envelope))
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key=test_public_key, sentry_client=sentry-ruby/7.1.0")
	resp, err := env.App.Test(req, 2000)
	if err != nil {
		t.Fatalf("POST envelope failed: %v", err)
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, raw)
	}
}

// listedWithoutIDs returns a Project's latest Logs, newest first, with their
// ids checked and cleared.
func listedWithoutIDs(t *testing.T, env *TestEnv) []listedLog {
	t.Helper()
	logs := latestLogs(t, env, 123, 100)
	for i := range logs {
		if logs[i].ID == "" {
			t.Errorf("Expected Log %d to have an id", i)
		}
		logs[i].ID = ""
	}
	return logs
}

func TestASentryEnvelopeWithOnlyALogItemHasItsLogsStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Two Sentry.logger calls in sentry-ruby 7.1, flushed together.
	records := []string{
		`{"level":"info","timestamp":1790942340.25,"trace_id":"5b8efff798038103d269b633813fc60c","span_id":"b0e6f15b45c36b12","body":"User ada has logged in!","attributes":{` +
			`"sentry.message.template":{"value":"User %s has logged in!","type":"string"},` +
			`"sentry.message.parameter.0":{"value":"ada","type":"string"},` +
			`"sentry.sdk.name":{"value":"sentry.ruby","type":"string"},"sentry.sdk.version":{"value":"7.1.0","type":"string"},` +
			`"sentry.environment":{"value":"production","type":"string"},"sentry.release":{"value":"shop@1.4.2","type":"string"},` +
			`"server.address":{"value":"web-1","type":"string"}}}`,
		`{"level":"error","timestamp":1790942341.5,"trace_id":"5b8efff798038103d269b633813fc60c","body":"Payment declined","attributes":{` +
			`"sentry.sdk.name":{"value":"sentry.ruby","type":"string"},"sentry.sdk.version":{"value":"7.1.0","type":"string"},` +
			`"sentry.environment":{"value":"production","type":"string"},"sentry.release":{"value":"shop@1.4.2","type":"string"},` +
			`"server.address":{"value":"web-1","type":"string"}}}`,
	}
	envelope := `{"sent_at":"2026-10-02T12:00:00Z"}` +
		sentryItem(logItemHeader(2), `{"items":[`+strings.Join(records, ",")+`]}`)
	postEnvelope(t, env, envelope)
	env.ProcessLogs(t)

	sdk := map[string]any{
		"sentry.sdk.name":    "sentry.ruby",
		"sentry.sdk.version": "7.1.0",
		"sentry.release":     "shop@1.4.2",
		"server.address":     "web-1",
	}
	withSDK := func(attributes map[string]any) map[string]any {
		for name, value := range sdk {
			attributes[name] = value
		}
		return attributes
	}
	want := []listedLog{
		{
			Time:        time.Date(2026, 10, 2, 11, 59, 1, 500_000_000, time.UTC),
			Level:       "error",
			Message:     "Payment declined",
			Environment: "production",
			TraceID:     "5b8efff798038103d269b633813fc60c",
			Attributes:  withSDK(map[string]any{}),
		},
		{
			Time:        time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC),
			Level:       "info",
			Message:     "User ada has logged in!",
			Environment: "production",
			TraceID:     "5b8efff798038103d269b633813fc60c",
			SpanID:      "b0e6f15b45c36b12",
			Attributes: withSDK(map[string]any{
				"sentry.message.template":    "User %s has logged in!",
				"sentry.message.parameter.0": "ada",
			}),
		},
	}
	if got := listedWithoutIDs(t, env); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
}

func TestAnEventAndALogItemInOneEnvelopeAreBothStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	eventPayload := `{"event_id":"9b7d475493954601ab7f3b599b2578cb","message":"Envelope Test","environment":"production"}`
	// A record as sentry-go 0.49 marshals it.
	record := `{"timestamp":"2026-10-02T11:59:00.25Z","trace_id":"5b8efff798038103d269b633813fc60c","level":"error",` +
		`"severity_number":17,"body":"charge failed","attributes":{` +
		`"sentry.environment":{"value":"production","type":"string"},` +
		`"sentry.sdk.name":{"value":"sentry.go","type":"string"},"sentry.sdk.version":{"value":"0.49.0","type":"string"}}}`
	envelope := `{"event_id":"9b7d475493954601ab7f3b599b2578cb","sent_at":"2026-10-02T12:00:00Z"}` +
		sentryItemWithLength(`{"type":"event"}`, eventPayload) + "\n" +
		sentryItemWithLength(logItemHeader(1), `{"items":[`+record+`]}`) + "\n"
	postEnvelope(t, env, envelope)
	env.Process(t)
	env.ProcessLogs(t)

	events, err := env.DuckDBWriter.QueryEvents(storage.QueryParams{ProjectID: 123})
	if err != nil {
		t.Fatalf("Query Events: %v", err)
	}
	if len(events) != 1 || events[0].Environment != "production" {
		t.Errorf("Expected the Event stored, got %+v", events)
	}

	want := []listedLog{{
		Time:        time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC),
		Level:       "error",
		Message:     "charge failed",
		Environment: "production",
		TraceID:     "5b8efff798038103d269b633813fc60c",
		Attributes: map[string]any{
			"severity_number":    17.0,
			"sentry.sdk.name":    "sentry.go",
			"sentry.sdk.version": "0.49.0",
		},
	}}
	if got := listedWithoutIDs(t, env); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
}

func TestTypedSentryAttributesAreListedAsPlainValues(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// A record as sentry-javascript 11.4 sends it, with user attributes of
	// every type, some named like JSON aliases.
	record := `{"timestamp":1790942340.25,"level":"warn","body":"Cart total over limit",` +
		`"trace_id":"5b8efff798038103d269b633813fc60c","severity_number":13,"attributes":{` +
		`"duration":{"value":12,"type":"integer","unit":"millisecond"},` +
		`"cart.total":{"value":1250.75,"type":"double"},` +
		`"retried":{"value":false,"type":"boolean"},` +
		`"skus":{"value":["sku-1","sku-2"],"type":"array"},` +
		`"order.id":{"value":9007199254740993,"type":"integer"},` +
		`"service.name":{"value":"checkout","type":"string"},` +
		`"msg":{"value":"not the message","type":"string"},` +
		`"env":{"value":"not the environment","type":"string"},` +
		`"sentry.environment":{"value":"staging","type":"string"},` +
		`"sentry.sdk.name":{"value":"sentry.javascript.node","type":"string"},` +
		`"sentry.timestamp.sequence":{"value":0,"type":"integer"}}}`
	envelope := `{"sdk":{"name":"sentry.javascript.node","version":"11.4.0"}}` +
		sentryItem(logItemHeader(1), `{"version":2,"items":[`+record+`]}`)
	postEnvelope(t, env, envelope)
	env.ProcessLogs(t)

	want := []listedLog{{
		Time:        time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC),
		Level:       "warn",
		Message:     "Cart total over limit",
		Service:     "checkout",
		Environment: "staging",
		TraceID:     "5b8efff798038103d269b633813fc60c",
		Attributes: map[string]any{
			"duration":                  12.0,
			"cart.total":                1250.75,
			"retried":                   false,
			"skus":                      []any{"sku-1", "sku-2"},
			"order.id":                  "9007199254740993",
			"msg":                       "not the message",
			"env":                       "not the environment",
			"sentry.sdk.name":           "sentry.javascript.node",
			"sentry.timestamp.sequence": 0.0,
			"severity_number":           13.0,
		},
	}}
	if got := listedWithoutIDs(t, env); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
}

func TestAMalformedSentryLogRecordIsDroppedAndTheEnvelopeGets200(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	good := `{"timestamp":1790942340.25,"level":"info","body":"kept","trace_id":"5b8efff798038103d269b633813fc60c"}`
	envelope := `{"sent_at":"2026-10-02T12:00:00Z"}` +
		sentryItem(logItemHeader(4), `{"items":["not a record",`+good+`,42,["also","not"]]}`)
	postEnvelope(t, env, envelope)
	env.ProcessLogs(t)

	want := []listedLog{{
		Time:    time.Date(2026, 10, 2, 11, 59, 0, 250_000_000, time.UTC),
		Level:   "info",
		Message: "kept",
		TraceID: "5b8efff798038103d269b633813fc60c",
	}}
	got := listedWithoutIDs(t, env)
	for i := range got {
		if len(got[i].Attributes) == 0 {
			got[i].Attributes = nil
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
}
