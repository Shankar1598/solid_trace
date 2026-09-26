package handler

import "github.com/gofiber/fiber/v2"

// RegisterIngestRoutes mounts the public routes. They authenticate with the
// project's Sentry key and must stay reachable by SDKs from anywhere.
func RegisterIngestRoutes(app *fiber.App, ingestHandler *IngestHandler, healthHandler *HealthHandler) {
	app.Get("/health", healthHandler.Health)

	app.Post("/api/:project_id/store", ingestHandler.Store)
	app.Post("/api/:project_id/envelope", ingestHandler.Envelope)
}

// RegisterQueryRoutes mounts the routes that serve stored events. They have no
// authentication, so they must only be served on the loopback query listener,
// where the console is the only client.
func RegisterQueryRoutes(app *fiber.App, eventsHandler *EventsHandler, healthHandler *HealthHandler) {
	app.Get("/health", healthHandler.Health)

	app.Get("/api/:project_id/events", eventsHandler.List)
	app.Get("/api/:project_id/events/context", eventsHandler.GetEventWithContext)
	app.Get("/api/:project_id/events/count", eventsHandler.Count)
	// Fiber matches in registration order: this must follow context and count,
	// or those paths are captured as an event UUID.
	app.Get("/api/:project_id/events/:event_uuid", eventsHandler.GetEvent)
}
