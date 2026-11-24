package normalise_test

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"

	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
)

var (
	pointTime  = time.Date(2026, 10, 2, 11, 59, 0, 500_000_000, time.UTC)
	pointStart = time.Date(2026, 10, 2, 11, 58, 0, 0, time.UTC)
)

// metricLogs normalises each data point of each metric in metrics.
func metricLogs(t *testing.T, metrics pmetric.Metrics, r normalise.Receipt) []models.Log {
	t.Helper()
	var points []normalise.MetricPoint
	for _, resourceMetrics := range metrics.ResourceMetrics().All() {
		for _, scopeMetrics := range resourceMetrics.ScopeMetrics().All() {
			for _, metric := range scopeMetrics.Metrics().All() {
				points = normalise.AppendMetricPoints(points, resourceMetrics.Resource(), scopeMetrics.Scope(), metric)
			}
		}
	}
	logs := make([]models.Log, 0, len(points))
	for _, point := range points {
		log, outcome := normalise.OTLPMetric(point, r)
		if outcome != normalise.Kept {
			t.Errorf("Expected each Log kept, got %v", outcome)
		}
		logs = append(logs, log)
	}
	return logs
}

// normaliseMetric normalises the one metric that fill fills, under an empty
// resource and scope, and returns the Log of each of its data points.
func normaliseMetric(t *testing.T, fill func(pmetric.Metric)) []models.Log {
	t.Helper()
	return metricLogs(t, oneMetric(func(_ pcommon.Resource, _ pcommon.InstrumentationScope, m pmetric.Metric) { fill(m) }), receipt)
}

// oneMetric returns metrics with the one metric that fill fills, under the
// resource and the scope that fill also fills.
func oneMetric(fill func(pcommon.Resource, pcommon.InstrumentationScope, pmetric.Metric)) pmetric.Metrics {
	metrics := pmetric.NewMetrics()
	resourceMetrics := metrics.ResourceMetrics().AppendEmpty()
	scopeMetrics := resourceMetrics.ScopeMetrics().AppendEmpty()
	fill(resourceMetrics.Resource(), scopeMetrics.Scope(), scopeMetrics.Metrics().AppendEmpty())
	return metrics
}

// onePoint returns the only Log in logs.
func onePoint(t *testing.T, logs []models.Log) models.Log {
	t.Helper()
	if len(logs) != 1 {
		t.Fatalf("Expected 1 Log, got %d", len(logs))
	}
	return logs[0]
}

// timed sets a data point's start time and time.
func timed(p interface {
	SetStartTimestamp(pcommon.Timestamp)
	SetTimestamp(pcommon.Timestamp)
}) {
	p.SetStartTimestamp(pcommon.NewTimestampFromTime(pointStart))
	p.SetTimestamp(pcommon.NewTimestampFromTime(pointTime))
}

func TestASumDataPointIsALogWithTheMetricNameAsItsMessage(t *testing.T) {
	cases := []struct {
		temporality pmetric.AggregationTemporality
		want        string
	}{
		{pmetric.AggregationTemporalityDelta, "delta"},
		// A cumulative value is stored as it comes, not as a delta.
		{pmetric.AggregationTemporalityCumulative, "cumulative"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			log := onePoint(t, normaliseMetric(t, func(m pmetric.Metric) {
				m.SetName("claude_code.cost.usage")
				m.SetUnit("USD")
				m.SetDescription("Cost of the Claude Code session")
				sum := m.SetEmptySum()
				sum.SetAggregationTemporality(c.temporality)
				sum.SetIsMonotonic(true)
				p := sum.DataPoints().AppendEmpty()
				timed(p)
				p.SetDoubleValue(0.25)
				p.Attributes().PutStr("model", "claude-haiku-5-5")
			}))

			want := builtIns{Time: pointTime, Level: "info", Message: "claude_code.cost.usage"}
			if got := builtInsOf(log); got != want {
				t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
			}
			// No metric.name, as the message holds it, and no description or
			// monotonic flag.
			wantAttributes := map[string]any{
				"model":              "claude-haiku-5-5",
				"metric.value":       0.25,
				"metric.type":        "sum",
				"metric.unit":        "USD",
				"metric.temporality": c.want,
				"metric.start_time":  "2026-10-02T11:58:00Z",
			}
			if got := attributes(t, log); !reflect.DeepEqual(got, wantAttributes) {
				t.Errorf("Expected Attributes\n%v\ngot\n%v", wantAttributes, got)
			}
		})
	}
}

