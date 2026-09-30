package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

var (
	ErrInvalidEnvelope   = errors.New("invalid envelope format")
	ErrInvalidItemHeader = errors.New("invalid item header")
	ErrInvalidEventJSON  = errors.New("invalid event JSON")
	ErrOverloaded        = errors.New("server overloaded")
)

// Service owns Event ingest after authentication succeeds.
// It depends on an IssueRepository (defined in repository.go) for
// Issue persistence, keeping the domain logic — classification,
// find-or-create, reopen-on-resolved — concentrated here.
type Service struct {
	issues     IssueRepository
	events     EventWriter
	duckdbChan chan<- models.Event

	// waiting counts requests from before their Issue work until their
	// Event is written. Past maxWaiting a request is rejected as overloaded.
	waiting    atomic.Int64
	maxWaiting int64
}

// NewService constructs the Event ingest module.
// The IssueRepository is the seam to Issue persistence; storage.SQLiteWriter
// is the production adapter. The EventWriter is the seam to Event storage;
// storage.PebbleWriter is the production adapter.
func NewService(issues IssueRepository, events EventWriter, duckdbChan chan<- models.Event, maxWaiting int) *Service {
	return &Service{
		issues:     issues,
		events:     events,
		duckdbChan: duckdbChan,
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
	var payload map[string]interface{}
	if err := json.Unmarshal(rawJSON, &payload); err != nil {
		return ErrInvalidEventJSON
	}

	// Counted before the Issue work, so a rejected request leaves no Issue
	// without an Event.
	if s.waiting.Add(1) > s.maxWaiting {
		s.waiting.Add(-1)
		return ErrOverloaded
	}
	defer s.waiting.Add(-1)

	logger.L.Debug("Processing event", "raw_json", string(rawJSON))
	issue := classifyIssue(payload)

	issueID, fingerprintID, issueStatus, found, err := s.issues.FindIssueByFingerprint(projectID, issue.fingerprint)
	if err != nil {
		return fmt.Errorf("find issue by fingerprint: %w", err)
	}

	isNewIssue := false
	if !found {
		// A concurrent Event may create this fingerprint's Issue after the
		// lookup above; FindOrCreateIssue then returns that Issue instead.
		issueID, fingerprintID, issueStatus, isNewIssue, err = s.issues.FindOrCreateIssue(projectID, issue.fingerprint, issue.title, issue.culprit, issue.kind)
		if err != nil {
			return fmt.Errorf("find or create issue: %w", err)
		}
	}
	if !isNewIssue && issueStatus == IssueStatusResolved {
		// Domain rule: new Events reopen resolved Issues.
		if err := s.issues.ReopenIssue(issueID); err != nil {
			return fmt.Errorf("reopen issue: %w", err)
		}
	}

	event := models.Event{
		ProjectID:          projectID,
		Timestamp:          extractTimestamp(payload),
		RawJSON:            bytes.Clone(rawJSON), // rawJSON is fasthttp's request buffer, reused after the handler returns
		Tags:               extractTags(payload),
		Environment:        extractString(payload, "environment"),
		ServerName:         extractString(payload, "server_name"),
		Release:            extractString(payload, "release"),
		Level:              extractString(payload, "level"),
		IssueFingerprintID: fingerprintID,
		IssueID:            issueID,
		IsNewIssue:         isNewIssue,
	}

	// A Pebble write is never retried: the SDK gets an error instead.
	if err := s.events.WriteEvent(&event); err != nil {
		return fmt.Errorf("write event: %w", err)
	}

	select {
	case s.duckdbChan <- event:
	default:
		logger.L.Warn("DuckDB channel full, dropping event")
	}
	return nil
}

// extractTimestamp reads the event's "timestamp", which Sentry allows as
// either Unix seconds or an RFC 3339 string (UTC when it has no offset).
func extractTimestamp(payload map[string]interface{}) time.Time {
	switch timestamp := payload["timestamp"].(type) {
	case float64:
		return time.UnixMilli(int64(timestamp * 1000))
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			return parsed
		}
		if parsed, err := time.Parse("2006-01-02T15:04:05.999999999", timestamp); err == nil {
			return parsed
		}
	}
	return time.Now()
}

func extractTags(payload map[string]interface{}) map[string]string {
	tags := make(map[string]string)
	rawTags, ok := payload["tags"].(map[string]interface{})
	if !ok {
		return tags
	}

	for key, value := range rawTags {
		if text, ok := value.(string); ok {
			tags[key] = text
		}
	}

	return tags
}

func extractString(payload map[string]interface{}, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}

	text, _ := value.(string)
	return text
}
