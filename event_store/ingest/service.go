package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
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
// DuckDB.
type Service struct {
	events       EventWriter
	notifyEvents func(projectID uint32)
	logs         LogWriter
	notifyLogs   func(projectID uint32)
	clock        clock.Clock
	retention    Retention

	// waiting counts requests waiting for their Event or Logs to be written.
	// Past maxWaiting a request is rejected as overloaded.
	waiting    atomic.Int64
	maxWaiting int64
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
	// Clock gives the receipt time of Logs.
	Clock clock.Clock
	// Retention gives each Project's Retention, which decides how old a
	// Log's own time may be.
	Retention Retention
	// MaxWaiting caps the requests, Events and Logs together, waiting for
	// their write. Past it, ingest answers 503.
	MaxWaiting int
}

// NewService constructs Event ingest and Log ingest.
func NewService(cfg Config) *Service {
	return &Service{
		events:       cfg.Events,
		notifyEvents: cfg.NotifyEvents,
		logs:         cfg.Logs,
		notifyLogs:   cfg.NotifyLogs,
		clock:        cfg.Clock,
		retention:    cfg.Retention,
		maxWaiting:   int64(cfg.MaxWaiting),
	}
}

// IngestStore ingests a raw Event payload from the store endpoint.
func (s *Service) IngestStore(projectID uint32, rawEventJSON []byte) error {
	return s.ingestEvent(projectID, rawEventJSON)
}

// IngestEnvelope ingests the first event item of an envelope, as the store
// endpoint does, and the Logs of its log items, as one Log batch. It ignores
// every other item type, transactions included.
//
// A log item or log record that can't be read is dropped and the envelope
// still succeeds, as it does on Sentry. Only a malformed envelope or event
// fails it.
func (s *Service) IngestEnvelope(projectID uint32, envelope []byte) error {
	items, err := readEnvelope(envelope)
	if err != nil {
		return err
	}
	if items.event != nil && !json.Valid(items.event) {
		return ErrInvalidEventJSON
	}
	if items.event == nil && len(items.logs) == 0 {
		return nil
	}

	if !s.enterGate() {
		return ErrOverloaded
	}
	defer s.leaveGate()

	if items.event != nil {
		if err := s.writeEvent(projectID, items.event); err != nil {
			return err
		}
	}
	if len(items.logs) > 0 {
		records, dropped := sentryLogRecords(items.logs)
		if _, err := s.storeLogs(projectID, records, dropped, normalise.Sentry); err != nil {
			return err
		}
	}
	return nil
}

// envelopeItems are the payloads of the items of an envelope that ingest
// reads: its first event item, and every log item.
type envelopeItems struct {
	event []byte
	logs  [][]byte
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
	var headers map[string]interface{}
	if err := json.Unmarshal(envelopeHeader, &headers); err != nil {
		return items, ErrInvalidEnvelope
	}

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
		case itemHeader.Type == "event" && items.event == nil:
			items.event = payload
		case itemHeader.Type == "log":
			items.logs = append(items.logs, payload)
		}
	}

	return items, nil
}

var newline = []byte("\n")

func (s *Service) ingestEvent(projectID uint32, rawJSON []byte) error {
	if !json.Valid(rawJSON) {
		return ErrInvalidEventJSON
	}

	if !s.enterGate() {
		return ErrOverloaded
	}
	defer s.leaveGate()

	return s.writeEvent(projectID, rawJSON)
}

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
