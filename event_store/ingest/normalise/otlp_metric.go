package normalise

import (
	"encoding/json"
	"strconv"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/solidtrace/event_store/models"
)

// A MetricPoint is one data point of an OTLP metric, with the metric, the
// resource and the instrumentation scope it was sent under. Each one becomes
// one Log.
type MetricPoint struct {
	resource pcommon.Resource
	scope    pcommon.InstrumentationScope
	metric   pmetric.Metric
	point    dataPoint
}

// dataPoint is what the data points of each metric type have.
type dataPoint interface {
	Attributes() pcommon.Map
	StartTimestamp() pcommon.Timestamp
	Timestamp() pcommon.Timestamp
	Flags() pmetric.DataPointFlags
}

// distribution is a Histogram or an ExponentialHistogram data point.
type distribution interface {
	Count() uint64
	HasSum() bool
	Sum() float64
	HasMin() bool
	Min() float64
	HasMax() bool
	Max() float64
}

// metricTypes are the metric.type values.
var metricTypes = map[pmetric.MetricType]string{
	pmetric.MetricTypeSum:                  "sum",
	pmetric.MetricTypeGauge:                "gauge",
	pmetric.MetricTypeHistogram:            "histogram",
	pmetric.MetricTypeExponentialHistogram: "exponential_histogram",
	pmetric.MetricTypeSummary:              "summary",
}

// temporalities are the metric.temporality values.
var temporalities = map[pmetric.AggregationTemporality]string{
	pmetric.AggregationTemporalityDelta:      "delta",
	pmetric.AggregationTemporalityCumulative: "cumulative",
}

// AppendMetricPoints appends each data point of metric to points, with the
// resource and the scope it was sent under.
func AppendMetricPoints(points []MetricPoint, resource pcommon.Resource, scope pcommon.InstrumentationScope, metric pmetric.Metric) []MetricPoint {
	add := func(p dataPoint) { points = append(points, MetricPoint{resource, scope, metric, p}) }
	switch metric.Type() {
	case pmetric.MetricTypeSum:
		for _, p := range metric.Sum().DataPoints().All() {
			add(p)
		}
	case pmetric.MetricTypeGauge:
		for _, p := range metric.Gauge().DataPoints().All() {
			add(p)
		}
	case pmetric.MetricTypeHistogram:
		for _, p := range metric.Histogram().DataPoints().All() {
			add(p)
		}
	case pmetric.MetricTypeExponentialHistogram:
		for _, p := range metric.ExponentialHistogram().DataPoints().All() {
			add(p)
		}
	case pmetric.MetricTypeSummary:
		for _, p := range metric.Summary().DataPoints().All() {
			add(p)
		}
	}
	return points
}

// OTLPMetric normalises one OTLP metric data point as one Log, by the rules
// OTLP follows for a log record (ADR 0002), with the data point in the place
// of the record.
//
// The message is the metric's name, the time is the data point's time, and
// the Level is info. The data point's values are numbers under metric., each
// one a DOUBLE: metric.value for a Sum or a Gauge, and metric.count and
// metric.sum, with metric.min and metric.max when sent, for a Histogram, an
// ExponentialHistogram or a Summary. metric.type, metric.unit,
// metric.temporality and metric.start_time tell what the values are. These
// win over a data point attribute of the same name, in the dotted or the
// nested form. A data point flagged as having no recorded value has no
// values. The description, the metadata, the monotonic flag, the buckets,
// the quantiles, the exemplars and the flags aren't kept.
func OTLPMetric(p MetricPoint, r Receipt) (models.Log, Outcome) {
	own := flatten(members(p.point.Attributes()))
	own["metric.type"] = quote(metricTypes[p.metric.Type()])
	if unit := p.metric.Unit(); unit != "" {
		own["metric.unit"] = quote(unit)
	}
	if temporality, ok := temporalities[temporalityOf(p.metric)]; ok {
		own["metric.temporality"] = quote(temporality)
	}
	if start := p.point.StartTimestamp(); start != 0 {
		own["metric.start_time"] = quote(start.AsTime().UTC().Format(time.RFC3339Nano))
	}
	if !p.point.Flags().NoRecordedValue() {
		setValues(own, p.point)
	}

	s := otlpSent(p.resource, p.scope, own)
	if t := p.point.Timestamp(); t != 0 {
		s.fields[timeField] = strconv.AppendUint(nil, uint64(t), 10)
	}
	s.fields[level] = quote("info")
	s.fields[message] = quote(p.metric.Name())
	return normaliseSent(s, r)
}

// setValues sets the values of a data point in attributes, each one a DOUBLE.
func setValues(attributes map[string]json.RawMessage, point dataPoint) {
	set := func(name string, d float64) { attributes[name] = appendDouble(nil, d) }
	switch p := point.(type) {
	case pmetric.NumberDataPoint:
		switch p.ValueType() {
		case pmetric.NumberDataPointValueTypeDouble:
			set("metric.value", p.DoubleValue())
		case pmetric.NumberDataPointValueTypeInt:
			set("metric.value", float64(p.IntValue()))
		}
	case distribution:
		set("metric.count", float64(p.Count()))
		if p.HasSum() {
			set("metric.sum", p.Sum())
		}
		if p.HasMin() {
			set("metric.min", p.Min())
		}
		if p.HasMax() {
			set("metric.max", p.Max())
		}
	case pmetric.SummaryDataPoint:
		set("metric.count", float64(p.Count()))
		set("metric.sum", p.Sum())
	}
}

// temporalityOf returns the aggregation temporality of a Sum, a Histogram or
// an ExponentialHistogram. A Gauge and a Summary have none.
func temporalityOf(metric pmetric.Metric) pmetric.AggregationTemporality {
	switch metric.Type() {
	case pmetric.MetricTypeSum:
		return metric.Sum().AggregationTemporality()
	case pmetric.MetricTypeHistogram:
		return metric.Histogram().AggregationTemporality()
	case pmetric.MetricTypeExponentialHistogram:
		return metric.ExponentialHistogram().AggregationTemporality()
	}
	return pmetric.AggregationTemporalityUnspecified
}
