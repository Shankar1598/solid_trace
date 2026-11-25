package ingest

import (
	"errors"
	"slices"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"

	"github.com/solidtrace/event_store/captured"
	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
)

// OTLPEncoding is how the body of an OTLP/HTTP request is encoded.
type OTLPEncoding int

const (
	// OTLPProtobuf is the binary protobuf encoding, application/x-protobuf.
	OTLPProtobuf OTLPEncoding = iota
	// OTLPJSON is the OTLP JSON encoding, application/json.
	OTLPJSON
)

// ErrInvalidOTLP is a body of the OTLP Log route that doesn't decode as an
// OTLP logs export request in its encoding.
var ErrInvalidOTLP = errors.New("request body is not an OTLP logs export request")

// ErrInvalidOTLPMetrics is a body of the OTLP metrics route that doesn't
// decode as an OTLP metrics export request in its encoding.
var ErrInvalidOTLPMetrics = errors.New("request body is not an OTLP metrics export request")

// otlpRecord is one OTLP log record, with the resource and the
// instrumentation scope it was sent under.
type otlpRecord struct {
	resource pcommon.Resource
	scope    pcommon.InstrumentationScope
	record   plog.LogRecord
}

// Attributes are the log record's own attributes.
func (r otlpRecord) Attributes() pcommon.Map {
	return r.record.Attributes()
}

// ownAttributed is an OTLP log record or metric data point, whose
// Attributes are its own: none of the resource or the scope.
type ownAttributed interface {
	Attributes() pcommon.Map
}

// IngestOTLPLogs ingests the body of the OTLP Log route: one OTLP logs export
// request in encoding. Each log record is one Log, as ingestOTLP says. A
// body that doesn't decode is ErrInvalidOTLP.
func (s *Service) IngestOTLPLogs(projectID uint32, body Body, encoding OTLPEncoding, filter *captured.Filter) (LogCounts, error) {
	request := plogotlp.NewExportRequest()
	records := func() []otlpRecord {
		records := make([]otlpRecord, 0, request.Logs().LogRecordCount())
		for _, resourceLogs := range request.Logs().ResourceLogs().All() {
			for _, scopeLogs := range resourceLogs.ScopeLogs().All() {
				for _, record := range scopeLogs.LogRecords().All() {
					records = append(records, otlpRecord{resourceLogs.Resource(), scopeLogs.Scope(), record})
				}
			}
		}
		return records
	}
	return s.ingestOTLP(projectID, body, encoding, filter, request, ErrInvalidOTLP, records, normaliseOTLP)
}

// IngestOTLPMetrics ingests the body of the OTLP metrics route: one OTLP
// metrics export request in encoding. Each metric data point is one Log
// (normalise.OTLPMetric), as ingestOTLP says, and counts as one Log. A body
// that doesn't decode is ErrInvalidOTLPMetrics.
func (s *Service) IngestOTLPMetrics(projectID uint32, body Body, encoding OTLPEncoding, filter *captured.Filter) (LogCounts, error) {
	request := pmetricotlp.NewExportRequest()
	points := func() []normalise.MetricPoint { return metricPoints(request) }
	return s.ingestOTLP(projectID, body, encoding, filter, request, ErrInvalidOTLPMetrics, points, normalise.OTLPMetric)
}

// metricPoints lists each metric data point of an OTLP metrics export
// request, in the order of its resources, scopes and metrics.
func metricPoints(request pmetricotlp.ExportRequest) []normalise.MetricPoint {
	points := make([]normalise.MetricPoint, 0, request.Metrics().DataPointCount())
	for _, resourceMetrics := range request.Metrics().ResourceMetrics().All() {
		for _, scopeMetrics := range resourceMetrics.ScopeMetrics().All() {
			for _, metric := range scopeMetrics.Metrics().All() {
				points = normalise.AppendMetricPoints(points, resourceMetrics.Resource(), scopeMetrics.Scope(), metric)
			}
		}
	}
	return points
}

// otlpRequest is an OTLP export request of one signal, which decodes from
// either OTLP encoding.
type otlpRequest interface {
	UnmarshalProto(data []byte) error
	UnmarshalJSON(data []byte) error
}

// ingestOTLP ingests the body of an OTLP route: it decodes the body into
// request, in encoding, and then each of the records that records lists is
// normalised by normaliseLog, against the receipt time and the Project's
// Retention, and stored as one Log batch, and Log processing is woken, as
// IngestLogs does. A body that doesn't decode is the error invalid, and
// nothing in it is stored.
//
// With a filter, the Agent Project's, a record is stored only when the
// filter keeps its own vcs.repository.url.full. The others are filtered
// out before they are normalised: they count as received and filtered, not
// as dropped, so the sender gets no rejection for them.
//
// Every request whose body is read adds to its Project's Ingest counts of
// its second of arrival, and so does one turned away as overloaded. A body
// that doesn't decode counts as one request with its bytes and no Logs.
func (s *Service) ingestOTLP[R ownAttributed](
	projectID uint32,
	body Body,
	encoding OTLPEncoding,
	filter *captured.Filter,
	request otlpRequest,
	invalid error,
	records func() []R,
	normaliseLog func(R, normalise.Receipt) (models.Log, normalise.Outcome),
) (LogCounts, error) {
	if !s.enterGate() {
		s.counts.count(projectID, s.clock.Now(), models.IngestCount{RequestsOverloaded: 1})
		return LogCounts{}, ErrOverloaded
	}
	defer s.leaveGate()

	raw, err := body.Read()
	if err != nil {
		return LogCounts{}, err
	}
	counted := models.IngestCount{BytesReceived: int64(len(raw)), BytesOnWire: int64(body.Wire)}
	unmarshal := request.UnmarshalProto
	if encoding == OTLPJSON {
		unmarshal = request.UnmarshalJSON
	}
	if unmarshal(raw) != nil {
		counted.Requests = 1
		s.counts.count(projectID, s.clock.Now(), counted)
		return LogCounts{}, invalid
	}
	sent := records()
	if filter != nil {
		received := len(sent)
		sent = slices.DeleteFunc(sent, func(r R) bool { return !filter.Keeps(repositoryURL(r.Attributes())) })
		counted.LogsFiltered = int64(received - len(sent))
	}
	return s.storeLogs(projectID, sent, 0, counted, normaliseLog)
}

// repositoryURL is the vcs.repository.url.full of a record's own
// attributes, which Claude Code sets with OTEL_METRICS_INCLUDE_REPOSITORY,
// or "". A sender can also set it on the resource, for every record, so the
// resource's is not read.
func repositoryURL(own pcommon.Map) string {
	value, ok := own.Get("vcs.repository.url.full")
	if !ok || value.Type() != pcommon.ValueTypeStr {
		return ""
	}
	return value.Str()
}

func normaliseOTLP(r otlpRecord, receipt normalise.Receipt) (models.Log, normalise.Outcome) {
	return normalise.OTLP(r.resource, r.scope, r.record, receipt)
}
