package normalise_test

import (
	"math"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
)

// otlpLog is one OTLP log record with the resource and the scope it is sent
// under, for a test to fill.
type otlpLog struct {
	logs     plog.Logs
	resource pcommon.Resource
	scope    pcommon.InstrumentationScope
	record   plog.LogRecord
}

func newOTLPLog() otlpLog {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	scopeLogs := resourceLogs.ScopeLogs().AppendEmpty()
	return otlpLog{logs, resourceLogs.Resource(), scopeLogs.Scope(), scopeLogs.LogRecords().AppendEmpty()}
}

func (l otlpLog) normalise() (models.Log, normalise.Outcome) {
	return normalise.OTLP(l.resource, l.scope, l.record, receipt)
}

// normaliseOTLPRecord normalises a log record that fill fills, under an
// empty resource and scope.
func normaliseOTLPRecord(fill func(plog.LogRecord)) models.Log {
	l := newOTLPLog()
	fill(l.record)
	log, _ := l.normalise()
	return log
}

func TestOTLPAttributesOfEachLayerShareOneFlatNamespaceAndTheRecordWins(t *testing.T) {
	l := newOTLPLog()
	l.resource.Attributes().PutStr("host.name", "web-1")
	l.resource.Attributes().PutStr("shared", "resource")
	l.resource.Attributes().PutStr("scope.over.resource", "resource")
	l.resource.Attributes().PutStr("http.method", "GET")
	l.scope.Attributes().PutStr("shared", "scope")
	l.scope.Attributes().PutStr("scope.over.resource", "scope")
	l.record.Attributes().PutStr("shared", "record")
	http := l.record.Attributes().PutEmptyMap("http")
	http.PutStr("method", "POST")
	http.PutEmptyMap("response").PutInt("status", 200)
	l.record.Attributes().PutEmptySlice("ids").FromRaw([]any{int64(1), "two"})
	l.record.Body().SetStr("m")

	log, outcome := l.normalise()

	if outcome != normalise.Kept {
		t.Errorf("Expected the Log kept, got %v", outcome)
	}
	// No layer prefix. A kvlist is flattened to dotted names, also when a
	// lower layer gave the dotted name, and an array is one value.
	want := map[string]any{
		"host.name":            "web-1",
		"shared":               "record",
		"scope.over.resource":  "scope",
		"http.method":          "POST",
		"http.response.status": 200.0,
		"ids":                  []any{1.0, "two"},
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", want, got)
	}
}

func TestOTLPTimeIsTheRecordTimeElseTheObservedTimeElseTheReceiptTime(t *testing.T) {
	recordTime := time.Date(2026, 10, 2, 11, 59, 0, 123_456_789, time.UTC)
	observed := time.Date(2026, 10, 2, 11, 59, 30, 0, time.UTC)
	cases := []struct {
		name               string
		recordTime, seenAt time.Time
		want               time.Time
	}{
		{"both", recordTime, observed, recordTime},
		{"observed only", time.Time{}, observed, observed},
		{"none", time.Time{}, time.Time{}, receivedAt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log := normaliseOTLPRecord(func(r plog.LogRecord) {
				if !c.recordTime.IsZero() {
					r.SetTimestamp(pcommon.NewTimestampFromTime(c.recordTime))
				}
				if !c.seenAt.IsZero() {
					r.SetObservedTimestamp(pcommon.NewTimestampFromTime(c.seenAt))
				}
			})
			if !log.Time.Equal(c.want) {
				t.Errorf("Expected time %v, got %v", c.want, log.Time)
			}
			if got := attributes(t, log); len(got) != 0 {
				t.Errorf("Expected no Attributes, got %v", got)
			}
		})
	}
}

func TestAnOTLPTimeOutsideTheWindowIsKeptAsOriginalTime(t *testing.T) {
	old := time.Date(2026, 8, 1, 12, 0, 0, 123_456_789, time.UTC)
	log := normaliseOTLPRecord(func(r plog.LogRecord) {
		r.SetTimestamp(pcommon.NewTimestampFromTime(old))
	})

	if !log.Time.Equal(receivedAt) {
		t.Errorf("Expected the receipt time, got %v", log.Time)
	}
	// The nanoseconds are more digits than a DOUBLE holds, so they stay exact
	// as a string.
	want := map[string]any{"original.time": strconv.FormatInt(old.UnixNano(), 10)}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}
}

