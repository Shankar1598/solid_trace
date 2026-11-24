package handler

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// minUploadBytesPerSecond is the slowest rate at which a client can send
// the largest body the ingest app reads, one unit of an Agent session's file
// as sent: 2 Mbit/s. A client that sends a unit with zstd (6.6 times on
// transcripts) needs much less.
const minUploadBytesPerSecond = 250_000

// ingestReadTimeout is the time to read one request, its headers and body.
// The body limit sets it, so that a full unit arrives in time at
// minUploadBytesPerSecond: about 2 minutes 20 seconds.
const ingestReadTimeout = time.Duration(agentBodyLimit/minUploadBytesPerSecond+1) * time.Second

// IngestOptions are the ingest app's settings from the config file.
type IngestOptions struct {
	// IdleTimeout is how long a keep-alive connection may wait for its next
	// request.
	IdleTimeout time.Duration
	// RefuseForwardedHTTP refuses an ingest request that a proxy says came
	// to it as plain HTTP (RefuseForwardedHTTP).
	RefuseForwardedHTTP bool
}

// NewIngestApp returns the public ingest app, serving the ingest routes.
func NewIngestApp(ingestHandler *IngestHandler, healthHandler *HealthHandler, options IngestOptions) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		// The server reads no larger body, as sent, for any route: an upload
		// of one unit of an Agent session's file. The Event and Log routes
		// refuse more than their own, smaller cap themselves.
		BodyLimit: agentBodyLimit,
		// Ingest faces clients directly, or through Caddy, which streams
		// requests without buffering them. Either way no proxy absorbs slow
		// clients, so it bounds a request and an idle connection. Fiber's
		// default is to wait forever. Caddy's keep-alive (deploy/caddy) is
		// shorter than the default IdleTimeout. Behind a load balancer that
		// keeps connections open longer, ingest_idle_timeout_seconds is
		// longer than its keep-alive. ReadTimeout starts at a request's first
		// byte, so it doesn't end an idle keep-alive connection.
		ReadTimeout: ingestReadTimeout, // headers and body of one request
		IdleTimeout: options.IdleTimeout,
	})
	if options.RefuseForwardedHTTP {
		app.Use("/api", RefuseForwardedHTTP)
		// OTLP senders use /v1/logs and /v1/metrics, outside /api.
		app.Use("/v1", RefuseForwardedHTTP)
	}
	RegisterIngestRoutes(app, ingestHandler, healthHandler)
	return app
}

// RefuseForwardedHTTP answers 403 to a request whose X-Forwarded-Proto says
// it came to the proxy as plain HTTP, so a client with an http:// URL fails
// clearly. It isn't a security control: the client sent its key already. A
// request with no X-Forwarded-Proto, as a load balancer's health check or a
// client on the network sends, passes. A proxy may append its value to the
// client's, so the last value is the proxy's.
func RefuseForwardedHTTP(c *fiber.Ctx) error {
	proto := c.Get(fiber.HeaderXForwardedProto)
	if i := strings.LastIndexByte(proto, ','); i >= 0 {
		proto = proto[i+1:]
	}
	if strings.EqualFold(strings.TrimSpace(proto), "http") {
		return c.Status(fiber.StatusForbidden).SendString("Use an https:// URL: this ingest refuses plain HTTP that a proxy forwarded.")
	}
	return c.Next()
}

// RegisterIngestRoutes mounts the public routes. They authenticate with the
// project's Sentry key and must stay reachable by SDKs from anywhere.
func RegisterIngestRoutes(app *fiber.App, ingestHandler *IngestHandler, healthHandler *HealthHandler) {
	app.Get("/health", healthHandler.Health)

	app.Post("/api/:project_id/store", ingestHandler.Store)
	app.Post("/api/:project_id/envelope", ingestHandler.Envelope)
	app.Post("/api/:project_id/logs", ingestHandler.Logs)
	app.Post("/api/logs", ingestHandler.KeyOnlyLogs)
	app.Post("/v1/logs", ingestHandler.OTLPLogs)
	app.Post("/v1/metrics", ingestHandler.OTLPMetrics)
}

// RegisterQueryRoutes mounts the routes that serve stored Events and Logs,
// and EventStore's config. They have no authentication, so they must only be
// served on the loopback query listener, where the console is the only
// client.
//
// Every route, and those mounted after them on the app, as the performance
// routes are, runs its tagged Log reads with the instance's Log read threads
// (LogsHandler.readThreads).
func RegisterQueryRoutes(app *fiber.App, eventsHandler *EventsHandler, logsHandler *LogsHandler, transformHandler *LogTransformHandler, overviewHandler *OverviewHandler, tracesHandler *TracesHandler, healthHandler *HealthHandler, configHandler *ConfigHandler) {
	app.Use(logsHandler.readThreads)

	app.Get("/health", healthHandler.Health)
	app.Get("/config", configHandler.Config)

	app.Post("/overview", overviewHandler.Overview)
	app.Post("/volumes", overviewHandler.Volumes)
	app.Post("/trace", tracesHandler.Trace)

	app.Get("/api/:project_id/events", eventsHandler.List)
	app.Get("/api/:project_id/events/context", eventsHandler.GetEventWithContext)
	app.Get("/api/:project_id/events/count", eventsHandler.Count)
	// Fiber matches in registration order: this must follow context and count,
	// or those paths are captured as an event UUID.
	app.Get("/api/:project_id/events/:event_uuid", eventsHandler.GetEvent)

	app.Post("/api/:project_id/logs/query", logsHandler.Query)
	app.Post("/api/:project_id/logs/validate", logsHandler.Validate)
	app.Post("/api/:project_id/logs/histogram", logsHandler.Histogram)
	app.Post("/api/:project_id/logs/sql", logsHandler.SQL)
	app.Post("/api/:project_id/logs/readable-sql", logsHandler.ReadableSQL)
	app.Post("/api/:project_id/logs/transform/check", transformHandler.Check)
	app.Post("/api/:project_id/logs/transform/preview", transformHandler.Preview)
	app.Get("/api/:project_id/logs/transform/stats", transformHandler.Stats)
	// As with Events, this must come before a Log's id, or fields is
	// captured as one.
	app.Get("/api/:project_id/logs/fields", logsHandler.Fields)
	app.Get("/api/:project_id/logs/:log_id", logsHandler.Get)
}

// RegisterPerformanceRoutes mounts the Query API's Transactions routes, on
// the loopback query listener as RegisterQueryRoutes' are. A Transaction's
// name goes in the body, since it may hold any character.
func RegisterPerformanceRoutes(app *fiber.App, performanceHandler *PerformanceHandler) {
	app.Post("/api/:project_id/transactions", performanceHandler.Transactions)
	app.Post("/api/:project_id/transaction", performanceHandler.Transaction)
}