func TestAGaugeValueIsADoubleAlsoWhenSentAsAnInt(t *testing.T) {
	logs := normaliseMetric(t, func(m pmetric.Metric) {
		m.SetName("queue.depth")
		points := m.SetEmptyGauge().DataPoints()
		asInt := points.AppendEmpty()
		timed(asInt)
		asInt.SetIntValue(42)
		asDouble := points.AppendEmpty()
		timed(asDouble)
		asDouble.SetDoubleValue(0.5)
	})

	if len(logs) != 2 {
		t.Fatalf("Expected a Log for each data point, got %d", len(logs))
	}
	// A gauge has no temporality, and the metric has no unit.
	for i, value := range []float64{42, 0.5} {
		want := models.Attributes{
			Number: map[string]float64{"metric.value": value},
			String: map[string]string{"metric.type": "gauge", "metric.start_time": "2026-10-02T11:58:00Z"},
		}
		if !reflect.DeepEqual(logs[i].Attributes, want) {
			t.Errorf("Expected Log %d's Attributes\n%+v\ngot\n%+v", i, want, logs[i].Attributes)
		}
	}
}

func TestAHistogramGivesItsCountSumMinAndMaxAndNotItsBuckets(t *testing.T) {
	log := onePoint(t, normaliseMetric(t, func(m pmetric.Metric) {
		m.SetName("http.server.request.duration")
		m.SetUnit("s")
		h := m.SetEmptyHistogram()
		h.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
		p := h.DataPoints().AppendEmpty()
		timed(p)
		p.SetCount(3)
		p.SetSum(0.75)
		p.SetMin(0.05)
		p.SetMax(0.5)
		p.BucketCounts().FromRaw([]uint64{1, 2})
		p.ExplicitBounds().FromRaw([]float64{0.1})
	}))

	want := map[string]any{
		"metric.count":       3.0,
		"metric.sum":         0.75,
		"metric.min":         0.05,
		"metric.max":         0.5,
		"metric.type":        "histogram",
		"metric.unit":        "s",
		"metric.temporality": "delta",
		"metric.start_time":  "2026-10-02T11:58:00Z",
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", want, got)
	}
}

func TestAnExponentialHistogramGivesItsCountAndOnlyTheSumMinAndMaxSent(t *testing.T) {
	log := onePoint(t, normaliseMetric(t, func(m pmetric.Metric) {
		m.SetName("rpc.duration")
		h := m.SetEmptyExponentialHistogram()
		h.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
		p := h.DataPoints().AppendEmpty()
		timed(p)
		p.SetCount(5)
		p.SetSum(12.5)
		p.SetScale(2)
		p.Positive().BucketCounts().FromRaw([]uint64{5})
	}))

	want := map[string]any{
		"metric.count":       5.0,
		"metric.sum":         12.5,
		"metric.type":        "exponential_histogram",
		"metric.temporality": "cumulative",
		"metric.start_time":  "2026-10-02T11:58:00Z",
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", want, got)
	}
}

func TestASummaryGivesItsCountAndSumAndNotItsQuantiles(t *testing.T) {
	log := onePoint(t, normaliseMetric(t, func(m pmetric.Metric) {
		m.SetName("gc.pause")
		p := m.SetEmptySummary().DataPoints().AppendEmpty()
		timed(p)
		p.SetCount(4)
		p.SetSum(2)
		q := p.QuantileValues().AppendEmpty()
		q.SetQuantile(0.99)
		q.SetValue(1.5)
	}))

	want := map[string]any{
		"metric.count":      4.0,
		"metric.sum":        2.0,
		"metric.type":       "summary",
		"metric.start_time": "2026-10-02T11:58:00Z",
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", want, got)
	}
}

