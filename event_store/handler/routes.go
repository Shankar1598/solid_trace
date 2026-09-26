package handler

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts every HTTP route. Ingest routes authenticate with the
// project's Sentry key and must stay public for SDKs. Query routes serve stored
// events and require the internal token, which only the console holds.
func RegisterRoutes(app *fiber.App, ingestHandler *IngestHandler, eventsHandler *EventsHandler, healthHandler *HealthHandler, internalToken string) {
	app.Get("/health", healthHandler.Health)

	app.Post("/api/:project_id/store", ingestHandler.Store)
	app.Post("/api/:project_id/envelope", ingestHandler.Envelope)

	internal := InternalAuth(internalToken)
	app.Get("/api/:project_id/events", internal, eventsHandler.List)
	app.Get("/api/:project_id/events/context", internal, eventsHandler.GetEventWithContext)
	app.Get("/api/:project_id/events/count", internal, eventsHandler.Count)
	// Fiber matches in registration order: this must follow context and count,
	// or those paths are captured as an event UUID.
	app.Get("/api/:project_id/events/:event_uuid", internal, eventsHandler.GetEvent)
}
