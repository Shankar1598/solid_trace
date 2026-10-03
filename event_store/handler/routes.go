package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// NewIngestApp returns the public ingest app, serving the ingest routes.
func NewIngestApp(ingestHandler *IngestHandler, healthHandler *HealthHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		// The server reads no larger body, as sent, for any route. The Event
		// routes refuse more than their own, smaller cap themselves.
		BodyLimit: logBodyLimit,
		// Ingest faces clients directly, with no proxy to buffer slow ones, so
		// it bounds a request and an idle connection. Fiber's default is to
		// wait forever.
		ReadTimeout: 30 * time.Second, // headers and body of one request
		IdleTimeout: 10 * time.Second, // between keep-alive requests
	})
	RegisterIngestRoutes(app, ingestHandler, healthHandler)
	return app
}

// RegisterIngestRoutes mounts the public routes. They authenticate with the
// project's Sentry key and must stay reachable by SDKs from anywhere.
func RegisterIngestRoutes(app *fiber.App, ingestHandler *IngestHandler, healthHandler *HealthHandler) {
	app.Get("/health", healthHandler.Health)

	app.Post("/api/:project_id/store", ingestHandler.Store)
	app.Post("/api/:project_id/envelope", ingestHandler.Envelope)
	app.Post("/api/:project_id/logs", ingestHandler.Logs)
}

// RegisterQueryRoutes mounts the routes that serve stored Events and Logs.
// They have no authentication, so they must only be served on the loopback
// query listener, where the console is the only client.
func RegisterQueryRoutes(app *fiber.App, eventsHandler *EventsHandler, logsHandler *LogsHandler, healthHandler *HealthHandler) {
	app.Get("/health", healthHandler.Health)

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
	app.Get("/api/:project_id/logs/:log_id", logsHandler.Get)
}