// Each metric. name is ours, also when the Log has no value of that name. A
// sender's attribute of that name, in any layer and in the dotted or the
// nested form, is kept as original.<name>, as one named message is. So a
// sum of metric.value adds only the values of data points.
func TestASendersAttributeNamedAsAMetricAttributeIsKeptAsOriginal(t *testing.T) {
	cases := []struct {
		name string
		fill func(pcommon.Resource, pcommon.InstrumentationScope, pmetric.Metric)
		want map[string]any
	}{
		{
			name: "a value we set, dotted and nested in the point",
			fill: func(_ pcommon.Resource, _ pcommon.InstrumentationScope, m pmetric.Metric) {
				p := m.SetEmptySum().DataPoints().AppendEmpty()
				p.SetIntValue(7)
				p.Attributes().PutStr("message", "the sender's")
				p.Attributes().PutStr("metric.value", "dotted")
				p.Attributes().PutEmptyMap("metric").PutStr("type", "nested")
			},
			want: map[string]any{
				"original.message":      "the sender's",
				"original.metric.value": "dotted",
				"original.metric.type":  "nested",
				"metric.value":          7.0,
				"metric.type":           "sum",
			},
		},
		{
			name: "a point with no recorded value",
			fill: func(_ pcommon.Resource, _ pcommon.InstrumentationScope, m pmetric.Metric) {
				p := m.SetEmptyGauge().DataPoints().AppendEmpty()
				p.SetFlags(pmetric.DefaultDataPointFlags.WithNoRecordedValue(true))
				p.Attributes().PutDouble("metric.value", 5)
			},
			want: map[string]any{"original.metric.value": 5.0, "metric.type": "gauge"},
		},
		{
			name: "a histogram with no sum, min or max",
			fill: func(_ pcommon.Resource, _ pcommon.InstrumentationScope, m pmetric.Metric) {
				p := m.SetEmptyHistogram().DataPoints().AppendEmpty()
				p.SetCount(2)
				nested := p.Attributes().PutEmptyMap("metric")
				nested.PutDouble("sum", 1)
				nested.PutDouble("min", 2)
				nested.PutDouble("max", 3)
			},
			want: map[string]any{
				"original.metric.sum": 1.0,
				"original.metric.min": 2.0,
				"original.metric.max": 3.0,
				"metric.count":        2.0,
				"metric.type":         "histogram",
			},
		},
		{
			name: "a metric with no unit, in the scope",
			fill: func(_ pcommon.Resource, s pcommon.InstrumentationScope, m pmetric.Metric) {
				s.Attributes().PutStr("metric.unit", "ms")
				m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(1)
			},
			want: map[string]any{"original.metric.unit": "ms", "metric.value": 1.0, "metric.type": "gauge"},
		},
		{
			name: "the temporality of a gauge",
			fill: func(_ pcommon.Resource, _ pcommon.InstrumentationScope, m pmetric.Metric) {
				p := m.SetEmptyGauge().DataPoints().AppendEmpty()
				p.SetDoubleValue(1)
				p.Attributes().PutStr("metric.temporality", "cumulative")
			},
			want: map[string]any{"original.metric.temporality": "cumulative", "metric.value": 1.0, "metric.type": "gauge"},
		},
		{
			name: "a point with no start time",
			fill: func(_ pcommon.Resource, _ pcommon.InstrumentationScope, m pmetric.Metric) {
				p := m.SetEmptyGauge().DataPoints().AppendEmpty()
				p.SetDoubleValue(1)
				p.Attributes().PutEmptyMap("metric").PutStr("start_time", "yesterday")
			},
			want: map[string]any{"original.metric.start_time": "yesterday", "metric.value": 1.0, "metric.type": "gauge"},
		},
		{
			name: "a value in the resource",
			fill: func(r pcommon.Resource, _ pcommon.InstrumentationScope, m pmetric.Metric) {
				r.Attributes().PutDouble("metric.value", 99)
				m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(1)
			},
			want: map[string]any{"original.metric.value": 99.0, "metric.value": 1.0, "metric.type": "gauge"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log := onePoint(t, metricLogs(t, oneMetric(c.fill), receipt))
			if got := attributes(t, log); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Expected Attributes\n%v\ngot\n%v", c.want, got)
			}
		})
	}
}

// A DOUBLE cannot hold 2^53 + 1, so it is the nearest DOUBLE, still a number
// a sum adds up. An int Attribute keeps its exact digits as a string.
func TestAnIntValueOver2To53IsTheNearestDouble(t *testing.T) {
	log := onePoint(t, normaliseMetric(t, func(m pmetric.Metric) {
		p := m.SetEmptyGauge().DataPoints().AppendEmpty()
		p.SetIntValue(9007199254740993)
		p.Attributes().PutInt("id", 9007199254740993)
	}))

	want := models.Attributes{
		Number: map[string]float64{"metric.value": 9007199254740992},
		String: map[string]string{"metric.type": "gauge", "id": "9007199254740993"},
	}
	if !reflect.DeepEqual(log.Attributes, want) {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, log.Attributes)
	}
}

