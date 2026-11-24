package handler

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

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

// storeOTLPLogs ingests an OTLP logs export request, in the encoding of its
// Content-Type, and answers with an OTLP export response in that encoding.
// Logs that normalising dropped are the response's rejected log records. An
// error is answered as on the JSON Log route.
func (h *IngestHandler) storeOTLPLogs(c *fiber.Ctx, projectID uint32, body ingest.Body) error {
	encoding, ok := otlpEncoding(mediaType(c))
	if !ok {
		return c.Status(fiber.StatusUnsupportedMediaType).JSON(fiber.Map{"error": "The Content-Type must be application/x-protobuf or application/json"})
	}
	counts, err := h.ingest.IngestOTLPLogs(projectID, body, encoding)
	if err != nil {
		return h.renderIngestError(c, err)
	}

	response := plogotlp.NewExportResponse()
	if counts.Dropped > 0 {
		partial := response.PartialSuccess()
		partial.SetRejectedLogRecords(int64(counts.Dropped))
		partial.SetErrorMessage(fmt.Sprintf("%d log records are over 1 MB, after the per-Log limits, and are not stored", counts.Dropped))
	}
	marshal := response.MarshalProto
	if encoding == ingest.OTLPJSON {
		marshal = response.MarshalJSON
	}
	answer, err := marshal()
	if err != nil {
		// The Logs are stored. A status that the sender retries stores them
		// again, so the answer is an empty export response.
		logger.L.Error("Marshal the OTLP export response failed", "error", err)
		answer = nil
		if encoding == ingest.OTLPJSON {
			answer = []byte("{}")
		}
	}
	c.Set(fiber.HeaderContentType, otlpContentTypes[encoding])
	return c.Send(answer)
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
