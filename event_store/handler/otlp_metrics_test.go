package handler_test

import (
	"os"
	"strings"
	"testing"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

// metricsFixture is a Claude Code metrics export of metrics-01's fixtures: 12
// data points in protobuf, and 6 in JSON.
func metricsFixture(t *testing.T, name string) func() []byte {
	t.Helper()
	body, err := os.ReadFile("../ingest/normalise/testdata/otlp/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return func() []byte { return body }
}

// metricsResponse expects 200 with an OTLP metrics export response in
// contentType, and decodes it.
func metricsResponse(t *testing.T, status int, contentType string, body []byte, wantType string) pmetricotlp.ExportResponse {
	t.Helper()
	if status != 200 {
		t.Fatalf("Expected 200, got %d: %s", status, body)
	}
	if contentType != wantType {
		t.Errorf("Expected Content-Type %s, got %q", wantType, contentType)
	}
	response := pmetricotlp.NewExportResponse()
	unmarshal := response.UnmarshalProto
	if wantType == "application/json" {
		unmarshal = response.UnmarshalJSON
	}
	if err := unmarshal(body); err != nil {
		t.Fatalf("Decode the export response %q: %v", body, err)
	}
	return response
}

func TestOTLPMetricsAreOneLogBatchOfALogForEachDataPointAndAnsweredInTheirEncoding(t *testing.T) {
	cases := []struct {
		name, contentType, responseType string
		body                            func() []byte
		header                          map[string]string
		logs                            string
	}{
		// As a Collector sends by default.
		{"gzip protobuf", "application/x-protobuf", "application/x-protobuf", func() []byte { return gzipped(metricsFixture(t, "claude_code_metrics.pb")()) }, map[string]string{"Content-Encoding": "gzip"}, "Logs: 12"},
		{"JSON", "application/json; charset=utf-8", "application/json", metricsFixture(t, "claude_code_metrics.json"), nil, "Logs: 6"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			header := with(c.header, "Authorization", "Bearer key-one", "Content-Type", c.contentType)
			resp, body, writer := sendToIngestApp(t, logRouteCase{path: "/v1/metrics", header: header, body: c.body})

			partial := metricsResponse(t, resp.StatusCode, resp.Header.Get("Content-Type"), body, c.responseType).PartialSuccess()
			if partial.RejectedDataPoints() != 0 || partial.ErrorMessage() != "" {
				t.Errorf("Expected an empty partial success, got %d rejected: %q", partial.RejectedDataPoints(), partial.ErrorMessage())
			}
			stored := writer.stored()
			if want := "  Log batch for Project 1, " + c.logs + "\n"; !strings.HasPrefix(stored, want) || strings.Count(stored, "Log batch") != 1 {
				t.Errorf("Expected one Log batch starting %q, got\n%s", want, stored)
			}
			if !strings.Contains(stored, `"message":"claude_code.cost.usage"`) || !strings.Contains(stored, `"metric.unit":"USD"`) {
				t.Errorf("Expected a claude_code.cost.usage Log in USD, got\n%s", stored)
			}
		})
	}
}

func TestOTLPMetricsRefuseABadKeyContentTypeOrBodyAndStoreNothing(t *testing.T) {
	protobuf := metricsFixture(t, "claude_code_metrics.pb")
	bearer := map[string]string{"Authorization": "Bearer key-one", "Content-Type": "application/x-protobuf"}
	cases := []struct {
		name   string
		c      logRouteCase
		status int
		answer string
	}{
		{"missing key", logRouteCase{header: map[string]string{"Content-Type": "application/x-protobuf"}, body: protobuf}, 401, "Missing authentication"},
		{"unknown key", logRouteCase{header: with(bearer, "Authorization", "Bearer no-such-key"), body: protobuf}, 401, "Invalid project key"},
		{"NDJSON", logRouteCase{header: with(bearer, "Content-Type", "application/x-ndjson"), body: protobuf}, 415, "Content-Type must be"},
		{"no Content-Type", logRouteCase{header: map[string]string{"Authorization": "Bearer key-one"}, body: protobuf}, 415, "Content-Type must be"},
		{"protobuf that isn't OTLP", logRouteCase{header: bearer, body: text("not protobuf")}, 400, "not an OTLP metrics export request"},
		{"JSON that isn't JSON", logRouteCase{header: with(bearer, "Content-Type", "application/json"), body: text(`{"resourceMetrics": [`)}, 400, "not an OTLP metrics export request"},
		{"body over the 10 MB limit", logRouteCase{header: bearer, body: func() []byte { return make([]byte, logBodyOverLimit) }}, 413, ""},
		{"overloaded", logRouteCase{header: bearer, body: protobuf, overloaded: true}, 503, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.c.path = "/v1/metrics"
			resp, body, writer := sendToIngestApp(t, tc.c)
			if resp.StatusCode != tc.status || !strings.Contains(string(body), tc.answer) {
				t.Errorf("Expected %d with %q, got %d: %s", tc.status, tc.answer, resp.StatusCode, body)
			}
			if tc.status == 503 && resp.Header.Get("Retry-After") == "" {
				t.Error("Expected a Retry-After on 503")
			}
			if stored := writer.stored(); stored != "  nothing" {
				t.Errorf("Expected nothing stored, got\n%s", stored)
			}
		})
	}
}

func TestAnOTLPDataPointOverOneMBAsALogIsARejectedDataPoint(t *testing.T) {
	metrics := pmetric.NewMetrics()
	gauge := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	gauge.SetName("queue.depth")
	points := gauge.SetEmptyGauge().DataPoints()
	points.AppendEmpty().SetDoubleValue(1)
	// 20 strings of 60 KB each: no per-Log limit cuts them, and together
	// they are over 1 MB.
	big := points.AppendEmpty()
	big.SetDoubleValue(2)
	for i := range 20 {
		big.Attributes().PutStr(strings.Repeat("a", i+1), strings.Repeat("x", 60<<10))
	}
	request, err := pmetricotlp.NewExportRequestFromMetrics(metrics).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}

	resp, body, writer := sendToIngestApp(t, logRouteCase{
		path:   "/v1/metrics",
		header: map[string]string{"Authorization": "Bearer key-one", "Content-Type": "application/x-protobuf"},
		body:   func() []byte { return request },
	})
	partial := metricsResponse(t, resp.StatusCode, resp.Header.Get("Content-Type"), body, "application/x-protobuf").PartialSuccess()
	if partial.RejectedDataPoints() != 1 || !strings.Contains(partial.ErrorMessage(), "1 MB") {
		t.Errorf("Expected 1 rejected data point over 1 MB, got %d: %q", partial.RejectedDataPoints(), partial.ErrorMessage())
	}
	if stored := writer.stored(); !strings.HasPrefix(stored, "  Log batch for Project 1, Logs: 1\n") {
		t.Errorf("Expected only the small data point stored, got\n%s", stored)
	}
}
