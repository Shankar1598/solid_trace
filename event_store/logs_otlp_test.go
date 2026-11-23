package main_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

// The OTLP fixtures are one request that otelcol 0.165.0 sent to two
// otlp_http exporters, one in protobuf and one in JSON, decoded from gzip:
// three Logs from telemetrygen (see ingest/normalise/otlp_test.go).
const otlpFixtures = "ingest/normalise/testdata/otlp/"

// collectorSentAt is a time just after the fixtures' Logs, so that their own
// times are in the time window.
var collectorSentAt = time.Date(2026, 11, 23, 23, 13, 0, 0, time.UTC)

func otlpFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(otlpFixtures + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// sendOTLP posts a body to the OTLP Log route.
func sendOTLP(t *testing.T, env *TestEnv, path, contentType string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := env.App.Test(req, 5000)
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	return resp
}

// otlpResponse expects 200 with an OTLP export response in contentType, and
// decodes it.
func otlpResponse(t *testing.T, resp *http.Response, contentType string) plogotlp.ExportResponse {
	t.Helper()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != contentType {
		t.Errorf("Expected Content-Type %s, got %q", contentType, got)
	}
	response := plogotlp.NewExportResponse()
	unmarshal := response.UnmarshalProto
	if contentType == "application/json" {
		unmarshal = response.UnmarshalJSON
	}
	if err := unmarshal(raw); err != nil {
		t.Fatalf("Decode the export response %q: %v", raw, err)
	}
	return response
}

func expectNoneRejected(t *testing.T, response plogotlp.ExportResponse) {
	t.Helper()
	if partial := response.PartialSuccess(); partial.RejectedLogRecords() != 0 || partial.ErrorMessage() != "" {
		t.Errorf("Expected an empty partial success, got %d rejected: %q", partial.RejectedLogRecords(), partial.ErrorMessage())
	}
}

func TestOTLPProtobufAndJSONLogsAreStoredWithTheirFieldsAndCounted(t *testing.T) {
	protobuf := otlpFixture(t, "collector.pb")
	cases := map[string]struct {
		contentType, responseType string
		sent, decoded             []byte
		headers                   map[string]string
	}{
		// As a Collector sends by default.
		"gzip protobuf": {"application/x-protobuf", "application/x-protobuf", gzipped(t, protobuf), protobuf, map[string]string{"Content-Encoding": "gzip"}},
		// With encoding: json, and a parameter that changes nothing.
		"JSON": {"application/json; charset=utf-8", "application/json", otlpFixture(t, "collector.json"), otlpFixture(t, "collector.json"), nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := setupTestEnv(t)
			defer env.Cleanup()
			seedProject(t, env.SQLitePath, 123, 0)
			env.Clock.Set(collectorSentAt)

			headers := map[string]string{"Authorization": "Bearer test_public_key"}
			for header, value := range c.headers {
				headers[header] = value
			}
			resp := sendOTLP(t, env, "/v1/logs", c.contentType, c.sent, headers)
			expectNoneRejected(t, otlpResponse(t, resp, c.responseType))
			env.ProcessLogs(t)

			// The records' times in nanoseconds, as a Log keeps them: to
			// the microsecond. Newest first.
			times := []time.Time{
				time.Date(2026, 11, 23, 23, 12, 34, 57_687_000, time.UTC),
				time.Date(2026, 11, 23, 23, 12, 34, 57_682_000, time.UTC),
				time.Date(2026, 11, 23, 23, 12, 34, 57_617_000, time.UTC),
			}
			var want []listedLog
			for _, at := range times {
				want = append(want, listedLog{
					Time:        at,
					Level:       "warn",
					Message:     "order placed",
					Service:     "checkout",
					Environment: "staging",
					TraceID:     "5b8efff798038103d269b633813fc60c",
					SpanID:      "b0e6f15b45c36b12",
					Attributes: map[string]any{
						"host.name":           "web-1",
						"app":                 "server",
						"http.request.method": "POST",
						"order.id":            "9007199254740993",
						"retried":             false,
						"tags":                []any{"a", "b"},
					},
				})
			}
			if got := listedWithoutIDs(t, env); !reflect.DeepEqual(got, want) {
				t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
			}

			env.Clock.Advance(time.Second)
			env.FlushIngestCounts(t)
			got := overview(t, env, collectorSentAt, collectorSentAt, 123)
			wantReceived := overviewReceived{Logs: 3, Bytes: int64(len(c.decoded)), BytesOnWire: int64(len(c.sent)), Requests: 1}
			if got.Band.Received != wantReceived || got.Band.UnreadableLogs != 0 {
				t.Errorf("Expected received %+v and no unreadable Log, got %+v and %d", wantReceived, got.Band.Received, got.Band.UnreadableLogs)
			}
		})
	}
}

