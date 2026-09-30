package ingest

import "github.com/solidtrace/event_store/models"

// EventWriter is the seam between Event ingest and Event storage.
// storage.PebbleWriter is the production adapter.
type EventWriter interface {
	// WriteEvent durably stores the Event's raw payload and sets its
	// EventUUID. Once the writer is closed it returns an error that the
	// caller must not retry.
	WriteEvent(event *models.Event) error
}
