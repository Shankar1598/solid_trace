package handler_test

import (
	"encoding/json"
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

const capturedURL = "https://github.com/acme/shop"

// storedAttributes are the Attributes of the stored Log with the message
// given.
func storedAttributes(t *testing.T, stored, message string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(stored, "\n") {
		var log struct {
			Message    string         `json:"message"`
			Attributes map[string]any `json:"attributes"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &log) == nil && log.Message == message {
			return log.Attributes
		}
	}
	t.Fatalf("Expected a stored Log %q, got\n%s", message, stored)
	return nil
}

// The JSON encoding of OTLP is filtered as protobuf is. A record whose vcs
// is in the nested form only has no vcs.repository.url.full of its own,
// so it is filtered out.
func TestTheAgentProjectsKeyFiltersAJSONExportAndKeepsNoNestedRepository(t *testing.T) {
	logs := plog.NewLogs()
	logRecords := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	metrics := pmetric.NewMetrics()
	metricList := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	for _, r := range []struct {
		name string
		put  func(pcommon.Map)
	}{
		{"captured", func(a pcommon.Map) { putRepository(a, capturedURL) }},
		{"nested", func(a pcommon.Map) {
			a.PutEmptyMap("vcs").PutEmptyMap("repository").PutEmptyMap("url").PutStr("full", capturedURL)
		}},
	} {
		record := logRecords.AppendEmpty()
		record.Body().SetStr(r.name)
		r.put(record.Attributes())
		metric := metricList.AppendEmpty()
		metric.SetName(r.name)
		point := metric.SetEmptyGauge().DataPoints().AppendEmpty()
		point.SetDoubleValue(1)
		r.put(point.Attributes())
	}
	logsJSON, err := plogotlp.NewExportRequestFromLogs(logs).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	metricsJSON, err := pmetricotlp.NewExportRequestFromMetrics(metrics).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string][]byte{"/v1/logs": logsJSON, "/v1/metrics": metricsJSON} {
		t.Run(path, func(t *testing.T) {
			resp, answer, writer := sendToIngestApp(t, logRouteCase{
				path: path, header: with(asJSON, "Authorization", "Bearer key-agent"), body: func() []byte { return body },
			})
			if resp.StatusCode != 200 || strings.Contains(string(answer), "rejected") {
				t.Fatalf("Expected 200 with no rejected record, got %d: %s", resp.StatusCode, answer)
			}
			stored := writer.stored()
			if !strings.HasPrefix(stored, "  Log batch for Project 3, Logs: 1\n") || storedAttributes(t, stored, "captured")["file"] != "otel" {
				t.Errorf("Expected only the captured record, in the file otel, got\n%s", stored)
			}
		})
	}
}

// The server, not the sender, puts each Log of the Agent Project's key in
// its range: the file otel, which no read of Runs, of the Dispatcher's
// Logs or of Agent sessions uses. A sender's file, from any layer, is
// kept as original.file, and wins over the sender's own original.file. An
// ordinary Project's key keeps the file as sent.
func TestTheAgentProjectsKeyStoresEachRecordInTheFileOTel(t *testing.T) {
	logs := plog.NewLogs()
	onResource := logs.ResourceLogs().AppendEmpty()
	onResource.Resource().Attributes().PutStr("file", "dispatcher")
	warning := onResource.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	warning.Body().SetStr("a warning")
	putRepository(warning.Attributes(), capturedURL)
	records := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	run := records.AppendEmpty()
	run.Body().SetStr("a run event")
	run.Attributes().FromRaw(map[string]any{"file": "runs/x1", "original.file": "the sender's own", "run.id": "x1", "run.seq": 99})
	putRepository(run.Attributes(), capturedURL)
	none := records.AppendEmpty()
	none.Body().SetStr("no file")
	putRepository(none.Attributes(), capturedURL)
	logsBody, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}

	metrics := pmetric.NewMetrics()
	metric := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("a metric")
	point := metric.SetEmptyGauge().DataPoints().AppendEmpty()
	point.SetDoubleValue(1)
	point.Attributes().PutStr("file", "runs/x2")
	putRepository(point.Attributes(), capturedURL)
	metricsBody, err := pmetricotlp.NewExportRequestFromMetrics(metrics).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}

	bodies := map[string][]byte{"/v1/logs": logsBody, "/v1/metrics": metricsBody}
	for _, c := range []struct{ path, key, message, file, original string }{
		{"/v1/logs", "key-agent", "a run event", "otel", "runs/x1"},
		{"/v1/logs", "key-agent", "a warning", "otel", "dispatcher"},
		{"/v1/logs", "key-agent", "no file", "otel", ""},
		{"/v1/metrics", "key-agent", "a metric", "otel", "runs/x2"},
		{"/v1/logs", "key-one", "a run event", "runs/x1", "the sender's own"},
	} {
		t.Run(c.key+" "+c.message, func(t *testing.T) {
			resp, answer, writer := sendToIngestApp(t, logRouteCase{
				path:   c.path,
				header: with(map[string]string{"Content-Type": "application/x-protobuf"}, "Authorization", "Bearer "+c.key),
				body:   func() []byte { return bodies[c.path] },
			})
			if resp.StatusCode != 200 {
				t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, answer)
			}
			attributes := storedAttributes(t, writer.stored(), c.message)
			original, hasOriginal := attributes["original.file"]
			if attributes["file"] != c.file || hasOriginal != (c.original != "") || (hasOriginal && original != c.original) {
				t.Errorf("Expected file %q and original.file %q, got %v", c.file, c.original, attributes)
			}
		})
	}
}
