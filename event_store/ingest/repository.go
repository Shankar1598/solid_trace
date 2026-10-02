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

// LogWriter is the seam between Log ingest and Log storage.
// storage.PebbleWriter is the production adapter.
type LogWriter interface {
	// WriteLogBatch durably stores one request's normalised Logs as one Log
	// batch. Once the writer is closed it returns an error that the caller
	// must not retry.
	WriteLogBatch(projectID uint32, logs []models.Log) error
}
