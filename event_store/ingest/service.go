package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
)

var (
	ErrInvalidEnvelope   = errors.New("invalid envelope format")
	ErrInvalidItemHeader = errors.New("invalid item header")
	ErrInvalidEventJSON  = errors.New("invalid event JSON")
	ErrOverloaded        = errors.New("server overloaded")
)

// Service owns Event ingest and Log ingest after authentication succeeds: it
// stores each Event, and each request's normalised Logs, in Pebble and nothing
// more. Event processing does the Issue work; Log processing moves Logs to
// ClickHouse.
type Service struct {
	events       EventWriter
	notifyEvents func(projectID uint32)
	logs         LogWriter
	notifyLogs   func(projectID uint32)
	transactions TransactionWriter
	notifySpans  func(projectID uint32)
	clock        clock.Clock
	retention    Retention

	// waiting counts requests waiting for their Event or Logs to be written.
	// Past maxWaiting a request is rejected as overloaded.
	waiting    atomic.Int64
	maxWaiting int64

	// counts is the Ingest counts not yet written (counts.go).
	counts *ingestCounts
}

// Config is what the Service writes to and whom it tells.
type Config struct {
	// Events and Logs are the seams to storage; storage.PebbleWriter is the
	// production adapter of both.
	Events EventWriter
	Logs   LogWriter
	// NotifyEvents and NotifyLogs are called with the Project of each write,
	// once it is stored, to wake Event processing and Log processing.
	NotifyEvents func(projectID uint32)
	NotifyLogs   func(projectID uint32)
	// Transactions stores envelopes' transaction items, and NotifySpans
	// wakes Span processing; with none, they are dropped.
	Transactions TransactionWriter
	NotifySpans  func(projectID uint32)
	// Clock gives the receipt time of Logs.
	Clock clock.Clock
	// Retention gives each Project's Retention, which decides how old a
	// Log's own time may be.
	Retention Retention
	// MaxWaiting caps the requests, Events and Logs together, waiting for
	// their write. Past it, ingest answers 503.
	MaxWaiting int
	// IngestCounts stores the Ingest counts of the Log routes' requests;
	// with none, they aren't kept.
	IngestCounts IngestCountsWriter
}

// NewService constructs Event ingest and Log ingest.
func NewService(cfg Config) *Service {
	return &Service{
		events:       cfg.Events,
		notifyEvents: cfg.NotifyEvents,
		logs:         cfg.Logs,
		notifyLogs:   cfg.NotifyLogs,
		transactions: cfg.Transactions,
		notifySpans:  cfg.NotifySpans,
		clock:        cfg.Clock,
		retention:    cfg.Retention,
		maxWaiting:   int64(cfg.MaxWaiting),
		counts:       &ingestCounts{writer: cfg.IngestCounts, seconds: map[projectSecond]*models.IngestCount{}},
	}
}

// Body is a request's body, read only once the request holds a waiting
// place, so that an overloaded ingest turns a request away before decoding
// its body.
type Body struct {
	// Wire is how many bytes the body took as sent, before decoding.
	Wire int
	read func() ([]byte, error)
}

// EncodedBody is a body of wire bytes as sent, which decode decodes. An
// error decode returns is returned as it is.
func EncodedBody(wire int, decode func() ([]byte, error)) Body {
	return Body{Wire: wire, read: decode}
}

// RawBody is a Body that needs no decoding.
func RawBody(body []byte) Body {
	return Body{Wire: len(body), read: func() ([]byte, error) { return body, nil }}
}

// Read decodes the body.
func (b Body) Read() ([]byte, error) {
	return b.read()
}

// IngestStore ingests a raw Event payload from the store endpoint.
func (s *Service) IngestStore(projectID uint32, body Body) error {
	if !s.enterGate() {
		return ErrOverloaded
	}
	defer s.leaveGate()

	rawJSON, err := body.Read()
	if err != nil {
		return err
	}
	if !json.Valid(rawJSON) {
		return ErrInvalidEventJSON
	}
	return s.writeEvent(projectID, rawJSON)
}

// IngestEnvelope ingests the first event item of an envelope, as the store
// endpoint does, the Logs of its log items, as one Log batch, and its
// transaction items, as one Transaction batch with the sample rate of the
// envelope's dynamic sampling context. It ignores every other item type.
//
// A log item, log record or transaction item that can't be read is dropped
// and the envelope still succeeds, as it does on Sentry. Once its Event is
// stored, Logs and Transactions that fail to store are dropped and logged,
// and the envelope succeeds too. Only a malformed envelope or event,
// overload, or a failure before anything is stored fails it.
func (s *Service) IngestEnvelope(projectID uint32, body Body) error {
	if !s.enterGate() {
		return ErrOverloaded
	}
	defer s.leaveGate()

	envelope, err := body.Read()
	if err != nil {
		return err
	}
	items, err := readEnvelope(envelope)
	if err != nil {
		return err
	}
	if items.hasEvent && !json.Valid(items.event) {
		return ErrInvalidEventJSON
	}

	if items.hasEvent {
		if err := s.writeEvent(projectID, items.event); err != nil {
			return err
		}
	}
	if len(items.logs) > 0 {
		records, dropped := sentryLogRecords(items.logs)
		// The envelope's Ingest counts are its log items': their payloads'
		// bytes, and that share of the bytes sent.
		received := 0
		for _, payload := range items.logs {
			received += len(payload)
		}
		request := models.IngestCount{BytesReceived: int64(received), BytesOnWire: int64(body.Wire * received / max(len(envelope), 1))}
		if _, err := s.storeLogs(projectID, records, dropped, request, normalise.Sentry); err != nil {
			if !items.hasEvent {
				return err
			}
			logger.L.Error("Dropping an envelope's Logs, as its Event is stored",
				"project_id", projectID, "logs", len(records), "error", err)
		}
	}
	if len(items.transactions) > 0 && s.transactions != nil {
		if err := s.storeTransactions(projectID, items); err != nil {
			if !items.hasEvent && len(items.logs) == 0 {
				return err
			}
			logger.L.Error("Dropping an envelope's Transactions, as its other items are stored",
				"project_id", projectID, "transactions", len(items.transactions), "error", err)
		}
	}
	return nil
}

