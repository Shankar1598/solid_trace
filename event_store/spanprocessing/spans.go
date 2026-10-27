package spanprocessing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/event_store/models"
)

// transactionPayload is the part of a Sentry transaction payload that Span
// processing reads. See
// https://develop.sentry.dev/sdk/data-model/event-payloads/transaction/
type transactionPayload struct {
	Transaction     string                  `json:"transaction"`
	TransactionInfo struct{ Source string } `json:"transaction_info"`
	StartTimestamp  json.RawMessage         `json:"start_timestamp"`
	Timestamp       json.RawMessage         `json:"timestamp"`
	Environment     string                  `json:"environment"`
	Release         string                  `json:"release"`
	ServerName      string                  `json:"server_name"`
	Tags            json.RawMessage         `json:"tags"`
	Contexts        struct {
		Trace spanPayload `json:"trace"`
	} `json:"contexts"`
	Spans []spanPayload `json:"spans"`
}

// spanPayload is a Span as SDKs send it, and the trace context, the
// segment's own Span.
type spanPayload struct {
	TraceID        string                     `json:"trace_id"`
	SpanID         string                     `json:"span_id"`
	ParentSpanID   string                     `json:"parent_span_id"`
	Op             string                     `json:"op"`
	Description    string                     `json:"description"`
	Status         string                     `json:"status"`
	StartTimestamp json.RawMessage            `json:"start_timestamp"`
	Timestamp      json.RawMessage            `json:"timestamp"`
	Tags           json.RawMessage            `json:"tags"`
	Data           map[string]json.RawMessage `json:"data"`
}

// rawTransactionAttribute holds the name an Unparameterised Transaction was
// sent with.
const rawTransactionAttribute = "transaction.raw"

// errUnreadable marks a Transaction that can't be stored for what it holds.
var errUnreadable = errors.New("transaction can't be read")

// Spans' times must lie in the days a partition can name, as Logs' must.
var (
	firstStorableTime = time.Unix(0, 0).UTC()
	endOfStorableTime = time.Date(2149, 6, 1, 0, 0, 0, 0, time.UTC)
)

// spansOf is a Transaction's rows: its segment, then each of its Spans, with
// each one's self time, group and repeats worked out over the Transaction,
// since all its Spans arrive together.
func spansOf(transaction models.Transaction) ([]models.Span, error) {
	var p transactionPayload
	if err := json.Unmarshal(transaction.Payload, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", errUnreadable, err)
	}
	trace := p.Contexts.Trace
	if trace.TraceID == "" || trace.SpanID == "" {
		return nil, fmt.Errorf("%w: it has no trace context", errUnreadable)
	}
	trace.StartTimestamp, trace.Timestamp = p.StartTimestamp, p.Timestamp
	trace.Description, trace.Tags = p.Transaction, p.Tags

	name := p.Transaction
	if p.TransactionInfo.Source == "" || p.TransactionInfo.Source == "url" {
		name = models.UnparameterisedTransaction
	}
	segment, err := spanRow(trace)
	if err != nil {
		return nil, err
	}
	segment.IsSegment = true
	if name != p.Transaction {
		segment.Attributes[rawTransactionAttribute] = p.Transaction
	}
	spans := []models.Span{segment}
	for _, s := range p.Spans {
		span, err := spanRow(s)
		if err != nil {
			return nil, err
		}
		span.TraceID = segment.TraceID
		spans = append(spans, span)
	}

	groups := map[string]uint32{}
	for i := range spans {
		s := &spans[i]
		s.SegmentID = segment.SpanID
		s.Transaction = name
		s.SampleRate = transaction.SampleRate
		s.Environment, s.Release, s.ServerName = p.Environment, p.Release, p.ServerName
		s.Group = group(s.Op, s.Description)
		if !s.IsSegment {
			groups[s.Group]++
		}
	}
	for i := range spans {
		spans[i].Repeats = 1
		if !spans[i].IsSegment {
			spans[i].Repeats = groups[spans[i].Group]
		}
	}
	setSelfTimes(spans)
	return spans, nil
}