func TestOTLPSeverityNumbersGiveTheLevelFourToALevel(t *testing.T) {
	cases := []struct {
		number plog.SeverityNumber
		want   string
	}{
		{1, "trace"}, {4, "trace"},
		{5, "debug"}, {8, "debug"},
		{9, "info"}, {12, "info"},
		{13, "warn"}, {16, "warn"},
		{17, "error"}, {20, "error"},
		{21, "fatal"}, {24, "fatal"},
	}
	for _, c := range cases {
		log := normaliseOTLPRecord(func(r plog.LogRecord) { r.SetSeverityNumber(c.number) })
		if log.Level != c.want {
			t.Errorf("Expected severity number %d to give %q, got %q", c.number, c.want, log.Level)
		}
	}
}

func TestWithNoSeverityNumberTheSeverityTextGivesTheLevel(t *testing.T) {
	cases := []struct {
		text           string
		number         plog.SeverityNumber
		want           string
		wantAttributes map[string]any
	}{
		{"WARNING", 0, "warn", map[string]any{}},
		{"err", 0, "error", map[string]any{}},
		{"Information", 0, "", map[string]any{"original.level": "Information"}},
		{"", 0, "", map[string]any{}},
		// A number out of the range is no Level, so the text is used, or the
		// number is kept when there is no text.
		{"info", 25, "info", map[string]any{}},
		{"", 25, "", map[string]any{"original.level": 25.0}},
	}
	for _, c := range cases {
		log := normaliseOTLPRecord(func(r plog.LogRecord) {
			r.SetSeverityText(c.text)
			r.SetSeverityNumber(c.number)
		})
		if log.Level != c.want {
			t.Errorf("Expected %q and %d to give %q, got %q", c.text, c.number, c.want, log.Level)
		}
		if got := attributes(t, log); !reflect.DeepEqual(got, c.wantAttributes) {
			t.Errorf("Expected %q and %d to give Attributes %v, got %v", c.text, c.number, c.wantAttributes, got)
		}
	}
}

func TestAnOTLPStringBodyIsTheMessage(t *testing.T) {
	log := normaliseOTLPRecord(func(r plog.LogRecord) { r.Body().SetStr("order placed") })

	if log.Message != "order placed" {
		t.Errorf("Expected the message from the body, got %q", log.Message)
	}
	if got := attributes(t, log); len(got) != 0 {
		t.Errorf("Expected no Attributes, got %v", got)
	}
}

func TestAnOTLPBodyOfAnotherTypeIsKeptAsOriginalMessage(t *testing.T) {
	cases := []struct {
		name string
		fill func(pcommon.Value)
		want any
	}{
		{"map", func(v pcommon.Value) { v.SetEmptyMap().PutStr("event", "login") }, map[string]any{"event": "login"}},
		{"int", func(v pcommon.Value) { v.SetInt(42) }, 42.0},
		{"double", func(v pcommon.Value) { v.SetDouble(0.5) }, 0.5},
		{"bool", func(v pcommon.Value) { v.SetBool(true) }, true},
		{"array", func(v pcommon.Value) { v.SetEmptySlice().FromRaw([]any{"a", "b"}) }, []any{"a", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log := normaliseOTLPRecord(func(r plog.LogRecord) { c.fill(r.Body()) })
			if log.Message != "" {
				t.Errorf("Expected no message, got %q", log.Message)
			}
			want := map[string]any{"original.message": c.want}
			if got := attributes(t, log); !reflect.DeepEqual(got, want) {
				t.Errorf("Expected Attributes %v, got %v", want, got)
			}
		})
	}
}

func TestOTLPTraceAndSpanIDsAreLowerCaseHexAndAllZeroIsNoValue(t *testing.T) {
	log := normaliseOTLPRecord(func(r plog.LogRecord) {
		r.SetTraceID(pcommon.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36})
		r.SetSpanID(pcommon.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7})
	})
	if log.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || log.SpanID != "00f067aa0ba902b7" {
		t.Errorf("Expected lower-case hex ids, got %q and %q", log.TraceID, log.SpanID)
	}

	log = normaliseOTLPRecord(func(r plog.LogRecord) {
		r.SetTraceID(pcommon.TraceID{})
		r.SetSpanID(pcommon.SpanID{})
	})
	if log.TraceID != "" || log.SpanID != "" {
		t.Errorf("Expected no ids, got %q and %q", log.TraceID, log.SpanID)
	}
	if got := attributes(t, log); len(got) != 0 {
		t.Errorf("Expected no Attributes, got %v", got)
	}
}

func TestOTLPServiceNameFillsTheServiceAndIsConsumed(t *testing.T) {
	l := newOTLPLog()
	l.resource.Attributes().PutStr("service.name", "checkout")
	l.resource.Attributes().PutStr("service.version", "1.4.2")

	log, _ := l.normalise()

	if log.Service != "checkout" {
		t.Errorf("Expected service checkout, got %q", log.Service)
	}
	want := map[string]any{"service.version": "1.4.2"}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}
}

