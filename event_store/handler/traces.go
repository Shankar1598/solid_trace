package handler

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// TraceReader reads a trace's Spans. *storage.ClickHouse is the production
// adapter.
type TraceReader interface {
	Trace(ctx context.Context, projectIDs []uint32, traceID string) ([]storage.TracedSpan, error)
}

// TracesHandler answers the Query API's trace route. As with the Overview,
// Console sends the Projects a trace may be in, its Organization's, and
// EventStore never reads Organizations.
type TracesHandler struct {
	spans TraceReader
}

func NewTracesHandler(spans TraceReader) *TracesHandler {
	return &TracesHandler{spans: spans}
}

// traceTimeout bounds a trace read.
const traceTimeout = 30 * time.Second

// traceID is a trace id as Sentry SDKs send it: 32 hex digits.
var traceID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type traceRequest struct {
	ProjectIDs []uint32 `json:"project_ids"`
	TraceID    string   `json:"trace_id"`
}

type (
	traceAnswer struct {
		TraceID string            `json:"trace_id"`
		Spans   []traceSpanAnswer `json:"spans"`
	}
	traceSpanAnswer struct {
		ProjectID    uint32            `json:"project_id"`
		SpanID       string            `json:"span_id"`
		ParentSpanID string            `json:"parent_span_id"`
		SegmentID    string            `json:"segment_id"`
		IsSegment    bool              `json:"is_segment"`
		Transaction  string            `json:"transaction"`
		Op           string            `json:"op"`
		Description  string            `json:"description"`
		Group        string            `json:"group"`
		Repeats      uint32            `json:"repeats"`
		Status       string            `json:"status"`
		Start        time.Time         `json:"start"`
		DurationUs   int64             `json:"duration_us"`
		SelfTimeUs   int64             `json:"self_time_us"`
		SampleRate   float64           `json:"sample_rate"`
		Environment  string            `json:"environment"`
		Release      string            `json:"release"`
		ServerName   string            `json:"server_name"`
		Attributes   map[string]string `json:"attributes"`
	}
)

// Trace answers POST /trace: a trace's Spans among the given Projects', in
// start order. A trace with none answers no Spans.
func (h *TracesHandler) Trace(c *fiber.Ctx) error {
	var req traceRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON"})
	}
	req.TraceID = strings.ToLower(req.TraceID)
	if len(req.ProjectIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "project_ids must name at least one Project"})
	}
	if !traceID.MatchString(req.TraceID) {
		return c.Status(400).JSON(fiber.Map{"error": "trace_id must be 32 hex digits"})
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), traceTimeout)
	defer cancel()
	spans, err := h.spans.Trace(ctx, req.ProjectIDs, req.TraceID)
	if err != nil {
		logger.L.Error("Reading a trace failed", "trace_id", req.TraceID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	answer := traceAnswer{TraceID: req.TraceID, Spans: make([]traceSpanAnswer, len(spans))}
	for i, s := range spans {
		answer.Spans[i] = traceSpanAnswer{
			ProjectID: s.ProjectID, SpanID: s.SpanID, ParentSpanID: s.ParentSpanID, SegmentID: s.SegmentID,
			IsSegment: s.IsSegment, Transaction: s.Transaction, Op: s.Op, Description: s.Description,
			Group: s.Group, Repeats: s.Repeats, Status: s.Status, Start: s.Start,
			DurationUs: s.Duration.Microseconds(), SelfTimeUs: s.SelfTime.Microseconds(),
			SampleRate: s.SampleRate, Environment: s.Environment, Release: s.Release, ServerName: s.ServerName,
			Attributes: s.Attributes,
		}
	}
	return c.JSON(answer)
}
