package handler

import (
	"errors"
	"regexp"
	"strings"

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

	key, err := h.validateKey(publicKey, false)
	if err != nil {
		return h.renderKeyError(c, err)
	}

	body, err := requestBody(c, eventBodyLimit)
	if err != nil {
		return renderBodyError(c, err)
	}

	if err := h.ingest.IngestStore(key.ProjectID, body); err != nil {
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

	key, err := h.validateKey(publicKey, false)
	if err != nil {
		return h.renderKeyError(c, err)
	}

	body, err := requestBody(c, eventBodyLimit)
	if err != nil {
		return renderBodyError(c, err)
	}

	if err := h.ingest.IngestEnvelope(key.ProjectID, body); err != nil {
		return h.renderIngestError(c, err)
	}

	return c.SendStatus(200)
}

// POST /api/:project_id/logs
// The key must belong to the Project in the path.
func (h *IngestHandler) Logs(c *fiber.Ctx) error {
	return h.ingestLogs(c, false, func(projectID uint32) bool {
		pathProjectID, err := parseProjectID(c)
		return err == nil && pathProjectID == projectID
	}, h.storeJSONLogs)
}

// POST /api/logs
// The Project is the one the key belongs to.
func (h *IngestHandler) KeyOnlyLogs(c *fiber.Ctx) error {
	return h.ingestLogs(c, false, anyProject, h.storeJSONLogs)
}

func anyProject(uint32) bool { return true }

// ingestLogs authenticates the request's project key, refuses it unless
// projectAllowed accepts its Project, reads the body within the Log route's
// limits, and gives it to store to ingest as that Project's Logs and answer.
// otlp is true for the OTLP routes, the only ones for the Agent Project's
// key.
func (h *IngestHandler) ingestLogs(
	c *fiber.Ctx,
	otlp bool,
	projectAllowed func(projectID uint32) bool,
	store func(c *fiber.Ctx, key auth.ProjectKey, body ingest.Body) error,
) error {
	publicKey := h.extractSentryKey(c)
	if publicKey == "" {
		publicKey = bearerToken(c)
	}
	if publicKey == "" {
		return c.Status(401).JSON(fiber.Map{"error": "Missing authentication"})
	}

	key, err := h.validateKey(publicKey, otlp)
	if err != nil {
		return h.renderKeyError(c, err)
	}
	if !projectAllowed(key.ProjectID) {
		return c.Status(401).JSON(fiber.Map{"error": "Project key is not for this project"})
	}

	body, err := requestBody(c, logBodyLimit)
	if err != nil {
		return renderBodyError(c, err)
	}
	return store(c, key, body)
}

// storeJSONLogs ingests a body of the JSON Log route, and answers with its
// counts.
func (h *IngestHandler) storeJSONLogs(c *fiber.Ctx, key auth.ProjectKey, body ingest.Body) error {
	counts, err := h.ingest.IngestLogs(key.ProjectID, body, strings.EqualFold(mediaType(c), "application/x-ndjson"))
	if err != nil {
		return h.renderIngestError(c, err)
	}
	return c.JSON(counts)
}

// bearerToken returns the token of an "Authorization: Bearer <token>" header,
// or "".
func bearerToken(c *fiber.Ctx) string {
	scheme, token, ok := strings.Cut(c.Get(fiber.HeaderAuthorization), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// mediaType returns the media type of the request's Content-Type, with its
// parameters removed.
func mediaType(c *fiber.Ctx) string {
	mediaType, _, _ := strings.Cut(c.Get(fiber.HeaderContentType), ";")
	return strings.TrimSpace(mediaType)
}

var (
	errInvalidProjectKey = errors.New("invalid project key")
	errAgentProjectKey   = errors.New("the Agent Project's key on a route that is not OTLP")
)

// validateKey returns the Project of a key. The Agent Project's key works
// on the OTLP routes only (otlp), so that it writes no Event, no Span and no
// JSON or Sentry Log.
func (h *IngestHandler) validateKey(publicKey string, otlp bool) (auth.ProjectKey, error) {
	key, err := h.auth.ValidateKey(publicKey)
	if err != nil {
		return auth.ProjectKey{}, err
	}
	if key.ProjectID == 0 {
		return auth.ProjectKey{}, errInvalidProjectKey
	}
	if key.Agent && !otlp {
		return auth.ProjectKey{}, errAgentProjectKey
	}
	return key, nil
}

// renderKeyError returns 401 for an unknown key, and 403 for the Agent
// Project's key on a route that is not OTLP. A failed lookup is our fault,
// so it is a 500 and is logged here. Sentry SDKs do not retry on these
// statuses: an event that gets any non-2xx response is lost.
func (h *IngestHandler) renderKeyError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errInvalidProjectKey):
		return c.Status(401).JSON(fiber.Map{"error": "Invalid project key"})
	case errors.Is(err, errAgentProjectKey):
		return c.Status(403).JSON(fiber.Map{"error": "The Agent Project's key sends only OpenTelemetry Logs and metrics, to /v1/logs and /v1/metrics"})
	}
	logger.L.Error("Project key lookup failed", "error", err)
	return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
}

// retryAfterSeconds is how long an overloaded or stopping ingest asks the
// sender to wait before retrying.
const retryAfterSeconds = "5"

func (h *IngestHandler) renderIngestError(c *fiber.Ctx, err error) error {
	switch {
	case isBodyError(err):
		return renderBodyError(c, err)
	case errors.Is(err, ingest.ErrInvalidEnvelope):
		return c.Status(400).SendString(err.Error())
	case errors.Is(err, ingest.ErrInvalidItemHeader):
		return c.Status(400).SendString(err.Error())
	case errors.Is(err, ingest.ErrInvalidEventJSON):
		return c.Status(400).SendString(err.Error())
	case errors.Is(err, ingest.ErrInvalidOTLP), errors.Is(err, ingest.ErrInvalidOTLPMetrics):
		return c.Status(400).SendString(err.Error())
	case errors.Is(err, ingest.ErrOverloaded), errors.Is(err, storage.ErrShuttingDown):
		// Not 429: on a 429 without Retry-After, Sentry SDKs stop sending
		// everything for 60s. A 503 drops only this request; sentry-javascript
		// and shippers that read Retry-After wait that long before sending
		// again.
		c.Set(fiber.HeaderRetryAfter, retryAfterSeconds)
		return c.Status(503).SendString(err.Error())
	default:
		logger.L.Error("Ingest failed", "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
}
