package handler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/redaction"
)

// AgentAPIPath is where the agent ingest API is on the ingest listener. A
// breaking change to it needs a new version path.
const AgentAPIPath = "/api/agent/v1"

// VersionHeader carries the server's version on each answer of the agent
// ingest API. The solidtrace CLI prints one warning line when it differs
// from its own.
const VersionHeader = "SolidTrace-Version"

// SupportedMinorVersions is how many minor versions before its own the
// server accepts a solidtrace client of: after an upgrade, laptops keep an
// older CLI until their install command or managed settings change.
const SupportedMinorVersions = 3

// ScopeSessionsWrite lets a token upload Agent sessions to its
// Organization's Agent Project and read its captured repositories.
const ScopeSessionsWrite = "sessions:write"

// AgentTokens checks a token that a client sent (auth.Tokens).
type AgentTokens interface {
	Check(secret string, now time.Time) (auth.Token, bool, error)
}

// AgentSettings is what the agent ingest API reads of Console's settings
// (storage.ConsoleSettings): an Organization's captured repositories and
// its Agent Project, and the extra Redaction patterns.
type AgentSettings interface {
	CapturedRepositories(organizationID int64) ([]string, error)
	AgentProject(organizationID int64) (uint32, bool, error)
	RedactionPatterns() ([]string, error)
}

// AgentConfig is what the agent ingest API is built on.
type AgentConfig struct {
	Tokens   AgentTokens
	Settings AgentSettings
	// Files stores the units of Agent sessions' files, and NotifyFiles
	// tells Agent file processing that a Project has new ones.
	Files       AgentFiles
	NotifyFiles func(projectID uint32)
	// Downloads reads the stored bytes of Agent sessions from ClickHouse.
	Downloads AgentDownloads
	// RunEvents stores the Dispatcher's run events as Logs.
	RunEvents RunEventsIngest
	Clock     clock.Clock
	// Version is the server's version, as SOLIDTRACE_VERSION gives it
	// (v0.19.0). A version that is not a release, such as dev, accepts a
	// client of each version.
	Version string
}

// AgentHandler serves the agent ingest API: the calls of the solidtrace CLI
// and of the wrapper, behind the tokens that Console makes.
type AgentHandler struct {
	tokens      AgentTokens
	settings    AgentSettings
	files       AgentFiles
	notifyFiles func(projectID uint32)
	downloads   AgentDownloads
	runEvents   RunEventsIngest
	clock       clock.Clock
	version     string
	minimum     *clientVersion
	// redaction gives the Redaction patterns that each unit goes through
	// before it is stored.
	redaction *redaction.Follower
}

// NewAgentHandler returns the agent ingest API of cfg.
func NewAgentHandler(cfg AgentConfig) *AgentHandler {
	return &AgentHandler{
		tokens:      cfg.Tokens,
		settings:    cfg.Settings,
		files:       cfg.Files,
		notifyFiles: cfg.NotifyFiles,
		downloads:   cfg.Downloads,
		runEvents:   cfg.RunEvents,
		clock:       cfg.Clock,
		version:     cfg.Version,
		minimum:     minimumClientVersion(cfg.Version),
		redaction:   redaction.NewFollower(cfg.Settings.RedactionPatterns),
	}
}

// RegisterAgentRoutes mounts the agent ingest API on the ingest app.
func RegisterAgentRoutes(app *fiber.App, h *AgentHandler) {
	api := app.Group(AgentAPIPath, h.sendVersion, h.refuseOldClients)
	api.Get("/captured-repositories", h.requireScope(ScopeSessionsWrite), h.CapturedRepositories)
	api.Get("/agent-sessions/:session_id/files", h.requireScope(ScopeSessionsWrite), h.AgentSessionFiles)
	api.Post("/agent-sessions/:session_id/files", h.requireScope(ScopeSessionsWrite), h.UploadAgentFile)
	api.Get("/agent-sessions/:session_id/download", h.requireScope(ScopeSessionsRead), h.DownloadAgentSession)
	api.Post("/run-events", h.requireScope(ScopeRunEventsWrite), h.SendRunEvents)
}

func (h *AgentHandler) sendVersion(c *fiber.Ctx) error {
	c.Set(VersionHeader, h.version)
	return c.Next()
}

// refuseOldClients answers 426 to a solidtrace client older than the
// supported minor versions, before it checks the token: signing in again
// does not help such a client.
func (h *AgentHandler) refuseOldClients(c *fiber.Ctx) error {
	if h.minimum == nil {
		return c.Next()
	}
	client := solidtraceClientVersion(c.Get(fiber.HeaderUserAgent))
	if client == nil || !client.less(*h.minimum) {
		return c.Next()
	}
	return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{
		"error": fmt.Sprintf("This solidtrace (%s) is too old for this SolidTrace server (%s). "+
			"Install solidtrace %s or later.", client, h.version, h.minimum),
		"minimum_version": h.minimum.String(),
	})
}

