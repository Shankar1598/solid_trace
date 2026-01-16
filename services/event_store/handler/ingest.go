package handler

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/models"
)

type IngestHandler struct {
	auth        *auth.ProjectAuth
	rocksdbChan chan<- models.Event
}

func NewIngestHandler(auth *auth.ProjectAuth, rocksdbChan chan<- models.Event) *IngestHandler {
	return &IngestHandler{auth: auth, rocksdbChan: rocksdbChan}
}

var sentryKeyRegex = regexp.MustCompile(`sentry_key=([^,\s]+)`)

func (h *IngestHandler) extractSentryKey(c *fiber.Ctx) string {
	// Check X-Sentry-Auth header
	authHeader := c.Get("X-Sentry-Auth")
	if authHeader == "" {
		authHeader = c.Get("Authorization")
	}

	if authHeader != "" {
		if match := sentryKeyRegex.FindStringSubmatch(authHeader); len(match) > 1 {
			return match[1]
		}
	}

	// Fallback to query param
	return c.Query("sentry_key")
}

// POST /api/:project_id/store (Legacy/JSON endpoint)
// Note: While the request asks for /store support, standard Sentry SDKs use envelopes mostly now.
// We'll treat the body as a single event JSON for now if used.
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

	// We need to parse fields to build the event model
	var eventPayload struct {
		EventID   string                 `json:"event_id"`
		Dt        string                 `json:"dt"` // Could be "timestamp" or "dt" depending on format. Sentry uses "timestamp" usually as float/string
		Timestamp interface{}            `json:"timestamp"`
		Tags      map[string]interface{} `json:"tags"`
	}

	if err := json.Unmarshal(rawEventJSON, &eventPayload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid event JSON"})
	}

	// Resolve timestamp
	var timestamp time.Time
	if eventPayload.Dt != "" {
		// "dt" field logic from legacy Rails code implies it might be present
		t, _ := time.Parse(time.RFC3339, eventPayload.Dt)
		timestamp = t
	} else if val, ok := eventPayload.Timestamp.(float64); ok {
		timestamp = time.UnixMilli(int64(val * 1000))
	}

	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// Resolve EventID
	if eventPayload.EventID == "" {
		// Generate if missing? Rails code implies it expects it.
		// For now let's assume client sends it or we default (omitted for brevity)
	}

	// Convert tags
	tags := make(map[string]string)
	for k, v := range eventPayload.Tags {
		if s, ok := v.(string); ok {
			tags[k] = s
		}
	}

	event := models.Event{
		ProjectID: projectID,
		EventUUID: eventPayload.EventID,
		Timestamp: timestamp,
		RawJSON:   rawEventJSON,
		Tags:      tags,
	}

	// Send to RocksDB channel (non-blocking)
	select {
	case h.rocksdbChan <- event:
	default:
		return c.Status(503).JSON(fiber.Map{"error": "Server overloaded"})
	}

	return c.SendStatus(200)
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

	// Parse envelope: header\nitem_header\nitem_payload\n...
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
		// We might only care about events/transactions.
		// If it's an attachment/session/etc we might ignore or log.
		// For now, accept "event" and treat others as success but ignore.
		if itemHeader["type"] == "transaction" {
			// treat transaction as event?
		} else {
			return c.SendStatus(200)
		}
	}

	rawEventJSON := []byte(lines[2])

	// Parse minimal fields for key generation
	var eventPayload struct {
		EventID   string                 `json:"event_id"`
		Dt        string                 `json:"dt"`
		Timestamp interface{}            `json:"timestamp"`
		Tags      map[string]interface{} `json:"tags"`
	}
	if err := json.Unmarshal(rawEventJSON, &eventPayload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid event payload"})
	}

	var timestamp time.Time
	// Logic to parse timestamp similarly to Store
	if eventPayload.Dt != "" {
		t, _ := time.Parse(time.RFC3339, eventPayload.Dt)
		timestamp = t
	} else if val, ok := eventPayload.Timestamp.(float64); ok {
		timestamp = time.UnixMilli(int64(val * 1000))
	}

	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// Convert tags to string map
	tags := make(map[string]string)
	for k, v := range eventPayload.Tags {
		if s, ok := v.(string); ok {
			tags[k] = s
		}
	}

	event := models.Event{
		ProjectID: projectID,
		EventUUID: eventPayload.EventID,
		Timestamp: timestamp,
		RawJSON:   rawEventJSON,
		Tags:      tags,
	}

	// Send to RocksDB channel (non-blocking)
	select {
	case h.rocksdbChan <- event:
	default:
		return c.Status(503).JSON(fiber.Map{"error": "Server overloaded"})
	}

	return c.SendStatus(200)
}
