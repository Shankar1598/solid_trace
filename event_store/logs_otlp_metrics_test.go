package main_test

import (
	"io"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

// The metrics fixtures are two real Claude Code exports, scrubbed (see
// ingest/normalise/otlp_metric_test.go). They are two exports, not one in two
// encodings, so their metric data points and costs differ.
var claudeCodeExportedAt = time.Date(2026, 11, 24, 12, 20, 0, 0, time.UTC)

func TestOTLPMetricsAreStoredAsALogForEachDataPointAndCountedAsLogs(t *testing.T) {
	protobuf := otlpFixture(t, "claude_code_metrics.pb")
	cases := map[string]struct {
		contentType string
		sent        []byte
		decoded     []byte
		headers     map[string]string
		points      int
		cost        string
	}{
		// As Claude Code sends with http/protobuf.
		"gzip protobuf": {"application/x-protobuf", gzipped(t, protobuf), protobuf, map[string]string{"Content-Encoding": "gzip"}, 12, `["claude-haiku-5-5",0.0051069]`},
		"JSON":          {"application/json", otlpFixture(t, "claude_code_metrics.json"), otlpFixture(t, "claude_code_metrics.json"), nil, 6, `["claude-haiku-5-5",0.00309356]`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := setupTestEnv(t)
			defer env.Cleanup()
			seedProject(t, env.SQLitePath, 123, 0)
			env.Clock.Set(claudeCodeExportedAt)

			headers := map[string]string{"Authorization": "Bearer test_public_key"}
			for header, value := range c.headers {
				headers[header] = value
			}
			resp := sendOTLP(t, env, "/v1/metrics", c.contentType, c.sent, headers)
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != c.contentType {
				t.Fatalf("Expected 200 in %s, got %d in %q: %s", c.contentType, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
			}
			response := pmetricotlp.NewExportResponse()
			unmarshal := response.UnmarshalProto
			if c.contentType == "application/json" {
				unmarshal = response.UnmarshalJSON
			}
			if err := unmarshal(raw); err != nil || response.PartialSuccess().RejectedDataPoints() != 0 {
				t.Errorf("Expected an export response with no rejected metric data point, got %q: %v", raw, err)
			}
			env.ProcessLogs(t)

			stored := latestLogs(t, env, 123, 100)
			if len(stored) != c.points {
				t.Fatalf("Expected %d Logs, got %d: %v", c.points, len(stored), messages(stored))
			}
			costs := 0
			for _, log := range stored {
				if log.Message != "claude_code.cost.usage" {
					continue
				}
				costs++
				if log.Level != "info" || log.Service != "claude-code" || log.Attributes["metric.unit"] != "USD" || log.Attributes["metric.type"] != "sum" {
					t.Errorf("Expected a cost Log of info from claude-code, a sum in USD, got %+v", log)
				}
			}
			if costs == 0 {
				t.Errorf("Expected claude_code.cost.usage Logs, got %v", messages(stored))
			}

			// Through the Query path, the cost adds up to the export's.
			columns, rows := runTable(t, env, `where message == "claude_code.cost.usage" | summarize sum(metric.value) by model`)
			if len(columns) != 2 || len(rows) != 1 || rows[0] != c.cost {
				t.Errorf("Expected the cost %s, got %v %v", c.cost, columns, rows)
			}

			env.Clock.Advance(time.Second)
			env.FlushIngestCounts(t)
			got := overview(t, env, claudeCodeExportedAt, claudeCodeExportedAt, 123)
			want := overviewReceived{Logs: int64(c.points), Bytes: int64(len(c.decoded)), BytesOnWire: int64(len(c.sent)), Requests: 1}
			if got.Band.Received != want || got.Band.UnreadableLogs != 0 {
				t.Errorf("Expected received %+v and no unreadable Log, got %+v and %d", want, got.Band.Received, got.Band.UnreadableLogs)
			}
		})
	}
}

func TestAnOTLPMetricsBodyThatDoesNotDecodeCountsAsARequestWithItsBytes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProject(t, env.SQLitePath, 123, 0)

	body := []byte(`{"resourceMetrics": [`)
	resp := sendOTLP(t, env, "/v1/metrics", "application/json", body, map[string]string{"Authorization": "Bearer test_public_key"})
	if resp.StatusCode != 400 {
		t.Fatalf("Expected 400, got %d", resp.StatusCode)
	}

	env.Clock.Advance(time.Second)
	env.FlushIngestCounts(t)
	got := overview(t, env, testNow, testNow, 123)
	want := overviewReceived{Bytes: int64(len(body)), BytesOnWire: int64(len(body)), Requests: 1}
	if got.Band.Received != want || got.Band.UnreadableLogs != 0 {
		t.Errorf("Expected received %+v and no unreadable Log, got %+v and %d", want, got.Band.Received, got.Band.UnreadableLogs)
	}
}
