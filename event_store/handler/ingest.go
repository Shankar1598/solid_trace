package handler

import (
	"errors"
	"regexp"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/intake"
	"github.com/solidtrace/event_store/pkg/logger"
)

type IngestHandler struct {
	auth   *auth.ProjectAuth
	intake *intake.Service
}

func NewIngestHandler(auth *auth.ProjectAuth, intakeService *intake.Service) *IngestHandler {
	return &IngestHandler{
		auth:   auth,
		intake: intakeService,
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

	if err := h.intake.IngestStore(projectID, c.Body()); err != nil {
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

	projectID, err := h.auth.ValidateKey(publicKey)
	if err != nil || projectID == 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid project key"})
	}

	if err := h.intake.IngestEnvelope(projectID, c.Body()); err != nil {
		return h.renderIngestError(c, err)
	}

	return c.SendStatus(200)
}

func (h *IngestHandler) renderIngestError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, intake.ErrInvalidEnvelope):
		return c.Status(400).JSON(fiber.Map{"error": "Invalid envelope format"})
	case errors.Is(err, intake.ErrInvalidItemHeader):
		return c.Status(400).JSON(fiber.Map{"error": "Invalid item header"})
	case errors.Is(err, intake.ErrInvalidEventJSON):
		return c.Status(400).JSON(fiber.Map{"error": "Invalid event JSON"})
	case errors.Is(err, intake.ErrOverloaded):
		return c.Status(503).JSON(fiber.Map{"error": "Server overloaded"})
	default:
		logger.L.Error("Event intake failed", "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
}
