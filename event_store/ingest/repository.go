package ingest

import (
	"time"

	"github.com/solidtrace/event_store/models"
)

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
	// WriteLogBatch durably stores one request's normalised Logs, received
	// at receivedAt, as one Log batch. Once the writer is closed it returns
	// an error that the caller must not retry.
	WriteLogBatch(projectID uint32, logs []models.Log, receivedAt time.Time) error
}

// Retention tells how many whole days a Project keeps its Logs. Log ingest
// reads it once per request: a Log's own time older than that is replaced
// with the receipt time.
type Retention interface {
	LogRetentionDays(projectID uint32) (int, error)
}

// FixedRetention is the same Retention, in whole days, for every Project.
type FixedRetention int

func (days FixedRetention) LogRetentionDays(uint32) (int, error) {
	return int(days), nil
}