func TestAValueThatJSONCannotHoldIsAString(t *testing.T) {
	logs := normaliseMetric(t, func(m pmetric.Metric) {
		points := m.SetEmptyGauge().DataPoints()
		points.AppendEmpty().SetDoubleValue(math.NaN())
		points.AppendEmpty().SetDoubleValue(math.Inf(1))
	})

	for i, want := range []string{"NaN", "+Inf"} {
		if got := logs[i].Attributes.String["metric.value"]; got != want {
			t.Errorf("Expected Log %d's metric.value %q, got %q", i, want, got)
		}
	}
}

func TestAMetricWithNoNameGivesLogsWithNoMessage(t *testing.T) {
	log := onePoint(t, normaliseMetric(t, func(m pmetric.Metric) {
		m.SetEmptySum().DataPoints().AppendEmpty().SetDoubleValue(1)
	}))

	want := builtIns{Time: receivedAt, Level: "info"}
	if got := builtInsOf(log); got != want {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
	// No message is set, so there is no original.message either.
	wantAttributes := map[string]any{"metric.value": 1.0, "metric.type": "sum"}
	if got := attributes(t, log); !reflect.DeepEqual(got, wantAttributes) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", wantAttributes, got)
	}
}

// A data point has no observed time, so one with no time gets the time of
// its receipt.
func TestADataPointWithNoTimeGetsTheTimeOfItsReceipt(t *testing.T) {
	log := onePoint(t, normaliseMetric(t, func(m pmetric.Metric) {
		m.SetName("queue.depth")
		m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(1)
	}))

	if !log.Time.Equal(receivedAt) {
		t.Errorf("Expected the time %v, got %v", receivedAt, log.Time)
	}
	if _, ok := attributes(t, log)["original.time"]; ok {
		t.Errorf("Expected no original.time, got %v", log.Attributes)
	}
}

func TestTheServiceAndEnvironmentComeFromTheResource(t *testing.T) {
	log := onePoint(t, metricLogs(t, oneMetric(func(r pcommon.Resource, _ pcommon.InstrumentationScope, m pmetric.Metric) {
		r.Attributes().PutStr("service.name", "checkout")
		r.Attributes().PutStr("deployment.environment.name", "production")
		m.SetName("orders.placed")
		m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(1)
	}), receipt))

	want := builtIns{Time: receivedAt, Level: "info", Message: "orders.placed", Service: "checkout", Environment: "production"}
	if got := builtInsOf(log); got != want {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
	wantAttributes := map[string]any{"metric.value": 1.0, "metric.type": "gauge"}
	if got := attributes(t, log); !reflect.DeepEqual(got, wantAttributes) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", wantAttributes, got)
	}
}

// A metric name over 64 KB is cut, and the Log is marked, as a long message
// is on the log route.
func TestAMetricLogOverALimitIsTruncatedAndMarked(t *testing.T) {
	long := strings.Repeat("a", 70*kb)
	var points []normalise.MetricPoint
	oneMetric(func(r pcommon.Resource, s pcommon.InstrumentationScope, m pmetric.Metric) {
		m.SetName(long)
		m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(1)
		points = normalise.AppendMetricPoints(points, r, s, m)
	})

	log, outcome := normalise.OTLPMetric(points[0], receipt)
	if outcome != normalise.Truncated || !truncatedFlag(log) {
		t.Errorf("Expected the Log truncated and marked, got %v and %v", outcome, log.Attributes.Bool)
	}
	if log.Message != long[:64*kb] {
		t.Errorf("Expected the message cut to 64 KB, got %d bytes", len(log.Message))
	}
	if got := log.Attributes.Number["metric.value"]; got != 1 {
		t.Errorf("Expected metric.value 1, got %v", got)
	}
}

func TestADataPointWithNoRecordedValueHasNoValues(t *testing.T) {
	logs := normaliseMetric(t, func(m pmetric.Metric) {
		m.SetName("queue.depth")
		p := m.SetEmptyGauge().DataPoints().AppendEmpty()
		p.SetFlags(pmetric.DefaultDataPointFlags.WithNoRecordedValue(true))
	})

	want := map[string]any{"metric.type": "gauge"}
	if got := attributes(t, onePoint(t, logs)); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}
}

