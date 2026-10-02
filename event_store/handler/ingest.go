package handler

import (
	"errors"
	"regexp"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type IngestHandler struct {
	auth   *auth.ProjectAuth
	ingest *ingest.Service
}

func NewIngestHandler(auth *auth.ProjectAuth, ingestService *ingest.Service) *IngestHandler {
	return &IngestHandler{
		auth:   auth,
		ingest: ingestService,
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

	projectID, err := h.validateKey(publicKey)
	if err != nil {
		return h.renderKeyError(c, err)
	}

	if err := h.ingest.IngestStore(projectID, c.Body()); err != nil {
		return h.renderIngestError(c, err)
	}

	return c.SendStatus(200)
}

// POST /api/:project_id/envelope
func (h *IngestHandler) Envelope(c *fiber.Ctx) error {
	publicKey := h.extractSentryKey(c)
	if publicKey == "" {
		return c.Status(401).JSON(fiber.Map{"error": "Missing authentication"})
	}

	projectID, err := h.validateKey(publicKey)
	if err != nil {
		return h.renderKeyError(c, err)
	}

	if err := h.ingest.IngestEnvelope(projectID, c.Body()); err != nil {
		return h.renderIngestError(c, err)
	}

	return c.SendStatus(200)
}

var errInvalidProjectKey = errors.New("invalid project key")

func (h *IngestHandler) validateKey(publicKey string) (uint32, error) {
	projectID, err := h.auth.ValidateKey(publicKey)
	if err != nil {
		return 0, err
	}
	if projectID == 0 {
		return 0, errInvalidProjectKey
	}
	return projectID, nil
}

// renderKeyError returns 401 only for an unknown key. A failed lookup is our
// fault, so it is a 500 and is logged here. Sentry SDKs do not retry on either
// status: an event that gets any non-2xx response is lost.
func (h *IngestHandler) renderKeyError(c *fiber.Ctx, err error) error {
	if errors.Is(err, errInvalidProjectKey) {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid project key"})
	}
	logger.L.Error("Project key lookup failed", "error", err)
	return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
}

func (h *IngestHandler) renderIngestError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ingest.ErrInvalidEnvelope):
		return c.Status(400).SendString(err.Error())
	case errors.Is(err, ingest.ErrInvalidItemHeader):
		return c.Status(400).SendString(err.Error())
	case errors.Is(err, ingest.ErrInvalidEventJSON):
		return c.Status(400).SendString(err.Error())
	case errors.Is(err, ingest.ErrOverloaded), errors.Is(err, storage.ErrShuttingDown):
		// Not 429: on a 429 without Retry-After, Sentry SDKs stop sending
		// everything for 60s. A 503 drops only this event.
		return c.Status(503).SendString(err.Error())
	default:
		logger.L.Error("Event ingest failed", "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
}
