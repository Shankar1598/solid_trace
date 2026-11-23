package ingest

import (
	"errors"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

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

// ErrInvalidOTLP is a body that doesn't decode as an OTLP export request in
// its encoding.
var ErrInvalidOTLP = errors.New("request body is not an OTLP logs export request")

// otlpRecord is one OTLP log record, with the resource and the
// instrumentation scope it was sent under.
type otlpRecord struct {
	resource pcommon.Resource
	scope    pcommon.InstrumentationScope
	record   plog.LogRecord
}

// IngestOTLPLogs ingests the body of the OTLP Log route: one OTLP logs export
// request in encoding. Each log record is one Log. They are normalised,
// against the receipt time and the Project's Retention, and stored as one
// Log batch, and Log processing is woken, as IngestLogs does. A body that
// doesn't decode is ErrInvalidOTLP, and nothing in it is stored.
//
// Every request whose body is read adds to its Project's Ingest counts of
// its second of arrival, and so does one turned away as overloaded. A body
// that doesn't decode counts as one request with its bytes and no Logs.
func (s *Service) IngestOTLPLogs(projectID uint32, body Body, encoding OTLPEncoding) (LogCounts, error) {
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
	request := plogotlp.NewExportRequest()
	unmarshal := request.UnmarshalProto
	if encoding == OTLPJSON {
		unmarshal = request.UnmarshalJSON
	}
	if unmarshal(raw) != nil {
		counted.Requests = 1
		s.counts.count(projectID, s.clock.Now(), counted)
		return LogCounts{}, ErrInvalidOTLP
	}

	records := make([]otlpRecord, 0, request.Logs().LogRecordCount())
	for _, resourceLogs := range request.Logs().ResourceLogs().All() {
		for _, scopeLogs := range resourceLogs.ScopeLogs().All() {
			for _, record := range scopeLogs.LogRecords().All() {
				records = append(records, otlpRecord{resourceLogs.Resource(), scopeLogs.Scope(), record})
			}
		}
	}
	return s.storeLogs(projectID, records, 0, counted, normaliseOTLP)
}

func normaliseOTLP(r otlpRecord, receipt normalise.Receipt) (models.Log, normalise.Outcome) {
	return normalise.OTLP(r.resource, r.scope, r.record, receipt)
}