// storeTransactions stores an envelope's transaction items that are JSON
// objects as one Transaction batch, and wakes Span processing. It adds their
// Spans and bytes to the Ingest counts of their second of arrival. The
// caller is in the gate.
func (s *Service) storeTransactions(projectID uint32, items envelopeItems) error {
	var transactions []models.Transaction
	var counted models.IngestCount
	for _, payload := range items.transactions {
		if object, ok := jsonObject(payload); ok {
			transactions = append(transactions, models.Transaction{SampleRate: items.sampleRate, Payload: bytes.Clone(payload)})
			counted.SpansReceived += 1 + spansIn(object)
			counted.SpanBytesReceived += int64(len(payload))
		}
	}
	if len(transactions) == 0 {
		return nil
	}
	s.counts.count(projectID, s.clock.Now(), counted)
	// A Pebble write is never retried: the SDK gets an error instead.
	if err := s.transactions.WriteTransactionBatch(projectID, transactions, s.clock.Now().UTC()); err != nil {
		return fmt.Errorf("write transaction batch: %w", err)
	}
	s.notifySpans(projectID)
	return nil
}

// spansIn is how many Spans a Transaction's spans array holds, 0 when it
// has none or isn't an array.
func spansIn(transaction map[string]json.RawMessage) int64 {
	var spans []json.RawMessage
	if json.Unmarshal(transaction["spans"], &spans) != nil {
		return 0
	}
	return int64(len(spans))
}

// envelopeItems are the payloads of the items of an envelope that ingest
// reads: its first event item, and every log and transaction item, with the
// sample rate of its dynamic sampling context.
type envelopeItems struct {
	hasEvent     bool
	event        []byte
	logs         [][]byte
	transactions [][]byte
	sampleRate   float64
}

// sampleRateOf reads the sample rate of an envelope's header: its dynamic
// sampling context's, a string as SDKs send it, or a number. A missing or
// unreadable rate, or one outside (0, 1], is 1.
func sampleRateOf(header map[string]json.RawMessage) float64 {
	var dsc struct {
		SampleRate json.RawMessage `json:"sample_rate"`
	}
	if json.Unmarshal(header["trace"], &dsc) != nil {
		return 1
	}
	raw := bytes.Trim(dsc.SampleRate, `"`)
	rate, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || !(rate > 0 && rate <= 1) {
		return 1
	}
	return rate
}

// readEnvelope walks the items of a Sentry envelope and returns the payloads
// of its first "event" item and of its "log" items. See
// https://develop.sentry.dev/sdk/data-model/envelopes/
//
//	Envelope = Headers { "\n" Item } [ "\n" ]
//	Item     = Headers "\n" Payload
//
// An item header with a "length" field owns exactly that many payload bytes,
// newlines included; without one, the payload runs to the next newline.
func readEnvelope(envelope []byte) (envelopeItems, error) {
	var items envelopeItems
	envelopeHeader, rest, _ := bytes.Cut(envelope, newline)
	var headers map[string]json.RawMessage
	if err := json.Unmarshal(envelopeHeader, &headers); err != nil {
		return items, ErrInvalidEnvelope
	}
	items.sampleRate = sampleRateOf(headers)

	for len(rest) > 0 {
		if rest[0] == '\n' {
			rest = rest[1:]
			continue
		}

		line, after, _ := bytes.Cut(rest, newline)
		var itemHeader struct {
			Type   string `json:"type"`
			Length *int   `json:"length"`
		}
		if err := json.Unmarshal(line, &itemHeader); err != nil || itemHeader.Type == "" {
			return items, ErrInvalidItemHeader
		}

		var payload []byte
		if itemHeader.Length != nil {
			length := *itemHeader.Length
			if length < 0 || length > len(after) {
				return items, ErrInvalidEnvelope
			}
			payload, after = after[:length], after[length:]
			if len(after) > 0 {
				if after[0] != '\n' {
					return items, ErrInvalidEnvelope
				}
				after = after[1:]
			}
		} else {
			payload, after, _ = bytes.Cut(after, newline)
		}
		rest = after

		switch {
		case itemHeader.Type == "event" && !items.hasEvent:
			items.hasEvent, items.event = true, payload
		case itemHeader.Type == "log":
			items.logs = append(items.logs, payload)
		case itemHeader.Type == "transaction":
			items.transactions = append(items.transactions, payload)
		}
	}

	return items, nil
}

var newline = []byte("\n")

// writeEvent stores a valid Event payload and wakes Event processing. The
// caller is in the gate.
func (s *Service) writeEvent(projectID uint32, rawJSON []byte) error {
	event := models.Event{
		ProjectID: projectID,
		RawJSON:   bytes.Clone(rawJSON), // rawJSON is fasthttp's request buffer, reused after the handler returns
	}

	// A Pebble write is never retried: the SDK gets an error instead.
	if err := s.events.WriteEvent(&event); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	s.notifyEvents(projectID)
	return nil
}

// enterGate counts a request as waiting for its write, unless maxWaiting are
// already waiting. A request that enters must call leaveGate.
func (s *Service) enterGate() bool {
	if s.waiting.Add(1) > s.maxWaiting {
		s.waiting.Add(-1)
		return false
	}
	return true
}

func (s *Service) leaveGate() {
	s.waiting.Add(-1)
}
