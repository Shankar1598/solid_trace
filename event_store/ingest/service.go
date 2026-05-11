package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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
	pebbleChan chan<- models.Event
}

// NewService constructs the Event ingest module.
// The IssueRepository is the seam to Issue persistence; storage.SQLiteWriter
// is the production adapter.
func NewService(issues IssueRepository, pebbleChan chan<- models.Event) *Service {
	return &Service{
		issues:     issues,
		pebbleChan: pebbleChan,
	}
}

// IngestStore ingests a raw Event payload from the store endpoint.
func (s *Service) IngestStore(projectID uint32, rawEventJSON []byte) error {
	return s.ingestEvent(projectID, rawEventJSON)
}

// IngestEnvelope ingests the first supported Event item from an envelope.
func (s *Service) IngestEnvelope(projectID uint32, envelope []byte) error {
	lines := strings.SplitN(string(envelope), "\n", 3)
	if len(lines) < 3 {
		return ErrInvalidEnvelope
	}

	var itemHeader map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &itemHeader); err != nil {
		return ErrInvalidItemHeader
	}

	itemType, _ := itemHeader["type"].(string)
	if itemType != "event" && itemType != "transaction" {
		return nil
	}

	return s.ingestEvent(projectID, []byte(lines[2]))
}

func (s *Service) ingestEvent(projectID uint32, rawJSON []byte) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(rawJSON, &payload); err != nil {
		return ErrInvalidEventJSON
	}

	logger.L.Debug("Processing event", "raw_json", string(rawJSON))
	issue := classifyIssue(payload)

	issueID, fingerprintID, issueStatus, found, err := s.issues.FindIssueByFingerprint(projectID, issue.fingerprint)
	if err != nil {
		return fmt.Errorf("find issue by fingerprint: %w", err)
	}

	isNewIssue := false
	if !found {
		issueID, fingerprintID, err = s.issues.CreateIssueWithFingerprint(projectID, issue.fingerprint, issue.title, issue.culprit, issue.kind)
		if err != nil {
			return fmt.Errorf("create issue with fingerprint: %w", err)
		}
		isNewIssue = true
	} else if issueStatus == IssueStatusResolved {
		// Domain rule: new Events reopen resolved Issues.
		if err := s.issues.ReopenIssue(issueID); err != nil {
			return fmt.Errorf("reopen issue: %w", err)
		}
	}

	eventUUID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate event uuid: %w", err)
	}

	event := models.Event{
		ProjectID:          projectID,
		EventUUID:          eventUUID.String(),
		Timestamp:          extractTimestamp(payload),
		RawJSON:            rawJSON,
		Tags:               extractTags(payload),
		Environment:        extractString(payload, "environment"),
		ServerName:         extractString(payload, "server_name"),
		Release:            extractString(payload, "release"),
		Level:              extractString(payload, "level"),
		IssueFingerprintID: fingerprintID,
		IssueID:            issueID,
		IsNewIssue:         isNewIssue,
	}

	select {
	case s.pebbleChan <- event:
		return nil
	default:
		return ErrOverloaded
	}
}

func extractTimestamp(payload map[string]interface{}) time.Time {
	if datetime, ok := payload["dt"].(string); ok && datetime != "" {
		timestamp, _ := time.Parse(time.RFC3339, datetime)
		return timestamp
	}
	if timestamp, ok := payload["timestamp"].(float64); ok {
		return time.UnixMilli(int64(timestamp * 1000))
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
