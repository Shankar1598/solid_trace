package models

import "time"

// Transaction is one transaction item of an envelope as received: the SDK's
// payload, with the sample rate of the envelope's dynamic sampling context.
//
// The msgpack tags are the format of Transaction batches in Pebble. A batch
// can outlive the process that wrote it, so don't rename them.
type Transaction struct {
	// SampleRate is the share of requests the SDK sampled, in (0, 1]: 1
	// when the envelope gave none, or none that could be read.
	SampleRate float64 `msgpack:"r"`
	Payload    []byte  `msgpack:"p"`
}

// UnparameterisedTransaction is the name of every Transaction named by its
// raw URL, or by nothing, so that /orders/123 doesn't make a name of each
// request.
const UnparameterisedTransaction = "<unparameterised>"

// Span is one timed operation of a Transaction as stored: its segment, the
// Transaction's own, or one of its Spans.
type Span struct {
	TraceID      string
	SpanID       string
	ParentSpanID string // "" for a trace's first
	// SegmentID is the span id of its Transaction's segment.
	SegmentID string
	IsSegment bool
	// Transaction is its Transaction's name, UnparameterisedTransaction
	// for a raw URL.
	Transaction string
	Op          string
	// Description is what it did; a segment's is its Transaction's name
	// as sent.
	Description string
	// Group is a hash of its op and its description with literals
	// replaced, so that the same query with other values is one group.
	Group string
	// Repeats is how many Spans of its Transaction are in its group: 1
	// for a segment.
	Repeats uint32
	// Status is as the SDK sent it, "" for none.
	Status     string
	Start      time.Time
	Duration   time.Duration
	SelfTime   time.Duration
	SampleRate float64

	Environment string
	Release     string
	ServerName  string
	// Attributes are the SDK's tags and data, data as JSON unless a
	// string.
	Attributes map[string]string
}