func TestOTLPRouteAcceptsTheProjectKeyAsBearerAndInEverySentryForm(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(collectorSentAt)

	forms := map[string]struct {
		path    string
		headers map[string]string
	}{
		"Authorization Bearer": {"/v1/logs", map[string]string{"Authorization": "Bearer test_public_key"}},
		"X-Sentry-Auth":        {"/v1/logs", map[string]string{"X-Sentry-Auth": "Sentry sentry_version=7, sentry_key=test_public_key"}},
		"Authorization Sentry": {"/v1/logs", map[string]string{"Authorization": "Sentry sentry_key=test_public_key, sentry_version=7"}},
		"sentry_key query":     {"/v1/logs?sentry_key=test_public_key", nil},
	}
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			resp := sendOTLP(t, env, form.path, "application/x-protobuf", otlpFixture(t, "collector.pb"), form.headers)
			expectNoneRejected(t, otlpResponse(t, resp, "application/x-protobuf"))
		})
	}

	env.ProcessLogs(t)
	if got := len(latestLogs(t, env, 123, 100)); got != 3*len(forms) {
		t.Errorf("Expected %d Logs stored, got %d", 3*len(forms), got)
	}
}

func TestOTLPRouteRefusesABadKeyContentTypeOrBody(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(collectorSentAt)

	bearer := map[string]string{"Authorization": "Bearer test_public_key"}
	protobuf := otlpFixture(t, "collector.pb")
	cases := map[string]struct {
		contentType string
		body        []byte
		headers     map[string]string
		status      int
	}{
		"missing key":               {"application/x-protobuf", protobuf, nil, 401},
		"unknown key":               {"application/x-protobuf", protobuf, map[string]string{"Authorization": "Bearer no_such_key"}, 401},
		"NDJSON":                    {"application/x-ndjson", otlpFixture(t, "collector.json"), bearer, 415},
		"no Content-Type":           {"", protobuf, bearer, 415},
		"protobuf that isn't OTLP":  {"application/x-protobuf", []byte("not protobuf"), bearer, 400},
		"JSON that isn't JSON":      {"application/json", []byte(`{"resourceLogs": [`), bearer, 400},
		"protobuf sent as JSON":     {"application/json", protobuf, bearer, 400},
		"unknown Content-Encoding":  {"application/x-protobuf", protobuf, map[string]string{"Authorization": "Bearer test_public_key", "Content-Encoding": "compress"}, 415},
		"gzip that doesn't decode":  {"application/x-protobuf", protobuf, map[string]string{"Authorization": "Bearer test_public_key", "Content-Encoding": "gzip"}, 400},
		"body over the 10 MB limit": {"application/x-protobuf", make([]byte, 10<<20+1), bearer, 413},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			resp := sendOTLP(t, env, "/v1/logs", c.contentType, c.body, c.headers)
			if resp.StatusCode != c.status {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("Expected %d, got %d: %s", c.status, resp.StatusCode, body)
			}
		})
	}

	env.ProcessLogs(t)
	if got := latestLogs(t, env, 123, 10); len(got) != 0 {
		t.Errorf("Expected no Logs stored, got %v", messages(got))
	}
}

func TestAnOTLPLogOverOneMBIsARejectedLogRecord(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)

	logs := plog.NewLogs()
	records := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	records.AppendEmpty().Body().SetStr("small")
	// 20 strings of 60 KB each: no per-Log limit cuts them, and together
	// they are over 1 MB.
	big := records.AppendEmpty()
	big.Body().SetStr("big")
	for i := range 20 {
		big.Attributes().PutStr(strings.Repeat("a", i+1), strings.Repeat("x", 60<<10))
	}
	body, err := plogotlp.NewExportRequestFromLogs(logs).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	resp := sendOTLP(t, env, "/v1/logs", "application/json", body, map[string]string{"Authorization": "Bearer test_public_key"})
	partial := otlpResponse(t, resp, "application/json").PartialSuccess()
	if partial.RejectedLogRecords() != 1 || !strings.Contains(partial.ErrorMessage(), "1 MB") {
		t.Errorf("Expected 1 rejected log record over 1 MB, got %d: %q", partial.RejectedLogRecords(), partial.ErrorMessage())
	}
	env.ProcessLogs(t)
	if got := messages(latestLogs(t, env, 123, 10)); !reflect.DeepEqual(got, []string{"small"}) {
		t.Errorf("Expected only the small Log stored, got %v", got)
	}

	env.Clock.Advance(time.Second)
	env.FlushIngestCounts(t)
	got := overview(t, env, testNow, testNow, 123)
	if got.Band.Received.Logs != 2 || got.Band.Received.Requests != 1 || got.Band.UnreadableLogs != 1 {
		t.Errorf("Expected 1 request of 2 Logs, 1 unreadable, got %+v and %d unreadable", got.Band.Received, got.Band.UnreadableLogs)
	}
}

func TestAnOverloadedIngestAnswersOTLP503WithRetryAfter(t *testing.T) {
	env := newTestEnv(t, func(env *TestEnv) { env.IngestMaxWaiting = 0 })
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)

	resp := sendOTLP(t, env, "/v1/logs", "application/x-protobuf", otlpFixture(t, "collector.pb"), map[string]string{"Authorization": "Bearer test_public_key"})
	if resp.StatusCode != 503 {
		t.Fatalf("Expected 503, got %d", resp.StatusCode)
	}
	assertRetryAfterAFewSeconds(t, resp)

	env.Clock.Advance(time.Second)
	env.FlushIngestCounts(t)
	got := overview(t, env, testNow, testNow, 123)
	if got.Band.OverloadedRequests != 1 || got.Band.Received != (overviewReceived{}) {
		t.Errorf("Expected 1 overloaded request and nothing received, got %+v", got.Band)
	}
}
