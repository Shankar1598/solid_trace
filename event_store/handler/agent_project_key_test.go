package handler_test

import (
	"strings"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

// The Agent Project's key (key-agent, writeProjectKeys) works on the OTLP
// routes only, and stores only the records of its Captured repository,
// github.com/acme/shop.

func TestTheAgentProjectsKeyIsRefusedOnTheRoutesThatAreNotOTLP(t *testing.T) {
	const key = "?sentry_key=key-agent"
	for _, c := range []logRouteCase{
		{name: "store", path: "/api/3/store" + key, body: text(`{"message":"boom"}`)},
		{name: "envelope", path: "/api/3/envelope" + key, body: text(envelopeWithLogs)},
		{name: "logs of the Project", path: "/api/3/logs" + key, header: asJSON, body: text(oneObject)},
		{name: "logs of the key", path: "/api/logs", header: with(asJSON, "Authorization", "Bearer key-agent"), body: text(oneObject)},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp, body, writer := sendToIngestApp(t, c)
			if resp.StatusCode != 403 || !strings.Contains(string(body), "/v1/logs and /v1/metrics") {
				t.Errorf("Expected 403 that names the OTLP routes, got %d: %s", resp.StatusCode, body)
			}
			if stored := writer.stored(); stored != "  nothing" {
				t.Errorf("Expected nothing stored, got\n%s", stored)
			}
		})
	}
}

// sentRecord is one OTLP log record or metric data point of a request,
// under a resource of its own: its own vcs.repository.url.full and its
// resource's, each "" for none. Its name is its body or metric name.
type sentRecord struct{ name, own, resource string }

var agentRecords = []sentRecord{
	{"captured", "https://github.com/acme/shop", ""},
	{"another repository", "https://github.com/acme/other", ""},
	{"no repository", "", ""},
	{"the repository on the resource only", "", "https://github.com/acme/shop"},
}

func putRepository(attributes pcommon.Map, url string) {
	if url != "" {
		attributes.PutStr("vcs.repository.url.full", url)
	}
}

func otlpLogsOf(t *testing.T, records []sentRecord) []byte {
	t.Helper()
	logs := plog.NewLogs()
	for _, r := range records {
		resourceLogs := logs.ResourceLogs().AppendEmpty()
		resourceLogs.Resource().Attributes().PutStr("service.name", "claude-code")
		putRepository(resourceLogs.Resource().Attributes(), r.resource)
		record := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
		record.Body().SetStr(r.name)
		putRepository(record.Attributes(), r.own)
	}
	body, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func otlpMetricsOf(t *testing.T, records []sentRecord) []byte {
	t.Helper()
	metrics := pmetric.NewMetrics()
	for _, r := range records {
		resourceMetrics := metrics.ResourceMetrics().AppendEmpty()
		resourceMetrics.Resource().Attributes().PutStr("service.name", "claude-code")
		putRepository(resourceMetrics.Resource().Attributes(), r.resource)
		metric := resourceMetrics.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
		metric.SetName(r.name)
		point := metric.SetEmptySum().DataPoints().AppendEmpty()
		point.SetDoubleValue(1)
		putRepository(point.Attributes(), r.own)
	}
	body, err := pmetricotlp.NewExportRequestFromMetrics(metrics).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// A record is kept only by its own vcs.repository.url.full: one on the
// resource keeps none, as a sender can set it there for every record. The
// records filtered out are not rejected. An ordinary Project's key stores
// every record.
func TestTheAgentProjectsKeyStoresOnlyTheOTLPRecordsOfACapturedRepository(t *testing.T) {
	asProtobuf := map[string]string{"Content-Type": "application/x-protobuf"}
	routes := []struct {
		path     string
		body     func(*testing.T, []sentRecord) []byte
		rejected func(t *testing.T, body []byte) int64
	}{
		{"/v1/logs", otlpLogsOf, func(t *testing.T, body []byte) int64 {
			response := plogotlp.NewExportResponse()
			if err := response.UnmarshalProto(body); err != nil || response.PartialSuccess().ErrorMessage() != "" {
				t.Errorf("Expected an export response with no message, got %q: %v", body, err)
			}
			return response.PartialSuccess().RejectedLogRecords()
		}},
		{"/v1/metrics", otlpMetricsOf, func(t *testing.T, body []byte) int64 {
			response := pmetricotlp.NewExportResponse()
			if err := response.UnmarshalProto(body); err != nil || response.PartialSuccess().ErrorMessage() != "" {
				t.Errorf("Expected an export response with no message, got %q: %v", body, err)
			}
			return response.PartialSuccess().RejectedDataPoints()
		}},
	}
	for _, route := range routes {
		for _, key := range []struct {
			key, stored string
			kept        []string
		}{
			{"key-agent", "  Log batch for Project 3, Logs: 1\n", []string{"captured"}},
			{"key-one", "  Log batch for Project 1, Logs: 4\n", []string{"captured", "another repository", "no repository", "the repository on the resource only"}},
		} {
			t.Run(route.path+" "+key.key, func(t *testing.T) {
				resp, body, writer := sendToIngestApp(t, logRouteCase{
					path:   route.path,
					header: with(asProtobuf, "Authorization", "Bearer "+key.key),
					body:   func() []byte { return route.body(t, agentRecords) },
				})
				if resp.StatusCode != 200 {
					t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
				}
				if rejected := route.rejected(t, body); rejected != 0 {
					t.Errorf("Expected no rejected record, got %d", rejected)
				}
				stored := writer.stored()
				if !strings.HasPrefix(stored, key.stored) {
					t.Fatalf("Expected the stored text to start %q, got\n%s", key.stored, stored)
				}
				for _, name := range key.kept {
					if !strings.Contains(stored, `"message":"`+name+`"`) {
						t.Errorf("Expected the record %q stored, got\n%s", name, stored)
					}
				}
			})
		}
	}
}
