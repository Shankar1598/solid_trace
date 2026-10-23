package models

import "time"

// IngestCount is the Ingest counts of one Project's second of arrival: what
// the requests to its Log routes that arrived in that second held.
type IngestCount struct {
	ProjectID uint32
	// Second is the second of arrival, in UTC, by ingest's clock.
	Second time.Time
	// LogsReceived counts every element or line the requests held, Logs or
	// not; LogsUnreadable those that weren't Logs, and the Logs normalising
	// dropped.
	LogsReceived   int64
	LogsUnreadable int64
	// BytesReceived is the requests' bodies once decoded, and BytesOnWire
	// as sent.
	BytesReceived int64
	BytesOnWire   int64
	// Requests counts the requests whose bodies were read, and
	// RequestsOverloaded those turned away as overloaded before.
	Requests           int64
	RequestsOverloaded int64
}

// Add adds o's counts to c's.
func (c *IngestCount) Add(o IngestCount) {
	c.LogsReceived += o.LogsReceived
	c.LogsUnreadable += o.LogsUnreadable
	c.BytesReceived += o.BytesReceived
	c.BytesOnWire += o.BytesOnWire
	c.Requests += o.Requests
	c.RequestsOverloaded += o.RequestsOverloaded
}
