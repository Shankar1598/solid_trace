package handler

import (
	"testing"
	"time"
)

// The ingest app reads a body of its full limit in time from a client that
// sends at the slowest rate it serves.
func TestTheIngestAppReadsAFullUnitAtTheSlowestUploadRate(t *testing.T) {
	app := NewIngestApp(nil, nil)
	read := app.Config().ReadTimeout
	if app.Config().BodyLimit != agentBodyLimit {
		t.Fatalf("Expected the ingest app's body limit to be one unit as sent, %d, got %d", agentBodyLimit, app.Config().BodyLimit)
	}
	need := time.Duration(float64(agentBodyLimit) / minUploadBytesPerSecond * float64(time.Second))
	if read < need {
		t.Errorf("Expected the read timeout to let %d bytes arrive at %d bytes a second (%v), got %v",
			agentBodyLimit, minUploadBytesPerSecond, need, read)
	}
}