// The fixtures are two real exports from Claude Code 2.1.323, with the email,
// the account, organization, user and session ids and the OS version swapped
// for fake values of the same length. The .pb and the .json fixtures are two
// different exports, not one export in two encodings, so their data points
// and costs differ. The protobuf one is the shutdown export of a session that
// wrote and edited files, and the JSON one is from another session.
var claudeCodeReceipt = normalise.Receipt{At: time.Date(2026, 11, 24, 12, 20, 0, 0, time.UTC), RetentionDays: 30}

// claudeCodeLogs decodes a fixture with decode and normalises each of its
// data points.
func claudeCodeLogs(t *testing.T, name string, decode func(pmetricotlp.ExportRequest, []byte) error) []models.Log {
	t.Helper()
	body, err := os.ReadFile("testdata/otlp/" + name)
	if err != nil {
		t.Fatal(err)
	}
	request := pmetricotlp.NewExportRequest()
	if err := decode(request, body); err != nil {
		t.Fatalf("Decode %s: %v", name, err)
	}
	return metricLogs(t, request.Metrics(), claudeCodeReceipt)
}

func TestAClaudeCodeExportGivesALogForEachDataPointAndItsCostAddsUp(t *testing.T) {
	cases := []struct {
		name   string
		decode func(pmetricotlp.ExportRequest, []byte) error
		points int
		// cost is the sum of the claude_code.cost.usage values in the export.
		cost float64
	}{
		{"claude_code_metrics.pb", pmetricotlp.ExportRequest.UnmarshalProto, 12, 0.0051069},
		{"claude_code_metrics.json", pmetricotlp.ExportRequest.UnmarshalJSON, 6, 0.00309356},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := claudeCodeLogs(t, c.name, c.decode)

			if len(logs) != c.points {
				t.Fatalf("Expected %d Logs, got %d", c.points, len(logs))
			}
			var cost float64
			for _, log := range logs {
				if log.Message == "claude_code.cost.usage" {
					cost += log.Attributes.Number["metric.value"]
				}
			}
			if cost != c.cost {
				t.Errorf("Expected the cost to add up to %v, got %v", c.cost, cost)
			}
		})
	}
}

func TestAClaudeCodeCostDataPointGivesTheseFieldsAndAttributes(t *testing.T) {
	logs := claudeCodeLogs(t, "claude_code_metrics.pb", pmetricotlp.ExportRequest.UnmarshalProto)

	var log models.Log
	for _, l := range logs {
		if l.Message == "claude_code.cost.usage" {
			log = l
		}
	}
	want := builtIns{
		Time:    time.Date(2026, 11, 24, 12, 16, 57, 620_000_000, time.UTC),
		Level:   "info",
		Message: "claude_code.cost.usage",
		Service: "claude-code",
	}
	if got := builtInsOf(log); got != want {
		t.Errorf("Expected\n%+v\ngot\n%+v", want, got)
	}
	// The user and organization ids are kept as sent (fake in the fixture).
	wantAttributes := map[string]any{
		"host.arch":                 "amd64",
		"os.type":                   "linux",
		"os.version":                "6.8.0-1234-dev",
		"service.version":           "2.1.323",
		"otel.scope.name":           "com.anthropic.claude_code",
		"otel.scope.version":        "2.1.323",
		"user.id":                   "123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0",
		"user.email":                "eeeeeeeeee@example.com",
		"user.account_uuid":         "3456789a-bcde-f012-3456-789abcdef012",
		"user.account_id":           "user_456789abcdef0123456789ab",
		"organization.id":           "23456789-abcd-ef01-2345-6789abcdef01",
		"session.id":                "56789abc-def0-1234-5678-9abcdef01234",
		"anthropic.provider":        "firstParty",
		"anthropic.custom_base_url": false,
		"anthropic.plan":            "max",
		"terminal.type":             "ghostty",
		"model":                     "claude-haiku-5-5",
		"query_source":              "main",
		"effort":                    "medium",
		"metric.value":              0.0051069,
		"metric.type":               "sum",
		"metric.unit":               "USD",
		"metric.temporality":        "delta",
		"metric.start_time":         "2026-11-24T12:16:53.993Z",
	}
	if got := attributes(t, log); !reflect.DeepEqual(got, wantAttributes) {
		t.Errorf("Expected Attributes\n%v\ngot\n%v", wantAttributes, got)
	}
}
