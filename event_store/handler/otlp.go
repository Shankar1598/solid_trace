package handler

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"

	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/pkg/logger"
)

// otlpContentTypes are the Content-Types of the OTLP/HTTP encodings.
var otlpContentTypes = map[ingest.OTLPEncoding]string{
	ingest.OTLPProtobuf: "application/x-protobuf",
	ingest.OTLPJSON:     "application/json",
}

// POST /v1/logs
// The OTLP/HTTP Log route, at the path an OpenTelemetry Collector or SDK
// adds to a bare endpoint. The Project is the one the key belongs to. The
// key is sent as on the JSON Log route.
func (h *IngestHandler) OTLPLogs(c *fiber.Ctx) error {
	return h.ingestLogs(c, anyProject, h.storeOTLPLogs)
}

// POST /v1/metrics
// The OTLP/HTTP metrics route, as the OTLP Log route is. Each metric data
// point is stored as one Log.
func (h *IngestHandler) OTLPMetrics(c *fiber.Ctx) error {
	return h.ingestLogs(c, anyProject, h.storeOTLPMetrics)
}

// storeOTLPLogs ingests an OTLP logs export request, as storeOTLP says. Logs
// that normalising dropped are the response's rejected log records.
func (h *IngestHandler) storeOTLPLogs(c *fiber.Ctx, projectID uint32, body ingest.Body) error {
	return h.storeOTLP(c, projectID, body, h.ingest.IngestOTLPLogs, func(dropped int) otlpResponse {
		response := plogotlp.NewExportResponse()
		if dropped > 0 {
			partial := response.PartialSuccess()
			partial.SetRejectedLogRecords(int64(dropped))
			partial.SetErrorMessage(fmt.Sprintf("%d log records are over 1 MB, after the per-Log limits, and are not stored", dropped))
		}
		return response
	})
}

// storeOTLPMetrics ingests an OTLP metrics export request, as storeOTLP says.
// Data points whose Logs normalising dropped are the response's rejected data
// points.
func (h *IngestHandler) storeOTLPMetrics(c *fiber.Ctx, projectID uint32, body ingest.Body) error {
	return h.storeOTLP(c, projectID, body, h.ingest.IngestOTLPMetrics, func(dropped int) otlpResponse {
		response := pmetricotlp.NewExportResponse()
		if dropped > 0 {
			partial := response.PartialSuccess()
			partial.SetRejectedDataPoints(int64(dropped))
			partial.SetErrorMessage(fmt.Sprintf("%d data points are over 1 MB as Logs, after the per-Log limits, and are not stored", dropped))
		}
		return response
	})
}

// otlpResponse is an OTLP export response of one signal, which encodes to
// either OTLP encoding.
type otlpResponse interface {
	MarshalProto() ([]byte, error)
	MarshalJSON() ([]byte, error)
}

// storeOTLP ingests the body of an OTLP route with ingestOTLP, in the
// encoding of its Content-Type, and answers with the export response that
// response makes for the dropped Logs, in that encoding. An error is
// answered as on the JSON Log route.
func (h *IngestHandler) storeOTLP(
	c *fiber.Ctx,
	projectID uint32,
	body ingest.Body,
	ingestOTLP func(projectID uint32, body ingest.Body, encoding ingest.OTLPEncoding) (ingest.LogCounts, error),
	response func(dropped int) otlpResponse,
) error {
	encoding, ok := otlpEncoding(mediaType(c))
	if !ok {
		return c.Status(fiber.StatusUnsupportedMediaType).JSON(fiber.Map{"error": "The Content-Type must be application/x-protobuf or application/json"})
	}
	counts, err := ingestOTLP(projectID, body, encoding)
	if err != nil {
		return h.renderIngestError(c, err)
	}

	answer := response(counts.Dropped)
	marshal := answer.MarshalProto
	if encoding == ingest.OTLPJSON {
		marshal = answer.MarshalJSON
	}
	encoded, err := marshal()
	if err != nil {
		// The Logs are stored. A status that the sender retries stores them
		// again, so the answer is an empty export response.
		logger.L.Error("Marshal the OTLP export response failed", "error", err)
		encoded = nil
		if encoding == ingest.OTLPJSON {
			encoded = []byte("{}")
		}
	}
	c.Set(fiber.HeaderContentType, otlpContentTypes[encoding])
	return c.Send(encoded)
}

// otlpEncoding returns the OTLP encoding of a media type, if it is one.
func otlpEncoding(mediaType string) (ingest.OTLPEncoding, bool) {
	for encoding, contentType := range otlpContentTypes {
		if strings.EqualFold(mediaType, contentType) {
			return encoding, true
		}
	}
	return 0, false
}
