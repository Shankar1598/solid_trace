package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/solidtrace/event_store/models"
)

var (
	ErrInvalidEnvelope   = errors.New("invalid envelope format")
	ErrInvalidItemHeader = errors.New("invalid item header")
	ErrInvalidEventJSON  = errors.New("invalid event JSON")
	ErrOverloaded        = errors.New("server overloaded")
)

// Service owns Event ingest after authentication succeeds: it stores each
// Event in Pebble and nothing more. Event processing does the Issue work.
type Service struct {
	events EventWriter
	notify func(projectID uint32)

	// waiting counts requests waiting for their Event to be written. Past
	// maxWaiting a request is rejected as overloaded.
	waiting    atomic.Int64
	maxWaiting int64
}

// NewService constructs the Event ingest module. The EventWriter is the seam
// to Event storage; storage.PebbleWriter is the production adapter. notify is
// called with the Project of each Event once it is written, to wake Event
// processing.
func NewService(events EventWriter, notify func(projectID uint32), maxWaiting int) *Service {
	return &Service{
		events:     events,
		notify:     notify,
		maxWaiting: int64(maxWaiting),
	}
}

// IngestStore ingests a raw Event payload from the store endpoint.
func (s *Service) IngestStore(projectID uint32, rawEventJSON []byte) error {
	return s.ingestEvent(projectID, rawEventJSON)
}

// IngestEnvelope ingests the event item of an envelope and ignores every
// other item type, transactions included.
func (s *Service) IngestEnvelope(projectID uint32, envelope []byte) error {
	payload, found, err := eventItem(envelope)
	if err != nil || !found {
		return err
	}
	return s.ingestEvent(projectID, payload)
}

// eventItem walks the items of a Sentry envelope and returns the payload of
// the first "event" item. See https://develop.sentry.dev/sdk/data-model/envelopes/
//
//	Envelope = Headers { "\n" Item } [ "\n" ]
//	Item     = Headers "\n" Payload
//
// An item header with a "length" field owns exactly that many payload bytes,
// newlines included; without one, the payload runs to the next newline.
func eventItem(envelope []byte) ([]byte, bool, error) {
	envelopeHeader, rest, _ := bytes.Cut(envelope, newline)
	var headers map[string]interface{}
	if err := json.Unmarshal(envelopeHeader, &headers); err != nil {
		return nil, false, ErrInvalidEnvelope
	}

	var event []byte
	found := false
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
			return nil, false, ErrInvalidItemHeader
		}

		var payload []byte
		if itemHeader.Length != nil {
			length := *itemHeader.Length
			if length < 0 || length > len(after) {
				return nil, false, ErrInvalidEnvelope
			}
			payload, after = after[:length], after[length:]
			if len(after) > 0 {
				if after[0] != '\n' {
					return nil, false, ErrInvalidEnvelope
				}
				after = after[1:]
			}
		} else {
			payload, after, _ = bytes.Cut(after, newline)
		}
		rest = after

		if itemHeader.Type == "event" && !found {
			event, found = payload, true
		}
	}

	return event, found, nil
}

var newline = []byte("\n")

func (s *Service) ingestEvent(projectID uint32, rawJSON []byte) error {
	if !json.Valid(rawJSON) {
		return ErrInvalidEventJSON
	}

	if s.waiting.Add(1) > s.maxWaiting {
		s.waiting.Add(-1)
		return ErrOverloaded
	}
	defer s.waiting.Add(-1)

	event := models.Event{
		ProjectID: projectID,
		RawJSON:   bytes.Clone(rawJSON), // rawJSON is fasthttp's request buffer, reused after the handler returns
	}

	// A Pebble write is never retried: the SDK gets an error instead.
	if err := s.events.WriteEvent(&event); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	s.notify(projectID)
	return nil
}