const agentTokenKey = "agent_token"

// requireScope checks the Bearer token and that it has scope. It answers
// 401 with no live token and 403 without the scope, or when the token's
// Organization requires Two-factor and its user has none.
func (h *AgentHandler) requireScope(scope string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		secret := bearerToken(c)
		var token auth.Token
		ok := false
		if secret != "" {
			var err error
			token, ok, err = h.tokens.Check(secret, h.clock.Now())
			if err != nil {
				logger.L.Error("Failed to check an agent ingest token", "error", err)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The token could not be checked. Try again later."})
			}
		}
		if !ok {
			c.Set(fiber.HeaderWWWAuthenticate, "Bearer")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Send a live token as Authorization: Bearer <token>. The token is missing, unknown, " +
					"expired or revoked, or its user is no longer a member of its Organization. " +
					"Sign in again with solidtrace login.",
			})
		}
		if !token.Has(scope) {
			c.Set(fiber.HeaderWWWAuthenticate, fmt.Sprintf(`Bearer error="insufficient_scope", scope=%q`, scope))
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": fmt.Sprintf("This token does not have the %s scope, which this call needs.", scope),
				"scope": scope,
			})
		}
		if token.TwoFactorMissing {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": fmt.Sprintf("%s requires Two-factor. Set it up in Console on Settings → User, then try again.",
					token.Organization.Name),
			})
		}
		c.Locals(agentTokenKey, token)
		return c.Next()
	}
}

// agentToken is the token that requireScope accepted.
func agentToken(c *fiber.Ctx) auth.Token {
	return c.Locals(agentTokenKey).(auth.Token)
}

// CapturedRepositories answers the token's Organization and the captured
// repositories of its Agent Project: an empty list with Agent sessions off.
// The solidtrace CLI uploads only Agent sessions in a repository that
// matches an entry.
func (h *AgentHandler) CapturedRepositories(c *fiber.Ctx) error {
	token := agentToken(c)
	entries, err := h.settings.CapturedRepositories(token.Organization.ID)
	if err != nil {
		logger.L.Error("Failed to read the captured repositories", "organization_id", token.Organization.ID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The captured repositories could not be read. Try again later."})
	}
	return c.JSON(fiber.Map{
		"organization": fiber.Map{
			"name": token.Organization.Name,
			"slug": token.Organization.Slug,
		},
		"captured_repositories": entries,
	})
}

// clientVersion is a release version, major.minor.patch.
type clientVersion struct {
	major, minor, patch int
}

var releaseVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$`)

func parseVersion(text string) *clientVersion {
	match := releaseVersion.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	parts := [3]int{}
	for i := range parts {
		n, err := strconv.Atoi(match[i+1])
		if err != nil {
			return nil
		}
		parts[i] = n
	}
	return &clientVersion{major: parts[0], minor: parts[1], patch: parts[2]}
}

func (v clientVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v clientVersion) less(other clientVersion) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	if v.minor != other.minor {
		return v.minor < other.minor
	}
	return v.patch < other.patch
}

// minimumClientVersion is the oldest solidtrace a server of version
// accepts: SupportedMinorVersions minors before its own. A new major
// version on the same API path is not a breaking change (that needs a new
// path, such as /api/agent/v2). So while the server has fewer than
// SupportedMinorVersions minors in its major version, it also accepts all
// of the earlier major version, because it does not know that major's
// last minor. Nil for a server version that is not a release.
func minimumClientVersion(version string) *clientVersion {
	server := parseVersion(version)
	if server == nil {
		return nil
	}
	if server.minor < SupportedMinorVersions && server.major > 0 {
		return &clientVersion{major: server.major - 1}
	}
	return &clientVersion{major: server.major, minor: max(server.minor-SupportedMinorVersions, 0)}
}

// solidtraceClientVersion is the version in a User-Agent that starts with
// solidtrace/<version>, as the solidtrace CLI sends it. Nil for another
// client, or for a version that is not a release, such as dev.
func solidtraceClientVersion(userAgent string) *clientVersion {
	product, _, _ := strings.Cut(strings.TrimSpace(userAgent), " ")
	name, version, ok := strings.Cut(product, "/")
	if !ok || !strings.EqualFold(name, "solidtrace") {
		return nil
	}
	return parseVersion(version)
}