func TestOTLPEnvironmentComesFromTheNewNameElseTheOldName(t *testing.T) {
	l := newOTLPLog()
	l.resource.Attributes().PutStr("deployment.environment.name", "staging")
	l.resource.Attributes().PutStr("deployment.environment", "old")
	log, _ := l.normalise()

	if log.Environment != "staging" {
		t.Errorf("Expected environment staging, got %q", log.Environment)
	}
	// The name used is consumed; the other stays an Attribute.
	want := map[string]any{"deployment.environment": "old"}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}

	l = newOTLPLog()
	l.resource.Attributes().PutStr("deployment.environment", "production")
	log, _ = l.normalise()

	if log.Environment != "production" {
		t.Errorf("Expected environment production, got %q", log.Environment)
	}
	if got := attributes(t, log); len(got) != 0 {
		t.Errorf("Expected no Attributes, got %v", got)
	}
}

func TestTheOTLPScopeNameAndVersionAreOtelScopeAttributes(t *testing.T) {
	l := newOTLPLog()
	l.scope.SetName("io.example.checkout")
	l.scope.SetVersion("1.2.0")

	log, _ := l.normalise()

	want := map[string]any{"otel.scope.name": "io.example.checkout", "otel.scope.version": "1.2.0"}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}
}

func TestTheOTLPEventNameIsTheEventNameAttribute(t *testing.T) {
	log := normaliseOTLPRecord(func(r plog.LogRecord) { r.SetEventName("order.failed") })

	want := map[string]any{"event.name": "order.failed"}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}
}

func TestOTLPIntsAreNumbersUnlessADoubleCannotHoldThemExactly(t *testing.T) {
	log := normaliseOTLPRecord(func(r plog.LogRecord) {
		r.Attributes().PutInt("status", 200)
		r.Attributes().PutInt("max_exact", 9007199254740992)
		r.Attributes().PutInt("past_exact", 9007199254740993)
		r.Attributes().PutInt("min", math.MinInt64)
		r.Attributes().PutDouble("ratio", 0.5)
		r.Attributes().PutDouble("not_a_number", math.NaN())
	})

	want := models.Attributes{
		// The smallest int64 is a power of two, which a DOUBLE holds exactly.
		Number: map[string]float64{"status": 200, "max_exact": 9007199254740992, "min": math.MinInt64, "ratio": 0.5},
		// JSON has no NaN, so it is a string.
		String: map[string]string{"past_exact": "9007199254740993", "not_a_number": "NaN"},
	}
	if !reflect.DeepEqual(log.Attributes, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, log.Attributes)
	}
}

func TestOTLPBytesAreBase64Strings(t *testing.T) {
	log := normaliseOTLPRecord(func(r plog.LogRecord) {
		r.Attributes().PutEmptyBytes("blob").FromRaw([]byte{0xde, 0xad, 0xbe, 0xef})
	})

	want := map[string]any{"blob": "3q2+7w=="}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}
}

func TestOTLPAttributesNamedAsBuiltInFieldsOrSolidTraceAreKeptAsOriginal(t *testing.T) {
	l := newOTLPLog()
	l.resource.Attributes().PutStr("service", "from the resource")
	l.record.Attributes().PutStr("message", "not the message")
	l.record.Attributes().PutStr("level", "debug")
	l.record.Attributes().PutStr("time", "yesterday")
	l.record.Attributes().PutStr("trace_id", "abc")
	l.record.Attributes().PutBool("solidtrace.truncated", false)
	l.record.Body().SetStr("the message")
	l.record.SetSeverityNumber(plog.SeverityNumberError)

	log, _ := l.normalise()

	if log.Message != "the message" || log.Level != "error" || log.Service != "" || log.TraceID != "" {
		t.Errorf("Expected the built-in fields from the record only, got %+v", builtInsOf(log))
	}
	want := map[string]any{
		"original.service":              "from the resource",
		"original.message":              "not the message",
		"original.level":                "debug",
		"original.time":                 "yesterday",
		"original.trace_id":             "abc",
		"original.solidtrace.truncated": false,
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", want, got)
	}
}