// spanRow is a Span's row before what its Transaction gives it.
func spanRow(s spanPayload) (models.Span, error) {
	start, err := timestamp(s.StartTimestamp)
	if err != nil {
		return models.Span{}, fmt.Errorf("%w: span %s's start: %v", errUnreadable, s.SpanID, err)
	}
	end, err := timestamp(s.Timestamp)
	if err != nil {
		return models.Span{}, fmt.Errorf("%w: span %s's end: %v", errUnreadable, s.SpanID, err)
	}
	if start.Before(firstStorableTime) || !start.Before(endOfStorableTime) {
		return models.Span{}, fmt.Errorf("%w: span %s starts at %s", errUnreadable, s.SpanID, start)
	}
	attributes := map[string]string{}
	for key, value := range s.Data {
		attributes[key] = attributeValue(value)
	}
	var tags map[string]json.RawMessage
	if json.Unmarshal(s.Tags, &tags) == nil {
		for key, value := range tags {
			attributes[key] = attributeValue(value)
		}
	}
	return models.Span{
		TraceID:      strings.ToLower(s.TraceID),
		SpanID:       strings.ToLower(s.SpanID),
		ParentSpanID: strings.ToLower(s.ParentSpanID),
		Op:           s.Op,
		Description:  s.Description,
		Status:       s.Status,
		Start:        start,
		Duration:     max(end.Sub(start), 0),
		Attributes:   attributes,
	}, nil
}

// timestamp reads a time as SDKs send it: a number of Unix seconds, as
// sentry-ruby does, or an RFC 3339 string, as sentry-go does. Seconds are
// read to the microsecond.
func timestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return time.Time{}, err
		}
		at, err := time.Parse(time.RFC3339Nano, s)
		return at.UTC().Truncate(time.Microsecond), err
	}
	seconds, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMicro(int64(math.Round(seconds * 1e6))).UTC(), nil
}

// attributeValue is a tag's or data's value as an Attribute: a string as it
// is, anything else as its JSON.
func attributeValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(bytes.TrimSpace(raw))
}

// setSelfTimes sets each Span's self time: its duration less the time its
// children cover within it. A Span whose parent isn't in its Transaction is
// the segment's child.
func setSelfTimes(spans []models.Span) {
	index := make(map[string]int, len(spans))
	for i, s := range spans {
		index[s.SpanID] = i
	}
	children := make([][]int, len(spans))
	for i, s := range spans {
		if s.IsSegment {
			continue
		}
		parent, ok := index[s.ParentSpanID]
		if !ok || parent == i {
			parent = 0
		}
		children[parent] = append(children[parent], i)
	}
	for i := range spans {
		spans[i].SelfTime = spans[i].Duration - covered(spans, i, children[i])
	}
}

// covered is how much of a Span's time its children cover, overlaps counted
// once.
func covered(spans []models.Span, parent int, children []int) time.Duration {
	start, end := spans[parent].Start, spans[parent].Start.Add(spans[parent].Duration)
	type interval struct{ from, to time.Time }
	var intervals []interval
	for _, c := range children {
		from, to := spans[c].Start, spans[c].Start.Add(spans[c].Duration)
		if from.Before(start) {
			from = start
		}
		if to.After(end) {
			to = end
		}
		if from.Before(to) {
			intervals = append(intervals, interval{from, to})
		}
	}
	slices.SortFunc(intervals, func(a, b interval) int { return a.from.Compare(b.from) })
	var total time.Duration
	var reached time.Time
	for _, iv := range intervals {
		if iv.from.Before(reached) {
			iv.from = reached
		}
		if iv.from.Before(iv.to) {
			total += iv.to.Sub(iv.from)
			reached = iv.to
		}
	}
	return total
}

// Literals a description's group ignores, in the order they are replaced.
var (
	quotedLiteral = regexp.MustCompile(`'(?:[^']|'')*'`)
	uuidLiteral   = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	hexLiteral    = regexp.MustCompile(`(?i)\b(?:0x[0-9a-f]+|[0-9a-f]*[0-9][0-9a-f]*)\b`)
	numberLiteral = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	inList        = regexp.MustCompile(`(?i)\bIN\s*\(\s*\?(?:\s*,\s*\?)*\s*\)`)
)

// normalised is a description with its literals replaced by ?: quoted
// strings, UUIDs, hex ids, numbers, and IN lists of them.
func normalised(description string) string {
	d := quotedLiteral.ReplaceAllString(description, "?")
	d = uuidLiteral.ReplaceAllString(d, "?")
	d = hexLiteral.ReplaceAllStringFunc(d, func(s string) string {
		if strings.HasPrefix(strings.ToLower(s), "0x") || len(s) >= 8 {
			return "?"
		}
		return s
	})
	d = numberLiteral.ReplaceAllString(d, "?")
	return inList.ReplaceAllString(d, "IN (?)")
}

// group is the group of a Span's op and description: 16 hex digits.
func group(op, description string) string {
	h := fnv.New64a()
	h.Write([]byte(op))
	h.Write([]byte{0})
	h.Write([]byte(normalised(description)))
	return fmt.Sprintf("%016x", h.Sum64())
}
