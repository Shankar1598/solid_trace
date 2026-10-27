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

// TraceReader reads a trace's Spans, Logs and Events. *storage.ClickHouse is
// the production adapter.
type TraceReader interface {
	Trace(ctx context.Context, projectIDs []uint32, traceID string) ([]storage.TracedSpan, error)
	TraceLogs(ctx context.Context, projectIDs []uint32, traceID string, from, to time.Time, limit int) ([]storage.TraceLog, error)
	TraceEvents(ctx context.Context, projectIDs []uint32, traceID string, from, to time.Time, limit int) ([]storage.TracedEvent, error)
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

const (
	// traceTimeout bounds a trace read.
	traceTimeout = 30 * time.Second
	// traceLogsMargin is how far before a trace's first Span, or after its
	// last, its Logs are read, or either side of its time hint when it has
	// no Spans.
	traceLogsMargin = 5 * time.Minute
	// traceLogsLimit and traceEventsLimit are the most Logs and Events a
	// trace answers.
	traceLogsLimit   = 500
	traceEventsLimit = 200
)

// traceID is a trace id as Sentry SDKs send it: 32 hex digits.
var traceID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type traceRequest struct {
	ProjectIDs []uint32 `json:"project_ids"`
	TraceID    string   `json:"trace_id"`
	// Time is when the trace happened, such as its Event's or Log's time,
	// for a trace with no Spans.
	Time *time.Time `json:"time"`
}

type (
	traceAnswer struct {
		TraceID string            `json:"trace_id"`
		Spans   []traceSpanAnswer `json:"spans"`
		// Logs are the trace's, read from LogsFrom to LogsTo: nil when the
		// trace's time isn't known. MoreLogs says there are more than
		// answered.
		Logs     []storage.TraceLog `json:"logs"`
		MoreLogs bool               `json:"more_logs"`
		LogsFrom *time.Time         `json:"logs_from"`
		LogsTo   *time.Time         `json:"logs_to"`
		Issues   []traceIssueAnswer `json:"issues"`
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
	// traceIssueAnswer is an Issue's Events in the trace, by the Issue's
	// fingerprint, which Console knows the Issue of.
	traceIssueAnswer struct {
		ProjectID          uint32             `json:"project_id"`
		IssueFingerprintID int64              `json:"issue_fingerprint_id"`
		Events             []traceEventAnswer `json:"events"`
	}
	traceEventAnswer struct {
		UUID      string    `json:"uuid"`
		Timestamp time.Time `json:"timestamp"`
	}
)

// Trace answers POST /trace: a trace's Spans among the given Projects', in
// start order; its Logs among theirs, in time order; and its Events by
// Issue, the Issue with the earliest first.
//
// Its Logs are read from 5 minutes before its first Span to 5 minutes after
// its last. A trace with no Spans, one its SDK didn't sample, has its Logs
// read 5 minutes either side of the time given, or else of its first
// Event; with neither, it answers no Logs.
func (h *TracesHandler) Trace(c *fiber.Ctx) error {
	var req traceRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON, its time an RFC 3339 time"})
	}
	req.TraceID = strings.ToLower(req.TraceID)
	if len(req.ProjectIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "project_ids must name at least one Project"})
	}
	if !traceID.MatchString(req.TraceID) {
		return c.Status(400).JSON(fiber.Map{"error": "trace_id must be 32 hex digits"})
	}

	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), req.ProjectIDs[0], storage.ReadTrace), traceTimeout)
	defer cancel()
	answer, err := h.read(ctx, req)
	if err != nil {
		logger.L.Error("Reading a trace failed", "trace_id", req.TraceID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	return c.JSON(answer)
}

func (h *TracesHandler) read(ctx context.Context, req traceRequest) (traceAnswer, error) {
	answer := traceAnswer{TraceID: req.TraceID, Spans: []traceSpanAnswer{}, Logs: []storage.TraceLog{}, Issues: []traceIssueAnswer{}}
	spans, err := h.spans.Trace(ctx, req.ProjectIDs, req.TraceID)
	if err != nil {
		return answer, err
	}
	var first, last time.Time
	for _, s := range spans {
		answer.Spans = append(answer.Spans, spanAnswer(s))
		if first.IsZero() || s.Start.Before(first) {
			first = s.Start
		}
		if end := s.Start.Add(s.Duration); end.After(last) {
			last = end
		}
	}
	if first.IsZero() && req.Time != nil {
		first, last = req.Time.UTC(), req.Time.UTC()
	}

	// Its Events within the Logs' times, or of any time when they aren't
	// known: an Event's trace id is indexed, and Events are few.
	var from, to time.Time
	if !first.IsZero() {
		from, to = first.Add(-traceLogsMargin), last.Add(traceLogsMargin)
	}
	events, err := h.spans.TraceEvents(ctx, req.ProjectIDs, req.TraceID, from, to, traceEventsLimit)
	if err != nil {
		return answer, err
	}
	answer.Issues = byIssue(events)
	if from.IsZero() {
		if len(events) == 0 {
			return answer, nil
		}
		from, to = events[0].Timestamp.Add(-traceLogsMargin), events[0].Timestamp.Add(traceLogsMargin)
	}

	logs, err := h.spans.TraceLogs(ctx, req.ProjectIDs, req.TraceID, from, to, traceLogsLimit+1)
	if err != nil {
		return answer, err
	}
	if len(logs) > traceLogsLimit {
		logs, answer.MoreLogs = logs[:traceLogsLimit], true
	}
	if logs != nil {
		answer.Logs = logs
	}
	answer.LogsFrom, answer.LogsTo = &from, &to
	return answer, nil
}

// byIssue groups Events in time order by Issue, in the order of each
// Issue's first.
func byIssue(events []storage.TracedEvent) []traceIssueAnswer {
	issues := []traceIssueAnswer{}
	at := map[int64]int{}
	for _, e := range events {
		i, ok := at[e.IssueFingerprintID]
		if !ok {
			i = len(issues)
			at[e.IssueFingerprintID] = i
			issues = append(issues, traceIssueAnswer{ProjectID: e.ProjectID, IssueFingerprintID: e.IssueFingerprintID})
		}
		issues[i].Events = append(issues[i].Events, traceEventAnswer{UUID: e.UUID, Timestamp: e.Timestamp})
	}
	return issues
}

func spanAnswer(s storage.TracedSpan) traceSpanAnswer {
	return traceSpanAnswer{
		ProjectID: s.ProjectID, SpanID: s.SpanID, ParentSpanID: s.ParentSpanID, SegmentID: s.SegmentID,
		IsSegment: s.IsSegment, Transaction: s.Transaction, Op: s.Op, Description: s.Description,
		Group: s.Group, Repeats: s.Repeats, Status: s.Status, Start: s.Start,
		DurationUs: s.Duration.Microseconds(), SelfTimeUs: s.SelfTime.Microseconds(),
		SampleRate: s.SampleRate, Environment: s.Environment, Release: s.Release, ServerName: s.ServerName,
		Attributes: s.Attributes,
	}
}