func TestOTLPFlagsCountsSchemaURLsAndAUsedSeverityTextAreNotKept(t *testing.T) {
	l := newOTLPLog()
	l.logs.ResourceLogs().At(0).SetSchemaUrl("https://opentelemetry.io/schemas/1.38.0")
	l.logs.ResourceLogs().At(0).ScopeLogs().At(0).SetSchemaUrl("https://opentelemetry.io/schemas/1.38.0")
	l.resource.SetDroppedAttributesCount(2)
	l.scope.SetDroppedAttributesCount(3)
	l.record.SetDroppedAttributesCount(4)
	l.record.SetFlags(plog.DefaultLogRecordFlags.WithIsSampled(true))
	l.record.SetSeverityNumber(plog.SeverityNumberWarn)
	l.record.SetSeverityText("WARNING")
	l.record.Body().SetStr("m")

	log, _ := l.normalise()

	if log.Level != "warn" {
		t.Errorf("Expected level warn, got %q", log.Level)
	}
	if got := attributes(t, log); len(got) != 0 {
		t.Errorf("Expected no Attributes, got %v", got)
	}
}

// The fixtures are one request that otelcol 0.165.0 sent to two otlp_http
// exporters, one in protobuf and one in JSON, both decoded from gzip. The
// Collector received three Logs from telemetrygen v0.165.0:
//
//	telemetrygen logs --logs 3 --body "order placed" \
//	  --severity-number 13 --severity-text Warn \
//	  --trace-id 5b8efff798038103d269b633813fc60c --span-id b0e6f15b45c36b12 \
//	  --otlp-attributes 'service.name="checkout"' \
//	  --otlp-attributes 'deployment.environment.name="staging"' \
//	  --otlp-attributes 'host.name="web-1"' \
//	  --telemetry-attributes 'http.request.method="POST"' \
//	  --telemetry-attributes 'order.id=9007199254740993' \
//	  --telemetry-attributes 'retried=false' --telemetry-attributes 'tags=["a","b"]'
var collectorReceipt = normalise.Receipt{At: time.Date(2026, 11, 23, 23, 13, 0, 0, time.UTC), RetentionDays: 30}

// collectorLogs decodes a fixture with decode and normalises each of its log
// records.
func collectorLogs(t *testing.T, name string, decode func(plogotlp.ExportRequest, []byte) error) []models.Log {
	t.Helper()
	body, err := os.ReadFile("testdata/otlp/" + name)
	if err != nil {
		t.Fatal(err)
	}
	request := plogotlp.NewExportRequest()
	if err := decode(request, body); err != nil {
		t.Fatalf("Decode %s: %v", name, err)
	}
	var logs []models.Log
	for _, resourceLogs := range request.Logs().ResourceLogs().All() {
		for _, scopeLogs := range resourceLogs.ScopeLogs().All() {
			for _, record := range scopeLogs.LogRecords().All() {
				log, outcome := normalise.OTLP(resourceLogs.Resource(), scopeLogs.Scope(), record, collectorReceipt)
				if outcome != normalise.Kept {
					t.Errorf("Expected each Log of %s kept, got %v", name, outcome)
				}
				logs = append(logs, log)
			}
		}
	}
	return logs
}

func TestACollectorsProtobufAndJSONRequestsGiveTheSameLogs(t *testing.T) {
	protobuf := collectorLogs(t, "collector.pb", plogotlp.ExportRequest.UnmarshalProto)
	fromJSON := collectorLogs(t, "collector.json", plogotlp.ExportRequest.UnmarshalJSON)

	if len(protobuf) != 3 {
		t.Fatalf("Expected 3 Logs in the protobuf request, got %d", len(protobuf))
	}
	if !reflect.DeepEqual(protobuf, fromJSON) {
		t.Errorf("Expected the same Logs\nprotobuf: %+v\nJSON:     %+v", protobuf, fromJSON)
	}
	// OTLP JSON writes ids as hex, not as base64.
	for i := range fromJSON {
		if got := fromJSON[i].TraceID; got != protobuf[i].TraceID || got != "5b8efff798038103d269b633813fc60c" {
			t.Errorf("Expected Log %d's trace id 5b8efff798038103d269b633813fc60c in both, got %q and %q", i, protobuf[i].TraceID, got)
		}
	}

	want := builtIns{
		Time:        time.Date(2026, 11, 23, 23, 12, 34, 57_617_942, time.UTC),
		Level:       "warn",
		Message:     "order placed",
		Service:     "checkout",
		Environment: "staging",
		TraceID:     "5b8efff798038103d269b633813fc60c",
		SpanID:      "b0e6f15b45c36b12",
	}
	if got := builtInsOf(protobuf[0]); got != want {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
	wantAttributes := map[string]any{
		"host.name":           "web-1",
		"app":                 "server",
		"http.request.method": "POST",
		"order.id":            "9007199254740993",
		"retried":             false,
		"tags":                []any{"a", "b"},
	}
	if got := attributes(t, protobuf[0]); !reflect.DeepEqual(got, wantAttributes) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", wantAttributes, got)
	}
}
