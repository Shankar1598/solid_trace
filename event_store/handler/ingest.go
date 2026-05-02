package handler

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/logic"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type IngestHandler struct {
	auth       *auth.ProjectAuth
	sqlite     *storage.SQLiteWriter
	pebbleChan chan<- models.Event
}

func NewIngestHandler(auth *auth.ProjectAuth, sqlite *storage.SQLiteWriter, pebbleChan chan<- models.Event) *IngestHandler {
	return &IngestHandler{
		auth:       auth,
		sqlite:     sqlite,
		pebbleChan: pebbleChan,
	}
}

var sentryKeyRegex = regexp.MustCompile(`sentry_key=([^,\s]+)`)

func (h *IngestHandler) extractSentryKey(c *fiber.Ctx) string {
	authHeader := c.Get("X-Sentry-Auth")
	if authHeader == "" {
		authHeader = c.Get("Authorization")
	}

	if authHeader != "" {
		if match := sentryKeyRegex.FindStringSubmatch(authHeader); len(match) > 1 {
			return match[1]
		}
	}
	return c.Query("sentry_key")
}

// POST /api/:project_id/store
func (h *IngestHandler) Store(c *fiber.Ctx) error {
	publicKey := h.extractSentryKey(c)
	if publicKey == "" {
		return c.Status(401).JSON(fiber.Map{"error": "Missing authentication"})
	}

	projectID, err := h.auth.ValidateKey(publicKey)
	if err != nil || projectID == 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid project key"})
	}

	rawEventJSON := c.Body()
	return h.processEvent(c, projectID, rawEventJSON)
}

// POST /api/:project_id/envelope
func (h *IngestHandler) Envelope(c *fiber.Ctx) error {
	publicKey := h.extractSentryKey(c)
	if publicKey == "" {
		return c.Status(401).JSON(fiber.Map{"error": "Missing authentication"})
	}

	projectID, err := h.auth.ValidateKey(publicKey)
	if err != nil || projectID == 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid project key"})
	}

	body := c.Body()
	lines := strings.SplitN(string(body), "\n", 3)
	if len(lines) < 3 {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid envelope format"})
	}

	var itemHeader map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &itemHeader); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid item header"})
	}

	if itemHeader["type"] != "event" && itemHeader["type"] != "transaction" {
		return c.SendStatus(200)
	}

	rawEventJSON := []byte(lines[2])
	return h.processEvent(c, projectID, rawEventJSON)
}

func (h *IngestHandler) processEvent(c *fiber.Ctx, projectID uint32, rawJSON []byte) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(rawJSON, &payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid event JSON"})
	}

	logger.L.Debug("Processing event", "raw_json", string(rawJSON))
	// 1. Extract issue attributes
	title := logic.ExtractTitle(payload)
	culprit := logic.ExtractCulprit(payload)
	kind := logic.DetermineKind(payload)

	var customFingerprint []string
	if fp, ok := payload["fingerprint"].([]interface{}); ok {
		for _, v := range fp {
			if s, ok := v.(string); ok {
				customFingerprint = append(customFingerprint, s)
			}
		}
	}

	fingerprint := logic.ComputeFingerprint(title, culprit, kind, customFingerprint)

	// 2. Find or create issue & fingerprint
	// Check existing logic
	issueID, fingerprintID, found, err := h.sqlite.FindIssueByFingerprint(projectID, fingerprint)
	if err != nil {
		logger.L.Error("SQLite error finding issue", "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}

	isNewIssue := false
	if !found {
		// Create new issue
		issueID, fingerprintID, err = h.sqlite.CreateIssueWithFingerprint(projectID, fingerprint, title, culprit, kind)
		if err != nil {
			logger.L.Error("SQLite error creating issue", "error", err)
			return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
		}
		isNewIssue = true
	}

	// 3. Resolve basic event fields
	// Enforce System-Generated UUID v7 for ordering in Pebble
	u, err := uuid.NewV7()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	eventID := u.String()

	// (We ignore client-provided event_id for storage key to ensure ordering,
	// but we might want to preserve it in Tags or similar if needed for debugging.
	// For now, we replace it.)

	timestamp := extractTimestamp(payload)
	tags := extractTags(payload)

	environment := extractString(payload, "environment")
	serverName := extractString(payload, "server_name")
	release := extractString(payload, "release")
	level := extractString(payload, "level")

	// 4. Build event model
	event := models.Event{
		ProjectID:          projectID,
		EventUUID:          eventID,
		Timestamp:          timestamp,
		RawJSON:            rawJSON,
		Tags:               tags,
		Environment:        environment,
		ServerName:         serverName,
		Release:            release,
		Level:              level,
		IssueFingerprintID: fingerprintID,
		IssueID:            issueID,
		IsNewIssue:         isNewIssue,
	}

	// 5. Send to Pebble channel
	select {
	case h.pebbleChan <- event:
	default:
		return c.Status(503).JSON(fiber.Map{"error": "Server overloaded"})
	}

	return c.SendStatus(200)
}

func extractEventID(payload map[string]interface{}) string {
	if s, ok := payload["event_id"].(string); ok {
		return s
	}
	return ""
}

func extractTimestamp(payload map[string]interface{}) time.Time {
	if dt, ok := payload["dt"].(string); ok && dt != "" {
		t, _ := time.Parse(time.RFC3339, dt)
		return t
	}
	if ts, ok := payload["timestamp"].(float64); ok {
		return time.UnixMilli(int64(ts * 1000))
	}
	return time.Now()
}

func extractTags(payload map[string]interface{}) map[string]string {
	tags := make(map[string]string)
	if t, ok := payload["tags"].(map[string]interface{}); ok {
		for k, v := range t {
			if s, ok := v.(string); ok {
				tags[k] = s
			}
		}
	}
	return tags
}

func extractString(payload map[string]interface{}, key string) string {
	if v, ok := payload[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
